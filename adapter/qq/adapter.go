package qq

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/lhpqaq/ggbot/config"
	"github.com/lhpqaq/ggbot/core"
	"github.com/tencent-connect/botgo"
	"github.com/tencent-connect/botgo/dto"
	"github.com/tencent-connect/botgo/dto/message"
	"github.com/tencent-connect/botgo/event"
	"github.com/tencent-connect/botgo/openapi"
	"github.com/tencent-connect/botgo/token"
	"golang.org/x/oauth2"
)

type QQAdapter struct {
	api             openapi.OpenAPI
	logger          *slog.Logger
	credentials     *token.QQBotCredentials
	tokenSource     oauth2.TokenSource
	
	commandHandlers map[string]core.Handler
	textHandler     core.Handler
}

func New(cfg config.BotConfig, logger *slog.Logger) (*QQAdapter, error) {
    creds := &token.QQBotCredentials{
        AppID:     cfg.QQAppID,
        AppSecret: cfg.QQSecret,
    }
    
    ts := token.NewQQBotTokenSource(creds)
    api := botgo.NewOpenAPI(creds.AppID, ts).WithTimeout(5 * time.Second)
    
	return &QQAdapter{
		api:             api,
		logger:          logger,
		credentials:     creds,
		tokenSource:     ts,
		commandHandlers: make(map[string]core.Handler),
	}, nil
}

func (a *QQAdapter) Name() string {
	return "QQ"
}

func (a *QQAdapter) Start() error {
	a.logger.Info("Starting QQ Bot...")
    
	// Register Handlers
    // event.RegisterHandlers returns Intent.
	intent := event.RegisterHandlers(
		a.GroupATMessageEventHandler(),
        a.DirectMessageEventHandler(),
	)
    
    // Get WS Info
    ws, err := a.api.WS(context.Background(), nil, "")
	if err != nil {
		return err
	}

	go func() {
        mgr := botgo.NewSessionManager()
        // Pass intent pointer as per SDK requirement (if it is *dto.Intent)
        // Check local definition again: Start(apInfo *dto.WebsocketAP, tokenSource oauth2.TokenSource, intents *dto.Intent)
        // event.RegisterHandlers returns dto.Intent (not pointer).
        // So we need &intent.
        
        if err := mgr.Start(ws, a.tokenSource, &intent); err != nil {
             a.logger.Error("QQ Bot stopped", "error", err)
        }
	}()
    
	return nil
}

func (a *QQAdapter) Stop() error {
	return nil
}

func (a *QQAdapter) RegisterCommand(cmd string, handler core.Handler) {
	a.commandHandlers[cmd] = handler
}

func (a *QQAdapter) RegisterText(handler core.Handler) {
	a.textHandler = handler
}

// Handlers

func (a *QQAdapter) GroupATMessageEventHandler() event.ATMessageEventHandler {
	return func(event *dto.WSPayload, data *dto.WSATMessageData) error {
        content := strings.TrimSpace(message.ETLInput(data.Content))
        
        ctx := &QQContext{
            api: a.api,
            content: content,
            isDirect: false,
            // Group Message Data
            channelID: data.ChannelID,
            author: data.Author,
            msgID: data.ID,
        }
        
        return a.dispatch(ctx, content)
	}
}

func (a *QQAdapter) DirectMessageEventHandler() event.DirectMessageEventHandler {
    return func(event *dto.WSPayload, data *dto.WSDirectMessageData) error {
        content := strings.TrimSpace(data.Content)
        
         ctx := &QQContext{
            api: a.api,
            content: content,
            isDirect: true,
            // Direct Message Data
            guildID: data.GuildID,
            channelID: data.ChannelID,
            author: data.Author,
            msgID: data.ID,
        }
        
        return a.dispatch(ctx, content)
    }
}

func (a *QQAdapter) dispatch(ctx *QQContext, content string) error {
    if strings.HasPrefix(content, "/") {
        parts := strings.Fields(content)
        cmd := parts[0]
        if handler, ok := a.commandHandlers[cmd]; ok {
            return handler(ctx)
        }
    }
    
    if a.textHandler != nil {
        return a.textHandler(ctx)
    }
    
    return nil
}

// QQContext Implementation
type QQContext struct {
    api openapi.OpenAPI
    content string
    isDirect bool
    
    guildID   string
    channelID string
    author    *dto.User
    msgID     string
}

func (c *QQContext) Sender() *core.User {
    if c.author == nil {
        return &core.User{ID: "unknown", Username: "Unknown"}
    }
    return &core.User{
        ID: c.author.ID,
        Username: c.author.Username,
        IsBot: c.author.Bot,
    }
}

func (c *QQContext) Text() string {
    return c.content
}

func (c *QQContext) Reply(text string) error {
    _, err := c.Send(text)
    return err
}

func (c *QQContext) Send(text string) (core.Message, error) {
    msgToPost := &dto.MessageToCreate{
        Content: text,
        MsgID:   c.msgID, // Reply to this message
    }
    
    var msg *dto.Message
    var err error
    
    if c.isDirect {
        dm := &dto.DirectMessage{
            GuildID: c.guildID,
            ChannelID: c.channelID,
        }
        msg, err = c.api.PostDirectMessage(context.Background(), dm, msgToPost)
    } else {
        msg, err = c.api.PostMessage(context.Background(), c.channelID, msgToPost)
    }
    
    if err != nil {
        return nil, err
    }
    
    return &QQMessage{msg: msg, api: c.api, channelID: c.channelID}, nil
}

func (c *QQContext) Edit(msg core.Message, text string) error {
    return fmt.Errorf("edit not supported on QQ")
}

func (c *QQContext) Platform() string {
    return "QQ"
}

type QQMessage struct {
    msg *dto.Message
    api openapi.OpenAPI
    channelID string
}

func (m *QQMessage) ID() string {
    return m.msg.ID
}
