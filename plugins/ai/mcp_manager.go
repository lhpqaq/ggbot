package ai

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"os/exec"
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
	proxyCfg   config.ProxyConfig
}

type mcpSession struct {
	session     *mcp.ClientSession
	name        string
	config      config.MCPConfig
	lastUsed    time.Time
	failCount   int
	mu          sync.Mutex
	closed      bool
	cmd         *exec.Cmd // For stdio-based connections
}

// NewMCPManager creates a new MCP manager
func NewMCPManager(proxyCfg config.ProxyConfig, logger *slog.Logger) *MCPManager {
	// 创建默认 HTTP 客户端（不使用代理）
	// MCP 服务器通过 use_proxy 字段单独控制是否使用代理
	defaultClient := &http.Client{
		Timeout: 30 * time.Second,
		Transport: &http.Transport{
			MaxIdleConns:        100,
			MaxIdleConnsPerHost: 10,
			IdleConnTimeout:     90 * time.Second,
		},
	}

	return &MCPManager{
		sessions:   make(map[string]*mcpSession),
		toolMap:    make(map[string]*mcpSession),
		tools:      []ToolDefinition{},
		logger:     logger,
		httpClient: defaultClient,
		proxyCfg:   proxyCfg,
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
	m.logger.Info("Connecting to MCP server", "name", name, "type", mcpCfg.Type, "url", mcpCfg.URL, "command", mcpCfg.Command)

	var transport mcp.Transport
	var cmd *exec.Cmd

	// Determine transport type
	if mcpCfg.Type == "stdio" || mcpCfg.Command != "" {
		// stdio type: launch command and communicate via stdin/stdout
		if mcpCfg.Command == "" {
			return fmt.Errorf("command is required for stdio type")
		}

		m.logger.Info("Starting MCP command", "name", name, "command", mcpCfg.Command, "args", mcpCfg.Args, "env_count", len(mcpCfg.Env), "use_proxy", mcpCfg.UseProxy)

		cmd = exec.CommandContext(ctx, mcpCfg.Command, mcpCfg.Args...)

		// Start with parent process environment
		cmd.Env = append([]string{}, cmd.Environ()...)

		// Set proxy environment variables for the child process
		if mcpCfg.UseProxy && m.proxyCfg.URL != "" {
			// Set standard proxy environment variables
			proxyEnvVars := []string{
				fmt.Sprintf("HTTP_PROXY=%s", m.proxyCfg.URL),
				fmt.Sprintf("HTTPS_PROXY=%s", m.proxyCfg.URL),
				fmt.Sprintf("http_proxy=%s", m.proxyCfg.URL),
				fmt.Sprintf("https_proxy=%s", m.proxyCfg.URL),
			}
			cmd.Env = append(cmd.Env, proxyEnvVars...)
			m.logger.Info("MCP command will use proxy", "name", name, "proxy", m.proxyCfg.URL)
		} else {
			// Explicitly disable proxy by unsetting proxy environment variables
			// This prevents the child process from inheriting proxy settings
			m.logger.Info("MCP command will NOT use proxy", "name", name)
			// Note: We don't unset here, just don't add proxy vars
			// If you want to explicitly disable, uncomment below:
			// cmd.Env = append(cmd.Env, "HTTP_PROXY=", "HTTPS_PROXY=", "http_proxy=", "https_proxy=")
		}

		// Add custom environment variables (these can override proxy settings if needed)
		if len(mcpCfg.Env) > 0 {
			for key, value := range mcpCfg.Env {
				envVar := fmt.Sprintf("%s=%s", key, value)
				cmd.Env = append(cmd.Env, envVar)
				m.logger.Debug("Setting MCP environment variable", "name", name, "key", key)
			}
		}

		transport = &mcp.CommandTransport{
			Command: cmd,
		}

		m.logger.Info("MCP server will use stdio transport", "name", name)
	} else {
		// HTTP/SSE type: create HTTP client with or without proxy
		var baseTransport http.RoundTripper
		if mcpCfg.UseProxy && m.proxyCfg.URL != "" {
			// Use proxy
			proxyURL, err := url.Parse(m.proxyCfg.URL)
			if err != nil {
				return fmt.Errorf("invalid proxy URL: %w", err)
			}
			baseTransport = &http.Transport{
				Proxy:               http.ProxyURL(proxyURL),
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			}
			m.logger.Info("MCP server will use proxy", "name", name, "proxy", m.proxyCfg.URL)
		} else {
			// No proxy - create new transport without proxy
			baseTransport = &http.Transport{
				MaxIdleConns:        100,
				MaxIdleConnsPerHost: 10,
				IdleConnTimeout:     90 * time.Second,
			}
			m.logger.Info("MCP server will NOT use proxy", "name", name)
		}

		// Create HTTP client with custom headers if needed
		var httpClient *http.Client
		if len(mcpCfg.Headers) > 0 {
			httpClient = &http.Client{
				Timeout: 30 * time.Second,
				Transport: &headerTransport{
					headers: mcpCfg.Headers,
					base:    baseTransport,
				},
			}
		} else {
			httpClient = &http.Client{
				Timeout:   30 * time.Second,
				Transport: baseTransport,
			}
		}

		// Create transport based on type
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
		cmd:       cmd, // Save command for cleanup (nil for HTTP/SSE)
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

			// If this is a stdio session, wait for command to exit
			if sess.cmd != nil && sess.cmd.Process != nil {
				m.logger.Info("Waiting for MCP command to exit", "name", name, "pid", sess.cmd.Process.Pid)
				// The CommandTransport.Close() should have already handled graceful shutdown
				// Just log if the process is still running
				if sess.cmd.ProcessState == nil || !sess.cmd.ProcessState.Exited() {
					m.logger.Debug("MCP command process cleanup in progress", "name", name)
				}
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
