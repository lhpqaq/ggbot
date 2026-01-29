package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/core"
	"github.com/lhpqaq/ggbot/plugins"
	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// headerTransport is an http.RoundTripper that adds custom headers to requests
type headerTransport struct {
	headers map[string]string
	base    http.RoundTripper
}

func (t *headerTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	for k, v := range t.headers {
		// Expand environment variables in header values (e.g., ${DASHSCOPE_API_KEY})
		expandedValue := os.ExpandEnv(v)
		req.Header.Set(k, expandedValue)
	}
	return t.base.RoundTrip(req)
}

type AIPlugin struct {
	sessions map[string]*mcp.ClientSession
	tools    []ToolDefinition
	toolMap  map[string]*mcp.ClientSession
}

func (p *AIPlugin) Name() string {
	return "AI"
}

func (p *AIPlugin) Init(ctx *plugins.Context) error {
	s := ctx.Storage
	cfg := ctx.Config
	logger := ctx.Logger

	// Initialize MCP Clients
	p.sessions = make(map[string]*mcp.ClientSession)
	p.toolMap = make(map[string]*mcp.ClientSession)
	p.tools = []ToolDefinition{}

	for name, mcpCfg := range cfg.MCPServers {
		logger.Info("Initializing MCP Server", "name", name, "url", mcpCfg.URL, "type", mcpCfg.Type)

		// Create HTTP client with custom headers if needed
		var httpClient *http.Client
		if len(mcpCfg.Headers) > 0 {
			httpClient = &http.Client{
				Transport: &headerTransport{
					headers: mcpCfg.Headers,
					base:    http.DefaultTransport,
				},
			}
		}

		// Create Transport based on type
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

		// Create Client
		client := mcp.NewClient(&mcp.Implementation{Name: "ggbot", Version: "1.0"}, nil)

		// Connect
		session, err := client.Connect(context.Background(), transport, nil)
		if err != nil {
			logger.Error("Failed to connect to MCP server", "name", name, "error", err)
			continue
		}

		p.sessions[name] = session

		// List tools
		toolIter := session.Tools(context.Background(), nil)

		for tool, err := range toolIter {
			if err != nil {
				logger.Error("Error listing tools", "name", name, "error", err)
				break
			}
			logger.Info("Tool discovered", "tool", tool.Name)

			// Convert InputSchema (any) to json.RawMessage
			schemaBytes, err := json.Marshal(tool.InputSchema)
			if err != nil {
				logger.Error("Failed to marshal tool schema", "tool", tool.Name, "error", err)
				continue
			}

			p.tools = append(p.tools, ToolDefinition{
				Type: "function",
				Function: Function{
					Name:        tool.Name,
					Description: tool.Description,
					Parameters:  json.RawMessage(schemaBytes),
				},
			})
			p.toolMap[tool.Name] = session
		}
	}

	// Schedule Push if enabled
	if cfg.Push.Enabled {
		go p.startScheduler(ctx)
	}

	// Handler: /set_ai
	ctx.RegisterCommand("/set_ai", func(c core.Context) error {
		text := c.Text()
		parts := strings.Fields(text)
		if len(parts) <= 1 {
			return c.Reply("使用方法: /set_ai key=你的KEY model=模型名称 url=API地址")
		}
		args := parts[1:]
		storageKey := c.Platform() + ":" + c.Sender().ID
		currentCfg := s.GetUserAIConfig(storageKey)
		var newCfg config.AIConfig
		if currentCfg != nil {
			newCfg = *currentCfg
		} else {
			newCfg = cfg.AI
		}
		for _, arg := range args {
			kv := strings.SplitN(arg, "=", 2)
			if len(kv) != 2 {
				continue
			}
			key, val := kv[0], kv[1]
			switch strings.ToLower(key) {
			case "key", "api_key":
				newCfg.APIKey = val
			case "model":
				newCfg.Model = val
			case "url", "base_url":
				newCfg.BaseURL = val
			case "provider":
				newCfg.Provider = val
			}
		}
		if err := s.UpdateUserAIConfig(storageKey, newCfg); err != nil {
			return c.Reply("保存设置失败: " + err.Error())
		}
		return c.Reply("AI 设置已更新！")
	})

	// Handler: /reset_ai
	ctx.RegisterCommand("/reset_ai", func(c core.Context) error {
		storageKey := c.Platform() + ":" + c.Sender().ID
		if err := s.ClearUserAIConfig(storageKey); err != nil {
			return c.Reply("重置设置失败: " + err.Error())
		}
		return c.Reply("AI 设置已重置为全局默认值。")
	})

	// Handler: /news
	ctx.RegisterCommand("/news", func(c core.Context) error {
		user := c.Sender()
		if !cfg.IsAllowed(c.Platform(), user.ID) {
			return nil
		}

		storageKey := c.Platform() + ":" + user.ID
		aiCfg := cfg.AI
		if userOverride := s.GetUserAIConfig(storageKey); userOverride != nil {
			aiCfg = *userOverride
		}

		sentMsg, err := c.Send("正在获取今日新闻... 📰")
		if err != nil {
			return c.Reply("发送消息失败: " + err.Error())
		}

		messages := []ChatMessage{
			{Role: "system", Content: "你是一个专业的新闻播报员。请获取最新新闻并进行简洁清晰的总结，用中文回复。"},
			{Role: "user", Content: "请搜索获取今日最新新闻并总结要点，列出具体的新闻事件"},
		}

		// 执行工具调用循环（最多5轮）
		for i := 0; i < 5; i++ {
			logger.Debug("News generation", "iteration", i)

			respMsg, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, messages, p.tools)
			if err != nil {
				logger.Error("News AI Generation Error", "error", err)
				_ = c.Edit(sentMsg, "获取新闻时出错: "+err.Error())
				return nil
			}

			messages = append(messages, *respMsg)
			// 如果有工具调用，执行它们
			if len(respMsg.ToolCalls) > 0 {
				for _, call := range respMsg.ToolCalls {
					session, ok := p.toolMap[call.Function.Name]
					if !ok {
						logger.Error("Tool not found", "name", call.Function.Name)
						messages = append(messages, ChatMessage{
							Role:       "tool",
							ToolCallID: call.ID,
							Content:    "Error: Tool not found",
						})
						continue
					}

					var args map[string]interface{}
					if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
						messages = append(messages, ChatMessage{
							Role:       "tool",
							ToolCallID: call.ID,
							Content:    fmt.Sprintf("Error parsing arguments: %v", err),
						})
						continue
					}

					logger.Info("Executing Tool for News", "tool", call.Function.Name)

					res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
						Name:      call.Function.Name,
						Arguments: args,
					})

					var contentStr string
					if err != nil {
						contentStr = fmt.Sprintf("Error executing tool: %v", err)
					} else {
						for _, content := range res.Content {
							if textContent, ok := content.(*mcp.TextContent); ok {
								contentStr += textContent.Text
							}
						}
					}
					logger.Debug("Tool execution result", "content", contentStr)
					messages = append(messages, ChatMessage{
						Role:       "tool",
						ToolCallID: call.ID,
						Content:    contentStr,
					})
				}
			} else {
				// 获得最终回复
				if err := c.Edit(sentMsg, respMsg.Content); err != nil {
					logger.Error("Failed to edit message", "error", err)
					return c.Reply(respMsg.Content)
				}
				return nil
			}
		}

		return c.Edit(sentMsg, "新闻获取轮次过多，已停止。")
	})

	// Handler: Text (AI Chat)
	ctx.RegisterText(func(c core.Context) error {
		if strings.HasPrefix(c.Text(), "/") {
			return nil
		}
		user := c.Sender()
		if !cfg.IsAllowed(c.Platform(), user.ID) {
			return nil
		}

		storageKey := c.Platform() + ":" + user.ID
		aiCfg := cfg.AI
		if userOverride := s.GetUserAIConfig(storageKey); userOverride != nil {
			aiCfg = *userOverride
		}

		// 获取女朋友定制提示词
		systemPrompt := aiCfg.DefaultPrompt
		if name, gfPrompt, ok := cfg.GetGirlfriendPrompt(storageKey); ok {
			logger.Debug("Using girlfriend prompt", "name", name, "user_id", user.ID)
			systemPrompt = gfPrompt
		}

		sentMsg, err := c.Send("AI 正在思考... ⏳")
		if err != nil {
			return c.Reply("发送消息失败: " + err.Error())
		}

		messages := []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: c.Text()},
		}

		// Loop for tool calls (max 5 turns)
		for i := 0; i < 5; i++ {
			logger.Debug("Generating AI response", "user_id", user.ID, "iteration", i)

			respMsg, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, messages, p.tools)
			if err != nil {
				logger.Error("AI Generation Error", "user_id", user.ID, "error", err)
				_ = c.Edit(sentMsg, "生成回复时出错: "+err.Error())
				return nil
			}

			messages = append(messages, *respMsg)

			// Check if tool calls
			if len(respMsg.ToolCalls) > 0 {
				// Execute tools
				for _, call := range respMsg.ToolCalls {
					session, ok := p.toolMap[call.Function.Name]
					if !ok {
						logger.Error("Tool not found", "name", call.Function.Name)
						messages = append(messages, ChatMessage{
							Role:       "tool",
							ToolCallID: call.ID,
							Content:    "Error: Tool not found",
						})
						continue
					}

					var args map[string]interface{}
					if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
						messages = append(messages, ChatMessage{
							Role:       "tool",
							ToolCallID: call.ID,
							Content:    fmt.Sprintf("Error parsing arguments: %v", err),
						})
						continue
					}

					logger.Info("Executing Tool", "tool", call.Function.Name)

					// CallTool using SDK
					res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
						Name:      call.Function.Name,
						Arguments: args,
					})

					var contentStr string
					if err != nil {
						contentStr = fmt.Sprintf("Error executing tool: %v", err)
					} else {
						// Extract text content from result
						for _, content := range res.Content {
							if textContent, ok := content.(*mcp.TextContent); ok {
								contentStr += textContent.Text
							} else {
								// Just in case, try JSON debug dump
								b, _ := json.Marshal(content)
								logger.Debug("Unknown tool content type", "json", string(b))
							}
						}
					}

					messages = append(messages, ChatMessage{
						Role:       "tool",
						ToolCallID: call.ID,
						Content:    contentStr,
					})
				}
				// Loop continues
			} else {
				// Final response
				if err := c.Edit(sentMsg, respMsg.Content); err != nil {
					logger.Error("Failed to edit message", "error", err)
					return c.Reply(respMsg.Content)
				}
				return nil
			}
		}

		return c.Edit(sentMsg, "AI 思考轮次过多，已停止。")
	})

	return nil
}

func (p *AIPlugin) startScheduler(ctx *plugins.Context) {
	targetTime := ctx.Config.Push.Time
	layout := "15:04"
	for {
		now := time.Now()
		parsed, err := time.Parse(layout, targetTime)
		if err != nil {
			ctx.Logger.Error("Invalid push time format", "time", targetTime)
			return
		}
		next := time.Date(now.Year(), now.Month(), now.Day(), parsed.Hour(), parsed.Minute(), 0, 0, now.Location())
		if next.Before(now) {
			next = next.Add(24 * time.Hour)
		}
		duration := next.Sub(now)
		ctx.Logger.Info("Push scheduled", "next_run", next, "duration", duration)
		time.Sleep(duration)
		p.executePush(ctx)
		time.Sleep(60 * time.Second)
	}
}

func (p *AIPlugin) executePush(ctx *plugins.Context) {
	ctx.Logger.Info("Executing Scheduled Push")
	aiCfg := ctx.Config.AI
	messages := []ChatMessage{
		{Role: "system", Content: "You are a news reporter."},
		{Role: "user", Content: ctx.Config.Push.Prompt},
	}
	var content string
	for i := 0; i < 5; i++ {
		respMsg, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, messages, p.tools)
		if err != nil {
			ctx.Logger.Error("Push Generation Error", "error", err)
			return
		}
		messages = append(messages, *respMsg)

		if len(respMsg.ToolCalls) > 0 {
			for _, call := range respMsg.ToolCalls {
				session, ok := p.toolMap[call.Function.Name]
				if !ok {
					continue
				}
				var args map[string]interface{}
				_ = json.Unmarshal([]byte(call.Function.Arguments), &args)

				res, err := session.CallTool(context.Background(), &mcp.CallToolParams{
					Name:      call.Function.Name,
					Arguments: args,
				})

				var contentStr string
				if err == nil {
					for _, c := range res.Content {
						if tc, ok := c.(*mcp.TextContent); ok {
							contentStr += tc.Text
						}
					}
				}
				messages = append(messages, ChatMessage{Role: "tool", ToolCallID: call.ID, Content: contentStr})
			}
		} else {
			content = respMsg.Content
			break
		}
	}
	if content == "" {
		ctx.Logger.Error("Push content empty")
		return
	}
	for _, target := range ctx.Config.Push.Targets {
		ctx.Logger.Info("Pushing to target", "target", target)
		if ctx.SendTo != nil {
			if err := ctx.SendTo(target, content); err != nil {
				ctx.Logger.Error("Failed to push", "target", target, "error", err)
			}
		}
	}
}
