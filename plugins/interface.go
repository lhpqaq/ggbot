package plugins

import (
	"log/slog"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/storage"
	tele "gopkg.in/telebot.v4"
)

type Context struct {
	Bot     *tele.Bot
	Config  *config.Config
	Storage *storage.Storage
	Logger  *slog.Logger
}

type Plugin interface {
	Name() string
	Init(ctx *Context) error
}
