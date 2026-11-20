// Package config 集中 lesson6.1 的默认配置：双服务地址、MCP 鉴权、
// DeepSeek 接入与业务常量（报价 TTL、演示倍速等）。flag/env 可覆盖。
package config

import (
	"os"
	"strconv"
	"time"
)

// 服务地址与鉴权。
const (
	GatewayAddr = "127.0.0.1:8080"
	RideMCPAddr = "127.0.0.1:8081"

	// MCPToken 是 ride-ops MCP 端点与内部 REST 的 Bearer token（lesson2.3 鉴权复用）。
	MCPTokenEnv = "RIDE_MCP_TOKEN"
	MCPTokenDef = "dev-token"
)

// DeepSeek（openai 兼容接口，同 lesson4.3）。
const (
	DeepSeekAPIKeyEnv = "DEEPSEEK_API_KEY"
	DeepSeekBaseURL   = "https://api.deepseek.com"
	DeepSeekModel     = "deepseek-chat"
	DefaultMaxTokens  = 2048
)

// 业务常量。
const (
	// QuoteTTL 报价有效期：过期必须重新估价（GUARD:QUOTE_EXPIRED）。
	QuoteTTL = 60 * time.Second
	// ConfirmTokenTTL 确认令牌有效期。
	ConfirmTokenTTL = 60 * time.Second

	// CancelFeeCents 司机接单后取消的取消费（元→分）。
	CancelFeeCents = 500
	// FreeCancelWindow 接单后免费取消窗口。
	FreeCancelWindow = 2 * time.Minute
)

// DemoSpeed 订单状态机演示倍速：基础延时/ speed。默认 10 倍速。
func DemoSpeed() float64 {
	if v := os.Getenv("DEMO_SPEED"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil && f > 0 {
			return f
		}
	}
	return 10
}

// Token 返回 MCP Bearer token。
func Token() string {
	if v := os.Getenv(MCPTokenEnv); v != "" {
		return v
	}
	return MCPTokenDef
}
