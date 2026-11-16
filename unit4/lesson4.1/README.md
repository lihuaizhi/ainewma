# lesson4.1 — Multi-Agent 电商客服分流（Routing / Handoff）

对应 `multi-agent.md` 里的 **Routing** 与 **Swarm / Handoff** 两种模式，
落地成大厂最常见的入门场景：**客服总台做意图判断，转交给对口专员**。
（架构上等同于阿里「Router Agent + 垂类导购 Agent」导购助手的缩小版。）

## Demo 架构

```
                     runner.Run（每个用户回合从入口进）
                           │
                  客服助手（总台·意图分流）
          transfer_to_agent（框架注入，来自 WithSubAgents）
          ┌──────────────────┼──────────────────┐
   退货助手             订单助手             导购助手
   退款专员             订单物流专员         导购专员
   query_order         query_order         search_products
   query_refund_policy query_delivery      query_promotion
   submit_refund
```

工具全部是进程内 mock（订单/物流/商品/退款政策），跑起来只依赖一个 OpenAI 兼容的 LLM。
Agent 注册名（router/专员）统一维护在 `internal/config/config.go`，中英文 ID 均可，
但 `transfer_to_agent` 的 `agent_name` 参数必须与注册名逐字符一致——这是评估环节
曾踩过的真实坑：测试集还在用旧英文名时，"全部 FAIL"未必是分流失效，先核对 ID。

## 快速开始

```bash
export DEEPSEEK_API_KEY=sk-xxxx

# 单轮脚本（推荐先跑这四条，三条分流线 + 一条免转接闲聊）
go run ./cmd/service -q '你好，我想退掉我买的那副耳机，订单号 O-1001'
go run ./cmd/service -q '我的电饭煲怎么还没送到？订单 O-1002'
go run ./cmd/service -q '想买个敲代码的键盘，预算 300 内'
go run ./cmd/service -q '今天天气真好呀'

# 观察转接过程（打印 TOOLCALL / AGENT SWITCH）
go run ./cmd/service -verbose -q 'O-1003 的跑鞋颜色不喜欢，要退货'

# 多轮 REPL：口头确认后退款才能落地（复述订单 + 确认 → submit_refund）
printf 'O-1004 智能手表按键失灵，要退款\n确认，操作退款吧\n' | go run ./cmd/service -verbose

# 分流准确率评估（12 条带标注用例）
go run ./cmd/evalroute
go run ./cmd/evalroute -limit 4 -verbose
go run ./cmd/evalroute -json /tmp/route.jsonl   # 每条结果落 JSONL，便于回归对比

# 请求 dumper（lesson3.3 agentfw 的同款玩法，多 Agent 版）
go run ./cmd/service -verbose -q '我想退货，订单 O-1003' -dump-request \
  -dump-request-file /tmp/cs-dump.jsonl
```

> 换模型：`-model` / `-base-url` / `-api-key` 均可覆盖；任何 OpenAI 兼容端点（如
> 本地 vLLM/Ollama）都能跑，`-stream` 已默认开启。

## 目录结构

```
cmd/service       客服入口：单轮 -q 与多轮 REPL，事件流渲染
cmd/evalroute     分流准确率评估：跑标注用例集，输出总体/分类准确率与混淆矩阵
internal/agents   客服助手 + 退货/订单/导购 四个 llmagent 与全部 function tools
internal/store    mock 业务系统：订单、物流、商品、退款政策（含护栏校验）
internal/config   模型与参数、Agent 注册名的默认值
internal/dumper   LLM 请求 dumper（-dump-request/-dump-request-file，按 Agent 标注）
testdata/route_cases.json   路由评估集（12 条，id 命名 r*/o*/g*/n* 对应四类）
multi-agent.md    主流编排模式小结（模式 → 场景对照表）
```

## 关键实现点

### 0. 请求 dumper（internal/dumper）——看清每一回合谁在调大模型

复用 lesson3.3 `agentfw` 的 `openai.WithChatRequestJSONCallback` 思路，但做成了
共享包，并且**按 Agent 标注**：callback 的 ctx 里带着 Agent 所在的 invocation，
`agent.InvocationFromContext(ctx)` 取 `AgentName` / `InvocationID` 写进记录，
一次转接产生的「总台决策 → 专员接手」序列一目了然。

```bash
go run ./cmd/service -dump-request -dump-request-file /tmp/cs-dump.jsonl -q '...'
# stderr：每条请求的人读版摘要
# /tmp/cs-dump.jsonl：一请求一行 JSON，raw requests 完整可回放

# 典型用法
jq -r '[.seq, .agent] | @tsv' /tmp/cs-dump.jsonl            # 请求时间线（哪个 agent 在调）
jq 'select(.agent=="退货助手") | .request.messages' /tmp/cs-dump.jsonl | less
```

两个阅读提示（踩过的）：
- dump 里的 `stream` 字段是 `ChatCompletionNewParams` 的原始状态（恒为 null），
  **不代表**本次调用是否走了流式；wire 级别的 streaming 由 SDK 在 NewStreaming
  时追加，与该字段无关；
- tools 数组给了你「这个 Agent 被授权了什么」的镜像——router 的请求只有
  `transfer_to_agent`，专员的请求才有业务工具，Human 层的工具隔离靠
  `WithSubAgents`/`WithTools` 两个声明决定。

### 1. Handoff 是框架自动注入的，不用手写路由代码

```go
llmagent.New(config.RouterAgentName,
    llmagent.WithInstruction(routerInstruction(...)),
    llmagent.WithSubAgents([]agent.Agent{退货, 订单, 导购}), // ← 核心一行
    ...)
```

注册子 Agent 后，框架会给主 Agent 自动挂一个 `transfer_to_agent` 工具：

- 参数里带全部候选 Agent 的名字与 Description，天然就是「意图分流表」；
- 转交发生在 **服务器端**（ADK 风格的 handoff），子 Agent 收到转交消息后以自己
  的 system instruction 接手对话，直到本回合结束；
- **决策交给模型**：每个新回合仍从 `客服助手` 子代理入口重新进，上一轮转给
  退款专员、这一轮还要查物流时，由总台再次分流——状态跟随靠 session，而不是
  把路由写死在代码里。

### 2. 会话状态跟随 = 所有人共用一个 session

`runner.Run(ctx, user, sessionKey, msg)` 里 `sessionKey` 固定为
`cs-<pid>`。转接前后的事件、消息历史都写进同一个 in-memory session，因此
「总台说了什么 → 专员接了什么 → 用户确认了什么」对下一个 Agent 不管是否可见
（本 demo 的 transfer message 显式转述诉求），上下文都不会凭空丢失。这是
lesson4.3（state & checkpoint）要展开的主题，这里先感受最小形态。

### 3. 护栏放在工具层，而不是提示词层

`internal/store.SubmitRefund` 是 Amazon MARCO 式做法的缩小版——把「幻觉参数」
挡在业务函数入口：空原因 / 未知订单号 / 待付款 / 重复退款都会被工具返回值
（而非模型自由发挥）拦下；提示词里再叠加「发起退款前必须复述订单并获用户确认」
这类流程约束。观察点：让模型去退「O-9999」，看工具如何回错并让它向用户要正确订单号。

### 4. 事件流渲染是展示层的事

`cmd/service` 的 `turn()` 做了三件小事，值得对照源码读：
- 过滤 `event.TransferTag` 与 `model.RoleTool` 的事件——框架回显的工具回执
  （转交回执 JSON 等）不应出现在面向用户的聊天流里；
- 流式时打印 `Delta.Content`，非流式时兜底打印 `Message.Content`；
- `-verbose` 打印 `[TOOLCALL]` / `[AGENT SWITCH]`，用 `ev.Author` 的跳变可视化
  handoff。

### 5. 评估：cmd/evalroute 就是路由准确率的第一版

`cmd/evalroute` 把「人工统计 verbose 输出」自动化了：

- **用例集**：`testdata/route_cases.json`，每条带 `expect`（退货助手 / 订单助手 /
  导购助手 / none）；`n*` 类的 none 表示总台应当自答、**任何** transfer 都算错；
- **判定**：取本回合**第一个** `transfer_to_agent` 调用的 `agent_name` 与 expect
  对比；`-json` 把每条结果（含路由决策耗时 RoutingMS）写成 JSONL；
- **产出**：总体准确率、分类准正确率、按「期望×实际」的混淆矩阵。

```bash
go run ./cmd/evalroute -verbose
```

（对应沙丘/Anthropic 的评估思路：路由准确率之外，任务完成率与护栏触发率
在 lesson4.5 的 tools/workflow suite 里继续扩展。）

## 与后续课程的衔接

| 课程 | 在本场景上的延伸 |
| --- | --- |
| lesson4.2 代码评审并行流水线 | 把 fan-out/fan-in 编排从 agent 对话搬到显式工作流 |
| lesson4.3 Supervisor + Agent-as-Tool | 多专家调度、MCP 工具、双层护栏与审计 |
| lesson4.4 Agent Runtime | 转接/审批的 subprocess 生命周期、Graph checkpoint 断点恢复、session 分层 |
| lesson4.5 eval | 已实现：读 evalroute JSONL，输出准确率/P·R·F1、混淆矩阵与 baseline 门槛 |
| lesson4.6 可观测性 | 把 AUTHOR SWITCH 事件升级成 trace 树（router→specialist span） |

## 已知边界 / 常见坑

- **转接后不是"移交整个对话"**：下一回合仍从总台重新分流。想让专员连续
  接管多轮，需要 lesson4.3 的会话级软切换（或给专家登记双向子 Agent），
  这是下一课的引子。
- **路由误判**：`-verbose` 下能看到 router 偶尔自答而不转接。加重试或让
  `routerInstruction` 里的判断规则更具体可以收敛。
- **重复回答**：若关掉 `Stream` 还同时打印 Delta，会与最终 `Message` 重叠，
  见 `turn()` 里的 `sawDelta` 兜底逻辑。
