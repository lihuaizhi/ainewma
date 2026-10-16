// Command milvusd runs milvus-lite as a standalone server process.
//
// It owns the local database file and exposes a fixed gRPC address so that the
// ingest, ask and eval commands can connect to it like to a real Milvus
// instance.
//
//	go run ./cmd/milvusd
//
// Keep it running in its own terminal; press Ctrl-C to stop.
package main

import (
	"flag"
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/config"
	milvuslite "github.com/lyyyuna/milvus-lite-go/v2"
)

func main() {
	addr := flag.String("addr", config.MilvusAddr, "gRPC address to listen on")
	dbFile := flag.String("db", config.MilvusDBFile, "milvus-lite database file")
	flag.Parse()

	server, err := milvuslite.StartWithAddress(*dbFile, *addr)
	if err != nil {
		log.Fatalf("start milvus-lite: %v", err)
	}
	log.Printf("milvus-lite listening on %s (db=%s)", server.Addr(), *dbFile)
	log.Printf("press Ctrl-C to stop")

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig

	log.Printf("stopping milvus-lite ...")
	if err := server.Stop(); err != nil {
		log.Printf("stop: %v", err)
	}
}
