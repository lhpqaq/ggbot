package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Client is a minimal MCP client supporting SSE transport
type Client struct {
	BaseURL    string
	Endpoint   string // The POST endpoint received via SSE
	SessionID  string
	HTTPClient *http.Client
	Logger     *slog.Logger
}

func NewClient(url string, logger *slog.Logger) *Client {
	return &Client{
		BaseURL:    url,
		HTTPClient: &http.Client{Timeout: 30 * time.Second},
		Logger:     logger,
	}
}

// Start connects to the SSE stream to get the endpoint and initializes the session
func (c *Client) Start(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, "GET", c.BaseURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "text/event-stream")

	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return err
	}
    
    started := make(chan error)
    
	go func() {
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		for scanner.Scan() {
			line := scanner.Text()
			if strings.HasPrefix(line, "event: endpoint") {
				if scanner.Scan() {
					dataLine := scanner.Text()
					if strings.HasPrefix(dataLine, "data: ") {
						endpoint := strings.TrimPrefix(dataLine, "data: ")
                        c.Endpoint = endpoint
                        c.Logger.Info("MCP Endpoint Discovered", "endpoint", endpoint)
                        
                        // Notify start
                        select {
                        case started <- nil:
                        default:
                        }
					}
				}
			}
		}
        if err := scanner.Err(); err != nil {
             c.Logger.Error("MCP SSE Error", "error", err)
        }
	}()

	select {
	case <-started:
        return c.Initialize(ctx)
	case <-time.After(10 * time.Second):
		return fmt.Errorf("timeout waiting for MCP endpoint")
	}
}

// JSON-RPC Types
type JSONRPCRequest struct {
	JSONRPC string      `json:"jsonrpc"`
	Method  string      `json:"method"`
	Params  interface{} `json:"params,omitempty"`
	ID      int         `json:"id"`
}

type JSONRPCResponse struct {
	JSONRPC string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result,omitempty"`
	Error   *JSONRPCError   `json:"error,omitempty"`
	ID      int             `json:"id"`
}

type JSONRPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (c *Client) Call(ctx context.Context, method string, params interface{}) (json.RawMessage, error) {
    url := c.Endpoint
    if strings.HasPrefix(url, "/") {
        // Construct full URL relative to BaseURL
        parts := strings.Split(c.BaseURL, "/")
        if len(parts) >= 3 {
            // scheme://host
            host := parts[0] + "//" + parts[2]
            url = host + url
        }
    }

	reqBody := JSONRPCRequest{
		JSONRPC: "2.0",
		Method:  method,
		Params:  params,
		ID:      1,
	}
	
	data, err := json.Marshal(reqBody)
	if err != nil {
		return nil, err
	}
    
	req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewBuffer(data))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
    
	resp, err := c.HTTPClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
    
    var rpcResp JSONRPCResponse
    if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
        return nil, err
    }
    
    if rpcResp.Error != nil {
        return nil, fmt.Errorf("RPC Error %d: %s", rpcResp.Error.Code, rpcResp.Error.Message)
    }
    
    return rpcResp.Result, nil
}

// Initialize performs the handshake
func (c *Client) Initialize(ctx context.Context) error {
    params := map[string]interface{}{
        "protocolVersion": "2024-11-05",
        "capabilities": map[string]interface{}{
            "roots": map[string]interface{}{
                "listChanged": true,
            },
            "sampling": map[string]interface{}{},
        },
        "clientInfo": map[string]string{
            "name": "ggbot",
            "version": "1.0",
        },
    }
    
    _, err := c.Call(ctx, "initialize", params)
    if err != nil {
        return err
    }
    
    // Notify initialized
    _, err = c.Call(ctx, "notifications/initialized", nil)
    return err
}

// Tool Definitions
type Tool struct {
    Name        string          `json:"name"`
    Description string          `json:"description"`
    InputSchema json.RawMessage `json:"inputSchema"` // JSON Schema
}

type ListToolsResult struct {
    Tools []Tool `json:"tools"`
}

func (c *Client) ListTools(ctx context.Context) ([]Tool, error) {
    res, err := c.Call(ctx, "tools/list", nil)
    if err != nil {
        return nil, err
    }
    
    var result ListToolsResult
    if err := json.Unmarshal(res, &result); err != nil {
        return nil, err
    }
    return result.Tools, nil
}

func (c *Client) CallTool(ctx context.Context, name string, args map[string]interface{}) (string, error) {
    params := map[string]interface{}{
        "name": name,
        "arguments": args,
    }
    
    res, err := c.Call(ctx, "tools/call", params)
    if err != nil {
        return "", err
    }
    
    // Result is usually { content: [{type:text, text:...}] }
    var callResult struct {
        Content []struct {
            Type string `json:"type"`
            Text string `json:"text"`
        } `json:"content"`
        IsError bool `json:"isError"`
    }
    
    if err := json.Unmarshal(res, &callResult); err != nil {
        return string(res), nil
    }
    
    if callResult.IsError {
        return "", fmt.Errorf("tool error")
    }
    
    var sb strings.Builder
    for _, item := range callResult.Content {
        if item.Type == "text" {
            sb.WriteString(item.Text)
        }
    }
    
    return sb.String(), nil
}