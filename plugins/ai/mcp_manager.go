package ai

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"github.com/lhpqaq/ggbot/config"
	"github.com/modelcontextprotocol/go-sdk/mcp"
	"log/slog"
)

// MCPManager manages MCP client sessions with connection pooling and health checks
type MCPManager struct {
	sessions   map[string]*mcpSession
	toolMap    map[string]*mcpSession
	tools      []ToolDefinition
	mu         sync.RWMutex
	logger     *slog.Logger
	httpClient *http.Client
}

type mcpSession struct {
	session     *mcp.ClientSession
	name        string
	config      config.MCPConfig
	lastUsed    time.Time
	failCount   int
	mu          sync.Mutex
	closed      bool
}

// NewMCPManager creates a new MCP manager
func NewMCPManager(logger *slog.Logger) *MCPManager {
	return &MCPManager{
		sessions: make(map[string]*mcpSession),
		toolMap:  make(map[string]*mcpSession),
		tools:    []ToolDefinition{},
		logger:   logger,
		httpClient: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			},
		},
	}
}

// ConnectServers connects to all configured MCP servers
func (m *MCPManager) ConnectServers(ctx context.Context, mcpConfigs map[string]config.MCPConfig) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	for name, mcpCfg := range mcpConfigs {
		if err := m.connectServer(ctx, name, mcpCfg); err != nil {
			m.logger.Error("Failed to connect to MCP server", "name", name, "error", err)
			continue
		}
	}

	return nil
}

// connectServer connects to a single MCP server with retry
func (m *MCPManager) connectServer(ctx context.Context, name string, mcpCfg config.MCPConfig) error {
	m.logger.Info("Connecting to MCP server", "name", name, "url", mcpCfg.URL, "type", mcpCfg.Type)

	// Create HTTP client with custom headers
	var httpClient *http.Client
	if len(mcpCfg.Headers) > 0 {
		httpClient = &http.Client{
			Timeout: 30 * time.Second,
			Transport: &headerTransport{
				headers: mcpCfg.Headers,
				base:    m.httpClient.Transport,
			},
		}
	} else {
		httpClient = m.httpClient
	}

	// Create transport
	var transport mcp.Transport
	switch mcpCfg.Type {
	case "sse":
		transport = &mcp.SSEClientTransport{
			Endpoint:   mcpCfg.URL,
			HTTPClient: httpClient,
		}
	default: // streamable_http or default
		transport = &mcp.StreamableClientTransport{
			Endpoint:   mcpCfg.URL,
			HTTPClient: httpClient,
		}
	}

	// Create client
	client := mcp.NewClient(&mcp.Implementation{Name: "ggbot", Version: "1.0"}, nil)

	// Connect with timeout
	connectCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	session, err := client.Connect(connectCtx, transport, nil)
	if err != nil {
		return fmt.Errorf("connect failed: %w", err)
	}

	// Create session wrapper
	mcpSess := &mcpSession{
		session:   session,
		name:      name,
		config:    mcpCfg,
		lastUsed:  time.Now(),
		failCount: 0,
	}

	m.sessions[name] = mcpSess

	// List and register tools
	if err := m.registerTools(ctx, mcpSess); err != nil {
		m.logger.Error("Failed to register tools", "name", name, "error", err)
		return err
	}

	m.logger.Info("Successfully connected to MCP server", "name", name, "tools", len(m.tools))
	return nil
}

// registerTools lists and registers tools from a session
func (m *MCPManager) registerTools(ctx context.Context, sess *mcpSession) error {
	toolCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	toolIter := sess.session.Tools(toolCtx, nil)

	for tool, err := range toolIter {
		if err != nil {
			return fmt.Errorf("error listing tools: %w", err)
		}

		m.logger.Info("Tool discovered", "server", sess.name, "tool", tool.Name)

		// Convert InputSchema to json.RawMessage
		schemaBytes, err := marshalSchema(tool.InputSchema)
		if err != nil {
			m.logger.Error("Failed to marshal tool schema", "tool", tool.Name, "error", err)
			continue
		}

		m.tools = append(m.tools, ToolDefinition{
			Type: "function",
			Function: Function{
				Name:        tool.Name,
				Description: tool.Description,
				Parameters:  schemaBytes,
			},
		})
		m.toolMap[tool.Name] = sess
	}

	return nil
}

// CallTool executes a tool with retry and timeout
func (m *MCPManager) CallTool(ctx context.Context, toolName string, args map[string]interface{}) (string, error) {
	m.mu.RLock()
	sess, ok := m.toolMap[toolName]
	m.mu.RUnlock()

	if !ok {
		return "", fmt.Errorf("tool not found: %s", toolName)
	}

	// Check if session is closed
	sess.mu.Lock()
	if sess.closed {
		sess.mu.Unlock()
		return "", fmt.Errorf("session closed for tool: %s", toolName)
	}
	sess.mu.Unlock()

	// Execute with timeout and retry
	const maxRetries = 2
	var lastErr error

	for attempt := 0; attempt < maxRetries; attempt++ {
		if attempt > 0 {
			m.logger.Debug("Retrying tool call", "tool", toolName, "attempt", attempt+1)
			time.Sleep(time.Duration(attempt) * 500 * time.Millisecond)
		}

		result, err := m.executeToolCall(ctx, sess, toolName, args)
		if err == nil {
			sess.mu.Lock()
			sess.lastUsed = time.Now()
			sess.failCount = 0
			sess.mu.Unlock()
			return result, nil
		}

		lastErr = err
		m.logger.Warn("Tool call failed", "tool", toolName, "attempt", attempt+1, "error", err)

		sess.mu.Lock()
		sess.failCount++
		sess.mu.Unlock()
	}

	return "", fmt.Errorf("tool call failed after %d attempts: %w", maxRetries, lastErr)
}

// executeToolCall executes a single tool call with timeout
func (m *MCPManager) executeToolCall(ctx context.Context, sess *mcpSession, toolName string, args map[string]interface{}) (string, error) {
	callCtx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()

	res, err := sess.session.CallTool(callCtx, &mcp.CallToolParams{
		Name:      toolName,
		Arguments: args,
	})

	if err != nil {
		return "", err
	}

	var contentStr string
	for _, content := range res.Content {
		if textContent, ok := content.(*mcp.TextContent); ok {
			contentStr += textContent.Text
		}
	}

	return contentStr, nil
}

// GetTools returns all registered tools
func (m *MCPManager) GetTools() []ToolDefinition {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.tools
}

// Close closes all MCP sessions
func (m *MCPManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.logger.Info("Closing all MCP sessions", "count", len(m.sessions))

	var errs []error
	for name, sess := range m.sessions {
		sess.mu.Lock()
		if !sess.closed {
			if err := sess.session.Close(); err != nil {
				errs = append(errs, fmt.Errorf("failed to close session %s: %w", name, err))
			} else {
				m.logger.Info("Closed MCP session", "name", name)
			}
			sess.closed = true
		}
		sess.mu.Unlock()
	}

	m.sessions = make(map[string]*mcpSession)
	m.toolMap = make(map[string]*mcpSession)
	m.tools = nil

	if len(errs) > 0 {
		return fmt.Errorf("errors closing sessions: %v", errs)
	}
	return nil
}

// HealthCheck checks the health of all sessions
func (m *MCPManager) HealthCheck(ctx context.Context) map[string]bool {
	m.mu.RLock()
	defer m.mu.RUnlock()

	health := make(map[string]bool)
	for name, sess := range m.sessions {
		sess.mu.Lock()
		isHealthy := !sess.closed && sess.failCount < 5
		sess.mu.Unlock()
		health[name] = isHealthy
	}

	return health
}
