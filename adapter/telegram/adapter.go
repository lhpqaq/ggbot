package telegram

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/core"
	tele "gopkg.in/telebot.v4"
)

type TelegramAdapter struct {
	bot    *tele.Bot
	logger *slog.Logger
}

func New(cfg config.BotConfig, logger *slog.Logger) (*TelegramAdapter, error) {
	// 设置代理 (本地 7890 端口)
	proxyURL, _ := url.Parse("http://127.0.0.1:7890")
	httpClient := &http.Client{
		Transport: &http.Transport{
			Proxy: http.ProxyURL(proxyURL),
		},
		Timeout: 30 * time.Second,
	}

	pref := tele.Settings{
		Token:  cfg.Token,
		Poller: &tele.LongPoller{Timeout: cfg.PollerTimeout},
		Client: httpClient,
		OnError: func(err error, c tele.Context) {
			logger.Error("Telegram error", "error", err)
		},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		return nil, err
	}

	logger.Info("Telegram adapter initialized with proxy", "proxy", "http://127.0.0.1:7890")
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

func (a *TelegramAdapter) SendTo(recipient string, text string) error {
	id, err := strconv.ParseInt(recipient, 10, 64)
	if err != nil {
		return fmt.Errorf("invalid telegram recipient id: %s", recipient)
	}
	_, err = a.bot.Send(&tele.User{ID: id}, text)
	return err
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
