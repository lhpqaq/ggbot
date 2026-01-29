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
    
	// Register Handlers for Guild, Group, and C2C
	intent := event.RegisterHandlers(
        // Guild Handlers
		a.GuildATMessageEventHandler(),
        a.DirectMessageEventHandler(),
        // Group Handler
        a.GroupATMessageEventHandler(),
        // C2C Handler
        a.C2CMessageEventHandler(),
	)
    
    // Get WS Info
    ws, err := a.api.WS(context.Background(), nil, "")
	if err != nil {
		return err
	}

	go func() {
        mgr := botgo.NewSessionManager()
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

func (a *QQAdapter) SendTo(recipient string, text string) error {
    // Expected format: "Group:ID" or "User:ID" or just "ID" (defaults to ?)
    // Let's require explicit prefix.
    parts := strings.SplitN(recipient, ":", 2)
    if len(parts) != 2 {
        return fmt.Errorf("invalid qq recipient format, expected 'Group:ID' or 'User:ID', got: %s", recipient)
    }
    
    targetType := strings.ToLower(parts[0])
    targetID := parts[1]
    
    msgToPost := &dto.MessageToCreate{
        Content: text,
        MsgType: 0,
        MsgSeq: 1, // Start seq
    }
    
    var err error
    switch targetType {
    case "group":
        _, err = a.api.PostGroupMessage(context.Background(), targetID, msgToPost)
    case "user", "c2c":
        _, err = a.api.PostC2CMessage(context.Background(), targetID, msgToPost)
    default:
        return fmt.Errorf("unknown qq target type: %s", targetType)
    }
    
    return err
}

// --- Handlers ---

// Guild @Bot
func (a *QQAdapter) GuildATMessageEventHandler() event.ATMessageEventHandler {
	return func(event *dto.WSPayload, data *dto.WSATMessageData) error {
        content := strings.TrimSpace(message.ETLInput(data.Content))
        ctx := &QQContext{
            api: a.api,
            content: content,
            ctxType: TypeGuild,
            channelID: data.ChannelID,
            author: data.Author,
            msgID: data.ID,
            msgSeq: 1, // Default seq
        }
        return a.dispatch(ctx, content)
	}
}

// Guild Direct Message
func (a *QQAdapter) DirectMessageEventHandler() event.DirectMessageEventHandler {
    return func(event *dto.WSPayload, data *dto.WSDirectMessageData) error {
        content := strings.TrimSpace(data.Content)
         ctx := &QQContext{
            api: a.api,
            content: content,
            ctxType: TypeGuildDirect,
            guildID: data.GuildID,
            channelID: data.ChannelID,
            author: data.Author,
            msgID: data.ID,
            msgSeq: 1,
        }
        return a.dispatch(ctx, content)
    }
}

// Group @Bot (Qun)
func (a *QQAdapter) GroupATMessageEventHandler() event.GroupATMessageEventHandler {
    return func(event *dto.WSPayload, data *dto.WSGroupATMessageData) error {
        content := strings.TrimSpace(message.ETLInput(data.Content))
        ctx := &QQContext{
            api: a.api,
            content: content,
            ctxType: TypeGroup,
            groupID: data.GroupID,
            author: data.Author,
            msgID: data.ID,
            msgSeq: 1, // Reset or manage internally
        }
        return a.dispatch(ctx, content)
    }
}

// C2C Message (Private)
func (a *QQAdapter) C2CMessageEventHandler() event.C2CMessageEventHandler {
    return func(event *dto.WSPayload, data *dto.WSC2CMessageData) error {
        content := strings.TrimSpace(data.Content)
        ctx := &QQContext{
            api: a.api,
            content: content,
            ctxType: TypeC2C,
            senderID: data.Author.ID, // OpenID
            author: data.Author,
            msgID: data.ID,
            msgSeq: 1,
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

// --- QQContext ---

type ContextType int

const (
    TypeGuild ContextType = iota
    TypeGuildDirect
    TypeGroup
    TypeC2C
)

type QQContext struct {
    api openapi.OpenAPI
    content string
    ctxType ContextType
    
    // Guild
    guildID   string
    channelID string
    
    // Group / C2C
    groupID  string
    senderID string // User OpenID for C2C
    
    // Common
    author    *dto.User
    msgID     string
    msgSeq    int
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
    slog.Info("QQ Sending Message", "type", c.ctxType, "id", c.msgID, "content", text, "seq", c.msgSeq+1)
    
    var msg *dto.Message
    var err error
    
    // Basic MessageToCreate
    msgToPost := &dto.MessageToCreate{
        Content: text,
        MsgID:   c.msgID,
        MsgType: 0, // Text
        MsgSeq: uint32(c.msgSeq + 1), 
    }

    switch c.ctxType {
    case TypeGuild:
         msg, err = c.api.PostMessage(context.Background(), c.channelID, msgToPost)
    case TypeGuildDirect:
         dm := &dto.DirectMessage{
            GuildID: c.guildID,
            ChannelID: c.channelID,
        }
        msg, err = c.api.PostDirectMessage(context.Background(), dm, msgToPost)
    case TypeGroup:
        // Group Reply
        msg, err = c.api.PostGroupMessage(context.Background(), c.groupID, msgToPost)
    case TypeC2C:
        // C2C Reply
        msg, err = c.api.PostC2CMessage(context.Background(), c.senderID, msgToPost)
    }
    
    if err != nil {
        slog.Error("QQ Send Failed", "error", err)
        return nil, err
    }
    
    // Increment seq for next message in this context (e.g. edit/reply)
    c.msgSeq++
    
    // Return wrapper. If msg is nil (some APIs return nil on success?), handle it.
    if msg == nil {
        msg = &dto.Message{ID: "unknown"}
    }
    
    return &QQMessage{msg: msg, api: c.api}, nil
}

func (c *QQContext) Edit(msg core.Message, text string) error {
    // QQ does not support editing messages.
    // As per requirement: "Edit sends a new message"
    _, err := c.Send(text)
    return err
}

func (c *QQContext) Platform() string {
    return "QQ"
}

type QQMessage struct {
    msg *dto.Message
    api openapi.OpenAPI
}

func (m *QQMessage) ID() string {
    return m.msg.ID
}