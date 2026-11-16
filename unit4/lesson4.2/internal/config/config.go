// Package config holds the tunable parameters for the lesson4.2 parallel
// code-review pipeline (S2: Parallelization + Aggregation).
package config

const (
	// DeepSeekBaseURL is the official DeepSeek API endpoint (OpenAI-compatible).
	DeepSeekBaseURL = "https://api.deepseek.com"
	// DeepSeekModel is the default chat model on the official API.
	DeepSeekModel = "deepseek-chat"
	// DeepSeekAPIKeyEnv is the env var that carries the DeepSeek API key.
	DeepSeekAPIKeyEnv = "DEEPSEEK_API_KEY"

	// DefaultMaxTokens caps each agent reply.
	DefaultMaxTokens = 1600

	// AggregatorName is the registered name of the final aggregation agent.
	AggregatorName = "聚合评审"

	// DefaultMaxFindings bounds the findings per reviewer (bias: 宁缺毋滥).
	DefaultMaxFindings = 6
)

// ReviewerNames is the fan-out list: four specialized reviewers, one concern
// each, all fed the same code. Registration names are Chinese to stay
// print-friendly (lesson4.1's rename convention).
var ReviewerNames = []string{
	"正确性评审",
	"安全评审",
	"性能评审",
	"风格评审",
}
