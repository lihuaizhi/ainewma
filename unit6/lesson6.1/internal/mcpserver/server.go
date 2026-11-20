// Package mcpserver 把 tools 暴露为 ride-ops MCP 能力服务：
//   - POST/GET /mcp：Streamable HTTP（mcp-go v0.37）+ Bearer 鉴权（lesson2.3）
//   - /internal/*：给 gateway 的内部 REST（确认令牌签发、订单查询/取消、事件增量）
//
// 数据主权：SQLite 只在本进程打开；确认令牌的签发与校验都在本服务闭环。
package mcpserver

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	"github.com/mark3labs/mcp-go/server"

	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/audit"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/config"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/machine"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/provider"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/store"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/tools"
)

// Server ride-ops 能力服务。
type Server struct {
	deps  *tools.Deps
	store *store.Store
	queue *machine.EventQueue
	audit *audit.Logger
	token string

	mcpSrv *server.MCPServer
	sh     *server.StreamableHTTPServer
}

// New 构造服务（deps.Machine 与 queue 共享）。
func New(deps *tools.Deps, st *store.Store, queue *machine.EventQueue, aud *audit.Logger, token string) *Server {
	s := &Server{deps: deps, store: st, queue: queue, audit: aud, token: token}

	mcpSrv := server.NewMCPServer("ride-ops", "1.0.0",
		server.WithToolCapabilities(true),
		server.WithInstructions("打车能力服务：地点搜索、路线规划、估价、下单、订单查询与取消。"+
			"会话上下文由调用方注入（_session_id/confirm_token），护栏拒绝以 GUARD: 开头。"),
	)
	for _, t := range tools.All(deps) {
		spec, handler := t.Spec, t.H
		mcpSrv.AddTool(spec, func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			return s.wrapCall(ctx, handler, spec.Name, req)
		})
	}
	s.mcpSrv = mcpSrv
	s.sh = server.NewStreamableHTTPServer(mcpSrv,
		server.WithEndpointPath("/mcp"),
		server.WithHTTPContextFunc(func(ctx context.Context, r *http.Request) context.Context {
			// trace_id 经 MCP header 贯穿两进程（gateway 注入）
			if tr := r.Header.Get("X-Trace-Id"); tr != "" {
				ctx = context.WithValue(ctx, traceKey{}, tr)
			}
			if sid := r.Header.Get("X-Session-Id"); sid != "" {
				ctx = context.WithValue(ctx, sessionKey{}, sid)
			}
			return ctx
		}),
	)
	return s
}

type traceKey struct{}
type sessionKey struct{}

// wrapCall 统一结果契约：成功 {"ok":true,"result":...}，失败 {"ok":false,"error":"GUARD:..."}。
// 模型与 gateway 都按此契约解析（tool_result.ok / data）。
func (s *Server) wrapCall(ctx context.Context, h tools.Handler, name string, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
	args := argsOf(req)
	// header 里的会话兜底（args["_session_id"] 由 gateway wrapper 注入，优先）
	if _, ok := args["_session_id"]; !ok {
		if sid, _ := ctx.Value(sessionKey{}).(string); sid != "" {
			args["_session_id"] = sid
		}
	}
	trace, _ := ctx.Value(traceKey{}).(string)
	start := time.Now()

	res, err := h(ctx, args)
	ms := time.Since(start).Milliseconds()

	if err != nil {
		s.audit.ToolCall(trace, str(args["_session_id"]), name, ms, false, err.Error())
		body := map[string]any{"ok": false, "error": err.Error()}
		return newToolText(body), nil
	}
	s.audit.ToolCall(trace, str(args["_session_id"]), name, ms, true, brief(res))
	body := map[string]any{"ok": true, "result": res}
	return newToolText(body), nil
}

// Handler 返回完整 HTTP 路由（/mcp 鉴权 + /internal/*）。
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	// MCP 端点：Bearer 鉴权
	mux.Handle("/mcp", s.auth(s.sh))

	// 内部 REST（gateway 转发用，同样鉴权）
	mux.Handle("POST /internal/quotes/{quoteID}/confirm", s.auth(http.HandlerFunc(s.handleConfirm)))
	mux.Handle("GET /internal/orders", s.auth(http.HandlerFunc(s.handleListOrders)))
	mux.Handle("GET /internal/orders/{id}", s.auth(http.HandlerFunc(s.handleGetOrder)))
	mux.Handle("POST /internal/orders/{id}/cancel", s.auth(http.HandlerFunc(s.handleCancel)))
	mux.Handle("GET /internal/events", s.auth(http.HandlerFunc(s.handleEvents)))
	mux.HandleFunc("GET /internal/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "service": "ride-ops"})
	})

	return logMiddleware(mux)
}

// auth Bearer token 中间件（lesson2.3 鉴权复用）。
func (s *Server) auth(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := r.Header.Get("Authorization")
		if h != "Bearer "+s.token {
			writeJSON(w, 401, map[string]any{"error": "unauthorized"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

// ---------- internal handlers ----------

// handleConfirm 确认报价并签发一次性令牌（前端确认按钮 → gateway → 此处）。
func (s *Server) handleConfirm(w http.ResponseWriter, r *http.Request) {
	quoteID := r.PathValue("quoteID")
	var body struct {
		SessionID string `json:"session_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	if body.SessionID == "" {
		writeJSON(w, 400, map[string]any{"error": "session_id required"})
		return
	}
	q, err := s.store.GetQuote(quoteID)
	if err != nil || q.SessionID != body.SessionID || q.ConsumedAt != nil {
		s.audit.Guardrail("", body.SessionID, "confirm_rejected",
			fmt.Sprintf("quote=%s err=%v", quoteID, err))
		writeJSON(w, 404, map[string]any{"error": "quote not found or not owned"})
		return
	}
	if time.Now().After(q.ExpiresAt) {
		writeJSON(w, 410, map[string]any{"error": "quote expired, please re-estimate"})
		return
	}
	token := randToken()
	if err := s.store.ConfirmQuote(quoteID, body.SessionID, token,
		time.Now().Add(config.ConfirmTokenTTL)); err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	s.audit.OrderEvent("", body.SessionID, "quote_confirmed", fmt.Sprintf("quote=%s", quoteID))
	// 令牌只发给报价所有者的鉴权通道（gateway），由它在 create_order 时注入
	writeJSON(w, 200, map[string]any{
		"ok": true, "quote_id": quoteID, "confirm_token": token,
		"token_ttl_s": int(config.ConfirmTokenTTL.Seconds()),
	})
}

func (s *Server) handleListOrders(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("session_id")
	if sid == "" {
		writeJSON(w, 400, map[string]any{"error": "session_id required"})
		return
	}
	orders, err := s.store.ListOrders(sid, r.URL.Query().Get("status"), 20)
	if err != nil {
		writeJSON(w, 500, map[string]any{"error": err.Error()})
		return
	}
	if orders == nil {
		orders = []*provider.Order{}
	}
	writeJSON(w, 200, map[string]any{"orders": orders})
}

func (s *Server) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("session_id")
	o, err := s.store.GetOrder(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "order not found"})
		return
	}
	if sid != "" && o.SessionID != sid { // 归属护栏（面板查询同样校验）
		s.audit.Guardrail("", sid, "foreign_order", fmt.Sprintf("order=%s", o.OrderID))
		writeJSON(w, 403, map[string]any{"error": "GUARD:FOREIGN_ORDER"})
		return
	}
	writeJSON(w, 200, map[string]any{"order": o})
}

func (s *Server) handleCancel(w http.ResponseWriter, r *http.Request) {
	var body struct {
		SessionID string `json:"session_id"`
		Reason    string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	o, err := s.store.GetOrder(r.PathValue("id"))
	if err != nil {
		writeJSON(w, 404, map[string]any{"error": "order not found"})
		return
	}
	if o.SessionID != body.SessionID {
		s.audit.Guardrail("", body.SessionID, "foreign_order", fmt.Sprintf("cancel %s", o.OrderID))
		writeJSON(w, 403, map[string]any{"error": "GUARD:FOREIGN_ORDER"})
		return
	}
	o, err = s.deps.Machine.Cancel(o.OrderID, body.Reason, s.deps.Ride)
	if err != nil {
		writeJSON(w, 409, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"order": o})
}

// handleEvents 事件增量（gateway 定时轮询，seq 单调、断线不丢）。
func (s *Server) handleEvents(w http.ResponseWriter, r *http.Request) {
	since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
	writeJSON(w, 200, map[string]any{"events": s.queue.Since(since)})
}

// ---------- helpers ----------

func argsOf(req mcp.CallToolRequest) map[string]any {
	if m, ok := req.Params.Arguments.(map[string]any); ok {
		return m
	}
	return map[string]any{}
}

func newToolText(body map[string]any) *mcp.CallToolResult {
	b, _ := json.Marshal(body)
	return mcp.NewToolResultText(string(b))
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func randToken() string {
	b := make([]byte, 16)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func str(v any) string {
	s, _ := v.(string)
	return s
}

// brief 审计摘要（单行截断）。
func brief(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return ""
	}
	s := strings.ReplaceAll(string(b), "\n", " ")
	if r := []rune(s); len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return s
}

func logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		log.Printf("[ride-mcp] %s %s %s", r.Method, r.URL.Path, time.Since(start).Round(time.Millisecond))
	})
}
