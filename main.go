package main

import (
	"log/slog"
	"os"
	"strings"

	"github.com/lhpqaq/ggbot/adapter/qq"
	"github.com/lhpqaq/ggbot/adapter/telegram"
	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/core"
	"github.com/lhpqaq/ggbot/plugins"
	"github.com/lhpqaq/ggbot/plugins/ai"
	"github.com/lhpqaq/ggbot/plugins/system"
	"github.com/lhpqaq/ggbot/storage"
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

	// 4. Initialize Platforms
	var platforms []core.Platform

	// Telegram
	if cfg.Bot.Token != "" {
		teleAdapter, err := telegram.New(cfg.Bot, cfg.Proxy, logger)
		if err != nil {
			logger.Error("Failed to init Telegram", "error", err)
		} else {
			platforms = append(platforms, teleAdapter)
		}
	}

	// QQ - 强制不使用代理
	if cfg.Bot.QQAppID != "" {
		qqAdapter, err := qq.New(cfg.Bot, logger)
		if err != nil {
			logger.Error("Failed to init QQ", "error", err)
		} else {
			platforms = append(platforms, qqAdapter)
		}
	}

	if len(platforms) == 0 {
		logger.Error("No platforms configured or initialized successfully")
		os.Exit(1)
	}

	// 5. Initialize Plugins
	// We create a composite registration function that registers on ALL platforms
	pluginCtx := &plugins.Context{
		Config:  cfg,
		Storage: store,
		Logger:  logger,
		RegisterCommand: func(cmd string, h core.Handler) {
			for _, p := range platforms {
				p.RegisterCommand(cmd, h)
			}
		},
		RegisterText: func(h core.Handler) {
			for _, p := range platforms {
				p.RegisterText(h)
			}
		},
		SendTo: func(recipient string, text string) error {
			// Recipient format: "Platform:Target"
			parts := strings.SplitN(recipient, ":", 2)
			if len(parts) != 2 {
				return nil // Or error "invalid format"
			}
			platformName := strings.ToLower(parts[0])
			target := parts[1]

			for _, p := range platforms {
				if strings.ToLower(p.Name()) == platformName {
					return p.SendTo(target, text)
				}
			}
			return nil // Platform not found
		},
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

	// 6. Start Platforms
	for _, p := range platforms {
		if err := p.Start(); err != nil {
			logger.Error("Failed to start platform", "platform", p.Name(), "error", err)
		}
	}

	// Block forever
	select {}
}
