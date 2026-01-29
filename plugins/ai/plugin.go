package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
    "time"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/core"
    "github.com/lhpqaq/ggbot/core/mcp"
	"github.com/lhpqaq/ggbot/plugins"
)

type AIPlugin struct {
    mcpClients map[string]*mcp.Client
    tools      []ToolDefinition
    toolMap    map[string]*mcp.Client // Map tool name to client
}

func (p *AIPlugin) Name() string {
	return "AI"
}

func (p *AIPlugin) Init(ctx *plugins.Context) error {
	s := ctx.Storage
	cfg := ctx.Config
	logger := ctx.Logger
    
    // Initialize MCP Clients
    p.mcpClients = make(map[string]*mcp.Client)
    p.toolMap = make(map[string]*mcp.Client)
    p.tools = []ToolDefinition{}
    
    for name, mcpCfg := range cfg.MCPServers {
        logger.Info("Initializing MCP Server", "name", name, "url", mcpCfg.URL)
        client := mcp.NewClient(mcpCfg.URL, logger)
        if err := client.Start(context.Background()); err != nil {
            logger.Error("Failed to start MCP client", "name", name, "error", err)
            continue
        }
        p.mcpClients[name] = client
        
        // List tools
        tools, err := client.ListTools(context.Background())
        if err != nil {
            logger.Error("Failed to list tools", "name", name, "error", err)
            continue
        }
        
        for _, t := range tools {
            logger.Info("Tool discovered", "tool", t.Name)
            p.tools = append(p.tools, ToolDefinition{
                Type: "function",
                Function: Function{
                    Name:        t.Name,
                    Description: t.Description,
                    Parameters:  t.InputSchema,
                },
            })
            p.toolMap[t.Name] = client
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

        sentMsg, err := c.Send("AI 正在思考... ⏳")
        if err != nil {
            return c.Reply("发送消息失败: " + err.Error())
        }

		messages := []ChatMessage{
			{Role: "system", Content: aiCfg.DefaultPrompt},
			{Role: "user", Content: c.Text()},
		}
        
        // Loop for tool calls (max 5 turns)
        for i := 0; i < 5; i++ {
            logger.Debug("Generating AI response", "user_id", user.ID, "iteration", i)
            
            respMsg, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, messages, p.tools)
            if err != nil {
                logger.Error("AI Generation Error", "user_id", user.ID, "error", err)
                _ = c.Edit(sentMsg, "生成回复时出错: " + err.Error())
                return nil
            }
            
            messages = append(messages, *respMsg)
            
            // Check if tool calls
            if len(respMsg.ToolCalls) > 0 {
                // Execute tools
                for _, call := range respMsg.ToolCalls {
                    client, ok := p.toolMap[call.Function.Name]
                    if !ok {
                         logger.Error("Tool not found", "name", call.Function.Name)
                         messages = append(messages, ChatMessage{
                             Role: "tool",
                             ToolCallID: call.ID,
                             Content: "Error: Tool not found",
                         })
                         continue
                    }
                    
                    var args map[string]interface{}
                    if err := json.Unmarshal([]byte(call.Function.Arguments), &args); err != nil {
                        messages = append(messages, ChatMessage{
                             Role: "tool",
                             ToolCallID: call.ID,
                             Content: fmt.Sprintf("Error parsing arguments: %v", err),
                         })
                         continue
                    }
                    
                    logger.Info("Executing Tool", "tool", call.Function.Name)
                    res, err := client.CallTool(context.Background(), call.Function.Name, args)
                    if err != nil {
                        res = fmt.Sprintf("Error executing tool: %v", err)
                    }
                    
                    messages = append(messages, ChatMessage{
                        Role: "tool",
                        ToolCallID: call.ID,
                        Content: res,
                    })
                }
                // Loop continues to generate next response with tool results
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
    targetTime := ctx.Config.Push.Time // "09:00"
    layout := "15:04"
    
    // Simple daily ticker
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
        
        // Sleep a bit to avoid double firing
        time.Sleep(60 * time.Second)
    }
}

func (p *AIPlugin) executePush(ctx *plugins.Context) {
    ctx.Logger.Info("Executing Scheduled Push")
    
    // Generate content using MCP
    aiCfg := ctx.Config.AI
    messages := []ChatMessage{
        {Role: "system", Content: "You are a news reporter."},
        {Role: "user", Content: ctx.Config.Push.Prompt},
    }
    
    var content string
    
    // Run generation loop
    for i := 0; i < 5; i++ {
        respMsg, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, messages, p.tools)
        if err != nil {
            ctx.Logger.Error("Push Generation Error", "error", err)
            return
        }
        messages = append(messages, *respMsg)
        
        if len(respMsg.ToolCalls) > 0 {
             for _, call := range respMsg.ToolCalls {
                 client, ok := p.toolMap[call.Function.Name]
                 if !ok { continue }
                 var args map[string]interface{}
                 _ = json.Unmarshal([]byte(call.Function.Arguments), &args)
                 res, _ := client.CallTool(context.Background(), call.Function.Name, args)
                 messages = append(messages, ChatMessage{Role: "tool", ToolCallID: call.ID, Content: res})
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
    
    // Send to targets
    for _, target := range ctx.Config.Push.Targets {
        // Find adapter?
        // Wait, SendTo is on Platform. 
        // ctx has no direct access to platforms list.
        // We defined SendTo in Platform interface.
        // But plugins don't have access to the list of platforms directly via Context?
        // Wait, ctx is PluginContext. It has `RegisterCommand` etc.
        // It DOES NOT have a way to invoke SendTo on a specific platform.
        // I need to extend PluginContext.
        
        // Let's modify core.PluginContext to include a SendToFunc.
        ctx.Logger.Info("Pushing to target", "target", target)
        // But we need to know WHICH platform.
        // Target format "Telegram:123" implies platform.
        // I need to inject a `SendTo` function into `PluginContext` that delegates to correct platform.
        
        if ctx.SendTo != nil {
            if err := ctx.SendTo(target, content); err != nil {
                 ctx.Logger.Error("Failed to push", "target", target, "error", err)
            }
        }
    }
}