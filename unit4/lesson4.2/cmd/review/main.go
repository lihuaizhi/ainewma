// Command review runs the S2 pipeline of lesson4.2: four specialized code
// reviewers (正确性/安全/性能/风格) Fan out in parallel over the same Go file,
// then an aggregator dedups, ranks (P0~P3) and emits a markdown report.
//
//	go run ./cmd/review                                  # 评审 testdata/sample.go
//	go run ./cmd/review -file ./path/to.go -report-file /tmp/report.md
//	go run ./cmd/review -sequential                      # 对照串行耗时
//	go run ./cmd/review -verbose -dump-request -dump-request-file /tmp/rq.jsonl
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"

	"github.com/lihuaizhi/ainewma/unit4/lesson4.2/internal/config"
	"github.com/lihuaizhi/ainewma/unit4/lesson4.2/internal/dumper"
	"github.com/lihuaizhi/ainewma/unit4/lesson4.2/internal/review"
)

func main() {
	file := flag.String("file", "testdata/sample.go", "go source file to review")
	modelName := flag.String("model", config.DeepSeekModel, "chat model")
	apiKey := flag.String("api-key", os.Getenv(config.DeepSeekAPIKeyEnv), "api key")
	baseURL := flag.String("base-url", config.DeepSeekBaseURL, "OpenAI-compatible base url")
	maxTokens := flag.Int("max-tokens", config.DefaultMaxTokens, "max reply tokens")
	sequential := flag.Bool("sequential", false, "run reviewers one by one (for timing comparison)")
	verbose := flag.Bool("verbose", false, "stream each reviewer's output live")
	timeout := flag.Duration("timeout", 240*time.Second, "per-reviewer timeout")
	dumpReq := flag.Bool("dump-request", false, "dump every LLM request pretty-printed to stderr")
	dumpFile := flag.String("dump-request-file", "", "also write each LLM request as a JSON record (one per line) to this file")
	reportFile := flag.String("report-file", "", "also write the final markdown report to this file")
	flag.Parse()

	if strings.TrimSpace(*apiKey) == "" {
		log.Fatalf("missing API key: set %s or pass -api-key", config.DeepSeekAPIKeyEnv)
	}
	raw, err := os.ReadFile(*file)
	if err != nil {
		log.Fatalf("read: %v", err)
	}

	dump, err := dumper.New(*dumpReq, *dumpFile)
	if err != nil {
		log.Fatalf("dumper: %v", err)
	}
	defer dump.Close()

	reviewers, aggregator := review.New(review.Options{
		Model: *modelName, APIKey: *apiKey, BaseURL: *baseURL, MaxTokens: *maxTokens,
		RequestDumpCallback: dump.DumpJSON,
	})

	// ---------- 并行 fan-out ----------
	fmt.Printf(" reviewing %s with %d reviewers (fan-out=%s)\n",
		*file, len(reviewers), seqWord(*sequential))

	start := time.Now()
	panels := fanOut(context.Background(), reviewers, review.AnnotateLines(string(raw)), *sequential, *timeout, *verbose)
	elapsed := time.Since(start)

	for _, p := range panels {
		if p.Err != nil {
			fmt.Printf("[REVIEW fail] %-10s %s (%s)\n", p.Name, mark(false), p.Err)
			continue
		}
		fmt.Printf("[REVIEW] %-10s %6.1fs %2d findings\n", p.Name, p.Duration.Seconds(), len(p.Findings))
	}
	fmt.Printf("[TIMING] panel wall %.1fs (sequential=%v)\n\n", elapsed.Seconds(), *sequential)

	// ---------- fan-in 聚合 ----------
	report, err := aggregate(context.Background(), aggregator, *file, panels, *verbose)
	if err != nil {
		log.Fatalf("aggregate: %v", err)
	}
	fmt.Println(report)

	if *reportFile != "" {
		if err := os.WriteFile(*reportFile, []byte(report), 0o644); err != nil {
			log.Fatalf("write report: %v", err)
		}
		fmt.Printf("(report saved → %s)\n", *reportFile)
	}
}

// ReviewerRun carries one reviewer's outcome through the fan-out/fan-in seam.
type ReviewerRun struct {
	Name     string
	Duration time.Duration
	Findings []review.Finding
	Raw      string
	Err      error
}

// fanOut drives the four reviewers concurrently (or sequentially for the
// timing demo). Every reviewer gets its own runner + isolated session, so a
// failure in one branch cannot poison the others.
func fanOut(ctx context.Context, reviewers []agent.Agent, code string, sequential bool, timeout time.Duration, verbose bool) []ReviewerRun {
	out := make([]ReviewerRun, len(reviewers))

	if sequential {
		for i, ag := range reviewers {
			out[i] = runReviewer(ctx, ag, code, timeout, verbose)
		}
		return out
	}

	var wg sync.WaitGroup
	for i, ag := range reviewers {
		wg.Add(1)
		go func(i int, ag agent.Agent) {
			defer wg.Done()
			out[i] = runReviewer(ctx, ag, code, timeout, verbose)
		}(i, ag)
	}
	wg.Wait()
	return out
}

// runReviewer runs one reviewer to completion and collects JSON findings.
func runReviewer(ctx context.Context, ag agent.Agent, code string, timeout time.Duration, verbose bool) (res ReviewerRun) {
	res.Name = ag.Info().Name
	started := time.Now()

	cctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	r := runner.NewRunner("lesson4.2", ag, runner.WithSessionService(inmemory.NewSessionService()))
	defer r.Close()

	events, err := r.Run(cctx, "reviewer", "panel-"+fmt.Sprintf("%d", time.Now().UnixNano()), model.NewUserMessage(code))
	if err != nil {
		res.Err = err
		res.Duration = time.Since(started)
		return res
	}

	var (
		deltas  strings.Builder
		sawFull strings.Builder // fallback for non-streaming agents
		failed  error
	)
	for ev := range events {
		if ev == nil || ev.Response == nil {
			continue
		}
		if ev.Error != nil {
			failed = fmt.Errorf("%v", ev.Error)
			continue
		}
		if len(ev.Choices) == 0 {
			continue
		}
		ch := ev.Choices[0]
		if ch.Delta.Content != "" {
			deltas.WriteString(ch.Delta.Content)
			if verbose {
				fmt.Print(ch.Delta.Content)
			}
		} else if ch.Message.Content != "" {
			sawFull.WriteString(ch.Message.Content)
		}
	}
	res.Duration = time.Since(started)
	if failed != nil {
		res.Err = failed
		return res
	}

	text := strings.TrimSpace(deltas.String())
	if text == "" {
		text = strings.TrimSpace(sawFull.String())
	}
	res.Raw = text
	res.Findings, res.Err = review.ParsePayload(text)
	return res
}

// aggregate feeds the merged findings JSON into the aggregator agent and
// streams back its markdown report.
func aggregate(ctx context.Context, ag agent.Agent, file string, panels []ReviewerRun, verbose bool) (string, error) {
	payload := buildAggregatorInput(file, panels)
	req, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	r := runner.NewRunner("lesson4.2", ag, runner.WithSessionService(inmemory.NewSessionService()))
	defer r.Close()

	events, err := r.Run(ctx, "reviewer", "aggregate", model.NewUserMessage(string(req)))
	if err != nil {
		return "", err
	}
	var (
		report   strings.Builder
		fallback strings.Builder
	)
	for ev := range events {
		if ev == nil || ev.Response == nil {
			continue
		}
		if ev.Error != nil {
			log.Printf("event error: %v", ev.Error)
			continue
		}
		if len(ev.Choices) == 0 {
			continue
		}
		ch := ev.Choices[0]
		if ch.Delta.Content != "" {
			report.WriteString(ch.Delta.Content)
			if verbose {
				fmt.Print(ch.Delta.Content)
			}
		} else if ch.Message.Content != "" {
			fallback.WriteString(ch.Message.Content)
		}
	}
	out := strings.TrimSpace(report.String())
	if out == "" {
		out = strings.TrimSpace(fallback.String())
	}
	return out, nil
}

// buildAggregatorInput assembles the fan-in structure the aggregator expects.
func buildAggregatorInput(file string, panels []ReviewerRun) map[string]any {
	type fp struct {
		Review string           `json:"reviewer"`
		Found  []review.Finding `json:"findings"`
	}
	items := make([]fp, 0, len(panels))
	for _, p := range panels {
		if p.Err != nil {
			items = append(items, fp{Review: p.Name,
				Found: []review.Finding{{Title: "评审失败", Detail: p.Err.Error(), Suggestion: "结果按缺位处理"}}})
			continue
		}
		items = append(items, fp{Review: p.Name, Found: nonzero(p.Findings)})
	}
	return map[string]any{"target_file": file, "reviewers": items}
}

func nonzero(fs []review.Finding) []review.Finding {
	if fs == nil {
		return []review.Finding{}
	}
	return fs
}

func seqWord(seq bool) string {
	if seq {
		return "off"
	}
	return "on"
}

func mark(ok bool) string {
	if ok {
		return "ok"
	}
	return "FAIL"
}
