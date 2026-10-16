// Command ask runs the full lesson3.2 RAG pipeline: retrieve (vector or
// vector+BM25 hybrid fused with RRF) -> (optional rerank) -> prompt ->
// generate, streaming the answer from Ollama or the official DeepSeek API.
//
//	go run ./cmd/ask -q "太阳病的主证是什么？"
//	go run ./cmd/ask -q "桂枝汤的组成？" -formula "桂枝汤"
//	go run ./cmd/ask -q "小柴胡汤让你想到什么？" -rerank lexical -mode vector
//	go run ./cmd/ask -q "太阳病提纲证是什么？" -rerank llama     # local llama-server
//	go run ./cmd/ask -q "太阳病提纲证是什么？" -provider deepseek
//	go run ./cmd/ask            # interactive mode
//	go run ./cmd/ask -q "..." -no-gen   # retrieval only (debugging)
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/config"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/embed"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/generate"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/prompt"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/rerank"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/retrieve"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/store"
)

// streamer is satisfied by both the local Ollama client and the DeepSeek API
// client, so ask does not care which provider is configured.
type streamer interface {
	Stream(ctx context.Context, system, user string, onToken func(string)) error
}

func main() {
	query := flag.String("q", "", "question (omit for interactive mode)")
	addr := flag.String("addr", config.MilvusAddr, "Milvus gRPC address")
	collection := flag.String("collection", config.CollectionName, "collection name")
	ollamaURL := flag.String("ollama", config.OllamaBaseURL, "Ollama base URL")
	embedModel := flag.String("embed-model", config.EmbedModel, "embedding model")
	mode := flag.String("mode", "hybrid", "retrieval mode: vector | hybrid (vector+BM25+RRF)")
	rrfK := flag.Int("rrf-k", config.RRFK, "RRF fusion constant (hybrid mode)")
	topK := flag.Int("topk", config.RetrieveTopK, "chunks fed to the LLM")
	recall := flag.Int("recall", config.RetrieveRecall, "candidate pool size per arm before rerank")
	rerankKind := flag.String("rerank", "none", "reranker: none | lexical | bm25 | tei | llama")
	rerankURL := flag.String("rerank-url", config.RerankURL, "reranker base URL (tei or llama-server)")
	volume := flag.String("volume", "", "filter by chapter, e.g. 辨太阳病脉证并治上")
	formula := flag.String("formula", "", "filter by formula, e.g. 桂枝汤")
	rawFilter := flag.String("filter", "", "raw Milvus boolean expression (overrides -volume/-formula)")
	noGen := flag.Bool("no-gen", false, "retrieval only; print sources and exit")

	provider := flag.String("provider", "ollama", "generation provider: ollama | deepseek")
	apiKey := flag.String("api-key", os.Getenv(config.DeepSeekAPIKeyEnv), "DeepSeek API key (falls back to DEEPSEEK_API_KEY)")
	deepSeekURL := flag.String("deepseek-url", config.DeepSeekBaseURL, "DeepSeek API base URL")
	chatModel := flag.String("chat-model", "", "generation model (empty = provider default)")
	flag.Parse()

	ctx := context.Background()
	st, err := store.Open(ctx, *addr)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close(ctx)

	embedder := embed.New(*ollamaURL, *embedModel, 1)
	r := retrieve.New(st, embedder, *collection, rerank.New(*rerankKind, *rerankURL))

	filter := retrieve.BuildFilter(*volume, *formula)
	if *rawFilter != "" {
		filter = *rawFilter
	}
	gen, err := buildGenerator(*provider, *ollamaURL, *chatModel, *apiKey, *deepSeekURL)
	if err != nil {
		log.Fatal(err)
	}

	opts := retrieve.Options{
		Filter:  filter,
		Volume:  *volume,
		Formula: *formula,
		Recall:  *recall,
		TopK:    *topK,
		Hybrid:  *mode == "hybrid",
		RRFK:    *rrfK,
	}

	if *query != "" {
		ask(ctx, r, gen, *query, opts, *noGen)
		return
	}

	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println("输入问题回车提问，Ctrl-D 退出。")
	for {
		fmt.Print("> ")
		if !scanner.Scan() {
			break
		}
		q := strings.TrimSpace(scanner.Text())
		if q == "" {
			continue
		}
		ask(ctx, r, gen, q, opts, *noGen)
	}
}

// buildGenerator picks the generation client: the DeepSeek official API or the
// local Ollama chat model.
func buildGenerator(provider, ollamaURL, chatModel, apiKey, deepSeekURL string) (streamer, error) {
	model := strings.TrimSpace(chatModel)
	switch provider {
	case "deepseek":
		if model == "" {
			model = config.DeepSeekModel
		}
		return generate.NewDeepSeek(generate.DeepSeekOptions{
			APIKey:  apiKey,
			BaseURL: deepSeekURL,
			Model:   model,
		})
	default:
		if model == "" {
			model = config.ChatModel
		}
		return generate.New(ollamaURL, model), nil
	}
}

func ask(ctx context.Context, r *retrieve.Retriever, gen streamer, question string, opts retrieve.Options, noGen bool) {
	hits, err := r.Retrieve(ctx, question, opts)
	if err != nil {
		log.Printf("retrieve: %v", err)
		return
	}
	if len(hits) == 0 {
		fmt.Println("（没有命中结果）")
		return
	}

	// Print the evidence, then stream the answer.
	fmt.Printf("\n────────────────── 引用来源 ──────────────────\n")
	for rank, h := range hits {
		fmt.Printf("[%d] %s  第%d页  score=%.4f\n", rank+1, h.Volume, h.PageNo, h.Score)
		if len(h.Formulas) > 0 {
			fmt.Printf("    方剂: %s\n", strings.Join(h.Formulas, "、"))
		}
		fmt.Printf("    %s\n", truncate(h.Context(), 160))
	}
	fmt.Printf("────────────────── 回答 ──────────────────\n")

	if noGen {
		return
	}
	context := prompt.Context(hits)
	err = gen.Stream(ctx, prompt.System, prompt.User(question, context), func(token string) {
		fmt.Print(token)
		os.Stdout.Sync()
	})
	if err != nil {
		fmt.Printf("\n（生成失败: %v）\n", err)
		return
	}
	fmt.Println()
}

func truncate(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
