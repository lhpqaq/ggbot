package config

import (
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Bot BotConfig `yaml:"bot"`
	AI  AIConfig  `yaml:"ai"`
	// Legacy: mixed list
	AllowedUsers []string `yaml:"allowed_users"`

	// Platform specific lists
	AllowedTelegram []string `yaml:"allowed_telegram"`
	AllowedQQ       []string `yaml:"allowed_qq"`
    
    // MCP Configuration
    MCPServers map[string]MCPConfig `yaml:"mcpServers"`
    
    // Push Configuration
    Push PushConfig `yaml:"push"`
}

type MCPConfig struct {
    Type string `yaml:"type"` // e.g. "streamable_http"
    URL  string `yaml:"url"`
}

type PushConfig struct {
    Enabled  bool     `yaml:"enabled"`
    Time     string   `yaml:"time"`   // e.g. "08:00"
    Targets  []string `yaml:"targets"` // e.g. ["Telegram:123", "QQ:Group:456"]
    Prompt   string   `yaml:"prompt"`  // Prompt to generate content, e.g. "Get hot news"
}

type BotConfig struct {
	Token         string        `yaml:"token"`
	PollerTimeout time.Duration `yaml:"poller_timeout"`
	LogLevel      string        `yaml:"log_level"` // debug, info, warn, error

	// QQ Configuration
	QQAppID  string `yaml:"qq_app_id"`
	QQSecret string `yaml:"qq_secret"`
	// Deprecated: use qq_secret
	QQToken string `yaml:"qq_token"`
}

type AIConfig struct {
	Provider      string `yaml:"provider"`
	BaseURL       string `yaml:"base_url"`
	APIKey        string `yaml:"api_key"`
	Model         string `yaml:"model"`
	DefaultPrompt string `yaml:"default_prompt"`
}

func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	err = yaml.Unmarshal(data, &cfg)
	if err != nil {
		return nil, err
	}

	// Fallback for QQSecret
	if cfg.Bot.QQSecret == "" && cfg.Bot.QQToken != "" {
		cfg.Bot.QQSecret = cfg.Bot.QQToken
	}

	return &cfg, nil
}

func (c *Config) IsAllowed(platform string, userID string) bool {
	// Check specific lists first
	switch strings.ToLower(platform) {
	case "telegram":
		for _, id := range c.AllowedTelegram {
			if id == userID {
				return true
			}
		}
	case "qq":
		for _, id := range c.AllowedQQ {
			if id == userID {
				return true
			}
		}
		return true
	}

	// Fallback to legacy global list (useful for simple migration)
	for _, id := range c.AllowedUsers {
		if id == userID {
			return true
		}
	}
	return false
}
