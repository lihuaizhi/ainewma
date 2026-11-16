// Command evalroute scores the router agent of the S1 customer service demo
// against a labeled set of queries: how often does the first
// transfer_to_agent call pick the right specialist, and how often do
// chit-chat queries stay at the front desk.
//
//	go run ./cmd/evalroute
//	go run ./cmd/evalroute -limit 5 -verbose
//	go run ./cmd/evalroute -cases testdata/route_cases.json -json /tmp/route.jsonl
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"

	"github.com/lihuaizhi/ainewma/unit4/lesson4.1/internal/agents"
	"github.com/lihuaizhi/ainewma/unit4/lesson4.1/internal/config"
	"github.com/lihuaizhi/ainewma/unit4/lesson4.1/internal/dumper"
)

type routeCase struct {
	ID     string `json:"id"`
	Expect string `json:"expect"`
	Text   string `json:"text"`
}

type caseResult struct {
	ID     string `json:"id"`
	Expect string `json:"expect"`
	Got    string `json:"got"`
	OK     bool   `json:"ok"`
	// Latency of the routing decision (first transfer call), ms.
	RoutingMS int64 `json:"routing_ms,omitempty"`
}

func main() {
	casesPath := flag.String("cases", "testdata/route_cases.json", "labeled query set")
	limit := flag.Int("limit", 0, "only run the first N cases (0 = all)")
	say := flag.String("say", "张三", "mock user name used in the runner keys")
	modelName := flag.String("model", config.DeepSeekModel, "chat model")
	apiKey := flag.String("api-key", os.Getenv(config.DeepSeekAPIKeyEnv), "api key")
	baseURL := flag.String("base-url", config.DeepSeekBaseURL, "OpenAI-compatible base url")
	verbose := flag.Bool("verbose", false, "print transfer tool calls per case")
	jsonOut := flag.String("json", "", "write per-case results as JSONL to this file")
	dumpReq := flag.Bool("dump-request", false, "dump every LLM request pretty-printed to stderr")
	dumpFile := flag.String("dump-request-file", "", "also write each LLM request as a JSON record (one per line) to this file")
	flag.Parse()

	if strings.TrimSpace(*apiKey) == "" {
		log.Fatalf("missing API key: set %s or pass -api-key", config.DeepSeekAPIKeyEnv)
	}

	raw, err := os.ReadFile(*casesPath)
	if err != nil {
		log.Fatalf("read cases: %v", err)
	}
	var cases []routeCase
	if err := json.Unmarshal(raw, &cases); err != nil {
		log.Fatalf("parse cases: %v", err)
	}
	if *limit > 0 && *limit < len(cases) {
		cases = cases[:*limit]
	}

	dumper, err := dumper.New(*dumpReq, *dumpFile)
	if err != nil {
		log.Fatalf("dumper: %v", err)
	}
	defer dumper.Close()

	cluster, err := agents.New(agents.Options{
		Model: *modelName, APIKey: *apiKey, BaseURL: *baseURL,
		RequestDumpCallback: dumper.DumpJSON,
	})
	if err != nil {
		log.Fatalf("agents: %v", err)
	}
	r := runner.NewRunner("lesson4.1", cluster.Router,
		runner.WithSessionService(inmemory.NewSessionService()))
	defer r.Close()

	var out *os.File
	if *jsonOut != "" {
		if out, err = os.Create(*jsonOut); err != nil {
			log.Fatalf("create %s: %v", *jsonOut, err)
		}
		defer out.Close()
	}

	results := make([]caseResult, 0, len(cases))
	for i, c := range cases {
		got, takeMS := runOne(context.Background(), r, *say, c.Text, *verbose)
		res := caseResult{
			ID: c.ID, Expect: c.Expect, Got: got,
			OK: got == c.Expect, RoutingMS: takeMS,
		}
		results = append(results, res)
		fmt.Printf("[%2d/%d] %-3s expect=%-12s got=%-12s %6dms %s\n",
			i+1, len(cases), c.ID, c.Expect, got, res.RoutingMS, mark(res.OK))
		if out != nil {
			line, _ := json.Marshal(res)
			out.Write(append(line, '\n'))
		}
	}
	fmt.Println()
	report(cases, results)
}

// runOne feeds a single message through the router on a fresh session and
// returns the target of the first transfer_to_agent call ("none" if the
// router answers itself) plus the time until that decision.
func runOne(ctx context.Context, r runner.Runner, user, msg string, verbose bool) (target string, takeMS int64) {
	session := "eval-" + strconv.Itoa(os.Getpid()) + "-" + strconv.FormatInt(int64(len(msg)), 10) + "-" + toKey(msg)
	events, err := r.Run(ctx, user, session, model.NewUserMessage(msg))
	if err != nil {
		return "error", 0
	}
	started := time.Now()
	for ev := range events {
		if ev == nil || ev.Response == nil || ev.Error != nil || len(ev.Choices) == 0 {
			continue
		}
		if ev.ContainsTag(event.TransferTag) || ev.Choices[0].Message.Role == model.RoleTool {
			continue
		}
		for _, tc := range ev.Choices[0].Message.ToolCalls {
			if tc.Function.Name != "transfer_to_agent" {
				continue
			}
			var args struct {
				AgentName string `json:"agent_name"`
			}
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			takeMS = time.Since(started).Milliseconds()
			if verbose {
				fmt.Printf("        [TOOLCALL] %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
			}
			if target == "" || target == "none" {
				target = args.AgentName
			}
		}
	}
	if target == "" {
		target = "none"
	}
	return target, takeMS
}

// report prints overall and per-class accuracy plus a confusion matrix, so a
// mis-route immediately shows which class bleeds into which specialist.
func report(cases []routeCase, results []caseResult) {
	byID := map[string]routeCase{}
	for _, c := range cases {
		byID[c.ID] = c
	}
	ok := 0
	classTotal := map[string]int{}
	classHit := map[string]int{}
	confuse := map[string]map[string]int{}
	for _, res := range results {
		c := byID[res.ID]
		classTotal[c.Expect]++
		if conf, exist := confuse[c.Expect]; !exist {
			confuse[c.Expect] = map[string]int{res.Got: 1}
		} else {
			conf[res.Got]++
		}
		if res.OK {
			ok++
			classHit[c.Expect]++
		}
	}
	total := len(results)
	fmt.Printf("routing accuracy: %d/%d (%.1f%%)\n", ok, total, 100*float64(ok)/float64(max(total, 1)))

	classes := make([]string, 0, len(classTotal))
	for k := range classTotal {
		classes = append(classes, k)
	}
	sort.Strings(classes)
	// "none" 可能自带在用例集中；没有时仍补一列总台自答，方便跨用例检查漏转。
	hdr := classes
	if !contains(classes, "none") {
		hdr = append([]string{"(none)"}, classes...)
	}
	for _, cl := range classes {
		fmt.Printf("  %-16s %d/%d\n", cl, classHit[cl], classTotal[cl])
	}
	fmt.Println("confusion (rows expect, columns got):")
	fmt.Printf("  %-18s", "expect\\got")
	for _, h := range hdr {
		fmt.Printf("%-14s", h)
	}
	fmt.Println()
	for _, row := range hdr {
		fmt.Printf("  %-18s", row)
		for _, col := range hdr {
			target := col
			if target == "(none)" {
				target = "none"
			}
			fmt.Printf("%-14d", mapVal(confuse, row, target))
		}
		fmt.Println()
	}
	fmt.Println("note: RoutingMS is wall-clock until the first transfer call;")
	fmt.Println("      'none' rows pass only when the router answers without any transfer.")
}

func mapVal(m map[string]map[string]int, row, col string) int {
	if conf, ok := m[row]; ok {
		return conf[col]
	}
	return 0
}

func contains(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}

func mark(ok bool) string {
	if ok {
		return "PASS"
	}
	return "FAIL"
}

// toKey keeps session ids deterministic per case text.
func toKey(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 24 {
		s = s[:24]
	}
	return strings.Map(func(r rune) rune {
		if r == ' ' || r == '\n' {
			return '_'
		}
		return r
	}, s)
}
