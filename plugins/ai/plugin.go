package ai

import (
	"strings"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/core"
	"github.com/lhpqaq/ggbot/plugins"
)

type AIPlugin struct{}

func (p *AIPlugin) Name() string {
	return "AI"
}

func (p *AIPlugin) Init(ctx *plugins.Context) error {
	s := ctx.Storage
	cfg := ctx.Config
	logger := ctx.Logger

	// Handler: /set_ai
	ctx.RegisterCommand("/set_ai", func(c core.Context) error {
        // Parsing args from Text
        text := c.Text()
        parts := strings.Fields(text)
        if len(parts) <= 1 {
			return c.Reply("使用方法: /set_ai key=你的KEY model=模型名称 url=API地址")
		}
        
        args := parts[1:]
        
        // Use Platform:UserID as key to avoid collisions
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
        // Check allowed user with platform context
		if !cfg.IsAllowed(c.Platform(), user.ID) {
            // Optional: Log or reply if debug mode
			return nil
		}
        
        storageKey := c.Platform() + ":" + user.ID

		aiCfg := cfg.AI
		if userOverride := s.GetUserAIConfig(storageKey); userOverride != nil {
			aiCfg = *userOverride
		}

		// Show typing status? core.Context doesn't have it yet.
		// Ignore for now.

        // Send placeholder
        sentMsg, err := c.Send("AI 正在思考... ⏳")
        if err != nil {
            return c.Reply("发送消息失败: " + err.Error())
        }

		messages := []ChatMessage{
			{Role: "system", Content: aiCfg.DefaultPrompt},
			{Role: "user", Content: c.Text()},
		}
        
        logger.Debug("Generating AI response", "user_id", user.ID, "model", aiCfg.Model)

		resp, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, messages)
		if err != nil {
			logger.Error("AI Generation Error", "user_id", user.ID, "error", err)
            _ = c.Edit(sentMsg, "生成回复时出错: " + err.Error())
			return nil
		}

        if err := c.Edit(sentMsg, resp); err != nil {
             logger.Error("Failed to edit message", "error", err)
             // Fallback
             return c.Reply(resp)
        }
        return nil
	})

	return nil
}