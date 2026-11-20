// Package gateway 是 agent-gateway（:8080）：SSE 聊天、确认按钮、订单面板
// 转发、订单事件广播与前端静态资源。它自己不持有业务数据——一切写读都
// 转发给 ride-ops（数据主权归能力方，方案 §三）。
package gateway

import (
	"bytes"
	"context"
	"crypto/rand"
	"embed"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"log"
	"net/http"
	"regexp"
	"strings"
	"sync"
	"time"

	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/agent"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/audit"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/provider"
)

//go:embed dist
var distRoot embed.FS

// Config gateway 配置。
type Config struct {
	RideBase string // ride-mcp http://127.0.0.1:8081
	MCPURL   string // ride-mcp streamable http://127.0.0.1:8081/mcp
	Token    string // Bearer
	Engine   agent.Engine
	Audit    *audit.Logger
}

// Gateway HTTP 服务。
type Gateway struct {
	cfg    Config
	audit  *audit.Logger
	engine agent.Engine

	mu      sync.Mutex
	entries map[string]*sessionEntry

	hub      *hub
	stopPoll chan struct{}
	pollOnce sync.Once
}

type sessionEntry struct {
	once sync.Once
	s    *Session
	err  error
}

// New 构造 gateway。
func New(cfg Config) *Gateway {
	return &Gateway{
		cfg:      cfg,
		audit:    cfg.Audit,
		engine:   cfg.Engine,
		entries:  map[string]*sessionEntry{},
		hub:      newHub(),
		stopPoll: make(chan struct{}),
	}
}

// Handler 全部路由。
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat", g.handleChat)
	mux.HandleFunc("POST /v1/quotes/{id}/confirm", g.handleConfirm)
	mux.HandleFunc("GET /v1/orders", g.handleListOrders)
	mux.HandleFunc("GET /v1/orders/{id}", g.handleGetOrder)
	mux.HandleFunc("POST /v1/orders/{id}/cancel", g.handleCancel)
	mux.HandleFunc("GET /v1/events", g.handleEvents)
	mux.HandleFunc("GET /v1/health", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, 200, map[string]any{"ok": true, "engine": g.engine.Name()})
	})

	// 前端静态资源（vite build 产物 go:embed，单二进制全栈）
	sub, err := fs.Sub(distRoot, "dist")
	if err == nil {
		mux.Handle("GET /", http.FileServer(http.FS(sub)))
	}
	return g.withLogging(mux)
}

// StartEventsPoller 轮询 ride-ops 事件增量并广播给前端 SSE。
func (g *Gateway) StartEventsPoller() {
	g.pollOnce.Do(func() {
		go g.pollEvents()
	})
}

// Close 停轮询、关会话。
func (g *Gateway) Close() {
	close(g.stopPoll)
	g.mu.Lock()
	for _, e := range g.entries {
		if e.s != nil {
			e.s.Close()
		}
	}
	g.entries = map[string]*sessionEntry{}
	g.mu.Unlock()
}

// ---------- 会话 ----------

func (g *Gateway) session(ctx context.Context, sid string) (*Session, error) {
	g.mu.Lock()
	e, ok := g.entries[sid]
	if !ok {
		e = &sessionEntry{}
		g.entries[sid] = e
	}
	g.mu.Unlock()

	e.once.Do(func() {
		e.s, e.err = newSession(ctx, sid, g.cfg.MCPURL, g.cfg.Token)
		if e.err != nil {
			log.Printf("[gateway] session %s mcp init failed: %v", sid, e.err)
		}
	})
	if e.err != nil {
		// 初始化失败允许下次重试
		g.mu.Lock()
		delete(g.entries, sid)
		g.mu.Unlock()
		return nil, e.err
	}
	return e.s, nil
}

// ---------- chat SSE ----------

// confirmRe 文本确认启发式（前端按钮是主通道，此为降级，方案 §二）。
var confirmRe = regexp.MustCompile(`确认|下单吧|帮我下|就这单|可以下|定了|下吧`)

func (g *Gateway) handleChat(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
		Message   string `json:"message"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Message) == "" {
		writeJSON(w, 400, map[string]any{"error": "message required"})
		return
	}
	sid := req.SessionID
	if sid == "" {
		sid = "s_" + randHex(6)
	}

	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]any{"error": "streaming unsupported"})
		return
	}
	writeSSEHeaders(w)

	emit := func(ev agent.Event) { writeSSE(w, flusher, ev) }
	finish := func(err error) {
		if err != nil {
			emit(agent.Event{Type: agent.EvError, Message: err.Error()})
		}
		emit(agent.Event{Type: agent.EvDone, SessionID: sid})
		flusher.Flush()
	}

	sess, err := g.session(r.Context(), sid)
	if err != nil {
		finish(fmt.Errorf("能力服务未就绪（ride-mcp 是否启动？）: %v", err))
		return
	}

	sess.mu.Lock()
	defer sess.mu.Unlock()

	trace := randHex(4)
	g.audit.Chat(trace, sid, req.Message)

	// 文本确认降级通道：命中确认词且有未确认报价 → 签发令牌
	g.maybeAutoConfirm(sess, req.Message)

	sess.setEmitter(emit)
	err = g.engine.Run(r.Context(), agent.RunRequest{
		SessionID: sid,
		Message:   req.Message,
		Caller:    sess,
		Tools:     sess.tools,
	}, emit)
	sess.setEmitter(nil)

	finish(err)
}

// maybeAutoConfirm 用户在聊天里说「确认」→ 走与按钮同一条签发链路。
func (g *Gateway) maybeAutoConfirm(sess *Session, msg string) {
	if !confirmRe.MatchString(msg) {
		return
	}
	if sess.lastQuoteID == "" {
		return
	}
	if _, ok := sess.tokens[sess.lastQuoteID]; ok {
		return
	}
	if err := g.confirmQuote(sess, sess.lastQuoteID); err != nil {
		log.Printf("[gateway] auto confirm %s: %v", sess.lastQuoteID, err)
	}
}

// confirmQuote 转发 ride-ops 签发一次性令牌（签发方=报价所有者）。
func (g *Gateway) confirmQuote(sess *Session, quoteID string) error {
	body := map[string]any{"session_id": sess.ID}
	status, resp, err := g.rideCall(http.MethodPost,
		"/internal/quotes/"+quoteID+"/confirm", body)
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("ride-ops %d: %s", status, brief(resp))
	}
	var out struct {
		OK           bool   `json:"ok"`
		ConfirmToken string `json:"confirm_token"`
	}
	if err := json.Unmarshal(resp, &out); err != nil || !out.OK {
		return fmt.Errorf("bad confirm response: %s", brief(resp))
	}
	sess.tokens[quoteID] = out.ConfirmToken
	return nil
}

// ---------- 确认按钮 / 订单面板（转发） ----------

func (g *Gateway) handleConfirm(w http.ResponseWriter, r *http.Request) {
	var req struct {
		SessionID string `json:"session_id"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)
	if req.SessionID == "" {
		writeJSON(w, 400, map[string]any{"error": "session_id required"})
		return
	}
	sess, err := g.session(r.Context(), req.SessionID)
	if err != nil {
		writeJSON(w, 503, map[string]any{"error": "capability service unavailable"})
		return
	}
	sess.mu.Lock()
	defer sess.mu.Unlock()
	if err := g.confirmQuote(sess, r.PathValue("id")); err != nil {
		writeJSON(w, 502, map[string]any{"error": err.Error()})
		return
	}
	writeJSON(w, 200, map[string]any{"ok": true, "quote_id": r.PathValue("id")})
}

func (g *Gateway) handleListOrders(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	if q.Get("session_id") == "" {
		writeJSON(w, 400, map[string]any{"error": "session_id required"})
		return
	}
	g.proxy(w, r.Method, "/internal/orders?"+q.Encode(), nil)
}

func (g *Gateway) handleGetOrder(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	g.proxy(w, r.Method, "/internal/orders/"+r.PathValue("id")+"?"+q.Encode(), nil)
}

func (g *Gateway) handleCancel(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	_ = json.NewDecoder(r.Body).Decode(&body)
	g.proxy(w, r.Method, "/internal/orders/"+r.PathValue("id")+"/cancel", body)
}

// proxy 转发 ride-ops 内部 REST（带 Bearer）。
func (g *Gateway) proxy(w http.ResponseWriter, method, path string, body any) {
	status, resp, err := g.rideCall(method, path, body)
	if err != nil {
		writeJSON(w, 502, map[string]any{"error": "ride-ops unreachable: " + err.Error()})
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_, _ = w.Write(resp)
}

func (g *Gateway) rideCall(method, path string, body any) (int, []byte, error) {
	var rd io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rd = bytes.NewReader(b)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, method, g.cfg.RideBase+path, rd)
	if err != nil {
		return 0, nil, err
	}
	req.Header.Set("Authorization", "Bearer "+g.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return 0, nil, err
	}
	defer resp.Body.Close()
	out, _ := io.ReadAll(resp.Body)
	return resp.StatusCode, out, nil
}

// ---------- 订单事件 SSE + 轮询 ----------

// hub 按会话广播订单事件。
type hub struct {
	mu      sync.Mutex
	clients map[string]map[chan provider.OrderEvent]struct{}
}

func newHub() *hub { return &hub{clients: map[string]map[chan provider.OrderEvent]struct{}{}} }

func (h *hub) sub(sid string) chan provider.OrderEvent {
	ch := make(chan provider.OrderEvent, 16)
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.clients[sid] == nil {
		h.clients[sid] = map[chan provider.OrderEvent]struct{}{}
	}
	h.clients[sid][ch] = struct{}{}
	return ch
}

func (h *hub) unsub(sid string, ch chan provider.OrderEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if m := h.clients[sid]; m != nil {
		delete(m, ch)
		close(ch)
	}
}

func (h *hub) broadcast(sid string, ev provider.OrderEvent) {
	h.mu.Lock()
	defer h.mu.Unlock()
	for ch := range h.clients[sid] {
		select {
		case ch <- ev:
		default: // 慢客户端丢事件（面板可 REST 兜底拉取）
		}
	}
}

// pollEvents 每 700ms 拉 ride-ops 事件增量（seq 单调、断线不丢）。
func (g *Gateway) pollEvents() {
	var since int64
	t := time.NewTicker(700 * time.Millisecond)
	defer t.Stop()
	for {
		select {
		case <-g.stopPoll:
			return
		case <-t.C:
		}
		_, resp, err := g.rideCall(http.MethodGet,
			fmt.Sprintf("/internal/events?since=%d", since), nil)
		if err != nil {
			continue
		}
		var out struct {
			Events []provider.OrderEvent `json:"events"`
		}
		if json.Unmarshal(resp, &out) != nil {
			continue
		}
		for _, ev := range out.Events {
			if ev.Order != nil {
				g.hub.broadcast(ev.Order.SessionID, ev)
			}
			if ev.Seq > since {
				since = ev.Seq
			}
		}
	}
}

func (g *Gateway) handleEvents(w http.ResponseWriter, r *http.Request) {
	sid := r.URL.Query().Get("session_id")
	if sid == "" {
		writeJSON(w, 400, map[string]any{"error": "session_id required"})
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		writeJSON(w, 500, map[string]any{"error": "streaming unsupported"})
		return
	}
	writeSSEHeaders(w)

	ch := g.hub.sub(sid)
	defer g.hub.unsub(sid, ch)

	keepalive := time.NewTicker(25 * time.Second)
	defer keepalive.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case ev, ok := <-ch:
			if !ok {
				return
			}
			b, _ := json.Marshal(ev)
			fmt.Fprintf(w, "event: order\ndata: %s\n\n", b)
			flusher.Flush()
		case <-keepalive.C:
			fmt.Fprint(w, ": ping\n\n")
			flusher.Flush()
		}
	}
}

// ---------- helpers ----------

func writeSSEHeaders(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "text/event-stream; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")
	w.Header().Set("X-Accel-Buffering", "no")
	w.WriteHeader(200)
}

func writeSSE(w http.ResponseWriter, f http.Flusher, ev agent.Event) {
	b, _ := json.Marshal(ev)
	fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev.Type, b)
	f.Flush()
}

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func brief(b []byte) string {
	s := strings.TrimSpace(string(b))
	if r := []rune(s); len(r) > 160 {
		return string(r[:160]) + "…"
	}
	return s
}

func (g *Gateway) withLogging(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		next.ServeHTTP(w, r)
		if strings.HasPrefix(r.URL.Path, "/v1") {
			log.Printf("[gateway] %s %s %s", r.Method, r.URL.Path,
				time.Since(start).Round(time.Millisecond))
		}
	})
}
