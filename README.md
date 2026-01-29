# GGBot - 多平台 AI 机器人

一个支持 Telegram 和 QQ 的高扩展性 AI 机器人，集成 MCP 工具调用能力。

## ✨ 功能特性

- **多平台支持**：同时支持 Telegram 和 QQ（群聊 @Bot、私聊）
- **AI 对话**：支持与大模型对话（兼容 OpenAI 接口，如通义千问等）
- **MCP 工具集成**：支持 MCP 协议，可调用搜索、新闻等外部工具
- **个性化配置**：用户可自定义 API Key、模型和提示词
- **女朋友模式**：为特定用户配置定制化的温柔提示词 💕
- **插件化设计**：轻松扩展新功能
- **本地持久化**：用户设置保存在本地

## 🚀 快速开始

### 1. 配置文件

```bash
cp config.yaml.example config.yaml
```

编辑 `config.yaml`：

```yaml
bot:
  token: "你的_TELEGRAM_BOT_TOKEN"
  qq_app_id: "QQ机器人AppID"
  qq_secret: "QQ机器人Secret"

ai:
  base_url: "https://dashscope.aliyuncs.com/compatible-mode/v1"
  api_key: "你的API_KEY"
  model: "qwen-plus"
  default_prompt: "你是一个得力的助手。"

# MCP 工具配置
mcpServers:
  hotnews:
    type: "sse"
    url: "https://dashscope.aliyuncs.com/api/v1/mcps/hotnews/sse"
    headers:
      Authorization: "Bearer 你的API_KEY"

# 女朋友定制配置
girlfriend:
  "QQ:她的OpenID":
    name: "宝贝"
    prompt: |
      你是一个温柔体贴的男朋友助手。
      对话时要温暖、关心、体贴。
```

### 2. 编译

```bash
go build -o ggbot
```

### 3. 运行

```bash
./ggbot
```

## 📋 指令说明

| 指令 | 说明 |
|------|------|
| `/start` | 启动机器人 |
| `/ping` | 状态检查 |
| `/info` | 查看个人信息（含 UserID/OpenID） |
| `/set_ai key=... model=... url=...` | 配置个人 AI 设置 |
| `/reset_ai` | 重置为默认配置 |
| `/news` | 获取今日新闻（MCP 工具） |
| `/s <内容>` | 搜索并总结（MCP 工具） |
| 直接聊天 | 发送任何文字，AI 自动回复 |

## 🏗️ 项目结构

```
├── adapter/          # 平台适配器
│   ├── telegram/     # Telegram 适配
│   └── qq/           # QQ 适配
├── botgo/            # QQ Bot SDK (本地)
├── config/           # 配置管理
├── core/             # 核心接口定义
├── plugins/          # 插件
│   ├── ai/           # AI 对话插件
│   └── system/       # 系统指令插件
├── storage/          # 本地存储
├── go-sdk/           # MCP SDK (本地)
├── config.yaml       # 配置文件
└── main.go           # 入口
```

## 🔧 开发指南

### 添加新插件

在 `plugins/` 目录下实现 `Plugin` 接口：

```go
type Plugin interface {
    Name() string
    Init(ctx *Context) error
}
```

### 添加新平台

在 `adapter/` 目录下实现 `core.Platform` 接口：

```go
type Platform interface {
    Name() string
    Start() error
    Stop() error
    RegisterCommand(cmd string, handler Handler)
    RegisterText(handler Handler)
}
```

## 📝 注意事项

- **QQ 群消息**：机器人只能被动回复（用户 @Bot 后），不支持主动推送
- **QQ URL 过滤**：QQ 平台会自动过滤消息中的 URL
- **代理配置**：Telegram 使用本地代理 (127.0.0.1:7890)，QQ 直连

## 📄 License

MIT