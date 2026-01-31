package ai

import (
	"context"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/core"
	"github.com/lhpqaq/ggbot/plugins"
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
	mcpManager   *MCPManager
	toolExecutor *ToolExecutor
}

func (p *AIPlugin) Name() string {
	return "AI"
}

func (p *AIPlugin) Init(ctx *plugins.Context) error {
	s := ctx.Storage
	cfg := ctx.Config
	logger := ctx.Logger

	// Initialize MCP Manager and Tool Executor
	p.mcpManager = NewMCPManager(cfg.Proxy, logger)
	p.toolExecutor = NewToolExecutor(p.mcpManager, logger)

	// Connect to all MCP servers
	if len(cfg.MCPServers) > 0 {
		connectCtx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()

		if err := p.mcpManager.ConnectServers(connectCtx, cfg.MCPServers); err != nil {
			logger.Error("Failed to connect to some MCP servers", "error", err)
			// Continue anyway - some servers may have connected successfully
		}

		// Log connection status
		health := p.mcpManager.HealthCheck(context.Background())
		for name, isHealthy := range health {
			if isHealthy {
				logger.Info("MCP server healthy", "name", name)
			} else {
				logger.Warn("MCP server unhealthy", "name", name)
			}
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

		// Use tool executor for cleaner code
		executeCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		// Get platform-specific prompt
		platformPrompt := cfg.GetPlatformPrompt(c.Platform())

		finalContent, err := p.toolExecutor.ExecuteWithTools(executeCtx, aiCfg, messages, 5, platformPrompt)
		if err != nil {
			logger.Error("News generation error", "error", err)
			_ = c.Edit(sentMsg, "获取新闻时出错: "+err.Error())
			return nil
		}

		if err := c.Edit(sentMsg, finalContent); err != nil {
			logger.Error("Failed to edit message", "error", err)
			return c.Reply(finalContent)
		}
		return nil
	})

	// Handler: /s - 搜索指令，使用 MCP 工具搜索
	ctx.RegisterCommand("/s", func(c core.Context) error {
		user := c.Sender()
		if !cfg.IsAllowed(c.Platform(), user.ID) {
			return nil
		}

		// 获取搜索关键词
		text := c.Text()
		parts := strings.SplitN(text, " ", 2)
		if len(parts) < 2 || strings.TrimSpace(parts[1]) == "" {
			return c.Reply("使用方法: /s 搜索内容\n例如: /s 今天天气怎么样")
		}
		query := strings.TrimSpace(parts[1])

		storageKey := c.Platform() + ":" + user.ID
		aiCfg := cfg.AI
		if userOverride := s.GetUserAIConfig(storageKey); userOverride != nil {
			aiCfg = *userOverride
		}

		// 获取女朋友定制提示词
		systemPrompt := `你是一个智能搜索助手。
你必须先使用搜索工具来获取最新信息，然后根据搜索结果用简洁清晰的中文回答用户的问题。
请注意：
1. 首先调用搜索工具获取相关信息
2. 获取到搜索结果后，对结果进行分析和总结
3. 用简洁、有条理的中文回复用户
4. 如果搜索结果不相关，请说明并尝试用其他关键词重新搜索`
		if name, gfPrompt, ok := cfg.GetGirlfriendPrompt(storageKey); ok {
			logger.Debug("Using girlfriend prompt for search", "name", name)
			systemPrompt = gfPrompt + "\n\n你需要使用搜索工具获取最新信息来回答问题，获取到结果后用温暖的语气总结回复。"
		}

		sentMsg, err := c.Send("🔍 正在搜索...")
		if err != nil {
			return c.Reply("发送消息失败: " + err.Error())
		}

		messages := []ChatMessage{
			{Role: "system", Content: systemPrompt},
			{Role: "user", Content: query},
		}

		// Use tool executor
		executeCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		// Get platform-specific prompt
		platformPrompt := cfg.GetPlatformPrompt(c.Platform())

		finalContent, err := p.toolExecutor.ExecuteWithTools(executeCtx, aiCfg, messages, 5, platformPrompt)
		if err != nil {
			logger.Error("Search error", "error", err)
			_ = c.Edit(sentMsg, "搜索时出错: "+err.Error())
			return nil
		}

		if err := c.Edit(sentMsg, finalContent); err != nil {
			logger.Error("Failed to edit message", "error", err)
			return c.Reply(finalContent)
		}
		return nil
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

		// Use tool executor
		executeCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
		defer cancel()

		// Get platform-specific prompt
		platformPrompt := cfg.GetPlatformPrompt(c.Platform())

		finalContent, err := p.toolExecutor.ExecuteWithTools(executeCtx, aiCfg, messages, 5, platformPrompt)
		if err != nil {
			logger.Error("AI generation error", "user_id", user.ID, "error", err)
			_ = c.Edit(sentMsg, "生成回复时出错: "+err.Error())
			return nil
		}

		if err := c.Edit(sentMsg, finalContent); err != nil {
			logger.Error("Failed to edit message", "error", err)
			return c.Reply(finalContent)
		}
		return nil
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

	// Use tool executor with timeout
	executeCtx, cancel := context.WithTimeout(context.Background(), 120*time.Second)
	defer cancel()

	// No platform prompt for scheduled push
	content, err := p.toolExecutor.ExecuteWithTools(executeCtx, aiCfg, messages, 5, "")
	if err != nil {
		ctx.Logger.Error("Push generation error", "error", err)
		return
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

// Cleanup closes MCP connections when plugin is unloaded
func (p *AIPlugin) Cleanup() error {
	if p.mcpManager != nil {
		return p.mcpManager.Close()
	}
	return nil
}
