package system

import (
	"fmt"

	"github.com/lhpqaq/ggbot/plugins"
	tele "gopkg.in/telebot.v4"
)

type SystemPlugin struct{}

func (p *SystemPlugin) Name() string {
	return "System"
}

func (p *SystemPlugin) Init(ctx *plugins.Context) error {
	b := ctx.Bot

	b.Handle("/start", func(c tele.Context) error {
		return c.Send("你好！我是你的 AI 助手。直接向我发送消息即可开始对话。")
	})

	b.Handle("/ping", func(c tele.Context) error {
		return c.Send("在呢！")
	})

	b.Handle("/help", func(c tele.Context) error {
		help := "可用指令：\n" +
			"/start - 启动机器人\n" +
			"/ping - 检查运行状态\n" +
			"/info - 查看你的账号信息\n" +
			"/set_ai - 配置个人 AI 设置\n" +
			"/reset_ai - 重置 AI 设置为全局默认值\n"
		return c.Send(help)
	})

	b.Handle("/info", func(c tele.Context) error {
		u := c.Sender()
		info := fmt.Sprintf("📂 *个人信息*\n\n"+
			"🆔 *ID:* `%d`\n"+
			"👤 *名字:* %s\n"+
			"🗣 *姓氏:* %s\n"+
			"🔖 *用户名:* @%s\n"+
			"🌐 *语言:* %s\n"+
			"🤖 *是否机器人:* %v\n"+
			"🌟 *Premium 会员:* %v\n"+
			"➕ *加入附件菜单:* %v\n",
			u.ID, u.FirstName, u.LastName, u.Username, u.LanguageCode, u.IsBot, u.IsPremium, u.AddedToMenu,
		)
		return c.Send(info, tele.ModeMarkdown)
	})
	return nil
}
