// Package audit 提供 lesson6.1 的 JSONL 审计日志：chat / tool_call /
// guardrail / order_event 四类记录，统一携带 trace_id，两个进程各自写
// 各自的 data/audit.jsonl（ride-mcp 写 guardrail+order_event，gateway 写
// chat+tool_call），离线评估时按 trace_id 拼接（承接 lesson4.5/4.6）。
package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Logger 追加写 JSONL；nil 安全（所有方法可在 nil receiver 下 no-op）。
type Logger struct {
	mu   sync.Mutex
	f    *os.File
	path string
}

// Record 一条审计记录。
type Record struct {
	Time      time.Time `json:"time"`
	Type      string    `json:"type"` // chat | tool_call | guardrail | order_event
	TraceID   string    `json:"trace_id,omitempty"`
	SessionID string    `json:"session_id,omitempty"`
	Actor     string    `json:"actor,omitempty"` // gateway / ride-ops / agent
	Name      string    `json:"name,omitempty"`  // 工具名/事件名
	DurationMs int64    `json:"duration_ms,omitempty"`
	OK        *bool     `json:"ok,omitempty"`
	Detail    any       `json:"detail,omitempty"`
}

// New 打开（或创建）path 对应的 JSONL 文件；目录自动创建。
func New(path string) (*Logger, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return nil, err
	}
	return &Logger{f: f, path: path}, nil
}

// Close 关闭文件（幂等）。
func (l *Logger) Close() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f != nil {
		_ = l.f.Close()
		l.f = nil
	}
}

// Log 写一条记录（nil 安全）。
func (l *Logger) Log(r Record) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.f == nil {
		return
	}
	if r.Time.IsZero() {
		r.Time = time.Now()
	}
	b, err := json.Marshal(r)
	if err != nil {
		return
	}
	_, _ = l.f.Write(append(b, '\n'))
}

// Convenience helpers.
func (l *Logger) Chat(trace, sid, detail string) {
	l.Log(Record{Type: "chat", TraceID: trace, SessionID: sid, Actor: "gateway", Detail: detail})
}

func (l *Logger) ToolCall(trace, sid, name string, ms int64, ok bool, detail any) {
	l.Log(Record{Type: "tool_call", TraceID: trace, SessionID: sid, Actor: "gateway",
		Name: name, DurationMs: ms, OK: &ok, Detail: detail})
}

func (l *Logger) Guardrail(trace, sid, name, detail string) {
	l.Log(Record{Type: "guardrail", TraceID: trace, SessionID: sid, Actor: "ride-ops",
		Name: name, Detail: detail})
}

func (l *Logger) OrderEvent(trace, sid, name, detail string) {
	l.Log(Record{Type: "order_event", TraceID: trace, SessionID: sid, Actor: "ride-ops",
		Name: name, Detail: detail})
}
