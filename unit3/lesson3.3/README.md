# lesson3.3 — Agentic RAG（《伤寒论》）

在 lesson3.2（混合检索 + 重排 + 生成）的基础上，把检索能力「工具化」，交给一个
ReAct 风格的 Agent 自主决定：**直接回答 / 改写查询 / 检索 / 自评是否足够 / 重试**。

本模块**不使用 lesson3.2 的内部包**，而是把需要的底层实现（store / embed / rerank /
retrieve / generate / prompt / config / bm25 / eval / pipeline）复制到本模块的
`internal/` 下，独立演进；但**复用同一套 Milvus 集合**（默认
`shl_chapter_512`，地址 `localhost:19531`），因此不需要重新灌库。

> LLM 说明：决策与回答默认使用 **DeepSeek 官方 API**（`-provider deepseek`，
> 需设置环境变量 `DEEPSEEK_API_KEY`，或传 `-api-key`）。本地 Ollama 仅用于
> **embedding（bge-m3）**。

## 目录结构

```
cmd/agentask   手写 ReAct 循环（结构化 JSON thought/action，单步工具调用）
cmd/agentfw    框架版对照实现（trpc-agent-go 的 llmagent + runner）
cmd/evalagent  Agentic 评测：Recall@k / MRR + Agent 行为指标
internal/adapter   把底层能力包装成 Agent 可用的入口
internal/tools     search_knowledge / rewrite_query / grade_relevance
internal/agent     手写 ReAct 状态机
internal/{store,embed,rerank,retrieve,generate,prompt,config,bm25,eval,pipeline}
               从 lesson3.2 复制而来的底层实现（复用同一 Milvus 集合）
testdata/qrels.jsonl   沿用 lesson3.2 的评测集
```

## 环境准备

```bash
# Milvus（复用 lesson3.2 的集合，若尚未运行）
go run ../lesson3.2/cmd/milvusd

# Ollama（仅 embedding）
ollama serve            # 默认 http://localhost:11434
ollama pull bge-m3

# DeepSeek API Key
export DEEPSEEK_API_KEY=sk-xxxx
```

## 快速开始

```bash
# 构建
make build

# 手写版 ReAct（-verbose 打印思考/动作）
go run ./cmd/agentask -q "太阳病提纲证是什么？" -verbose
go run ./cmd/agentask -q "桂枝汤由哪几味药组成？"

# 框架版对照（trpc-agent-go llmagent + runner）
go run ./cmd/agentfw -q "太阳病提纲证是什么？" -verbose

# 打印 agentfw 实际发给大模型的完整请求（messages / tools / 参数）
go run ./cmd/agentfw -q "太阳病提纲证是什么？" -dump-request
# 同时落盘为 JSONL，便于逐轮比对/jq 分析
go run ./cmd/agentfw -q "太阳病提纲证是什么？" -dump-request \
  -dump-request-file /tmp/agentfw-requests.jsonl

# Agentic 评测
go run ./cmd/evalagent -limit 10 -verbose
```

`agentask` 常用参数：`-max-iters`（默认 5）、`-grade-threshold`（默认 0.7）、
`-enable-rewrite`、`-enable-grade`、`-provider`（默认 deepseek）、`-chat-model`、
`-api-key`、`-no-gen`（只打印检索结果）。

## 设计要点

### 工具集（两种实现共用同一语义）

| 工具 | 作用 | 关键参数 |
| --- | --- | --- |
| `search_knowledge` | 检索伤寒论知识库 | `query`, `mode`(vector\|hybrid)、`topk`、`volume`、`formula` |
| `rewrite_query`     | 改写问题以提升召回 | `question`、`goal` |
| `grade_relevance`   | 判断证据是否足够（0~1） | `question`、命中片段 |

### 查看 agentfw 实际发送给模型的请求

`agentfw` 通过 `openai.WithChatRequestJSONCallback` 截获主 Agent 每次调用大模型
的完整请求；`rewrite_query` / `grade_relevance` 这两个辅助 LLM 调用也会以
system/user 原文的形式打印：

```bash
go run ./cmd/agentfw -q "桂枝汤的煎服法是什么？" -dump-request
```

- 主请求包含：system instruction、当前对话/工具结果消息、`tools` 定义
  （含 jsonschema）、`model`、`max_tokens` 等参数；
- 输出到 **stderr**，不影响 stdout 的流式回答（可单独重定向）；
- `-dump-request-file xxx.jsonl` 会把每一轮请求写成一行 JSON
  （`kind=main` 为完整请求，`kind=tool` 为辅助 LLM 的 system/user），
  便于用 `jq` 对比「为什么框架版的回答更准」。

### 手写 ReAct 循环（cmd/agentask）

每轮让决策 LLM 输出结构化 JSON：

```json
{"thought":"...","action":"tool_call|answer","tool":"search_knowledge","args":{...},"final_answer":"..."}
```

- 一次最多调用 1 个工具；
- `rewrite_query` 会顺带用第一条改写结果发起一次检索；
- `grade_relevance` 返回 `score < grade-threshold 且 sufficient=false` 时，
  自动触发「改写 + 重试检索」（受 `-enable-grade` / `-enable-rewrite` 控制）；
- 命中按 `chunk_id` 去重合并；
- 达到 `max_iters` 或模型给出 `answer` 后，用累积证据生成最终回答。

`internal/agent` 另提供 `RunDetailed`，返回完整状态（历史、命中、迭代次数），
供评测使用。

## 评测（cmd/evalagent）

沿用 lesson3.2 的 `testdata/qrels.jsonl`（条文正文作为 query，前 12 字作为
相关性锚点，锚定原文而非 chunk_id）。

- **检索质量**：对 Agent 累积使用的证据集合计算 `Recall@k` 与 `MRR@k`；
- **Agent 行为**：`tool_calls/query`、`iterations/query`、`search_rate`、
  `rewrite_rate`、`grade_rate`、`direct_answer_rate`、`early_stop_rate`。

```bash
go run ./cmd/evalagent -k 1,3,5,10 -verbose
```

常用参数：`-limit`（只跑前 N 条）、`-max-iters`、`-enable-rewrite`、`-enable-grade`、
`-provider`、`-api-key`。

## 与 lesson3.2 的对照

| | lesson3.2 | lesson3.3 (agentask/agentfw) |
|---|---|---|
| 流程 | 固定：检索 → 重排 → 生成 | Agent 自主选择工具与顺序 |
| 查询 | 原始问题 | 可改写 / 可路由（vector/hybrid） |
| 质量控制 | 无 | `grade_relevance` 自评 + 重试 |
| 集合 | `shl_chapter_512` | 复用同一集合 |
