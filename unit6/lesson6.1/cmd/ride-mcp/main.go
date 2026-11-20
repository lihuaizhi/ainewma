// ride-mcp：打车能力服务（MCP Streamable HTTP + 内部 REST）。
//
//	go run ./cmd/ride-mcp            # :8081，SQLite data/ride.db，10 倍速演示
//	go run ./cmd/ride-mcp -speed 1   # 真实比例（整单约 4 分钟走完）
package main

import (
	"flag"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/audit"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/config"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/machine"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/mcpserver"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/provider"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/store"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/tools"
)

func main() {
	addr := flag.String("addr", config.RideMCPAddr, "监听地址")
	dbPath := flag.String("db", "data/ride.db", "SQLite 路径")
	token := flag.String("token", config.Token(), "Bearer token")
	speed := flag.Float64("speed", config.DemoSpeed(), "状态机演示倍速")
	auditPath := flag.String("audit", "data/audit-ride.jsonl", "审计 JSONL")
	flag.Parse()

	aud, err := audit.New(*auditPath)
	if err != nil {
		log.Fatalf("audit: %v", err)
	}
	defer aud.Close()

	var st, err = store.Open(*dbPath)
	if err != nil {
		log.Fatalf("store: %v", err)
	}
	defer st.Close()

	queue := machine.NewEventQueue()
	engine := machine.New(st, aud, queue, *speed)

	// 免费取消窗口按演示倍速缩放（真实 2min；10x → 12s，保证演示能看到取消费）
	freeWindow := time.Duration(float64(2*time.Minute) / *speed)

	deps := &tools.Deps{
		Map:     provider.NewMapMock(),
		Ride:    provider.NewRideMock(freeWindow),
		Store:   st,
		Machine: engine,
		Audit:   aud,
	}

	srv := mcpserver.New(deps, st, queue, aud, *token)

	httpSrv := &http.Server{Addr: *addr, Handler: srv.Handler()}
	go func() {
		log.Printf("[ride-mcp] MCP streamable :%s/mcp (token=%s)", *addr, mask(*token))
		log.Printf("[ride-mcp] internal REST  :%s/internal/*", *addr)
		if err := httpSrv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("listen: %v", err)
		}
	}()

	// 优雅退出：停状态机推进
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
	<-sig
	log.Println("[ride-mcp] shutting down…")
	engine.StopAll()
	_ = httpSrv.Close()
}

func mask(t string) string {
	if len(t) <= 4 {
		return "****"
	}
	return t[:2] + "****"
}
