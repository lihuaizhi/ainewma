# lession2.2 — 用 openai-go/v3 调用大模型

在 lession2.1（只做流式转发）的基础上，补齐大模型调用的其余能力：

| 能力 | 说明 | 端点 |
| --- | --- | --- |
| 非流式调用 | 一次性拿到完整结果 | `POST /v1/chat/completions` |
| 流式 SSE | 逐 token 推送，带事件名 | `POST /v1/chat/completions` (`stream: true`) |
| 结构化输出 | JSON Schema 强约束 | 请求体 `response_format` |
| 工具调用 | function calling 完整回合 | 请求体 `tools` / `tool_choice` |
| Responses API | 新版接口，支持 `instructions` 与多轮 `previous_response_id` | `POST /v1/responses` |

## 目录结构

```
cmd/
  server/            HTTP 服务入口
  demo/              命令行示例，逐个演示上面 5 种能力
internal/
  config/            环境变量配置，启动时一次性校验
  llm/               与厂商无关的领域类型（不依赖 SDK）
    openai/          openai-go/v3 适配层
      client.go      客户端构造、Base URL 归一化
      params.go      领域类型 -> SDK 参数
      chat.go        Chat Completions（流式 / 非流式）
      responses.go   Responses API（流式 / 非流式）
  httpapi/           HTTP 处理器
    handler.go       路由、请求校验
    sse.go           SSE 输出
```

分层原则：`internal/llm` 里的类型不引用 SDK 结构体，`httpapi` 只依赖 `llm.Client`
接口。因此换厂商或升级 SDK 时，改动被限制在 `internal/llm/openai` 之内。

## 快速开始

```bash
export LLM_API_KEY=sk-xxx
export LLM_API_URL=https://api.openai.com/v1   # 也可填完整的 /v1/chat/completions
export LLM_MODEL=gpt-4o-mini

go run ./cmd/server
```

## 命令行示例

`cmd/demo` 每个子命令演示一种能力，都支持 `-model` 与 `-base-url` 覆盖环境变量。

```bash
go run ./cmd/demo chat             # 非流式
go run ./cmd/demo stream           # 流式
go run ./cmd/demo json             # 结构化输出
go run ./cmd/demo tool             # 工具调用（自动回填一次工具结果）
go run ./cmd/demo responses        # Responses API
go run ./cmd/demo responses-stream # Responses API 流式
```

把 `LLM_API_URL` 指向本地 vLLM / Ollama / one-api 等兼容网关即可在无外网时调试，
此时 `LLM_API_KEY` 可以留空。

## HTTP 接口

### 非流式对话

```bash
curl -X POST localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"messages":[{"role":"user","content":"你好"}]}'
```

```json
{
  "id": "chatcmpl-...",
  "object": "chat.completion",
  "model": "gpt-4o-mini",
  "content": "你好！有什么可以帮你的？",
  "finish_reason": "stop",
  "usage": {"prompt_tokens": 9, "completion_tokens": 12, "total_tokens": 21}
}
```

### 流式对话

同一条路径，`stream` 置为 `true`。响应是 SSE，每帧带 `event` 名：

```
event: content.delta
data: {"type":"content.delta","delta":"你"}

event: finish
data: {"type":"finish","finish_reason":"stop","usage":{"prompt_tokens":9,"completion_tokens":3,"total_tokens":12}}

data: [DONE]
```

事件类型：`content.delta`、`tool_calls.delta`、`finish`、`usage`。

### 结构化输出

```bash
curl -X POST localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "messages": [{"role": "user", "content": "张三，32岁，住在上海，职位是后端工程师。"}],
    "response_format": {
      "type": "json_schema",
      "json_schema": {
        "name": "person_profile",
        "strict": true,
        "schema": {
          "type": "object",
          "properties": {"name": {"type": "string"}, "age": {"type": "integer"}},
          "required": ["name", "age"],
          "additionalProperties": false
        }
      }
    }
  }'
```

`content` 即符合 schema 的 JSON 文本。`response_format.type` 还支持 `text` 与
`json_object`。

### 工具调用

```bash
curl -X POST localhost:8080/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{
    "messages": [{"role": "user", "content": "北京今天天气怎么样？"}],
    "tools": [{
      "type": "function",
      "function": {
        "name": "get_weather",
        "description": "查询指定城市的天气",
        "parameters": {
          "type": "object",
          "properties": {"city": {"type": "string"}},
          "required": ["city"]
        }
      }
    }],
    "tool_choice": "auto"
  }'
```

返回的 `tool_calls` 原样带回，并追加一条 `role: "tool"` 消息（带
`tool_call_id`）即可拿到最终答案。完整两轮示例见 `cmd/demo/main.go` 的 `runTool`。

### Responses API

```bash
curl -X POST localhost:8080/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{
    "instructions": "你是一个简洁的中文助手。",
    "input": [{"role": "user", "content": "用一句话介绍 Go 的 goroutine。"}],
    "text": {"verbosity": "low"}
  }'

# 多轮：把上一轮返回的 id 作为 previous_response_id 传回
curl -X POST localhost:8080/v1/responses \
  -H 'Content-Type: application/json' \
  -d '{"previous_response_id": "resp_...", "input": [{"role": "user", "content": "再详细点"}]}'

# 流式
curl -N -X POST localhost:8080/v1/responses/stream -H 'Content-Type: application/json' -d '{"input":[{"role":"user","content":"你好"}]}'
```

响应把 `output` 拍平成 `output_text` 字段，方便直接取用。

## 配置项

| 环境变量 | 默认值 | 说明 |
| --- | --- | --- |
| `HTTP_ADDR` | `:8080` | 监听地址 |
| `LLM_API_URL` | `https://api.openai.com/v1` | 兼容网关地址，接受 base URL 或完整 endpoint |
| `LLM_API_KEY` | 空 | 上游密钥，本地网关可留空 |
| `LLM_MODEL` | `gpt-4o-mini` | 请求未指定 model 时使用 |
| `LLM_ORG_ID` / `LLM_PROJECT_ID` | 空 | 可选的上游路由字段 |
| `LLM_MAX_RETRIES` | `0` | 上游重试次数，0 表示不重试 |
| `LLM_RESPONSE_HEADER_TIMEOUT` | `60s` | 等待上游响应头的上限 |
| `LLM_MAX_REQUEST_BODY` | `1048576` | 请求体大小上限（字节） |
| `SHUTDOWN_TIMEOUT` | `15s` | 优雅退出等待时长 |

## 实现要点

**流式不会被缓冲。** `WriteTimeout` 保持为 0，SSE 响应头立即 `Flush`，
并设置 `X-Accel-Buffering: no`，否则 token 会在流结束时一次性到达。

**取消会向下传递。** 请求 context 直接传给 SDK；客户端断开时上游连接随之关闭，
流 pump 协程通过 `select` 感知 `ctx.Done()` 后退出，不会泄漏。

**Base URL 归一化。** `LLM_API_URL` 既可以写 `https://api.openai.com/v1`，
也可以写完整的 `.../v1/chat/completions`，`normalizeBaseURL` 会去掉尾部路径段，
再交给 SDK 拼接请求路径。

**工具与 response_format 走原始 JSON。** 这些字段嵌套较深且基本是透传，
以 `SetExtraFields` 注入，避免 SDK 联合体新增分支时破坏本项目代码。

**错误不外泄。** 上游报错统一转成 502 与固定文案，具体原因只写服务端日志，
避免把上游内部信息返回给调用方。

## 与 lession2.1 的差异

| | lession2.1 | lession2.2 |
| --- | --- | --- |
| 能力 | 仅流式 | 非流式 / 流式 / 结构化输出 / 工具调用 / Responses API |
| 领域层 | `llm.Message` + `Streamer` | `llm.Client`，返回归一化后的完整响应 |
| 上游 | 透传原始 chunk | 解析为 `StreamEvent`，带类型与 usage |