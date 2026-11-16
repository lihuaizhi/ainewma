// Command service is the S1 (Routing / Handoff) demo of lesson4.1: an
// e-commerce customer service where a triage agent transfers the user to a
// refund / order / shopping-guide specialist through the framework-injected
// transfer_to_agent tool.
//
//	one shot:  go run ./cmd/service -q "我想退掉 O-1001 这个耳机"
//	animation: go run ./cmd/service -verbose -q "..."
//	 REPL:     go run ./cmd/service            （-q 为空时进入多轮对话）
package main

import (
	"bufio"
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/event"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/runner"
	"trpc.group/trpc-go/trpc-agent-go/session/inmemory"

	"github.com/lihuaizhi/ainewma/unit4/lesson4.1/internal/agents"
	"github.com/lihuaizhi/ainewma/unit4/lesson4.1/internal/config"
	"github.com/lihuaizhi/ainewma/unit4/lesson4.1/internal/dumper"
)

func main() {
	q := flag.String("q", "", "user message; empty = interactive REPL")
	say := flag.String("say", "张三", "mock user name used in the runner keys")
	modelName := flag.String("model", config.DeepSeekModel, "chat model")
	apiKey := flag.String("api-key", os.Getenv(config.DeepSeekAPIKeyEnv), "api key")
	baseURL := flag.String("base-url", config.DeepSeekBaseURL, "OpenAI-compatible base url")
	maxTokens := flag.Int("max-tokens", config.DefaultMaxTokens, "max reply tokens")
	maxIters := flag.Int("max-iters", config.DefaultMaxIters, "max tool iterations per agent")
	verbose := flag.Bool("verbose", false, "print tool calls / agent transfer events")
	dumpReq := flag.Bool("dump-request", false, "dump every LLM request pretty-printed to stderr")
	dumpFile := flag.String("dump-request-file", "", "also write each LLM request as a JSON record (one per line) to this file")
	flag.Parse()

	if strings.TrimSpace(*apiKey) == "" {
		log.Fatalf("missing API key: set %s or pass -api-key", config.DeepSeekAPIKeyEnv)
	}

	dumper, err := dumper.New(*dumpReq, *dumpFile)
	if err != nil {
		log.Fatalf("dumper: %v", err)
	}
	defer dumper.Close()

	cluster, err := agents.New(agents.Options{
		Model: *modelName, APIKey: *apiKey, BaseURL: *baseURL,
		MaxTokens: *maxTokens, MaxToolIters: *maxIters,
		RequestDumpCallback: dumper.DumpJSON,
	})
	if err != nil {
		log.Fatalf("agents: %v", err)
	}

	// 单人单 session：转接发生时控制权切换，但历史消息都在同一个 session 里，
	// 这就是 handoff 模式里「会话状态跟随」的最小演示。
	r := runner.NewRunner("lesson4.1", cluster.Router,
		runner.WithSessionService(inmemory.NewSessionService()))
	defer r.Close()

	sessionKey := fmt.Sprintf("cs-%d", os.Getpid())
	userKey := *say

	ctx := context.Background()
	if msg := strings.TrimSpace(*q); msg != "" {
		if err := turn(ctx, r, sessionKey, userKey, msg, *verbose); err != nil {
			log.Fatalf("turn: %v", err)
		}
		fmt.Println()
		return
	}

	fmt.Println(`电商客服演示（router → refund/order/guide）。输入消息回车发送，exit 退出。
试试：
  你好，我想退掉 O-1001 的耳机
  我的 O-1002 怎么还没到？
  想买个敲代码的键盘，预算 300 内`)
	sc := bufio.NewScanner(os.Stdin)
	for {
		fmt.Print("user> ")
		if !sc.Scan() {
			break
		}
		msg := strings.TrimSpace(sc.Text())
		if msg == "" {
			continue
		}
		if strings.EqualFold(msg, "exit") || strings.EqualFold(msg, "quit") {
			return
		}
		if err := turn(ctx, r, sessionKey, userKey, msg, *verbose); err != nil {
			log.Printf("turn: %v", err)
		}
		fmt.Println()
	}
}

// turn feeds one user message to the router and streams whatever agent is in
// control (including the message each specialist emits after a transfer).
func turn(ctx context.Context, r runner.Runner, session, user, msg string, verbose bool) error {
	events, err := r.Run(ctx, user, session, model.NewUserMessage(msg))
	if err != nil {
		return err
	}
	current := config.RouterAgentName
	sawDelta := false
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
		if verbose {
			for _, tc := range ch.Message.ToolCalls {
				fmt.Printf("[TOOLCALL] %s(%s)\n", tc.Function.Name, tc.Function.Arguments)
			}
		}
		if verbose && ev.Author != "" && ev.Author != current {
			fmt.Printf("[AGENT SWITCH] %s → %s\n", current, ev.Author)
			current = ev.Author
		}
		// transfer_to_agent / 工具回执等内部事件不外显给用户。
		if ev.ContainsTag(event.TransferTag) || ch.Message.Role == model.RoleTool {
			continue
		}
		// 流式时逐 token 打 Delta；整段返回（非流式）时才兜底打印 Message。
		if ch.Delta.Content != "" {
			fmt.Print(ch.Delta.Content)
			sawDelta = true
		} else if !sawDelta && ch.Message.Content != "" && ch.Message.ToolCalls == nil {
			fmt.Print(ch.Message.Content)
		}
	}
	fmt.Println()
	return nil
}
