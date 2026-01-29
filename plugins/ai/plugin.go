package ai

import (
	"strings"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/plugins"
	tele "gopkg.in/telebot.v4"
)

type AIPlugin struct{}

func (p *AIPlugin) Name() string {
	return "AI"
}

func (p *AIPlugin) Init(ctx *plugins.Context) error {
	b := ctx.Bot
	s := ctx.Storage
	cfg := ctx.Config

	// Handler: /set_ai
	b.Handle("/set_ai", func(c tele.Context) error {
		args := c.Args()
		if len(args) == 0 {
			return c.Send("使用方法: /set_ai key=你的KEY model=模型名称 url=API地址")
		}

		userID := c.Sender().ID
		currentCfg := s.GetUserAIConfig(userID)

		var newCfg config.AIConfig
		if currentCfg != nil {
			newCfg = *currentCfg
		} else {
			newCfg = cfg.AI
		}

		for _, arg := range args {
			parts := strings.SplitN(arg, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key, val := parts[0], parts[1]
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

		if err := s.UpdateUserAIConfig(userID, newCfg); err != nil {
			return c.Send("保存设置失败: " + err.Error())
		}

		return c.Send("AI 设置已更新！")
	})

	// Handler: /reset_ai
	b.Handle("/reset_ai", func(c tele.Context) error {
		if err := s.ClearUserAIConfig(c.Sender().ID); err != nil {
			return c.Send("重置设置失败: " + err.Error())
		}
		return c.Send("AI 设置已重置为全局默认值。")
	})

	// Handler: Text (AI Chat)
	b.Handle(tele.OnText, func(c tele.Context) error {
		if strings.HasPrefix(c.Text(), "/") {
			return nil
		}

		user := c.Sender()
		if !cfg.IsAllowed(user.ID) {
			return nil
		}

		aiCfg := cfg.AI
		if userOverride := s.GetUserAIConfig(user.ID); userOverride != nil {
			aiCfg = *userOverride
		}

		_ = c.Notify(tele.Typing)

		messages := []ChatMessage{
			{Role: "system", Content: aiCfg.DefaultPrompt},
			{Role: "user", Content: c.Text()},
		}

		ctx.Logger.Debug("Generating AI response", "user_id", user.ID, "model", aiCfg.Model)

		resp, err := Generate(aiCfg.BaseURL, aiCfg.APIKey, aiCfg.Model, messages)
		if err != nil {
			ctx.Logger.Error("AI Generation Error", "user_id", user.ID, "error", err)
			return c.Send("生成回复时出错: " + err.Error())
		}

		return c.Send(resp)
	})

	return nil
}
