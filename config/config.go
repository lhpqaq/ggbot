package config

import (
	"os"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Bot          BotConfig `yaml:"bot"`
	AI           AIConfig  `yaml:"ai"`
	AllowedUsers []int64   `yaml:"allowed_users"`
}

type BotConfig struct {
	Token         string        `yaml:"token"`
	PollerTimeout time.Duration `yaml:"poller_timeout"`
	LogLevel      string        `yaml:"log_level"` // debug, info, warn, error
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

	return &cfg, nil
}

func (c *Config) IsAllowed(userID int64) bool {
	for _, id := range c.AllowedUsers {
		if id == userID {
			return true
		}
	}
	return false
}
