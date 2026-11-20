package agent

import (
	"context"
	"fmt"
	"sync"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// LLMEngine trpc-agent-go 主链路：DeepSeek function calling。
// tool_call / tool_result 事件由 gateway 的工具包装层发射（执行时可见），
// 本引擎只负责文本 delta 与错误。
type LLMEngine struct {
	mdl    model.Model
	genCfg model.GenerationConfig

	mu      sync.Mutex
	runners map[string]runner.Runner // per-session 常驻 → 多轮历史保留
}

// NewLLM 构造真实 LLM 引擎（key 由 config.DeepSeekAPIKeyEnv 提供）。
func NewLLM(apiKey, baseURL, modelName string, maxTokens int) *LLMEngine {
	mdl := openai.New(modelName,
		openai.WithAPIKey(apiKey),
		openai.WithBaseURL(baseURL))
	return &LLMEngine{
		mdl:     mdl,
		genCfg:  model.GenerationConfig{MaxTokens: &maxTokens, Stream: true},
		runners: map[string]runner.Runner{},
	}
}

// Name 引擎标识。
func (e *LLMEngine) Name() string { return "llm" }

// runnerFor 取/建会话 runner（inmemory session service 挂 runner 上，
// 常驻才能跨轮记住上文——报价、已确认等）。
func (e *LLMEngine) runnerFor(sid string, tools []tool.Tool) runner.Runner {
	e.mu.Lock()
	defer e.mu.Unlock()
	if r, ok := e.runners[sid]; ok {
		return r
	}
	ag := llmagent.New("ride-agent",
		llmagent.WithModel(e.mdl),
		llmagent.WithDescription("打车助手"),
		llmagent.WithInstruction(instruction()),
		llmagent.WithTools(tools),
		llmagent.WithMaxToolIterations(8),
		llmagent.WithGenerationConfig(e.genCfg))
	r := runner.NewRunner("lesson6.1", ag,
		runner.WithSessionService(inmemory.NewSessionService()))
	e.runners[sid] = r
	return r
}

// Run 跑一轮：流式读 LLM chunk，Delta.Content → emit delta。
func (e *LLMEngine) Run(ctx context.Context, req RunRequest, emit func(Event)) error {
	r := e.runnerFor(req.SessionID, req.Tools)
	events, err := r.Run(ctx, "ride-agent", req.SessionID, model.NewUserMessage(req.Message))
	if err != nil {
		EmitError(emit, fmt.Sprintf("模型调用失败：%v", err))
		return nil
	}
	for ev := range events {
		if ev == nil {
			continue
		}
		if ev.Error != nil {
			EmitError(emit, fmt.Sprintf("模型错误：%v", ev.Error))
			return nil
		}
		if ev.Response == nil || len(ev.Choices) == 0 {
			continue
		}
		ch := ev.Choices[0]
		if ch.Delta.Content != "" {
			EmitDelta(emit, ch.Delta.Content)
		} else if ch.Message.Content != "" && ch.Message.ToolCalls == nil {
			EmitDelta(emit, ch.Message.Content)
		}
	}
	return nil
}

// instruction 打车助手系统提示（槽位补全、确认纪律、GUARD 转述）。
func instruction() string {
	return `你是打车助手「小行」，为用户规划行程、估价、下单与管理订单。所有数据来自工具，绝不编造价格、地址、订单号。

可用工具：
- search_poi：搜索地点（起终点必须用它的 poi_id，禁止臆造地址；多个候选时列出让用户选）
- plan_route：规划路线（2-3 条方案），拿到 route_id 才能估价
- estimate_price：估价（返回 quote_id 与明细，60 秒有效）
- list_vehicle_classes：车型与预估价
- create_order：下单（必须 quote_id；价格由服务端锁定，你无法自填）
- get_order / list_my_orders：查订单
- cancel_order：取消订单（取消费以工具返回为准）
- list_saved_places：常用地址（家/公司）

工作纪律：
1. 一轮最多规划一次行程；先 search_poi 起终点，再 plan_route，最后 estimate_price。
2. 估价后把结论讲清楚：距离/时长/预估价/溢价原因，然后**停下来等用户确认**——
   绝不在同一轮直接 create_order；只有用户明确回复「确认/下单/就这单」后才调 create_order。
3. 收到 GUARD: 开头的错误必须原样转告并给出下一步（NEED_CONFIRM→请用户确认；QUOTE_EXPIRED→重新估价；NOT_CANCELLABLE→说明不可取消）。
4. 订单状态、取消费、金额一律引用工具返回值。
5. 回复用中文、口语化、先结论后细节；价格用 ¥ 格式；不要输出 JSON。`
}
