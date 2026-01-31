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

	// Proxy Configuration
	Proxy ProxyConfig `yaml:"proxy"`

	// MCP Configuration
	MCPServers map[string]MCPConfig `yaml:"mcpServers"`

	// Push Configuration
	Push PushConfig `yaml:"push"`

	// Platform specific prompts
	PlatformPrompts map[string]string `yaml:"platform_prompts"`

	// 女朋友定制配置
	Girlfriend map[string]GirlfriendConfig `yaml:"girlfriend"`
}

// GirlfriendConfig 女朋友定制配置
type GirlfriendConfig struct {
	Name   string `yaml:"name"`   // 昵称
	Prompt string `yaml:"prompt"` // 定制提示词
}

// ProxyConfig 代理配置
type ProxyConfig struct {
	URL              string `yaml:"url"`               // 代理地址，如 "http://127.0.0.1:7890"
	TelegramUseProxy bool   `yaml:"telegram_use_proxy"` // Telegram 是否使用代理，默认 false
	QQUseProxy       bool   `yaml:"qq_use_proxy"`       // QQ 是否使用代理，默认 false (强制不走代理)
}

type MCPConfig struct {
	Type     string            `yaml:"type"`      // e.g. "streamable_http", "sse", "stdio"
	URL      string            `yaml:"url"`       // For http/sse type
	Headers  map[string]string `yaml:"headers"`   // Custom headers for authentication
	UseProxy bool              `yaml:"use_proxy"` // Whether to use proxy, default true

	// For stdio type (command-based)
	Command string   `yaml:"command"` // Command to execute, e.g. "npx"
	Args    []string `yaml:"args"`    // Command arguments, e.g. ["bing-cn-mcp"]
}

type PushConfig struct {
	Enabled bool     `yaml:"enabled"`
	Time    string   `yaml:"time"`    // e.g. "08:00"
	Targets []string `yaml:"targets"` // e.g. ["Telegram:123", "QQ:Group:456"]
	Prompt  string   `yaml:"prompt"`  // Prompt to generate content, e.g. "Get hot news"
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
		// 全部允许
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

// GetGirlfriendPrompt 获取女朋友的定制提示词
// key 格式: "Platform:UserID" 如 "QQ:ABC123" 或 "Telegram:12345"
func (c *Config) GetGirlfriendPrompt(storageKey string) (string, string, bool) {
	if c.Girlfriend == nil {
		return "", "", false
	}
	if gf, ok := c.Girlfriend[storageKey]; ok {
		return gf.Name, gf.Prompt, true
	}
	return "", "", false
}

// GetPlatformPrompt 获取平台专属的提示词（用于最终回复）
// platform: "telegram", "qq" 等
func (c *Config) GetPlatformPrompt(platform string) string {
	if c.PlatformPrompts == nil {
		return ""
	}
	platformLower := strings.ToLower(platform)
	if prompt, ok := c.PlatformPrompts[platformLower]; ok {
		return prompt
	}
	return ""
}
