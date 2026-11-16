// Package review builds the parallel code-review pipeline of lesson4.2
// (S2: Parallelization + Aggregation).
//
// 结构：
//
//	同一份代码 ──┬─▶ 正确性评审  ┐
//	            ├─▶ 安全评审    │  并行 fan-out（goroutine + 独立 runner）
//	            ├─▶ 性能评审    │
//	            └─▶ 风格评审    ┘
//	                       │  findings JSON（WithOutputSchema 结构化输出）
//	                       ▼
//	              聚合评审（去重 → P0~P3 定级 → markdown 报告）
//
// 关注点：为什么四个 Agent 各管一面？——一位"全栈评审员"上下文里八种关注点
// 互相稀释，往往每类只糊一层；拆开之后每个 Agent 的 system instruction 单一、
// 输出可并行、可替换（挂掉一个 panel 照常出报告）。
package review

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"

	"github.com/lihuaizhi/ainewma/unit4/lesson4.2/internal/config"
)

// Options tunes the panel.
type Options struct {
	Model     string
	APIKey    string
	BaseURL   string
	MaxTokens int
	// MaxFindings biases reviewers towards 宁缺毋滥.
	MaxFindings int
	// RequestDumpCallback, when set, receives the marshaled chat request of
	// every LLM call in the panel ((*dumper.Dumper).DumpJSON).
	RequestDumpCallback openai.ChatRequestJSONCallbackFunc
}

// FillDefaults applies the config defaults to empty option fields.
func (o *Options) FillDefaults() {
	if o.Model == "" {
		o.Model = config.DeepSeekModel
	}
	if o.APIKey == "" {
		o.APIKey = os.Getenv(config.DeepSeekAPIKeyEnv)
	}
	if o.BaseURL == "" {
		o.BaseURL = config.DeepSeekBaseURL
	}
	if o.MaxTokens == 0 {
		o.MaxTokens = config.DefaultMaxTokens
	}
	if o.MaxFindings == 0 {
		o.MaxFindings = config.DefaultMaxFindings
	}
}

// Finding is one atomic issue reported by a reviewer.
type Finding struct {
	Line       int    `json:"line"`
	Title      string `json:"title"`
	Detail     string `json:"detail"`
	Suggestion string `json:"suggestion"`
}

// Panel is one reviewer's normalized output.
type Panel struct {
	Name     string
	Findings []Finding
	// Raw is the unparsed final text (kept for debugging / 失败兜底).
	Raw string
	Err error
}

// ParsePayload tolerantly extracts {"findings":[...]} from a reviewer's final
// message: strips markdown code fences, falls back to first balanced object.
func ParsePayload(text string) ([]Finding, error) {
	var payload struct {
		Findings []Finding `json:"findings"`
	}
	if err := json.Unmarshal([]byte(strings.TrimSpace(text)), &payload); err == nil {
		return payload.Findings, nil
	}
	obj := extractJSONObject(text)
	if obj == "" {
		return nil, errInvalidPayload(text)
	}
	if err := json.Unmarshal([]byte(obj), &payload); err != nil {
		return nil, errInvalidPayload(text)
	}
	return payload.Findings, nil
}

type payloadErr string

func (e payloadErr) Error() string { return string(e) }

func errInvalidPayload(text string) error {
	preview := text
	if r := []rune(preview); len(r) > 80 {
		preview = string(r[:80]) + "…"
	}
	return payloadErr("no findings JSON in: " + preview)
}

// extractJSONObject returns the first balanced top-level {...} in s.
func extractJSONObject(s string) string {
	// 先剥 markdown 围栏，避免 "{" 计数被代码示例干扰。
	s = strings.TrimSpace(s)
	if i := strings.Index(s, "```"); i >= 0 {
		s = strings.TrimPrefix(s[i:], "```")
		s = strings.TrimPrefix(s, "json")
		if j := strings.Index(s, "```"); j >= 0 {
			s = s[:j]
		}
	}
	start := -1
	depth := 0
	for i, r := range s {
		switch r {
		case '{':
			if depth == 0 {
				start = i
			}
			depth++
		case '}':
			depth--
			if depth == 0 && start >= 0 {
				return s[start : i+1]
			}
			if depth < 0 {
				return ""
			}
		}
	}
	return ""
}

// FindingsSchema is the JSON schema handed to WithOutputSchema so reviewers
// emit machine-checkable {"findings":[...]} payloads.
func FindingsSchema() map[string]any {
	str := func(desc string) map[string]any {
		return map[string]any{"type": "string", "description": desc}
	}
	return map[string]any{
		"type": "object",
		"properties": map[string]any{
			"findings": map[string]any{
				"type":        "array",
				"description": "问题列表；没有问题时给空数组",
				"items": map[string]any{
					"type": "object",
					"properties": map[string]any{
						"line":       map[string]any{"type": "integer", "description": "问题所在行号（输入代码前缀里的编号）"},
						"title":      str("问题一句话标题，≤24字"),
						"detail":     str("为什么这是问题"),
						"suggestion": str("建议的修法"),
					},
					"required": []string{"title", "detail", "suggestion"},
				},
			},
		},
		"required": []string{"findings"},
	}
}

// New builds the whole panel: 4 reviewers + 1 aggregator. All share one
// OpenAI-compatible model handle so a request dumper can correlate every call.
func New(opts Options) (reviewers []agent.Agent, aggregator agent.Agent) {
	opts.FillDefaults()
	openaiOpts := []openai.Option{
		openai.WithAPIKey(opts.APIKey),
		openai.WithBaseURL(opts.BaseURL),
	}
	if opts.RequestDumpCallback != nil {
		openaiOpts = append(openaiOpts, openai.WithChatRequestJSONCallback(opts.RequestDumpCallback))
	}
	mdl := openai.New(opts.Model, openaiOpts...)
	genCfg := model.GenerationConfig{MaxTokens: &opts.MaxTokens, Stream: true}

	for _, name := range config.ReviewerNames {
		schema := FindingsSchema()
		reviewers = append(reviewers, llmagent.New(name,
			llmagent.WithModel(mdl),
			llmagent.WithDescription("代码评审:"+name+"，只报"+name+"范围的问题"),
			llmagent.WithInstruction(reviewerInstruction(name, opts.MaxFindings)),
			llmagent.WithOutputSchema(schema),
			llmagent.WithGenerationConfig(genCfg),
		))
	}

	maxF := opts.MaxFindings
	aggregator = llmagent.New(config.AggregatorName,
		llmagent.WithModel(mdl),
		llmagent.WithDescription("聚合四路评审的发现，去重、定级、出报告"),
		llmagent.WithInstruction(aggregatorInstruction(maxF)),
		llmagent.WithGenerationConfig(genCfg),
	)
	return reviewers, aggregator
}

func reviewerInstruction(name string, maxFindings int) string {
	focus := ""
	switch name {
	case "正确性评审":
		focus = `你的唯一关注点：代码在运行时会算错或坏掉。包括
- 逻辑 bug 与边界条件（空集合、off-by-one、越界）
- 错误处理遗漏/吞错/错误的 err==nil 判断、变量遮蔽
- 并发问题（数据竞争、共享 map/slice、泄漏的 goroutine）
- 资源生命周期（连接/body 未关闭的泄漏路径）`
	case "安全评审":
		focus = `你的唯一关注点：漏洞与敏感信息。包括
- 注入（SQL 拼接、命令注入、路径穿越）
- 硬编码的密钥/密码/证书与敏感信息打印进日志
- 鉴权与越权、SSRF、不安全的随机数
- 缺失的输入校验（对外部输入直接信任）`
	case "性能评审":
		focus = `你的唯一关注点：会拖慢或放大资源消耗。包括
- 循环内重复 IO / N+1 查询、串行可并行的请求
- 无超时的网络调用、无上限的轮询等待
- 大量小对象/字符串拼接带来的内存与 GC 压力
- 锁的粒度与持锁做慢操作`
	case "风格评审":
		focus = `你的唯一关注点：可读性与维护性。包括
- 命名不达意、魔法数字、重复代码
- 缺注释的导出符号、函数过长、职责混杂
- 明显不 idiomatic 的 Go 习惯（如 _ = err 丢弃）`
	}

	return `你是代码评审流水线里的一位专职评审员：` + name + `。

` + focus + `

工作要求：
- 输入是带行号前缀（"3: "样式）的 Go 源码；只输出 {"findings":[...]} JSON：
  line=行号(整数)，title/detail/suggestion 为中文一句话。
- 发现按严重程度从高到低排，最多 ` + fmtN(maxFindings) + ` 条，宁缺毋滥；
  只报你职责范围内的问题，不要越界（比如风格评审不谈 SQL 注入）。
- 不确定的问题不要报；报告里不要出现 JSON 之外的任何文字。`
}

func aggregatorInstruction(maxFindings int) string {
	return `你是评审流水线的聚合评审。输入是四位评审 agents 的发现 JSON
（reviewers[].reviewer = 注册名，findings[].line/title/detail/suggestion）。

产出一份 markdown 报告，规则：
1. 去重合并：两位以上评审报的同一问题合并成一条，"来自"列出全部来源评审名；
   表述不同但指同一行同一问题的视为重复。
2. 定级：
   - P0 必须改：安全漏洞、数据损坏、会崩/死循环
   - P1 应该改：正确性错误、资损风险、无超时等隐患
   - P2 建议改：性能问题
   - P3 可选：风格/可读性
3. 报告结构：
   ## 结论（一句话：该文件当前是否可合入，为什么）
   ## 必须修复（P0/P1）——表格：级别 | 问题 | 行号 | 说明 | 修法 | 来自
   ## 建议修复（P2/P3）——同样式
   ## 行动清单（Top 3，不超过3行）
4. 不新增发现、不逐条复述 detail 全文（说关键句），不改代码。
5. 总条数最多 ` + fmtN(maxFindings*4) + ` 条以外的部分合并归类。全中文输出。`
}

// fmtN keeps the instruction templates free of strconv plumbing at call sites.
func fmtN(n int) string {
	if n <= 0 {
		n = 6
	}
	return fmt.Sprintf("%d", n)
}

// AnnotateLines prefixes each line of code with a 1-based "N: " number so the
// reviewers can reference line indexes in their findings.
func AnnotateLines(code string) string {
	var b strings.Builder
	for i, line := range strings.Split(strings.TrimRight(code, "\n"), "\n") {
		fmt.Fprintf(&b, "%2d: %s\n", i+1, line)
	}
	return b.String()
}
