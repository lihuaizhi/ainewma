# lesson4.2 — 并行代码评审流水线（Parallelization + Aggregation）

四路专职评审 Agent **并行**评审同一份 Go 源码，再由聚合 Agent **去重、定级、
出报告**。这个场景的训练点与 lesson4.1 的路由/转交互补：

| | lesson4.1（户型路由） | lesson4.2（本课） |
| --- | --- | --- |
| 协调模式 | Routing / Handoff（转交控制权） | Parallelization（分头发散）+ Aggregation（集中收敛） |
| 关注点 | 意图判断、状态跟随 | 结构化输出、并发隔离、意见聚合 |
| 会话形状 | 长对话流水 | 单回合批处理 |

背景对照：美团技术团队公开过 AI 代码评审实践；Amazon MARCO 的层次式
多 Agent + 护栏同属"一次任务多角色纪律分工"的形态。

## Demo 架构

```
同一份带行号注释的代码 ──┬─▶ 正确性评审  ┐
                        ├─▶ 安全评审    │  并行 fan-out
                        ├─▶ 性能评审    │  （goroutine + 独立 runner/session）
                        └─▶ 风格评审    ┘
                               │  {"findings":[{line,title,detail,suggestion}]}
                               ▼
                        聚合评审（去重 → P0~P3 定级 → markdown 报告）
                        ↳ 也可把报告交给人工（human-in-the-loop 收口）
```

## 快速开始

```bash
export DEEPSEEK_API_KEY=sk-xxxx

# 评审样例（testdata/sample.go 刻意埋了安全/正确性/性能/风格交叉问题）
go run ./cmd/review

# 并行 vs 串行对照（教学点：为什么先 fan-out 再 fan-in）
go run ./cmd/review -sequential   # ≈11s
go run ./cmd/review               # ≈3.3s（加速比≈3.4x）

# 评自己的代码
go run ./cmd/review -file ./path/to.go -report-file /tmp/report.md

# 请求追踪（lesson4.1 dumper 同款 flag；可看到 4 路请求并发时间线）
go run ./cmd/review -dump-request -dump-request-file /tmp/review-rq.jsonl
```

跑完的输出第一段是各评审耗时与发现数汇总，之后是聚合 Agent 的 markdown 报告
（结论 / 必须修复 P0-P1 / 建议修复 P2-P3 / Top-3 行动清单），报告里每条问题
带"来自"列——同一条问题被多元评审命中时自动合并并标注全部来源。

## 目录结构

```
cmd/review            流水线入口：fan-out（并行/串行开关）+ fan-in 聚合
internal/review       4 位评审 Agent + 聚合 Agent 的构造；findings JSON 解析（含围栏剥离兜底）
internal/dumper       LLM 请求 dumper（lesson4.1 同款，按 Agent 标注）
internal/config       模型默认值、评审 Agent 注册名（中文，沿用 4.1 约定）
testdata/sample.go    带埋点问题的样例（testdata 目录被 go 工具忽略，不影响构建）
```

## 关键实现点

### 1. 为什么是四个专职评委，而不是一个全知评委

一个"什么都评"的 Agent 在同一份上下文里八种关注点互相稀释——最典型的失败
是每类都糊一层、且无法单独失败。拆成四个 Agent 后：

- 每个 Agent 的 system instruction 只剩一个纪律，越界会被指令显式禁止
  （风格评审不谈 SQL 注入）；
- 分支**可失败**：`runReviewer` 捕获失败并把占位 finding 交给聚合 Agent，
  流水线不因单路挂掉而停（这也是 Anthropic 多 Agent 论文里冗余性的朴素形态）；
- 大模型调用天然是 IO 密集，四路并发把墙钟从 ~11s 压到 ~3.3s。

### 2. fan-out 是手写的 goroutine + 独立 runner，框架也有原生对应物

```go
var wg sync.WaitGroup
for i, ag := range reviewers {        // reviewers 来自 review.New()
    go func(i int, ag agent.Agent) {   // 每路：独立 runner + 隔离 session
        defer wg.Done()
        out[i] = runReviewer(ctx, ag, code, timeout, verbose)
    }(i, ag)
}
wg.Wait()                              // fan-in：顺序恢复 + 汇总 JSON
```

隔离的动机与 `parallelagent` 相同——`createBranchInvocation` 的注释写得很直白：
"parallel execution doesn't interfere with each other"。框架等价写法（可作
课堂对照实验）：

```go
panel := parallelagent.New("review-panel",
    parallelagent.WithSubAgents(reviewers))   // 四路真并行，事件流按 Author 合流
```

手写版换来的是：每路独立计时、串行/并行一键对照、失败占位直接可用。

### 3. 结构化输出：WithOutputSchema + 本地容错解析

评审 Agent 配了 `llmagent.WithOutputSchema(FindingsSchema())`，输出被约束为
`{"findings":[{line,title,detail,suggestion}]}`。注意两个边界：

- DeepSeek 走 openai 兼容栈时 schema 落成 `response_format=json_object`
  （只锁 JSON 合法性，不锁 schema 形状），所以本地仍做一次**容错解析**：
  直解析失败 → 剥 markdown 围栏 → 抽取首个平衡的 `{...}` 再解（lesson3.3
  agentfw 的同款提炼）；
- 框架的 schema 校验失败只是 `Warnf` + 跳过状态写入，**不会**终止该 Agent，
  也不影响报告继续生成（`internal/flow/processor/output.go` 里的行为）。

### 4. fan-in：把友好结构换格式，再交给聚合 Agent

`buildAggregatorInput` 只做格式拼装、不做判断：

```json
{"target_file":"testdata/sample.go",
 "reviewers":[{"reviewer":"正确性评审","findings":[...]}]}
```

聚合 Agent 的指令里只写**规则**（去重合并的条件、P0~P3 定级标准、报告骨架），
判断全部交给模型——这与 lesson4.1 的分工一致：护栏放工具层，规则放提示词层，
但本场景没有工具，所以规则就是最后防线，聚合指令里同时禁止"新增发现"防越界。

### 5. human-in-the-loop 收口

美团实践 + Anthropic 调研里反复出现的一点：评审建议由人拍板。报告末尾的
Top-3 行动清单就是给人看的入口；`-report-file` 落盘后可作为 MR 评论流程
的输入（lesson4.6 可观测性电路里可挂 PR 机器人）。

## 观察点 / 简易评估

- **并行收益**：`-sequential` 与默认模式的 wall time 差值（期望 ≥2.5x）；
- **交叉印证率**：报告里"来自"含 ≥2 个评审的问题占比——高交叉说明聚合收敛
  有效，也天然构成"多 Agent 意见投票"的质量信号；
- **幻觉率抽查**：挑报告里的行号到源码核对（如本样例 17/21/37/39/55 行）；
- **失败容忍**：给单路评审塞一个非法 key（`-model` 无效名）看报告是否仍出。

## 与后续课程的衔接

| 课程 | 衔接点 |
| --- | --- |
| lesson4.4 Agent Runtime | 评审中间态（findings JSON）持久化，checkpoint 断点续跑 |
| lesson4.5 eval | 第二阶段：把 findings 与人工标签做 P/R/F1，并接 LLM judge |
| lesson4.6 可观测性 | dumper 的 seq/agent 标签升级为 span 树：panel → 4 branch → aggregate |

## 已知边界 / 常见坑

- 评审的行号是模型读"行号注释"报出来的，源码行 == 注释行号；`-file` 的输入若
  与注释存在错位（如 BOM/CRLF 混排）会放大行号漂移；
- 聚合 Agent 可能过度合并（把相似但不同的两行问题捏成一条）；
  把聚合指令的"表述不同但指同一行同一问题"写得更具体可收敛；
- testdata/*.go 可用任何内容（go 工具忽略该目录），但别把样例里的坏味道抄进
  生产代码——那可是四位评委一致盖章的🫠。
