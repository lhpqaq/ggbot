package system

import (
	"fmt"

	"github.com/lhpqaq/ggbot/core"
	"github.com/lhpqaq/ggbot/plugins"
)

type SystemPlugin struct{}

func (p *SystemPlugin) Name() string {
	return "System"
}

func (p *SystemPlugin) Init(ctx *plugins.Context) error {
	// Start
	ctx.RegisterCommand("/start", func(c core.Context) error {
		return c.Reply("你好！我是你的 AI 助手。直接向我发送消息即可开始对话。\n")
	})

	// Ping
	ctx.RegisterCommand("/ping", func(c core.Context) error {
		return c.Reply("在呢！\n")
	})
    
	// Help
	ctx.RegisterCommand("/help", func(c core.Context) error {
		help := "可用指令：\n" +
			"/start - 启动机器人\n" +
			"/ping - 检查运行状态\n" +
			"/info - 查看你的账号信息\n" +
			"/set_ai - 配置个人 AI 设置\n" +
			"/reset_ai - 重置 AI 设置为全局默认值\n"
		return c.Reply(help)
	})

	// Info
	ctx.RegisterCommand("/info", func(c core.Context) error {
		u := c.Sender()
		// Convert ID to int if possible for legacy display, or just display as string
		id := u.ID
		
		info := fmt.Sprintf("📂 *个人信息*\n\n" +
			"🆔 *ID:* `%s`\n" +
			"👤 *名字:* %s\n" +
			"🤖 *是否机器人:* %v\n",
			id, u.Username, u.IsBot,
		)
		
		// Markdown mode is platform specific?
		// Core interface abstracts Reply. TelegramAdapter handles defaults.
		// If we need Markdown, maybe we need options in Reply.
		// For now simple reply.
		return c.Reply(info)
	})

	return nil
}