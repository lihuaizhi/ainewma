package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/adapter"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/agent"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/config"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/store"
)

func main() {
	q := flag.String("q", "", "question")
	addr := flag.String("addr", config.DefaultMilvusAddr, "milvus addr")
	coll := flag.String("collection", config.DefaultCollection, "collection")
	ollama := flag.String("ollama", config.DefaultOllamaURL, "ollama")
	embed := flag.String("embed-model", config.DefaultEmbedModel, "embed")
	maxIters := flag.Int("max-iters", config.DefaultMaxIters, "max iters")
	gradeThresh := flag.Float64("grade-threshold", config.DefaultGradeThresh, "grade thresh")
	enableRew := flag.Bool("enable-rewrite", config.EnableRewriteDefault, "rewrite")
	enableGrade := flag.Bool("enable-grade", config.EnableGradeDefault, "grade")
	provider := flag.String("provider", "deepseek", "decision provider ollama|deepseek")
	chatModel := flag.String("chat-model", "", "decision model")
	answerProv := flag.String("answer-provider", "deepseek", "answer provider ollama|deepseek")
	apiKey := flag.String("api-key", os.Getenv(config.DeepSeekAPIKeyEnv), "deepseek key")
	deepURL := flag.String("deepseek-url", config.DefaultDeepSeekURL, "deepseek url")
	rerank := flag.String("rerank", config.DefaultRerank, "rerank")
	verbose := flag.Bool("verbose", false, "verbose")
	noGen := flag.Bool("no-gen", false, "no answer")
	flag.Parse()

	ctx := context.Background()
	a, err := adapter.New(ctx, *addr, *ollama, *embed, *coll, *rerank, "")
	if err != nil {
		log.Fatal(err)
	}
	defer a.Close(ctx)

	var dec adapter.Streamer
	switch *provider {
	case "ollama":
		dec = adapter.NewOllama(*ollama, *chatModel)
	default:
		var e error
		dec, e = adapter.NewDeepSeek(*apiKey, *deepURL, *chatModel)
		if e != nil {
			log.Fatal(e)
		}
	}
	loop := agent.NewLoop(a, dec, *maxIters, *gradeThresh, *enableRew, *enableGrade, *verbose)
	// Optionally use a different provider for the final answer.
	if *answerProv != *provider {
		if *answerProv == "ollama" {
			loop.SetAnswerStreamer(adapter.NewOllama(*ollama, *chatModel))
		} else {
			if ad, e := adapter.NewDeepSeek(*apiKey, *deepURL, *chatModel); e == nil {
				loop.SetAnswerStreamer(ad)
			}
		}
	}

	if *q != "" {
		ans, hits := loop.Run(ctx, *q)
		if *noGen {
			printHits(hits)
			return
		}
		printHits(hits)
		fmt.Println(ans)
		return
	}
	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("> ")
		if !sc.Scan() {
			break
		}
		qq := strings.TrimSpace(sc.Text())
		if qq == "" {
			continue
		}
		ans, hits := loop.Run(ctx, qq)
		printHits(hits)
		fmt.Println(ans)
	}
}

func printHits(hits []store.Hit) {
	if len(hits) == 0 {
		return
	}
	fmt.Println("\n--- refs ---")
	for i, h := range hits[:min(5, len(hits))] {
		fmt.Printf("[%d] %s p%d score=%.3f\n", i+1, h.Volume, h.PageNo, h.Score)
	}
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}
