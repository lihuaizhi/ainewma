// Command demo calls an LLM directly through the openai-go/v3 adapter,
// showing each supported capability. Every subcommand reads its settings from
// the same environment variables as the server.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/config"
	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/llm"
	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/llm/openai"
)

func main() {
	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}

func usage() string {
	return strings.Join([]string{
		"usage: demo <command> [flags]",
		"",
		"commands:",
		"  chat        blocking chat completion",
		"  stream      chat completion streamed token by token",
		"  json        chat completion constrained to a JSON schema",
		"  tool        chat completion with a callable function",
		"  responses   responses endpoint, blocking",
		"  responses-stream  responses endpoint, streamed",
		"",
		"environment:",
		"  LLM_API_KEY   required for api.openai.com, optional for local gateways",
		"  LLM_API_URL   defaults to https://api.openai.com/v1",
		"  LLM_MODEL     defaults to gpt-4o-mini",
	}, "\n")
}

func run(args []string) error {
	if len(args) == 0 {
		fmt.Println(usage())
		return flag.ErrHelp
	}
	command, args := args[0], args[1:]

	// Ctrl-C must cancel the in-flight request, not just kill the process, so
	// that streams close their upstream connection cleanly.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	switch command {
	case "help", "-h", "--help":
		fmt.Println(usage())
		return nil
	case "chat":
		return runChat(ctx, args)
	case "stream":
		return runStream(ctx, args)
	case "json":
		return runJSON(ctx, args)
	case "tool":
		return runTool(ctx, args)
	case "responses":
		return runResponses(ctx, args)
	case "responses-stream":
		return runResponsesStream(ctx, args)
	default:
		fmt.Println(usage())
		return fmt.Errorf("unknown command %q", command)
	}
}

// newClient builds the adapter from the environment, applying the command's
// model and base URL flags as overrides.
func newClient(model, baseURL string) (llm.Client, string, error) {
	cfg, err := config.Load()
	if err != nil {
		return nil, "", err
	}
	apiURL := cfg.LLMAPIURL
	if strings.TrimSpace(baseURL) != "" {
		apiURL = strings.TrimSpace(baseURL)
	}
	client, err := openai.New(openai.Options{
		APIURL:       apiURL,
		APIKey:       cfg.APIKey,
		DefaultModel: cfg.Model,
		OrgID:        cfg.OrgID,
		ProjectID:    cfg.ProjectID,
		MaxRetries:   cfg.MaxRetries,
		// This bounds only the wait for response headers, not the body, so it
		// is safe for the streaming commands too.
		ResponseHeaderTimeout: cfg.ResponseHeaderTimeout,
	})
	if err != nil {
		return nil, "", err
	}
	if strings.TrimSpace(model) != "" {
		return client, strings.TrimSpace(model), nil
	}
	return client, cfg.Model, nil
}

// runChat shows a blocking call.
func runChat(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("chat", flag.ExitOnError)
	prompt := flags.String("prompt", "用一句话解释什么是 token。", "user prompt")
	system := flags.String("system", "你是一个简洁的中文助手。", "system prompt")
	model := flags.String("model", "", "override the configured model")
	baseURL := flags.String("base-url", "", "override LLM_API_URL")
	temperature := flags.Float64("temperature", 0.7, "sampling temperature")
	if err := flags.Parse(args); err != nil {
		return err
	}

	client, resolvedModel, err := newClient(*model, *baseURL)
	if err != nil {
		return err
	}
	started := time.Now()
	response, err := client.Chat(ctx, llm.ChatRequest{
		Model: resolvedModel,
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: *system},
			{Role: llm.RoleUser, Content: *prompt},
		},
		Temperature: temperature,
	})
	if err != nil {
		return err
	}
	report(response.Content, response.Usage, time.Since(started))
	return nil
}

// runStream shows incremental output, including streamed tool call fragments.
func runStream(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("stream", flag.ExitOnError)
	prompt := flags.String("prompt", "写一句关于秋天的一句话。", "user prompt")
	model := flags.String("model", "", "override the configured model")
	baseURL := flags.String("base-url", "", "override LLM_API_URL")
	if err := flags.Parse(args); err != nil {
		return err
	}

	client, resolvedModel, err := newClient(*model, *baseURL)
	if err != nil {
		return err
	}
	started := time.Now()
	events, err := client.StreamChat(ctx, llm.ChatRequest{
		Model:    resolvedModel,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: *prompt}},
	})
	if err != nil {
		return err
	}
	fmt.Print("streaming: ")
	usage, err := drain(events)
	fmt.Println()
	if err != nil {
		return err
	}
	report("", usage, time.Since(started))
	return nil
}

// runJSON shows Structured Outputs: the model must return schema-valid JSON.
func runJSON(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("json", flag.ExitOnError)
	prompt := flags.String("prompt", "张三，32岁，住在上海，职位是后端工程师。", "text to extract a profile from")
	model := flags.String("model", "", "override the configured model")
	baseURL := flags.String("base-url", "", "override LLM_API_URL")
	if err := flags.Parse(args); err != nil {
		return err
	}

	client, resolvedModel, err := newClient(*model, *baseURL)
	if err != nil {
		return err
	}

	strict := true
	schema := json.RawMessage(`{
		"type": "object",
		"properties": {
			"name":  {"type": "string"},
			"age":   {"type": "integer"},
			"city":  {"type": "string"},
			"job":   {"type": "string"}
		},
		"required": ["name", "age", "city", "job"],
		"additionalProperties": false
	}`)

	started := time.Now()
	response, err := client.Chat(ctx, llm.ChatRequest{
		Model:    resolvedModel,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: *prompt}},
		ResponseFormat: &llm.ResponseFormat{
			Type: llm.ResponseFormatJSONSchema,
			Schema: &llm.JSONSchemaFormat{
				Name:        "person_profile",
				Description: "从自然语言描述中抽取的人物档案",
				Schema:      schema,
				Strict:      &strict,
			},
		},
	})
	if err != nil {
		return err
	}
	// Strict mode guarantees the content parses, but re-validate rather than
	// trusting the guarantee.
	var pretty any
	if err := json.Unmarshal([]byte(response.Content), &pretty); err != nil {
		return fmt.Errorf("model returned invalid JSON: %w", err)
	}
	indented, _ := json.MarshalIndent(pretty, "", "  ")
	fmt.Printf("%s\n", indented)
	report("", response.Usage, time.Since(started))
	return nil
}

// runTool shows function calling. The loop feeds the model's own arguments
// back in so it can produce a final answer.
func runTool(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("tool", flag.ExitOnError)
	prompt := flags.String("prompt", "北京今天天气怎么样？", "user prompt")
	model := flags.String("model", "", "override the configured model")
	baseURL := flags.String("base-url", "", "override LLM_API_URL")
	if err := flags.Parse(args); err != nil {
		return err
	}

	client, resolvedModel, err := newClient(*model, *baseURL)
	if err != nil {
		return err
	}

	parameters := json.RawMessage(`{
		"type": "object",
		"properties": {
			"city": {"type": "string", "description": "城市名称"}
		},
		"required": ["city"]
	}`)

	messages := []llm.Message{{Role: llm.RoleUser, Content: *prompt}}
	started := time.Now()

	// Two rounds are enough to exercise the tool-call round trip. A real agent
	// would loop until the model stops requesting tools.
	for round := 0; round < 2; round++ {
		response, err := client.Chat(ctx, llm.ChatRequest{
			Model:    resolvedModel,
			Messages: messages,
			Tools: []llm.Tool{{
				Type: "function",
				Function: llm.ToolFunctionSpec{
					Name:        "get_weather",
					Description: "查询指定城市的天气",
					Parameters:  parameters,
				},
			}},
			ToolChoice: json.RawMessage(`"auto"`),
		})
		if err != nil {
			return err
		}
		messages = append(messages, llm.Message{
			Role:      llm.RoleAssistant,
			Content:   response.Content,
			ToolCalls: response.ToolCalls,
		})
		if len(response.ToolCalls) == 0 {
			fmt.Println(response.Content)
			report("", response.Usage, time.Since(started))
			return nil
		}

		for _, toolCall := range response.ToolCalls {
			fmt.Printf("tool call: %s(%s)\n", toolCall.Function.Name, toolCall.Function.Arguments)
			messages = append(messages, llm.Message{
				Role:       llm.RoleTool,
				ToolCallID: toolCall.ID,
				Content:    mockWeather(toolCall.Function.Arguments),
			})
		}
	}
	return errors.New("model kept requesting tools")
}

// mockWeather stands in for a real tool implementation.
func mockWeather(arguments string) string {
	var payload struct {
		City string `json:"city"`
	}
	if err := json.Unmarshal([]byte(arguments), &payload); err != nil {
		return `{"error":"arguments are not valid JSON"}`
	}
	if payload.City == "" {
		payload.City = "未知城市"
	}
	result, _ := json.Marshal(map[string]any{
		"city":        payload.City,
		"temperature": 22,
		"condition":   "晴",
	})
	return string(result)
}

// runResponses shows the responses endpoint, which carries instructions
// separately from the input messages.
func runResponses(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("responses", flag.ExitOnError)
	prompt := flags.String("prompt", "用一句话介绍 Go 的 goroutine。", "user prompt")
	instructions := flags.String("instructions", "你是一个简洁的中文助手。", "developer instructions")
	verbosity := flags.String("verbosity", "low", "output verbosity: low, medium or high")
	model := flags.String("model", "", "override the configured model")
	baseURL := flags.String("base-url", "", "override LLM_API_URL")
	if err := flags.Parse(args); err != nil {
		return err
	}

	client, resolvedModel, err := newClient(*model, *baseURL)
	if err != nil {
		return err
	}
	started := time.Now()
	response, err := client.CreateResponse(ctx, llm.ResponsesRequest{
		Model:        resolvedModel,
		Instructions: *instructions,
		Input:        []llm.Message{{Role: llm.RoleUser, Content: *prompt}},
		Text:         &llm.ResponsesText{Verbosity: *verbosity},
	})
	if err != nil {
		return err
	}
	report(response.OutputText, response.Usage, time.Since(started))
	return nil
}

// runResponsesStream shows the streamed form of the responses endpoint.
func runResponsesStream(ctx context.Context, args []string) error {
	flags := flag.NewFlagSet("responses-stream", flag.ExitOnError)
	prompt := flags.String("prompt", "写一句关于冬天的话。", "user prompt")
	model := flags.String("model", "", "override the configured model")
	baseURL := flags.String("base-url", "", "override LLM_API_URL")
	if err := flags.Parse(args); err != nil {
		return err
	}

	client, resolvedModel, err := newClient(*model, *baseURL)
	if err != nil {
		return err
	}
	started := time.Now()
	events, err := client.StreamResponse(ctx, llm.ResponsesRequest{
		Model: resolvedModel,
		Input: []llm.Message{{Role: llm.RoleUser, Content: *prompt}},
	})
	if err != nil {
		return err
	}
	fmt.Print("streaming: ")
	usage, err := drain(events)
	fmt.Println()
	if err != nil {
		return err
	}
	report("", usage, time.Since(started))
	return nil
}

// drain prints incremental deltas as they arrive and returns the last usage
// totals the model reported.
func drain(events <-chan llm.StreamEvent) (llm.Usage, error) {
	var usage llm.Usage
	for event := range events {
		switch {
		case event.Err != nil:
			return usage, event.Err
		case event.Delta != "":
			fmt.Print(event.Delta)
		case len(event.ToolCalls) > 0:
			// Streamed tool calls arrive as fragments keyed by Index, so print
			// each fragment as it comes rather than buffering the whole call.
			for _, toolCall := range event.ToolCalls {
				fmt.Print(toolCall.Function.Arguments)
			}
		}
		if event.Usage != nil {
			usage = *event.Usage
		}
	}
	return usage, nil
}

func report(content string, usage llm.Usage, elapsed time.Duration) {
	if content != "" {
		fmt.Printf("\n%s\n", content)
	}
	slog.Info("usage",
		"prompt_tokens", usage.PromptTokens,
		"completion_tokens", usage.CompletionTokens,
		"total_tokens", usage.TotalTokens,
		"elapsed", elapsed.Round(time.Millisecond),
	)
}
