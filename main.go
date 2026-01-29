package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/plugins"
	"github.com/lhpqaq/ggbot/plugins/ai"
	"github.com/lhpqaq/ggbot/plugins/system"
	"github.com/lhpqaq/ggbot/storage"
	tele "gopkg.in/telebot.v4"
)

func main() {
	// 1. Load Configuration
	cfg, err := config.Load("config.yaml")
	if err != nil {
		slog.Error("Failed to load config", "error", err)
		os.Exit(1)
	}

	// 2. Setup Logger
	var level slog.Level
	switch strings.ToLower(cfg.Bot.LogLevel) {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}

	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
	}))
	slog.SetDefault(logger)

	// 3. Initialize Storage
	store, err := storage.New("storage.json")
	if err != nil {
		logger.Error("Failed to init storage", "error", err)
		os.Exit(1)
	}

	// 4. Initialize Bot
	pref := tele.Settings{
		Token:  cfg.Bot.Token,
		Poller: &tele.LongPoller{Timeout: cfg.Bot.PollerTimeout},
		OnError: func(err error, c tele.Context) {
			logger.Error("Telebot error", "error", err)
		},
	}

	b, err := tele.NewBot(pref)
	if err != nil {
		logger.Error("Failed to start bot", "error", err)
		os.Exit(1)
	}

	// Middleware
	b.Use(LoggerMiddleware(logger))

	// 5. Initialize Plugins
	pluginCtx := &plugins.Context{
		Bot:     b,
		Config:  cfg,
		Storage: store,
		Logger:  logger,
	}

	allPlugins := []plugins.Plugin{
		&system.SystemPlugin{},
		&ai.AIPlugin{},
	}

	for _, p := range allPlugins {
		logger.Info("Loading plugin", "name", p.Name())
		if err := p.Init(pluginCtx); err != nil {
			logger.Error("Failed to init plugin", "plugin", p.Name(), "error", err)
			os.Exit(1)
		}
	}

	logger.Info("Bot started!")
	b.Start()
}

func LoggerMiddleware(logger *slog.Logger) tele.MiddlewareFunc {
	return func(next tele.HandlerFunc) tele.HandlerFunc {
		return func(c tele.Context) error {
			sender := c.Sender()
			text := c.Text()

			// Log basic info about the incoming update
			logger.Info("Update received",
				"user_id", sender.ID,
				"username", sender.Username,
				"text", text,
			)

			return next(c)
		}
	}
}
