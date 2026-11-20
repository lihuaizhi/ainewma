// server：agent-gateway（:8080）。SSE 聊天编排（offline/LLM 双引擎）、
// 确认按钮、订单面板转发、订单事件广播与前端静态资源（go:embed dist）。
//
//	go run ./cmd/server                 # offline 引擎（无 key 默认可跑）
//	DEEPSEEK_API_KEY=sk-... go run ./cmd/server   # 自动切 LLM 主链路
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/agent"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/audit"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/config"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/gateway"
)

func main() {
	addr := flag.String("addr", config.GatewayAddr, "监听地址")
	rideBase := flag.String("ride", "http://"+config.RideMCPAddr, "ride-ops 地址")
	token := flag.String("token", config.Token(), "ride-ops Bearer token")
	engineName := flag.String("engine", "", "引擎：offline|llm（默认按 DEEPSEEK_API_KEY 自动选择）")
	auditPath := flag.String("audit", "data/audit-gateway.jsonl", "审计 JSONL")
	flag.Parse()

	aud, err := audit.New(*auditPath)
	if err != nil {
		log.Fatalf("audit: %v", err)
	}
	defer aud.Close()

	// 引擎选择：有 key → LLM 主链路；无 key → offline（默认可跑）
	key := os.Getenv(config.DeepSeekAPIKeyEnv)
	eng := strings.ToLower(*engineName)
	var engine agent.Engine
	switch {
	case eng == "llm" || (eng == "" && key != ""):
		if key == "" {
			log.Fatalf("-engine llm 需要 %s", config.DeepSeekAPIKeyEnv)
		}
		engine = agent.NewLLM(key, config.DeepSeekBaseURL, config.DeepSeekModel, config.DefaultMaxTokens)
		log.Printf("[gateway] engine=llm (model=%s)", config.DeepSeekModel)
	default:
		engine = agent.NewOffline()
		log.Printf("[gateway] engine=offline（未检测到 %s，脚本编排可跑全链路）", config.DeepSeekAPIKeyEnv)
	}

	g := gateway.New(gateway.Config{
		RideBase: *rideBase,
		MCPURL:   strings.TrimSuffix(*rideBase, "/") + "/mcp",
		Token:    *token,
		Engine:   engine,
		Audit:    aud,
	})
	g.StartEventsPoller()
	defer g.Close()

	httpSrv := &http.Server{Addr: *addr, Handler: g.Handler()}
	go func() {
		log.Printf("[gateway] http :%s (SSE /v1/chat, /v1/events; web /)", *addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("[gateway] shutting down…")
	_ = httpSrv.Close()
}
