// Package agents builds the routing multi-agent cluster of the customer
// service demo (S1: Routing / Handoff).
//
// 结构：
//
//	客服助手（总台，意图分流）── transfer_to_agent ──▶ 退货助手 退款专员
//	                                        ├────▶ 订单助手  订单物流专员
//	                                        └────▶ 导购助手  导购专员
//
// trpc-agent-go 的玩法：给主 Agent 配 llmagent.WithSubAgents(...) 后，框架会
// 自动注入 transfer_to_agent 工具；主 Agent 通过 function calling 转交控制权，
// 会话状态（历史消息）留在同一个 session 里，由专家 Agent 接着往下聊。
// Agent 的注册名统一在 internal/config 里维护，中文英文都行，但注意：
// transfer_to_agent 的 agent_name 参数必须与注册名逐字符一致。
package agents

import (
	"context"
	"fmt"
	"os"
	"strings"

	"trpc.group/trpc-go/trpc-agent-go/agent"
	"trpc.group/trpc-go/trpc-agent-go/agent/llmagent"
	"trpc.group/trpc-go/trpc-agent-go/model"
	"trpc.group/trpc-go/trpc-agent-go/model/openai"
	"trpc.group/trpc-go/trpc-agent-go/tool"
	"trpc.group/trpc-go/trpc-agent-go/tool/function"

	"github.com/lihuaizhi/ainewma/unit4/lesson4.1/internal/config"
	"github.com/lihuaizhi/ainewma/unit4/lesson4.1/internal/store"
)

// Options tunes the agent cluster.
type Options struct {
	Model        string // deepseek-chat
	APIKey       string // env DEEPSEEK_API_KEY by default
	BaseURL      string // https://api.deepseek.com
	MaxTokens    int    // per reply
	MaxToolIters int    // tool loop bound of each agent
	// RequestDumpCallback, when set, receives the marshaled chat request of
	// every LLM call made by every agent in the cluster (lesson3.3-style
	// request tracing). Typically (*dumper.Dumper).DumpJSON.
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
	if o.MaxToolIters == 0 {
		o.MaxToolIters = config.DefaultMaxIters
	}
}

// Cluster bundles the router and its specialists.
type Cluster struct {
	Router agent.Agent // 总台：唯一交给 runner.Run 的入口
}

// New builds the whole cluster. Every agent shares one OpenAI-compatible
// model handle; the router additionally gets transfer_to_agent via
// WithSubAgents.
func New(opts Options) (*Cluster, error) {
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

	newAgent := func(name, desc string, instr string, tools []tool.Tool) agent.Agent {
		return llmagent.New(name,
			llmagent.WithModel(mdl),
			llmagent.WithDescription(desc),
			llmagent.WithInstruction(instr),
			llmagent.WithTools(tools),
			llmagent.WithMaxToolIterations(opts.MaxToolIters),
			llmagent.WithGenerationConfig(genCfg),
		)
	}

	order := newAgent(config.OrderAgentName,
		"订单物流专员：处理订单查询、发货状态、物流轨迹类问题",
		orderInstruction(),
		[]tool.Tool{newQueryOrderTool(), newQueryDeliveryTool()},
	)

	refund := newAgent(config.RefundAgentName,
		"退款专员：处理退款、退货、换货申请与退款政策咨询",
		refundInstruction(),
		[]tool.Tool{newQueryOrderTool(), newQueryPolicyTool(), newSubmitRefundTool()},
	)

	guide := newAgent(config.GuideAgentName,
		"导购专员：理解购物需求，推荐商品并介绍促销活动",
		guideInstruction(),
		[]tool.Tool{newSearchProductsTool(), newQueryPromotionTool()},
	)

	router := llmagent.New(config.RouterAgentName,
		llmagent.WithModel(mdl),
		llmagent.WithDescription("电商客服总台：识别用户意图并分流给对口专员"),
		llmagent.WithInstruction(routerInstruction(
			config.RefundAgentName, config.OrderAgentName, config.GuideAgentName,
		)),
		// 核心：登记子 Agent 后框架自动加 transfer_to_agent 工具。
		llmagent.WithSubAgents([]agent.Agent{refund, order, guide}),
		llmagent.WithGenerationConfig(genCfg),
	)

	return &Cluster{Router: router}, nil
}

func routerInstruction(refund, order, guide string) string {
	return fmt.Sprintf(`你是电商客服总台「小雅」。你的唯一职责是判断用户意图，并把对话转接给对口专员。

可用的专员：
- %s：退款、退货、换货、退款进度与政策咨询
- %s：订单查询、发货状态、物流/快递跟踪
- %s：想买什么、要推荐、比价、促销活动

工作要求：
- 退款退货类 → 转给 %s；订单物流类 → 转给 %s；购买/推荐类 → 转给 %s。
- 转接用 transfer_to_agent 工具。agent_name 必须原样填专员的英文ID：
  退款=%s、订单=%s、导购=%s。
  严禁自创名字（如「退货助手」「订单员」），填了不存在的ID转接会失败。
- message 里用一句话转述用户诉求（含已知的订单号/需求要点）。
- 只做打招呼、道歉这类寒暄；信息不足以判断时自己简短回应，并引导用户说明来意（是售后还是想买东西）。
- 用户一句话里既有售后又有新需求时，先满足更紧急的一方（退款优先），并在转接消息里注明另一个诉求。
- 不要抢专家的活：不要替专员查订单、报价格、做退款动作。
- 转接即可，不需要你向用户长篇解释。`, refund, order, guide, refund, order, guide, refund, order, guide)
}

func refundInstruction() string {
	return `你是退款专员。处理用户的退款/退货/换货请求。

可用工具：
- query_order：核实订单是否存在、状态、金额（未提供订单号时先向用户索要，订单号形如 O-1001）
- query_refund_policy：查退款政策（七天无理由 / 退款时限 / 特殊类目 / 运费规则 / 未支付订单 / 运费险）
- submit_refund：为用户发起退款，前提是已明确订单号且用户明确要求退款

工作要求：
- 先核实订单信息，再用政策工具查对应条款，结合两者回答，不要凭空承诺。
- 发起退款前向用户复述一遍订单和原因，获用户确认后再调用 submit_refund。
- 订单不存在、待付款、或不符合政策的场景，礼貌说明原因并给出替代建议。
- 回复用户时用中文，简明、有条理；一次只调用一个工具。`
}

func orderInstruction() string {
	return `你是订单物流专员。处理订单查询与快递跟踪。

可用工具：
- query_order：查订单状态与金额（用户没给订单号时先索要，订单号形如 O-1001）
- query_delivery：查物流轨迹

工作要求：
- 先核订单再查物流；没有轨迹时说明「还在打包/尚未揽收」而不是编造时间点。
- 用时间线方式播报轨迹；顺便提醒用户「已发货/已签收」会影响后续售后选项。
- 回复用中文、简洁；一次只调用一个工具。`
}

func guideInstruction() string {
	return `你是导购专员。帮用户选商品。

可用工具：
- search_products：按关键词/类目/预算搜索商品库
- query_promotion：查某个商品的促销/优惠券信息

工作要求：
- 用户没有说清楚用途或预算时，先追问一句再推荐，但最多问一轮，问完就给方案。
- 推荐给 2~3 个候选并给出各自适配理由，价格用工具查询结果，不要报记忆中的价格。
- 用户看中候选时，主动用 query_promotion 查对应优惠再报价。
- 回复用中文；一次只调用一个工具。`
}

// ---------- tools（均为进程内 mock 实现，跑起来不需要外部依赖） ----------

// query_order

type queryOrderIn struct {
	OrderID string `json:"order_id" jsonschema:"description=订单号 例如 O-1001"`
}

func orderKV(o *store.Order) map[string]any {
	return map[string]any{
		"order_id": o.ID, "sku": o.SKU, "amount": o.Amount,
		"status": o.Status, "user": o.User, "refunded": o.Refunded,
	}
}

func newQueryOrderTool() tool.Tool {
	fn := func(_ context.Context, in queryOrderIn) (map[string]any, error) {
		o, err := store.GetOrder(strings.TrimSpace(in.OrderID))
		if err != nil {
			return nil, err
		}
		return orderKV(o), nil
	}
	return function.NewFunctionTool(fn,
		function.WithName("query_order"),
		function.WithDescription("查询订单的状态、金额、商品；订单不存在会报错并给出示例订单号"),
	)
}

// query_delivery

func newQueryDeliveryTool() tool.Tool {
	fn := func(_ context.Context, in queryOrderIn) (map[string]any, error) {
		tr, err := store.GetDelivery(strings.TrimSpace(in.OrderID))
		if err != nil {
			return nil, err
		}
		points := make([]map[string]any, 0, len(tr))
		for _, tp := range tr {
			points = append(points, map[string]any{
				"time": tp.Time, "state": tp.State, "desc": tp.Desc,
			})
		}
		return map[string]any{"order_id": strings.TrimSpace(in.OrderID), "points": points}, nil
	}
	return function.NewFunctionTool(fn,
		function.WithName("query_delivery"),
		function.WithDescription("查询订单的物流轨迹（时间线），无轨迹说明尚未揽收"),
	)
}

// query_refund_policy

type queryPolicyIn struct {
	Topic string `json:"topic,omitempty" jsonschema:"description=政策主题 可选 七天无理由/退款时限/特殊类目/运费规则/未支付订单/运费险，为空返回全部"`
}

func newQueryPolicyTool() tool.Tool {
	fn := func(_ context.Context, in queryPolicyIn) (map[string]any, error) {
		ps := store.GetPolicy(in.Topic)
		items := make([]map[string]any, 0, len(ps))
		for _, p := range ps {
			items = append(items, map[string]any{"topic": p.Topic, "text": p.Text})
		}
		return map[string]any{"count": len(items), "policies": items}, nil
	}
	return function.NewFunctionTool(fn,
		function.WithName("query_refund_policy"),
		function.WithDescription("查询本店退款政策条款，回答退款问题前应先查政策"),
	)
}

// submit_refund

type submitRefundIn struct {
	OrderID string `json:"order_id" jsonschema:"description=订单号 例如 O-1001"`
	Reason  string `json:"reason" jsonschema:"description=退款原因 需与用户确认过"`
}

func newSubmitRefundTool() tool.Tool {
	fn := func(_ context.Context, in submitRefundIn) (map[string]any, error) {
		refundID, err := store.SubmitRefund(strings.TrimSpace(in.OrderID), in.Reason)
		if err != nil {
			return nil, err
		}
		return map[string]any{
			"refund_id": refundID, "order_id": strings.TrimSpace(in.OrderID),
			"eta": "1~3 个工作日原路退回",
		}, nil
	}
	return function.NewFunctionTool(fn,
		function.WithName("submit_refund"),
		function.WithDescription("发起退款并返回退款单号；订单不存在/未支付/已退款会被拒绝"),
	)
}

// search_products

type searchProductsIn struct {
	Keyword  string  `json:"keyword,omitempty" jsonschema:"description=关键词或类目 空表示全部 例如 耳机/数码"`
	MaxPrice float64 `json:"max_price,omitempty" jsonschema:"description=价格上限 可选 单位元"`
	Limit    int     `json:"limit,omitempty" jsonschema:"description=最多返回条数 默认5"`
}

func newSearchProductsTool() tool.Tool {
	fn := func(_ context.Context, in searchProductsIn) (map[string]any, error) {
		limit := in.Limit
		if limit <= 0 || limit > 10 {
			limit = 5
		}
		res := store.SearchProducts(in.Keyword, in.MaxPrice, limit)
		items := make([]map[string]any, 0, len(res))
		for _, p := range res {
			items = append(items, map[string]any{
				"product_id": p.ID, "name": p.Name, "category": p.Category, "price": p.Price,
			})
		}
		return map[string]any{"count": len(items), "products": items}, nil
	}
	return function.NewFunctionTool(fn,
		function.WithName("search_products"),
		function.WithDescription("在商品库中按关键词、类目和预算搜索商品"),
	)
}

// query_promotion

type queryPromotionIn struct {
	ProductID string `json:"product_id" jsonschema:"description=商品ID 例如 P-2001"`
}

func newQueryPromotionTool() tool.Tool {
	fn := func(_ context.Context, in queryPromotionIn) (map[string]any, error) {
		p, err := store.GetProduct(strings.TrimSpace(in.ProductID))
		if err != nil {
			return nil, err
		}
		return promotionOf(p), nil
	}
	return function.NewFunctionTool(fn,
		function.WithName("query_promotion"),
		function.WithDescription("查询商品的当前促销活动与到手价"),
	)
}

// promotionOf generates deterministic promo info per product id.
func promotionOf(p *store.Product) map[string]any {
	promos := map[string][]string{
		"P-2001": {"限时直降" + fmt.Sprintf("%.0f", p.Price*0.1) + "元", "满 99 减 10 券可叠加"},
		"P-2002": {"以旧换新最高抵 500 元", "赠防滑垫"},
		"P-2003": {"第二件半价"},
		"P-2004": {"前 100 名赠键帽套件"},
		"P-2005": {"满 2 件 9 折"},
		"P-2006": {"赠腰靠", "免息 3 期"},
	}
	acts, ok := promos[p.ID]
	if !ok {
		acts = []string{"暂无促销活动"}
	}
	return map[string]any{
		"product_id": p.ID, "name": p.Name, "price": p.Price,
		"activities": acts,
		"note":       "到手价按活动实时计算，请以页面为准",
	}
}
