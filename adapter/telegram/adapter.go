package telegram

import (
	"fmt"
	"log/slog"
	"strconv"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/core"
	tele "gopkg.in/telebot.v4"
)

type TelegramAdapter struct {
	bot    *tele.Bot
	logger *slog.Logger
}

func New(cfg config.BotConfig, logger *slog.Logger) (*TelegramAdapter, error) {
	pref := tele.Settings{
		Token:  cfg.Token,
		Poller: &tele.LongPoller{Timeout: cfg.PollerTimeout},
		OnError: func(err error, c tele.Context) {
			logger.Error("Telegram error", "error", err)
		},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		return nil, err
	}

	return &TelegramAdapter{bot: b, logger: logger}, nil
}

func (a *TelegramAdapter) Name() string {
	return "Telegram"
}

func (a *TelegramAdapter) Start() error {
	a.logger.Info("Starting Telegram Bot")
	go a.bot.Start()
	return nil
}

func (a *TelegramAdapter) Stop() error {
	a.bot.Stop()
	return nil
}

func (a *TelegramAdapter) RegisterCommand(cmd string, handler core.Handler) {
	a.bot.Handle(cmd, func(c tele.Context) error {
		return handler(&TeleContext{ctx: c, bot: a.bot})
	})
}

func (a *TelegramAdapter) RegisterText(handler core.Handler) {
	a.bot.Handle(tele.OnText, func(c tele.Context) error {
		// Telebot OnText might catch commands too, filter if necessary or let handler decide
		// Usually Telebot dispatches specific commands first.
		return handler(&TeleContext{ctx: c, bot: a.bot})
	})
}

// We need a concrete context implementation
type TeleContext struct {
	ctx tele.Context
	bot *tele.Bot
}

func (c *TeleContext) Sender() *core.User {
	u := c.ctx.Sender()
	return &core.User{
		ID:       strconv.FormatInt(u.ID, 10),
		Username: u.Username,
		IsBot:    u.IsBot,
	}
}

func (c *TeleContext) Text() string {
	return c.ctx.Text()
}

func (c *TeleContext) Reply(text string) error {
	return c.ctx.Send(text)
}

func (c *TeleContext) Send(text string) (core.Message, error) {
	msg, err := c.ctx.Bot().Send(c.ctx.Recipient(), text)
	if err != nil {
		return nil, err
	}
	return &TeleMessage{msg: msg, bot: c.bot}, nil
}

func (c *TeleContext) Edit(msg core.Message, text string) error {
	tm, ok := msg.(*TeleMessage)
	if !ok {
		return fmt.Errorf("invalid message type for telegram")
	}
	_, err := c.bot.Edit(tm.msg, text)
	return err
}

func (c *TeleContext) Platform() string {
	return "Telegram"
}

type TeleMessage struct {
	msg *tele.Message
	bot *tele.Bot
}

func (m *TeleMessage) ID() string {
	return strconv.Itoa(m.msg.ID)
}
