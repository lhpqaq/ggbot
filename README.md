# 我的 Telegram 机器人

一个基于本地 Telebot SDK 开发的高扩展性 AI 机器人。

## 功能特性

- **AI 问答**：支持与大模型对话（兼容 OpenAI 接口）。
- **个性化配置**：用户可以通过聊天指令设置自己的 API Key 和模型。
- **插件化设计**：可以轻松通过新增插件来扩展指令。
- **本地持久化**：用户信息和 AI 设置保存在本地。

## 快速开始

1. **配置文件**：
   复制示例配置：
   ```bash
   cp config.yaml.example config.yaml
   ```
   修改 `config.yaml` 中的机器人 Token 和默认 AI 设置。

2. **编译**：
   ```bash
   go build -o mybot
   ```

3. **运行**：
   ```bash
   ./mybot
   ```

## 指令说明

- `/start` - 启动机器人。
- `/ping` - 状态检查。
- `/info` - 查看个人账号信息（含 UserID）。
- `/set_ai key=... model=... url=...` - 为自己配置独立的 AI 设置。
- `/reset_ai` - 重置为全局默认配置。
- **直接聊天**：发送任何非指令文字，机器人将调用 AI 进行回复。

## 开发指南

- **插件**：在 `plugins/` 目录下实现 `Plugin` 接口即可新增功能。