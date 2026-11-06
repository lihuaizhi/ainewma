// Command agentfw is the framework-based counterpart of cmd/agentask. It uses
// trpc-agent-go (llmagent + runner) to drive the same RAG tools, so you can
// compare the hand-written ReAct loop with an off-the-shelf agent framework.
//
// The LLM (decision + answer) is DeepSeek by default.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/adapter"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/config"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/store"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/tools"
)

const instruction = `你是中医古籍《伤寒论》的问答助手，采用 Agentic RAG 工作方式。

可用工具：
- search_knowledge：检索伤寒论知识库，获取原文/条文/方剂证据
- rewrite_query：把问题改写为更适合检索的表述（提升召回）
- grade_relevance：判断已检索到的片段是否足够回答问题（score 0~1 + sufficient）

工作要求：
- 闲聊或与本书无关的问题可直接回答，不必检索
- 需要古籍依据时，先用 search_knowledge 检索（必要时先 rewrite_query）
- 如果证据不足（grade_relevance 返回 sufficient=false），可改写问题后重查，最多重试几次
- 回答用中文，简洁准确，并标注依据的条文/方剂
- 一次只调用一个工具`

func main() {
	q := flag.String("q", "", "question")
	addr := flag.String("addr", config.DefaultMilvusAddr, "milvus addr")
	coll := flag.String("collection", config.DefaultCollection, "collection")
	ollama := flag.String("ollama", config.DefaultOllamaURL, "ollama url (embeddings)")
	embed := flag.String("embed-model", config.DefaultEmbedModel, "embedding model")
	rerank := flag.String("rerank", config.DefaultRerank, "rerank kind (none|tei|llama)")
	modelName := flag.String("model", config.DeepSeekModel, "deepseek model")
	apiKey := flag.String("api-key", os.Getenv(config.DeepSeekAPIKeyEnv), "deepseek api key")
	baseURL := flag.String("base-url", config.DefaultDeepSeekURL, "deepseek base url")
	maxIters := flag.Int("max-iters", config.DefaultMaxIters, "max tool iterations")
	verbose := flag.Bool("verbose", false, "verbose (print tool calls)")
	dumpReq := flag.Bool("dump-request", false, "dump the full LLM request (messages/tools/params) to stderr")
	dumpFile := flag.String("dump-request-file", "", "also write each LLM request JSON (one per line) to this file")
	flag.Parse()

	ctx := context.Background()
	dumper, err := newRequestDumper(*dumpReq, *dumpFile)
	if err != nil {
		log.Fatalf("dump file: %v", err)
	}
	defer dumper.Close()

	a, err := adapter.New(ctx, *addr, *ollama, *embed, *coll, *rerank, "")
	if err != nil {
		log.Fatalf("adapter: %v", err)
	}
	defer a.Close(ctx)

	// One DeepSeek streamer shared by the rewrite/grade tools.
	dec, err := adapter.NewDeepSeek(*apiKey, *baseURL, "")
	if err != nil {
		log.Fatalf("deepseek: %v", err)
	}
	// Surface the prompts sent by rewrite/grade too (the main model is dumped
	// through the openai request callback below).
	dec = &promptDumper{d: dumper, label: "rewrite_query/grade_relevance", inner: dec}

	toolList := []tool.Tool{
		newSearchTool(a),
		newRewriteTool(dec),
		newGradeTool(dec),
	}

	mdl := openai.New(*modelName,
		openai.WithAPIKey(*apiKey),
		openai.WithBaseURL(*baseURL),
		openai.WithChatRequestJSONCallback(func(_ context.Context, raw []byte, marshalErr error) {
			dumper.DumpJSON(raw, marshalErr)
		}),
	)
	maxTokens := 1500
	ag := llmagent.New("shl-rag-agent",
		llmagent.WithModel(mdl),
		llmagent.WithDescription("中医古籍《伤寒论》Agentic RAG 助手"),
		llmagent.WithInstruction(instruction),
		llmagent.WithTools(toolList),
		llmagent.WithMaxToolIterations(*maxIters),
		llmagent.WithGenerationConfig(model.GenerationConfig{
			MaxTokens: &maxTokens,
			Stream:    true,
		}),
	)

	r := runner.NewRunner("lesson3.3", ag, runner.WithSessionService(inmemory.NewSessionService()))
	defer r.Close()

	question := strings.TrimSpace(*q)
	if question == "" {
		log.Fatal("please pass -q")
	}

	events, err := r.Run(ctx, "user", fmt.Sprintf("session-%d", os.Getpid()), model.NewUserMessage(question))
	if err != nil {
		log.Fatalf("run: %v", err)
	}

	for ev := range events {
		if ev == nil || ev.Response == nil {
			continue
		}
		if ev.Error != nil {
			log.Printf("event error: %v", ev.Error)
		}
		if len(ev.Choices) == 0 {
			continue
		}
		ch := ev.Choices[0]
		if *verbose {
			for _, tc := range ch.Message.ToolCalls {
				fmt.Printf("\n[TOOLCALL] %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
			}
		}
		if ch.Delta.Content != "" {
			fmt.Print(ch.Delta.Content)
		}
	}
	fmt.Println()
}

type searchIn struct {
	Query   string `json:"query" jsonschema:"description=检索查询 用中文"`
	Mode    string `json:"mode,omitempty" jsonschema:"description=检索模式 vector 或 hybrid 默认 hybrid"`
	TopK    int    `json:"topk,omitempty" jsonschema:"description=返回条数 默认5"`
	Volume  string `json:"volume,omitempty" jsonschema:"description=按篇名过滤 例如 辨太阳病脉证并治"`
	Formula string `json:"formula,omitempty" jsonschema:"description=按方剂名过滤 例如 桂枝汤"`
}

func newSearchTool(a *adapter.Adapter) tool.Tool {
	fn := func(ctx context.Context, in searchIn) (map[string]any, error) {
		fmt.Println(fmt.Sprintf("search origin: %s , %s, %s", in.Query, in.Volume, in.Formula))
		res, err := tools.SearchKnowledge(a, tools.SearchArgs{
			Query: in.Query, Mode: in.Mode, TopK: in.TopK,
			Volume: in.Volume, Formula: in.Formula,
		})
		if err != nil {
			return nil, err
		}
		hits := make([]map[string]any, 0, len(res.Hits))
		for _, h := range res.Hits {
			text := h.Context()
			if r := []rune(text); len(r) > 400 {
				text = string(r[:400]) + "…"
			}
			hits = append(hits, map[string]any{
				"id": h.ID, "volume": h.Volume, "page": h.PageNo,
				"score": h.Score, "text": text,
			})
		}
		return map[string]any{"count": res.Count, "hits": hits}, nil
	}
	return function.NewFunctionTool(fn,
		function.WithName("search_knowledge"),
		function.WithDescription("检索《伤寒论》知识库，返回相关原文/条文/方剂片段"),
	)
}

type rewriteIn struct {
	Question string `json:"question" jsonschema:"description=需要改写的问题"`
	Goal     string `json:"goal,omitempty" jsonschema:"description=改写目标 例如补全缺失信息"`
}

func newRewriteTool(dec adapter.Streamer) tool.Tool {
	fn := func(ctx context.Context, in rewriteIn) (map[string]any, error) {
		res, err := tools.RewriteQuery(dec, tools.RewriteArgs{Question: in.Question, Goal: in.Goal})
		if err != nil {
			return nil, err
		}
		return map[string]any{"rewritten": res.Rewritten, "reason": res.Reason}, nil
	}
	return function.NewFunctionTool(fn,
		function.WithName("rewrite_query"),
		function.WithDescription("把问题改写为更适合《伤寒论》检索的查询，返回 1~3 条改写建议"),
	)
}

type gradeIn struct {
	Question string   `json:"question" jsonschema:"description=用户原始问题"`
	Snippets []string `json:"snippets" jsonschema:"description=待评估的检索片段文本列表"`
}

func newGradeTool(dec adapter.Streamer) tool.Tool {
	fn := func(ctx context.Context, in gradeIn) (map[string]any, error) {
		hits := make([]store.Hit, 0, len(in.Snippets))
		for i, s := range in.Snippets {
			hits = append(hits, store.Hit{ID: fmt.Sprintf("snippet-%d", i+1), Text: s})
		}
		res, err := tools.GradeRelevance(dec, tools.GradeArgs{Question: in.Question, Hits: hits})
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"score":      res.Score,
			"sufficient": res.Sufficient,
			"missing":    res.Missing,
			"reason":     res.Reason,
		}, nil
	}
	return function.NewFunctionTool(fn,
		function.WithName("grade_relevance"),
		function.WithDescription("判断检索片段是否足以回答问题，返回 score/sufficient/missing/reason"),
	)
}

// requestDumper prints exactly what agentfw sends to the LLM. The main agent
// request is captured through openai.WithChatRequestJSONCallback; the auxiliary
// rewrite/grade calls are wrapped by promptDumper. Output goes to stderr so the
// streamed answer on stdout stays clean (and can still be redirected to a file).
type requestDumper struct {
	mu      sync.Mutex
	count   int
	enabled bool
	file    *os.File
}

type dumpRecord struct {
	Time    string          `json:"time"`
	Kind    string          `json:"kind"`
	Label   string          `json:"label,omitempty"`
	Request json.RawMessage `json:"request,omitempty"`
	System  string          `json:"system,omitempty"`
	User    string          `json:"user,omitempty"`
	Error   string          `json:"error,omitempty"`
}

func newRequestDumper(enabled bool, path string) (*requestDumper, error) {
	d := &requestDumper{enabled: enabled}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, err
		}
		d.file = f
	}
	return d, nil
}

func (d *requestDumper) Close() error {
	if d == nil || d.file == nil {
		return nil
	}
	return d.file.Close()
}

func (d *requestDumper) active() bool {
	return d != nil && (d.enabled || d.file != nil)
}

func (d *requestDumper) writeRecord(rec dumpRecord) {
	if d.file == nil {
		return
	}
	rec.Time = time.Now().Format(time.RFC3339Nano)
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	d.file.Write(append(line, '\n'))
}

// DumpJSON implements openai.ChatRequestJSONCallbackFunc.
func (d *requestDumper) DumpJSON(raw []byte, marshalErr error) {
	if !d.active() {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count++

	if marshalErr != nil {
		d.writeRecord(dumpRecord{Kind: "main", Error: marshalErr.Error()})
		if d.enabled {
			fmt.Fprintf(os.Stderr, "\n===== LLM REQUEST #%d: marshal error: %v =====\n", d.count, marshalErr)
		}
		return
	}
	d.writeRecord(dumpRecord{Kind: "main", Request: json.RawMessage(raw)})
	if !d.enabled {
		return
	}

	var meta struct {
		Model    string            `json:"model"`
		Stream   bool              `json:"stream"`
		Messages []json.RawMessage `json:"messages"`
		Tools    []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(raw, &meta)
	names := make([]string, 0, len(meta.Tools))
	for _, t := range meta.Tools {
		names = append(names, t.Function.Name)
	}
	if len(names) == 0 {
		names = []string{"(none)"}
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		pretty.Write(raw)
	}
	fmt.Fprintf(os.Stderr, "\n===== LLM REQUEST #%d =====\n", d.count)
	fmt.Fprintf(os.Stderr, "model: %s\nstream: %v\nmessages: %d\ntools: %s\n",
		meta.Model, meta.Stream, len(meta.Messages), strings.Join(names, ", "))
	fmt.Fprintln(os.Stderr, "----- request json -----")
	fmt.Fprintln(os.Stderr, pretty.String())
	fmt.Fprintln(os.Stderr, "===== END LLM REQUEST =====")
}

// DumpPrompt prints the plain system/user prompt used by helper LLMs.
func (d *requestDumper) DumpPrompt(label, system, user string) {
	if !d.active() {
		return
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.count++
	d.writeRecord(dumpRecord{Kind: "tool", Label: label, System: system, User: user})
	if !d.enabled {
		return
	}
	fmt.Fprintf(os.Stderr, "\n===== LLM REQUEST #%d [%s] =====\n", d.count, label)
	fmt.Fprintf(os.Stderr, "----- system -----\n%s\n----- user -----\n%s\n===== END LLM REQUEST =====\n", system, user)
}

type promptDumper struct {
	d     *requestDumper
	label string
	inner adapter.Streamer
}

func (p *promptDumper) Stream(ctx context.Context, system, user string, onToken func(string)) error {
	p.d.DumpPrompt(p.label, system, user)
	return p.inner.Stream(ctx, system, user, onToken)
}
