package core

import (
	"log/slog"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/storage"
)

// Platform represents a bot platform (Telegram, QQ, etc.)
type Platform interface {
	Name() string
	Start() error
	Stop() error
	
	// Registration
	RegisterCommand(cmd string, handler Handler)
	RegisterText(handler Handler)
    
    // Actions
    SendTo(recipient string, text string) error
}

// Handler is a function that handles a generic context
type Handler func(Context) error

// Context represents a message context, abstracting the platform
type Context interface {
	// Basic Info
	Sender() *User
	Text() string
	
	// Actions
	Reply(text string) error
	Send(text string) (Message, error)
	Edit(msg Message, text string) error
	
	// Platform specifics (if needed for advanced usage)
	Platform() string
}

// Message represents a sent message (for editing)
type Message interface {
	ID() string
}

type User struct {
	ID       string
	Username string
	IsBot    bool
}

// PluginContext is passed to plugins to initialize
type PluginContext struct {
	Config  *config.Config
	Storage *storage.Storage
	Logger  *slog.Logger
	// Platforms allows plugins to register handlers on all platforms
	RegisterCommand func(cmd string, h Handler)
	RegisterText    func(h Handler)
    
    // SendTo allows plugins to send messages to specific targets (e.g. "Telegram:123")
    SendTo func(recipient string, text string) error
}

type Plugin interface {
	Name() string
	Init(ctx *PluginContext) error
}