package agent

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// OfflineEngine 脚本化编排引擎（无 LLM key 的全链路演示与离线评估）。
// 意图按优先级匹配：取消 → 查询 → 确认下单 → 行程（搜→规划→估价）→ 帮助。
// 工具调用走 ToolCaller（与 LLM 引擎同一条 MCP 链路，不 mock 工具本身）。
type OfflineEngine struct{}

// NewOffline 构造。
func NewOffline() *OfflineEngine { return &OfflineEngine{} }

// Name 引擎标识。
func (o *OfflineEngine) Name() string { return "offline" }

// Run 执行一轮编排（sessionID 目前只用于审计上下文，槽位状态在服务端）。
func (o *OfflineEngine) Run(ctx context.Context, req RunRequest, emit func(Event)) error {
	sessionID, userMsg, tc := req.SessionID, req.Message, req.Caller
	_ = sessionID
	msg := strings.TrimSpace(userMsg)
	low := strings.ToLower(msg)

	switch {
	case containsAny(msg, "取消") && containsAny(low, "订单", "单", "车"):
		return o.cancelTrip(ctx, tc, emit)
	case containsAny(msg, "订单", "查单", "我的单", "打到", "到哪", "接单", "司机"):
		return o.listOrders(ctx, tc, emit)
	case containsAny(msg, "确认", "下单吧", "帮我下", "就定", "可以下"):
		return o.createOrder(ctx, tc, emit)
	case looksLikeTrip(msg):
		return o.planTrip(ctx, tc, emit, msg)
	default:
		EmitDelta(emit, helpText())
		return nil
	}
}

// ---------- 意图实现 ----------

// planTrip 搜 → 规划 → 估价（M1 主链路）。
func (o *OfflineEngine) planTrip(ctx context.Context, tc ToolCaller, emit func(Event), msg string) error {
	pickupKW, dropoffKW := extractTrip(msg)
	if dropoffKW == "" && pickupKW == "" {
		EmitDelta(emit, helpText())
		return nil
	}
	if dropoffKW == "" {
		EmitDelta(emit, fmt.Sprintf("收到出发地「%s」，请问目的地是哪里？（例如：去首都机场T3）", pickupKW))
		return nil
	}
	if pickupKW == "" {
		pickupKW = "望京SOHO" // 演示缺省上车点，回复中明确告知
		EmitDelta(emit, fmt.Sprintf("（未指定出发地，以上车点「望京SOHO」计算；如不对请说「从哪里到哪里」）\n"))
	}

	// 1. 搜上车点
	body := tc.Call(ctx, "search_poi", map[string]any{"keyword": pickupKW, "limit": 5})
	pick, ok := firstPOI(body)
	if !ok {
		EmitDelta(emit, fmt.Sprintf("没找到出发地「%s」，请换个说法试试（例如：望京SOHO、北京南站）。", pickupKW))
		return nil
	}
	// 2. 搜目的地
	body = tc.Call(ctx, "search_poi", map[string]any{"keyword": dropoffKW, "limit": 5})
	drop, ok := firstPOI(body)
	if !ok {
		EmitDelta(emit, fmt.Sprintf("没找到目的地「%s」，请换个说法试试（例如：首都机场T3、国贸）。", dropoffKW))
		return nil
	}

	// 3. 规划路线
	body = tc.Call(ctx, "plan_route", map[string]any{
		"pickup_poi_id": strOf(pick["id"]), "dropoff_poi_id": strOf(drop["id"]),
	})
	route := nestedMap(resultOf(body), "route")
	if len(route) == 0 {
		EmitDelta(emit, "路线规划失败："+errText(body))
		return nil
	}
	opt, _ := route["options"].([]any)
	var best map[string]any
	if len(opt) > 0 {
		best, _ = opt[0].(map[string]any)
	}

	// 4. 估价（快车）
	body = tc.Call(ctx, "estimate_price", map[string]any{
		"route_id": strOf(route["route_id"]), "vehicle_class": "comfort",
	})
	q := nestedMap(resultOf(body), "quote")
	if q == nil {
		EmitDelta(emit, "估价失败："+errText(body))
		return nil
	}

	km := floatOf(best["distance_m"]) / 1000
	min := int(floatOf(best["duration_s"]) / 60)
	price := floatOf(q["price_cents"]) / 100
	surge := floatOf(q["surge"])
	place := fmt.Sprintf("%s → %s", strOf(pick["name"]), strOf(drop["name"]))

	var b strings.Builder
	fmt.Fprintf(&b, "已规划行程：%s\n", place)
	fmt.Fprintf(&b, "约 %.1f km / %d 分钟（%s方案）\n", km, min, strOr(best["label"], "推荐"))
	fmt.Fprintf(&b, "快车预估 **¥%.2f**", price)
	if surge > 1.0 {
		fmt.Fprintf(&b, "（动态溢价 %.1f 倍：%s）", surge, surgeReason(pick, drop))
	}
	b.WriteString("\n\n报价 60 秒内有效。确认出行的话，点击右侧报价卡的【确认下单】，或回复「确认下单」。")
	EmitDelta(emit, b.String())
	return nil
}

// createOrder 下单（三道闸在服务端；令牌由 gateway 注入）。
func (o *OfflineEngine) createOrder(ctx context.Context, tc ToolCaller, emit func(Event)) error {
	qid := tc.LastQuoteID()
	if qid == "" {
		EmitDelta(emit, "还没有待确认的报价。先告诉我行程吧，例如：「从望京SOHO去首都机场T3」。")
		return nil
	}
	body := tc.Call(ctx, "create_order", map[string]any{"quote_id": qid})
	if !isOK(body) {
		err := errText(body)
		switch {
		case strings.Contains(err, "GUARD:NEED_CONFIRM"):
			EmitDelta(emit, "报价尚未确认，不能下单。请点击右侧报价卡的【确认下单】按钮，或回复「确认下单」完成确认。")
		case strings.Contains(err, "GUARD:QUOTE_EXPIRED"):
			EmitDelta(emit, "报价已过期（60 秒），需要重新估价。请再说一次行程，我重新给你报价。")
		default:
			EmitDelta(emit, "下单失败："+err)
		}
		return nil
	}
	order, _ := resultOf(body)["order"].(map[string]any)
	if order == nil {
		EmitDelta(emit, "下单返回异常，请稍后重试。")
		return nil
	}
	// 下单成功：清掉本单报价，避免重复下单语义
	EmitDelta(emit, fmt.Sprintf(
		"✅ 下单成功！订单号 **%s**\n%s · %s · ¥%.2f\n司机接单后订单状态会自动更新（右侧面板与地图可见）。想看进度回复「查询订单」，不想打了回复「取消订单」。",
		strOf(order["order_id"]), strOf(order["vehicle_name"]), routePlace(order),
		floatOf(order["price_cents"])/100))
	return nil
}

// listOrders 查询订单。
func (o *OfflineEngine) listOrders(ctx context.Context, tc ToolCaller, emit func(Event)) error {
	body := tc.Call(ctx, "list_my_orders", map[string]any{"limit": 5})
	orders, _ := resultOf(body)["orders"].([]any)
	if len(orders) == 0 {
		EmitDelta(emit, "你还没有订单。先规划一次行程吧，例如：「从北京南站到国贸，打车要多少钱」。")
		return nil
	}
	var b strings.Builder
	b.WriteString("你的最近订单：\n")
	for i, raw := range orders {
		if i >= 3 {
			break
		}
		ord, _ := raw.(map[string]any)
		fmt.Fprintf(&b, "- %s  %s  ¥%.2f  %s → %s  [%s]\n",
			strOf(ord["order_id"]), strOf(ord["vehicle_name"]),
			floatOf(ord["price_cents"])/100,
			strOf(nestedMap(ord, "pickup")["name"]), strOf(nestedMap(ord, "dropoff")["name"]),
			statusCN(strOf(ord["status"])))
	}
	if active := activeOrder(orders); active != nil {
		b.WriteString("\n回复「取消订单」可取消进行中的订单。")
	} else if len(orders) > 0 {
		b.WriteString("\n当前没有进行中的订单。")
	}
	EmitDelta(emit, b.String())
	return nil
}

// cancelTrip 取消进行中订单（服务端计取消费）。
func (o *OfflineEngine) cancelTrip(ctx context.Context, tc ToolCaller, emit func(Event)) error {
	body := tc.Call(ctx, "list_my_orders", map[string]any{"limit": 10})
	orders, _ := resultOf(body)["orders"].([]any)
	target := activeOrder(orders)
	if target == nil {
		EmitDelta(emit, "当前没有可取消的订单。")
		return nil
	}
	oid := strOf(target["order_id"])
	body = tc.Call(ctx, "cancel_order", map[string]any{"order_id": oid, "reason": "用户取消"})
	if !isOK(body) {
		EmitDelta(emit, "取消失败："+errText(body)) // GUARD:NOT_CANCELLABLE 原样转述
		return nil
	}
	ord, _ := resultOf(body)["order"].(map[string]any)
	fee := floatOf(ord["cancel_fee_cents"]) / 100
	if fee > 0 {
		EmitDelta(emit, fmt.Sprintf("已取消订单 %s，收取取消费 **¥%.2f**（已超过免费取消窗口）。", oid, fee))
		return nil
	}
	EmitDelta(emit, fmt.Sprintf("已取消订单 %s，免费。", oid))
	return nil
}

// ---------- 意图解析小工具 ----------

var (
	reFrom   = regexp.MustCompile(`从\s*([^到去，,。！!？?]+?)\s*(?:到|去|赴)`)
	reTo     = regexp.MustCompile(`(?:到|去|赴)\s*([^，,。！!？?]+)`)
	reTripOK = regexp.MustCompile(`从|去|到|打车|多少钱|路线|规划|估价|机场|车站|站`)
)

// extractTrip 从消息提取（出发地, 目的地）；允许任一为空。
func extractTrip(msg string) (string, string) {
	pickup := ""
	rest := msg
	if m := reFrom.FindStringSubmatch(msg); m != nil {
		pickup = strings.TrimSpace(m[1])
		rest = msg[strings.Index(msg, m[0])+len(m[0]):] // 「从X去/到」之后即目的地
	}
	dropoff := ""
	if pickup != "" {
		dropoff = strings.TrimSpace(rest)
	} else if m := reTo.FindStringSubmatch(msg); m != nil {
		dropoff = strings.TrimSpace(m[1]) // 无出发地：只解析「去/到Y」
	}
	// 二次清理：目的地里可能粘着诉求尾巴（"首都机场T3，打车要多少钱"）
	for _, cut := range []string{"，", ",", "。", "打车", "多少钱", "的价格"} {
		if i := strings.Index(dropoff, cut); i > 0 {
			dropoff = dropoff[:i]
		}
	}
	return pickup, strings.TrimSpace(dropoff)
}

func looksLikeTrip(msg string) bool { return reTripOK.MatchString(msg) }

func containsAny(s string, subs ...string) bool {
	for _, sub := range subs {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// firstPOI 从 search_poi 结果取第一个候选。
func firstPOI(body map[string]any) (map[string]any, bool) {
	pois, _ := resultOf(body)["pois"].([]any)
	if len(pois) == 0 {
		return nil, false
	}
	p, ok := pois[0].(map[string]any)
	return p, ok
}

// activeOrder 找进行中的订单。
func activeOrder(orders []any) map[string]any {
	for _, raw := range orders {
		ord, _ := raw.(map[string]any)
		switch strOf(ord["status"]) {
		case "created", "dispatched", "accepted", "arriving", "in_trip":
			return ord
		}
	}
	return nil
}

// isOK / resultOf / errText 解析 MCP 结果契约 {ok, result|error}。
func isOK(body map[string]any) bool {
	if body == nil {
		return false
	}
	ok, _ := body["ok"].(bool)
	return ok
}

func resultOf(body map[string]any) map[string]any {
	if body == nil {
		return map[string]any{}
	}
	if r, ok := body["result"].(map[string]any); ok {
		return r
	}
	return map[string]any{}
}

func errText(body map[string]any) string {
	if body == nil {
		return "empty result"
	}
	if e, ok := body["error"].(string); ok {
		return e
	}
	return "unknown error"
}

func nestedMap(m map[string]any, key string) map[string]any {
	if m == nil {
		return map[string]any{}
	}
	v, _ := m[key].(map[string]any)
	if v == nil {
		return map[string]any{}
	}
	return v
}

func strOf(v any) string {
	s, _ := v.(string)
	return s
}

func strOr(v any, def string) string {
	if s := strOf(v); s != "" {
		return s
	}
	return def
}

func floatOf(v any) float64 {
	switch x := v.(type) {
	case float64:
		return x
	case int:
		return float64(x)
	case int64:
		return float64(x)
	}
	return 0
}

func statusCN(s string) string {
	switch s {
	case "created":
		return "已下单"
	case "dispatched":
		return "派单中"
	case "accepted":
		return "司机已接单"
	case "arriving":
		return "前往上车点"
	case "in_trip":
		return "行程中"
	case "completed":
		return "已完成"
	case "canceled":
		return "已取消"
	}
	return s
}

func routePlace(order map[string]any) string {
	pu := nestedMap(order, "pickup")["name"]
	do := nestedMap(order, "dropoff")["name"]
	return fmt.Sprintf("%v → %v", pu, do)
}

func surgeReason(pick, drop map[string]any) string {
	if strOf(pick["category"]) == "airport" || strOf(drop["category"]) == "airport" ||
		strOf(pick["category"]) == "railway" || strOf(drop["category"]) == "railway" {
		return "枢纽运力紧张"
	}
	return "早晚高峰"
}

func helpText() string {
	return "我是打车助手，可以帮你：\n" +
		"📍 **规划行程**：「从望京SOHO去首都机场T3」\n" +
		"💰 **价格估算**：「从北京南站到国贸，打车要多少钱」\n" +
		"✅ **确认下单**：报价后回复「确认下单」（或点报价卡按钮）\n" +
		"📋 **查询订单**：「查询我的订单」\n" +
		"❌ **取消订单**：「取消订单」（按状态计取消费）"
}
