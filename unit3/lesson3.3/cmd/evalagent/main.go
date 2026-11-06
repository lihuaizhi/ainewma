// Command evalagent evaluates the agentic RAG pipeline against the lesson3.2
// qrels. It reports two families of numbers:
//
//   - retrieval quality of the evidence the agent ends up using (Recall@k, MRR);
//   - agent behaviour (how often it searched / rewrote / graded, how many
//     iterations it used, how often it answered directly or stopped early).
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/adapter"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/agent"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/config"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/eval"
)

func main() {
	qrels := flag.String("qrels", config.QrelsPath, "qrels jsonl")
	addr := flag.String("addr", config.DefaultMilvusAddr, "milvus addr")
	coll := flag.String("collection", config.DefaultCollection, "collection")
	ollama := flag.String("ollama", config.DefaultOllamaURL, "ollama url (embeddings)")
	embed := flag.String("embed-model", config.DefaultEmbedModel, "embedding model")
	rerank := flag.String("rerank", config.DefaultRerank, "rerank kind (none|tei|llama)")
	maxIters := flag.Int("max-iters", config.DefaultMaxIters, "max iters")
	gradeThresh := flag.Float64("grade-threshold", config.DefaultGradeThresh, "grade threshold")
	enableRew := flag.Bool("enable-rewrite", config.EnableRewriteDefault, "enable rewrite")
	enableGrade := flag.Bool("enable-grade", config.EnableGradeDefault, "enable grade")
	provider := flag.String("provider", "deepseek", "decision provider ollama|deepseek")
	chatModel := flag.String("chat-model", "", "decision model")
	apiKey := flag.String("api-key", os.Getenv(config.DeepSeekAPIKeyEnv), "deepseek api key")
	deepURL := flag.String("deepseek-url", config.DefaultDeepSeekURL, "deepseek base url")
	ksFlag := flag.String("k", "1,3,5,10", "cut-offs")
	limit := flag.Int("limit", 0, "only evaluate the first N queries (0 = all)")
	verbose := flag.Bool("verbose", false, "verbose per-query output")
	flag.Parse()

	queries, err := eval.LoadQrels(*qrels)
	if err != nil {
		log.Fatalf("load qrels: %v", err)
	}
	if *limit > 0 && *limit < len(queries) {
		queries = queries[:*limit]
	}

	ctx := context.Background()
	a, err := adapter.New(ctx, *addr, *ollama, *embed, *coll, *rerank, "")
	if err != nil {
		log.Fatalf("adapter: %v", err)
	}
	defer a.Close(ctx)

	var dec adapter.Streamer
	switch *provider {
	case "ollama":
		dec = adapter.NewOllama(*ollama, *chatModel)
	default:
		dec, err = adapter.NewDeepSeek(*apiKey, *deepURL, *chatModel)
		if err != nil {
			log.Fatalf("deepseek: %v", err)
		}
	}

	loop := agent.NewLoop(a, dec, *maxIters, *gradeThresh, *enableRew, *enableGrade, *verbose)
	ks := parseKs(*ksFlag)
	maxK := ks[len(ks)-1]

	// Retrieval-quality accumulators (over the union of hits the agent used).
	mrrSum := 0.0
	counts := make(map[int]float64, len(ks))

	// Agent-behaviour accumulators.
	var totalIters, totalToolCalls int
	var searched, rewrote, graded, direct, earlyStop int

	for i, q := range queries {
		st := loop.RunDetailed(ctx, q.Query)

		firstRank := 0
		for j, h := range st.Hits {
			if q.Matches(h) {
				firstRank = j + 1
				break
			}
		}
		if firstRank > 0 {
			mrrSum += 1 / float64(firstRank)
			for _, k := range ks {
				if firstRank <= k {
					counts[k]++
				}
			}
		}

		totalIters += st.Iters
		var qSearched, qRewrote, qGraded bool
		for _, t := range st.History {
			totalToolCalls++
			switch t.Tool {
			case "search_knowledge":
				qSearched = true
			case "rewrite_query":
				qRewrote = true
			case "grade_relevance":
				qGraded = true
			}
		}
		if qSearched {
			searched++
		}
		if qRewrote {
			rewrote++
		}
		if qGraded {
			graded++
		}
		if len(st.Hits) == 0 {
			direct++ // answered without any evidence
		}
		if st.Done && st.Iters < *maxIters {
			earlyStop++
		}

		if *verbose {
			fmt.Printf("[%d/%d] iters=%d hits=%d firstRel=%d rewrote=%v graded=%v q=%s\n",
				i+1, len(queries), st.Iters, len(st.Hits), firstRank, qRewrote, qGraded, q.Query)
		}
	}

	n := float64(len(queries))
	if n == 0 {
		log.Fatal("no queries")
	}

	fmt.Printf("evaluated %d queries (maxK=%d)\n\n", len(queries), maxK)

	fmt.Println("== retrieval quality (agent evidence) ==")
	for _, k := range ks {
		fmt.Printf("Recall@%-3d %.4f\n", k, counts[k]/n)
	}
	fmt.Printf("MRR@%-4d %.4f\n", maxK, mrrSum/n)

	fmt.Println("\n== agent behaviour ==")
	fmt.Printf("tool_calls/query    %.2f\n", float64(totalToolCalls)/n)
	fmt.Printf("iterations/query    %.2f\n", float64(totalIters)/n)
	fmt.Printf("search_rate         %.2f\n", float64(searched)/n)
	fmt.Printf("rewrite_rate        %.2f\n", float64(rewrote)/n)
	fmt.Printf("grade_rate          %.2f\n", float64(graded)/n)
	fmt.Printf("direct_answer_rate  %.2f\n", float64(direct)/n)
	fmt.Printf("early_stop_rate     %.2f\n", float64(earlyStop)/n)
}

// parseKs parses "1,3,5,10" into a sorted, de-duplicated slice.
func parseKs(s string) []int {
	var out []int
	seen := make(map[int]bool)
	for _, part := range strings.Split(s, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		var n int
		if _, err := fmt.Sscanf(part, "%d", &n); err != nil || n <= 0 || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	if len(out) == 0 {
		out = []int{1, 3, 5, 10}
	}
	// insertion sort keeps the file dependency-free
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j-1] > out[j]; j-- {
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
