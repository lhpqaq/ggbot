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
        // c.Text() includes command like "/set_ai key=..."
        text := c.Text()
        parts := strings.Fields(text)
        if len(parts) <= 1 {
			return c.Reply("使用方法: /set_ai key=你的KEY model=模型名称 url=API地址")
		}
        
        args := parts[1:]

		// User ID is string now
        // Storage expects int64?
        // We need to update Storage to support string IDs or Parse int64.
        // Telegram IDs are int64, QQ IDs are string (OpenID).
        // Let's update storage to use string keys or Hash string to int64 (bad idea).
        // Better: Update storage to use string.
        
        userIDStr := c.Sender().ID
        // Temporary hack: storage uses int64. 
        // If Platform is Telegram, we can parse.
        // If QQ, we have a problem if storage expects int64.
        
        // Let's assume we update storage later. For now, try parse.
        // If error, maybe use hash or 0?
        // Actually I MUST update storage.
        
        // For now, let's defer storage update and implement logic assuming I fix storage.
        
		currentCfg := s.GetUserAIConfig(userIDStr)
		
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

		if err := s.UpdateUserAIConfig(userIDStr, newCfg); err != nil {
			return c.Reply("保存设置失败: " + err.Error())
		}

		return c.Reply("AI 设置已更新！")
	})

	// Handler: /reset_ai
	ctx.RegisterCommand("/reset_ai", func(c core.Context) error {
		if err := s.ClearUserAIConfig(c.Sender().ID); err != nil {
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
        // AllowedUsers is []int64. Need to check string ID.
        // Config needs update too.
		if !cfg.IsAllowed(user.ID) {
			return nil
		}

		aiCfg := cfg.AI
		if userOverride := s.GetUserAIConfig(user.ID); userOverride != nil {
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