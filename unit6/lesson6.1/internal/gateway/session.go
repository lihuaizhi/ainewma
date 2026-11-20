package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/tool"
	mcpbridge "trpc.group/trpc-go/trpc-agent-go/tool/mcp"

	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/agent"
)

// Session 会话运行时：持有该会话的 MCP 连接、包装后的工具、确认令牌、
// 最近报价与当前 SSE emitter。同会话串行（mu）——打车对话天然是串行的。
type Session struct {
	ID        string
	CreatedAt time.Time

	mu      sync.Mutex
	toolset *mcpbridge.ToolSet
	tools   []tool.Tool
	byName  map[string]tool.Tool

	lastQuoteID string
	tokens      map[string]string // quote_id → confirm_token（确认动作签发）
	emit        func(agent.Event) // 当前 chat 的事件发射器（mu 保护）
	seq         int64
}

// newSession 建会话并初始化 MCP streamable 连接。
func newSession(ctx context.Context, sid, mcpURL, token string) (*Session, error) {
	ts := mcpbridge.NewMCPToolSet(mcpbridge.ConnectionConfig{
		Transport: "streamable",
		ServerURL: mcpURL,
		Headers:   map[string]string{"Authorization": "Bearer " + token},
	})
	if err := ts.Init(ctx); err != nil {
		return nil, fmt.Errorf("mcp toolset init: %w", err)
	}

	s := &Session{
		ID:        sid,
		CreatedAt: time.Now(),
		toolset:   ts,
		byName:    map[string]tool.Tool{},
		tokens:    map[string]string{},
	}
	for _, t := range ts.Tools(ctx) {
		ct, ok := t.(tool.CallableTool)
		if !ok {
			continue
		}
		w := &injectedTool{inner: ct, sess: s}
		s.tools = append(s.tools, w)
		s.byName[w.Declaration().Name] = w
	}
	if len(s.tools) == 0 {
		ts.Close()
		return nil, fmt.Errorf("mcp toolset returned no tools（ride-mcp 是否已启动？）")
	}
	return s, nil
}

// Close 释放 MCP 连接。
func (s *Session) Close() {
	if s != nil && s.toolset != nil {
		s.toolset.Close()
	}
}

// ---------- agent.ToolCaller 实现 ----------

// Call 调用工具（发射 tool_call/tool_result 事件，返回契约 body）。
func (s *Session) Call(ctx context.Context, name string, args map[string]any) map[string]any {
	t, ok := s.byName[name]
	if !ok {
		return map[string]any{"ok": false, "error": fmt.Sprintf("unknown tool %q", name)}
	}
	id := s.nextID()

	// 发射 tool_call（剔除服务端注入字段，不给前端看）
	callArgs := map[string]any{}
	for k, v := range args {
		if k == "_session_id" || k == "confirm_token" {
			continue
		}
		callArgs[k] = v
	}
	s.emitEvent(agent.Event{Type: agent.EvToolCall, ID: id, Name: name, Args: callArgs})

	start := time.Now()
	raw, _ := json.Marshal(args)
	res, err := t.(tool.CallableTool).Call(ctx, raw)
	_ = time.Since(start) // 耗时指标位（可挂 metrics）

	body := decodeToolResult(res, err)
	okFlag, _ := body["ok"].(bool)
	s.emitEvent(agent.Event{
		Type:    agent.EvToolResult,
		ID:      id,
		OK:      &okFlag,
		Summary: summarize(name, body),
		Data:    resultData(body),
	})

	// 记录最近报价（离线下单脚本与自动确认用）
	if name == "estimate_price" && okFlag {
		if r, ok := body["result"].(map[string]any); ok {
			if q, ok := r["quote"].(map[string]any); ok {
				if qid, ok := q["quote_id"].(string); ok {
					s.lastQuoteID = qid
				}
			}
		}
	}
	return body
}

// LastQuoteID 最近一次估价。
func (s *Session) LastQuoteID() string { return s.lastQuoteID }

// TokenFor 确认令牌（wrapper 注入 create_order）。
func (s *Session) TokenFor(quoteID string) string { return s.tokens[quoteID] }

func (s *Session) nextID() string {
	atomic.AddInt64(&s.seq, 1)
	return fmt.Sprintf("c%d", s.seq)
}

func (s *Session) emitEvent(ev agent.Event) {
	if s.emit != nil {
		s.emit(ev)
	}
}

// setEmitter 绑定/解绑当前 chat 的 SSE 发射器（须持 s.mu 调用）。
func (s *Session) setEmitter(fn func(agent.Event)) { s.emit = fn }

// ---------- 工具包装：会话注入 + 事件 + 审计 ----------

// injectedTool 包一层 MCP 桥工具：
//   - 注入 _session_id / confirm_token（模型与 schema 均不可见——三道闸之二）
//   - 服务端兜底：session 未确认时 token 为空 → ride-ops 返回 GUARD:NEED_CONFIRM
type injectedTool struct {
	inner tool.CallableTool
	sess  *Session
}

func (it *injectedTool) Declaration() *tool.Declaration { return it.inner.Declaration() }

func (it *injectedTool) Call(ctx context.Context, jsonArgs []byte) (any, error) {
	var m map[string]any
	_ = json.Unmarshal(jsonArgs, &m)
	if m == nil {
		m = map[string]any{}
	}
	m["_session_id"] = it.sess.ID

	name := it.inner.Declaration().Name
	if name == "create_order" {
		qid, _ := m["quote_id"].(string)
		m["confirm_token"] = it.sess.tokens[qid] // 未确认 → 空 → GUARD:NEED_CONFIRM
	}

	raw, _ := json.Marshal(m)
	return it.inner.Call(ctx, raw)
}

// ---------- 结果解析与摘要 ----------

// decodeToolResult MCP 桥返回 [{type:text,text:JSON}] → body map。
// 协议层错误也包成 {ok:false,error}，保持单一契约。
func decodeToolResult(res any, err error) map[string]any {
	if err != nil {
		return map[string]any{"ok": false, "error": err.Error()}
	}
	b, _ := json.Marshal(res)
	var contents []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	}
	if err := json.Unmarshal(b, &contents); err != nil {
		return map[string]any{"ok": false, "error": "decode tool result: " + err.Error()}
	}
	var text strings.Builder
	for _, c := range contents {
		if c.Type == "text" || c.Type == "" {
			text.WriteString(c.Text)
		}
	}
	body := map[string]any{}
	if err := json.Unmarshal([]byte(text.String()), &body); err != nil {
		// 非 JSON 文本：包成 result
		return map[string]any{"ok": true, "result": map[string]any{"text": text.String()}}
	}
	return body
}

// resultData 前端 tool_result.data：成功给 result，失败给 {error}。
func resultData(body map[string]any) map[string]any {
	if ok, _ := body["ok"].(bool); ok {
		if r, ok := body["result"].(map[string]any); ok {
			return r
		}
		return map[string]any{}
	}
	e, _ := body["error"].(string)
	return map[string]any{"error": e}
}

// summarize 一句话摘要（工具卡默认收起时显示）。
func summarize(name string, body map[string]any) string {
	ok, _ := body["ok"].(bool)
	if !ok {
		e, _ := body["error"].(string)
		if r := []rune(e); len(r) > 80 {
			return string(r[:80]) + "…"
		}
		return e
	}
	r, _ := body["result"].(map[string]any)
	switch name {
	case "search_poi":
		n, _ := r["found"].(float64)
		return fmt.Sprintf("找到 %d 个地点", int(n))
	case "plan_route":
		route, _ := r["route"].(map[string]any)
		opts, _ := route["options"].([]any)
		if len(opts) > 0 {
			best, _ := opts[0].(map[string]any)
			km := num(best["distance_m"]) / 1000
			return fmt.Sprintf("规划出 %d 条方案，推荐 %.1f km", len(opts), km)
		}
		return "路线规划完成"
	case "estimate_price":
		q, _ := r["quote"].(map[string]any)
		return fmt.Sprintf("预估 ¥%.2f", num(q["price_cents"])/100)
	case "create_order":
		o, _ := r["order"].(map[string]any)
		return "下单成功 " + str(o["order_id"])
	case "list_my_orders":
		orders, _ := r["orders"].([]any)
		return fmt.Sprintf("共 %d 个订单", len(orders))
	case "cancel_order":
		o, _ := r["order"].(map[string]any)
		return fmt.Sprintf("已取消（取消费 ¥%.2f）", num(o["cancel_fee_cents"])/100)
	case "get_order":
		o, _ := r["order"].(map[string]any)
		return fmt.Sprintf("订单 %s · %s", str(o["order_id"]), str(o["status"]))
	case "list_vehicle_classes":
		vs, _ := r["vehicles"].([]any)
		return fmt.Sprintf("%d 种车型可选", len(vs))
	}
	return "完成"
}

func num(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

func str(v any) string {
	s, _ := v.(string)
	return s
}
