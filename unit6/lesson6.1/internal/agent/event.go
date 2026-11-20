// Package agent 定义 chat 编排的事件模型与两种引擎：
//   - OfflineEngine：脚本化工具编排（录制回放思路），无 LLM key 也能跑通
//     全链路（搜→规划→估价→确认→下单→查询→取消），供演示与离线评估；
//   - LLMEngine：trpc-agent-go（DeepSeek）真实 function calling 主链路。
//
// 两种引擎共用 ToolCaller（gateway 的工具包装层：会话注入 + 审计 + 事件发射）。
package agent

import (
	"context"

	"trpc.group/trpc-go/trpc-agent-go/tool"
)

// 事件类型（SSE event 字段，前端按此渲染）。
const (
	EvDelta      = "delta"       // 文本增量：{text}
	EvToolCall   = "tool_call"   // 工具发起：{id,name,args}
	EvToolResult = "tool_result" // 工具返回：{id,ok,summary,data}
	EvError      = "error"       // {message}
	EvDone       = "done"        // {session_id}
)

// Event 一条 SSE 事件。
type Event struct {
	Type      string         `json:"-"`
	Text      string         `json:"text,omitempty"`
	ID        string         `json:"id,omitempty"`
	Name      string         `json:"name,omitempty"`
	Args      map[string]any `json:"args,omitempty"`
	OK        *bool          `json:"ok,omitempty"`
	Summary   string         `json:"summary,omitempty"`
	Data      map[string]any `json:"data,omitempty"`
	Message   string         `json:"message,omitempty"`
	SessionID string         `json:"session_id,omitempty"`
}

// ToolCaller 工具调用门面（gateway 实现）：负责会话上下文注入、
// tool_call/tool_result 事件发射与 MCP 结果契约解析。
type ToolCaller interface {
	// Call 调用工具并发射 tool_call/tool_result 事件。
	// 返回 MCP 结果契约 body：成功 {ok:true, result:...}，失败 {ok:false,error:...}
	// （含协议层错误，一律包成 body，不返回第二值）。
	Call(ctx context.Context, name string, args map[string]any) map[string]any
	// LastQuoteID 会话内最近一次估价的 quote_id（离线下单脚本用）。
	LastQuoteID() string
}

// RunRequest 一轮编排的输入。
type RunRequest struct {
	SessionID string
	Message   string
	// Caller 工具调用门面（离线引擎用；与 Tools 是同一套包装工具的两个视角）。
	Caller ToolCaller
	// Tools gateway 包装后的 MCP 桥工具（LLM 引擎用）。
	Tools []tool.Tool
}

// Engine 引擎接口：把一条用户消息编排成事件流。
type Engine interface {
	Name() string
	Run(ctx context.Context, req RunRequest, emit func(Event)) error
}

// emitDelta / emitTool 等便捷函数。
func EmitDelta(emit func(Event), text string) {
	emit(Event{Type: EvDelta, Text: text})
}

func EmitToolCall(emit func(Event), id, name string, args map[string]any) {
	emit(Event{Type: EvToolCall, ID: id, Name: name, Args: args})
}

func EmitToolResult(emit func(Event), id string, ok bool, summary string, data map[string]any) {
	emit(Event{Type: EvToolResult, ID: id, OK: &ok, Summary: summary, Data: data})
}

func EmitDone(emit func(Event), sid string) {
	emit(Event{Type: EvDone, SessionID: sid})
}

func EmitError(emit func(Event), msg string) {
	emit(Event{Type: EvError, Message: msg})
}
