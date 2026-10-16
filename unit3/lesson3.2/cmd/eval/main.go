// Command eval scores retrieval against a qrels file, printing Recall@k and
// MRR per collection. This is the offline knob you tune chunking and rerank
// with.
//
// Build the labels once:
//
//	command:  go run ./cmd/genqrels -out testdata/qrels.jsonl
//
// Then compare collections (start cmd/milvusd first):
//
//	go run ./cmd/eval -collections shl_fixed_512,shl_chapter_512,shl_s2b_1024_256
//	go run ./cmd/eval -collections shl_chapter_512 -rerank lexical
//	go run ./cmd/eval -collections shl_chapter_512 -rerank llama -recall 80
//	go run ./cmd/eval -collections shl_fixed_512,shl_chapter_512,shl_s2b_1024_256 -mode hybrid
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/config"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/embed"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/eval"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/rerank"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/retrieve"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/store"
)

func main() {
	qrelsPath := flag.String("qrels", config.QrelsPath, "qrels JSONL file")
	collections := flag.String("collections", config.CollectionName, "comma-separated collections to evaluate")
	addr := flag.String("addr", config.MilvusAddr, "Milvus gRPC address")
	ollamaURL := flag.String("ollama", config.OllamaBaseURL, "Ollama base URL")
	model := flag.String("model", config.EmbedModel, "embedding model")
	ksFlag := flag.String("ks", "1,3,5,10", "Recall@k cut-offs (comma separated)")
	recall := flag.Int("recall", config.RetrieveRecall, "candidate pool size per arm before rerank")
	rerankKind := flag.String("rerank", "none", "reranker: none | lexical | bm25 | tei | llama")
	rerankURL := flag.String("rerank-url", config.RerankURL, "reranker base URL (tei or llama-server)")
	mode := flag.String("mode", "vector", "retrieval mode: vector | hybrid (vector+BM25+RRF)")
	rrfK := flag.Int("rrf-k", config.RRFK, "RRF fusion constant (hybrid mode)")
	flag.Parse()

	ks, err := parseKs(*ksFlag)
	if err != nil {
		log.Fatalf("parse -ks: %v", err)
	}

	queries, err := eval.LoadQrels(*qrelsPath)
	if err != nil {
		log.Fatalf("load qrels: %v", err)
	}
	if len(queries) == 0 {
		log.Fatalf("qrels file %s contains no queries", *qrelsPath)
	}
	log.Printf("evaluating %d queries, ks=(%s), recall=%d, rerank=%s, mode=%s",
		len(queries), eval.Ks(ks), *recall, *rerankKind, *mode)

	ctx := context.Background()
	st, err := store.Open(ctx, *addr)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close(ctx)
	embedder := embed.New(*ollamaURL, *model, 1)

	printHeader(ks)
	for _, name := range strings.Split(*collections, ",") {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		ok, err := st.HasCollection(ctx, name)
		if err != nil || !ok {
			log.Printf("collection %q missing, skipping", name)
			continue
		}
		log.Printf("evaluating %s ...", name)
		r := retrieve.New(st, embedder, name, rerank.New(*rerankKind, *rerankURL))
		metric, err := eval.Evaluate(ctx, func(ctx context.Context, q string, k int) ([]store.Hit, error) {
			return r.Retrieve(ctx, q, retrieve.Options{
				Recall: *recall,
				TopK:   k,
				Hybrid: *mode == "hybrid",
				RRFK:   *rrfK,
			})
		}, queries, ks)
		if err != nil {
			log.Printf("evaluate %s: %v", name, err)
			continue
		}
		printRow(name, ks, metric)
	}
}

func printHeader(ks []int) {
	var b strings.Builder
	fmt.Fprintf(&b, "%-26s", "collection")
	for _, k := range ks {
		fmt.Fprintf(&b, " %-6s", fmt.Sprintf("R@%d", k))
	}
	fmt.Fprintf(&b, " %-8s\n", fmt.Sprintf("MRR@%d", ks[len(ks)-1]))
	fmt.Fprintf(&b, "%s\n", strings.Repeat("-", 10+len(ks)*7+9))
	fmt.Print(b.String())
}

func printRow(name string, ks []int, m eval.Metrics) {
	var b strings.Builder
	fmt.Fprintf(&b, "%-26s", name)
	for _, k := range ks {
		fmt.Fprintf(&b, " %-6.3f", m.RecallAt[k])
	}
	fmt.Fprintf(&b, " %-8.3f\n", m.MRR)
	fmt.Print(b.String())
}

func parseKs(s string) ([]int, error) {
	var ks []int
	for _, p := range strings.Split(s, ",") {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(p, "%d", &n); err != nil || n <= 0 {
			return nil, fmt.Errorf("invalid cut-off %q", p)
		}
		ks = append(ks, n)
	}
	if len(ks) == 0 {
		return nil, fmt.Errorf("no ks given")
	}
	return ks, nil
}
