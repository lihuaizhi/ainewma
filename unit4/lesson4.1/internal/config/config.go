// Package config holds the tunable parameters for the lesson4.1
// multi-agent customer service demo (S1: Routing / Handoff).
package config

const (
	// DeepSeekBaseURL is the official DeepSeek API endpoint (OpenAI-compatible).
	DeepSeekBaseURL = "https://api.deepseek.com"
	// DeepSeekModel is the default chat model on the official API.
	DeepSeekModel = "deepseek-chat"
	// DeepSeekAPIKeyEnv is the env var that carries the DeepSeek API key.
	DeepSeekAPIKeyEnv = "DEEPSEEK_API_KEY"

	// DefaultMaxTokens caps a single agent reply.
	DefaultMaxTokens = 1200
	// DefaultMaxIters bounds the tool loop of each specialist agent.
	DefaultMaxIters = 5

	// Agent names. The router hands control over to a specialist through
	// framework-injected transfer_to_agent; the names must match the
	// router's sub-agent list (see internal/agents).
	RouterAgentName = "客服助手"
	RefundAgentName = "退货助手"
	OrderAgentName  = "订单助手"
	GuideAgentName  = "导购助手"
)
