// Package tools 定义 ride-ops 的 8+1 个 MCP 工具：schema（mcp.NewTool）
// 与 handler（纯函数，参数为 JSON 解析后的 map）。handler 是能力的唯一
// 入口——MCP server 与离线引擎共用；服务端护栏（确认令牌/归属/幂等/TTL/
// 车型白名单/取消规则）全部在本包执行，不信模型传来的任何价格与订单参数。
//
// 会话上下文：args["_session_id"] 由 gateway 的工具包装层注入（模型不可见），
// confirm_token 同理——schema 不暴露 price/token 字段（方案 §4.4 三道闸）。
package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/mark3labs/mcp-go/mcp"

	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/audit"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/config"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/machine"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/provider"
	"github.com/lihuaizhi/ainewma/unit6/lesson6.1/internal/store"
)

// Deps 工具依赖。
type Deps struct {
	Map     provider.MapProvider
	Ride    provider.RideProvider
	Store   *store.Store
	Machine *machine.Engine
	Audit   *audit.Logger
	Now     func() time.Time // 可注入（测试/离线）
}

func (d *Deps) now() time.Time {
	if d.Now != nil {
		return d.Now()
	}
	return time.Now()
}

// sessionFrom 读注入的会话 id。
func sessionFrom(args map[string]any) (string, error) {
	sid, _ := args["_session_id"].(string)
	if strings.TrimSpace(sid) == "" {
		return "", fmt.Errorf("GUARD:NO_SESSION 缺少会话上下文（_session_id）")
	}
	return sid, nil
}

// Handler 工具处理函数：入参为已解析的 JSON 对象，返回可序列化结果。
type Handler func(ctx context.Context, args map[string]any) (any, error)

// All 返回全部工具的 schema + handler。
func All(d *Deps) []ToolSpec {
	return []ToolSpec{
		{Spec: searchPOISpec(), H: d.searchPOI},
		{Spec: planRouteSpec(), H: d.planRoute},
		{Spec: estimatePriceSpec(), H: d.estimatePrice},
		{Spec: listVehiclesSpec(), H: d.listVehicles},
		{Spec: createOrderSpec(), H: d.createOrder},
		{Spec: getOrderSpec(), H: d.getOrder},
		{Spec: listOrdersSpec(), H: d.listOrders},
		{Spec: cancelOrderSpec(), H: d.cancelOrder},
		{Spec: savedPlacesSpec(), H: d.savedPlaces},
	}
}

// ToolSpec 一份工具定义。
type ToolSpec struct {
	Spec mcp.Tool
	H    Handler
}

// HandlerOf 按名取 handler（离线引擎用）。
func HandlerOf(d *Deps, name string) (Handler, bool) {
	for _, t := range All(d) {
		if t.Spec.Name == name {
			return t.H, true
		}
	}
	return nil, false
}

// ---------- schema ----------

func searchPOISpec() mcp.Tool {
	return mcp.NewTool("search_poi",
		mcp.WithDescription("按关键词搜索地点（POI）：机场、车站、商圈、写字楼、小区等。"+
			"起终点必须来自本工具的返回结果（poi_id），禁止臆造地址。多个候选时要向用户确认。"),
		mcp.WithString("keyword", mcp.Description("地点关键词，如「首都机场」「望京SOHO」「南站」"), mcp.Required()),
		mcp.WithString("city", mcp.Description("城市过滤，如「北京」「上海」；可选")),
		mcp.WithNumber("limit", mcp.Description("返回条数，默认 5，最大 10")),
	)
}

func planRouteSpec() mcp.Tool {
	return mcp.NewTool("plan_route",
		mcp.WithDescription("规划路线：给出 2-3 条方案（推荐/少拥堵/少收费），"+
			"含距离、时长、过路费与简要步骤。参数必须是 search_poi 返回的 poi_id。"+
			"拿到 route_id 后才能 estimate_price 估价。"),
		mcp.WithString("pickup_poi_id", mcp.Description("上车点 poi_id（来自 search_poi）"), mcp.Required()),
		mcp.WithString("dropoff_poi_id", mcp.Description("目的地 poi_id（来自 search_poi）"), mcp.Required()),
		mcp.WithString("departure_time", mcp.Description("出发时间 RFC3339，如 2026-11-20T08:00:00+08:00；省略则用当前时间")),
	)
}

func estimatePriceSpec() mcp.Tool {
	return mcp.NewTool("estimate_price",
		mcp.WithDescription("对某条路线估价，返回报价（quote_id、价格明细、动态溢价原因）。"+
			"必须先 plan_route 拿到 route_id；报价 60 秒有效，过期须重新估价并告知用户。"+
			"报价需用户确认（前端确认按钮或明确回复确认）后，才能 create_order。"),
		mcp.WithString("route_id", mcp.Description("plan_route 返回的 route_id"), mcp.Required()),
		mcp.WithString("vehicle_class", mcp.Description("车型：taxi出租车/comfort快车/premium优享/pool拼车，默认 comfort")),
	)
}

func listVehiclesSpec() mcp.Tool {
	return mcp.NewTool("list_vehicle_classes",
		mcp.WithDescription("列出可选车型（出租车/快车/优享/拼车）与运力说明；"+
			"给了 route_id 时同时给出各车型预估价区间。"),
		mcp.WithString("route_id", mcp.Description("可选：plan_route 返回的 route_id，用于给各车型估价")),
	)
}

func createOrderSpec() mcp.Tool {
	return mcp.NewTool("create_order",
		mcp.WithDescription("下单：必须在用户明确确认报价之后调用。"+
			"以 quote_id 绑定服务端价格（schema 无 price 字段，价格不可自填）。"+
			"若被 GUARD:NEED_CONFIRM 拒绝，说明用户尚未确认——应请用户确认报价后再试，不要重试下单。"),
		mcp.WithString("quote_id", mcp.Description("estimate_price 返回的 quote_id"), mcp.Required()),
		mcp.WithString("idempotency_key", mcp.Description("幂等键（可选）：重复调用不会重复下单")),
	)
}

func getOrderSpec() mcp.Tool {
	return mcp.NewTool("get_order",
		mcp.WithDescription("查询单个订单详情：状态、司机、金额。只能查当前会话自己的订单。"),
		mcp.WithString("order_id", mcp.Description("订单号，来自 create_order 或 list_my_orders"), mcp.Required()),
	)
}

func listOrdersSpec() mcp.Tool {
	return mcp.NewTool("list_my_orders",
		mcp.WithDescription("列出当前用户的订单（最近优先）。用户问「我的订单/打到车了吗」时用。"),
		mcp.WithString("status", mcp.Description("状态过滤：created/dispatched/accepted/arriving/in_trip/completed/canceled；可选")),
		mcp.WithNumber("limit", mcp.Description("返回条数，默认 10")),
	)
}

func cancelOrderSpec() mcp.Tool {
	return mcp.NewTool("cancel_order",
		mcp.WithDescription("取消订单。服务端按状态计费：未接单免费；接单后超过免费窗口取消收 ¥5 取消费；"+
			"行程中不可取消（GUARD:NOT_CANCELLABLE）。取消费以返回结果为准，不要自己计算。"),
		mcp.WithString("order_id", mcp.Description("要取消的订单号"), mcp.Required()),
		mcp.WithString("reason", mcp.Description("取消原因，如「行程有变」")),
	)
}

func savedPlacesSpec() mcp.Tool {
	return mcp.NewTool("list_saved_places",
		mcp.WithDescription("读取用户保存的常用地址（家/公司）。用户说「回家」「去公司」时优先用。"),
	)
}

// ---------- handler 实现 ----------

func (d *Deps) searchPOI(ctx context.Context, args map[string]any) (any, error) {
	if _, err := sessionFrom(args); err != nil {
		return nil, err
	}
	kw, _ := args["keyword"].(string)
	if strings.TrimSpace(kw) == "" {
		return nil, fmt.Errorf("GUARD:ARGS_INVALID keyword 不能为空")
	}
	city, _ := args["city"].(string)
	limit := 5
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	pois := d.Map.SearchPOI(kw, city, limit)
	if len(pois) == 0 {
		return map[string]any{
			"pois":   []any{},
			"hint":   "没有找到匹配地点，请用户补充更具体的名称或城市",
			"found":  0,
		}, nil
	}
	return map[string]any{"pois": pois, "found": len(pois)}, nil
}

func (d *Deps) planRoute(ctx context.Context, args map[string]any) (any, error) {
	if _, err := sessionFrom(args); err != nil {
		return nil, err
	}
	pickID, _ := args["pickup_poi_id"].(string)
	dropID, _ := args["dropoff_poi_id"].(string)
	pickup, ok := d.Map.GetPOI(pickID)
	if !ok {
		return nil, fmt.Errorf("GUARD:UNKNOWN_POI pickup_poi_id=%q 不存在，请先 search_poi", pickID)
	}
	dropoff, ok := d.Map.GetPOI(dropID)
	if !ok {
		return nil, fmt.Errorf("GUARD:UNKNOWN_POI dropoff_poi_id=%q 不存在，请先 search_poi", dropID)
	}
	var dep time.Time
	if s, _ := args["departure_time"].(string); s != "" {
		t, err := time.Parse(time.RFC3339, s)
		if err != nil {
			return nil, fmt.Errorf("GUARD:ARGS_INVALID departure_time 需为 RFC3339 格式")
		}
		dep = t
	}
	route := d.Map.PlanRoute(pickup, dropoff, dep)
	return map[string]any{"route": route}, nil
}

func (d *Deps) estimatePrice(ctx context.Context, args map[string]any) (any, error) {
	sid, err := sessionFrom(args)
	if err != nil {
		return nil, err
	}
	rid, _ := args["route_id"].(string)
	route, ok := d.Map.GetRoute(rid)
	if !ok {
		return nil, fmt.Errorf("GUARD:UNKNOWN_ROUTE route_id=%q 不存在或已过期，请重新 plan_route", rid)
	}
	class, _ := args["vehicle_class"].(string)
	if class == "" {
		class = "comfort"
	}
	price, bd, surge, ok := d.Ride.Estimate(route, class, time.Now())
	if !ok {
		return nil, fmt.Errorf("GUARD:UNKNOWN_VEHICLE vehicle_class=%q 不在运力列表（taxi/comfort/premium/pool）", class)
	}
	v, _ := provider.FindVehicle(class)
	q := &provider.Quote{
		QuoteID:      fmt.Sprintf("q_%d", time.Now().UnixNano()%1e9),
		SessionID:    sid,
		RouteID:      rid,
		VehicleClass: class,
		VehicleName:  v.Name,
		PriceCents:   price,
		Breakdown:    bd,
		Surge:        surge,
		ExpiresAt:    d.now().Add(config.QuoteTTL),
		Pickup:       route.Pickup,
		Dropoff:      route.Dropoff,
	}
	if err := d.Store.InsertQuote(q); err != nil {
		return nil, fmt.Errorf("存报价失败: %w", err)
	}
	d.Audit.Log(audit.Record{Type: "tool_call", SessionID: sid, Actor: "ride-ops",
		Name: "estimate_price", OK: boolPtr(true),
		Detail: map[string]any{"quote_id": q.QuoteID, "price_cents": price, "surge": surge}})
	return map[string]any{
		"quote":             q,
		"note":              "报价 60 秒有效；需用户确认后才能 create_order",
		"surge_explanation": surgeNote(surge, route),
	}, nil
}

func surgeNote(surge float64, route *provider.Route) string {
	if surge <= 1.0 {
		return "当前无动态溢价"
	}
	switch {
	case isHubPOI(route.Pickup) || isHubPOI(route.Dropoff):
		return fmt.Sprintf("机场/火车站枢纽区域运力紧张，溢价 %.1f 倍", surge)
	default:
		return fmt.Sprintf("早晚高峰时段运力紧张，溢价 %.1f 倍", surge)
	}
}

func isHubPOI(p provider.POI) bool { return p.Category == "airport" || p.Category == "railway" }

func (d *Deps) listVehicles(ctx context.Context, args map[string]any) (any, error) {
	if _, err := sessionFrom(args); err != nil {
		return nil, err
	}
	rid, _ := args["route_id"].(string)
	type row struct {
		Class     string `json:"class"`
		Name      string `json:"name"`
		Estimate  *int   `json:"estimate_cents,omitempty"`
		Available bool   `json:"available"`
	}
	out := []row{}
	for _, v := range provider.VehicleCatalog {
		r := row{Class: v.Class, Name: v.Name, Available: true}
		if rid != "" {
			if route, ok := d.Map.GetRoute(rid); ok {
				if price, _, _, ok := d.Ride.Estimate(route, v.Class, time.Now()); ok {
					r.Estimate = &price
				}
			}
		}
		out = append(out, r)
	}
	return map[string]any{"vehicles": out}, nil
}

func (d *Deps) createOrder(ctx context.Context, args map[string]any) (any, error) {
	sid, err := sessionFrom(args)
	if err != nil {
		return nil, err
	}
	quoteID, _ := args["quote_id"].(string)
	if quoteID == "" {
		return nil, fmt.Errorf("GUARD:ARGS_INVALID quote_id 不能为空")
	}

	// —— 三道闸（全在服务端）——
	q, err := d.Store.GetQuote(quoteID)
	if err != nil {
		return nil, fmt.Errorf("GUARD:UNKNOWN_QUOTE quote_id=%q 不存在", quoteID)
	}
	// 闸 0：归属（白名单，同 lesson4.3）
	if q.SessionID != sid {
		d.Audit.Guardrail("", sid, "foreign_quote", fmt.Sprintf("quote=%s", quoteID))
		return nil, fmt.Errorf("GUARD:FOREIGN_ORDER 该报价不属于当前会话")
	}
	if q.ConsumedAt != nil {
		// 幂等：已下过单 → 返回已有订单，不重复创建
		if orders, e := d.Store.ListOrders(sid, "", 5); e == nil {
			for _, o := range orders {
				if o.QuoteID == quoteID {
					return map[string]any{"order": o, "idempotent": true}, nil
				}
			}
		}
		return nil, fmt.Errorf("GUARD:QUOTE_CONSUMED 报价已被使用，请重新估价")
	}
	// 闸 1：报价 TTL
	if d.now().After(q.ExpiresAt) {
		d.Audit.Guardrail("", sid, "quote_expired", fmt.Sprintf("quote=%s", quoteID))
		return nil, fmt.Errorf("GUARD:QUOTE_EXPIRED 报价已过期（60s），请重新 estimate_price 并告知用户")
	}
	// 闸 2：确认令牌（由 wrapper 注入；未确认则为空）
	token, _ := args["confirm_token"].(string)
	if token == "" || q.ConfirmToken == "" || token != q.ConfirmToken {
		d.Audit.Guardrail("", sid, "need_confirm", fmt.Sprintf("quote=%s token_len=%d", quoteID, len(token)))
		return nil, fmt.Errorf("GUARD:NEED_CONFIRM 报价尚未获得用户确认，不能下单；请向用户展示报价并等待其明确确认")
	}
	if d.now().After(q.TokenExpiresAt) {
		d.Audit.Guardrail("", sid, "token_expired", fmt.Sprintf("quote=%s", quoteID))
		return nil, fmt.Errorf("GUARD:NEED_CONFIRM 确认已失效（60s），请用户重新确认报价")
	}

	// —— 落单（价格以服务端 quote 为准，schema 无 price 字段）——
	now := d.now()
	vName := q.VehicleName
	if vName == "" { // 兜底：车型名以目录为准（quote 未持久化该列）
		if v, ok := provider.FindVehicle(q.VehicleClass); ok {
			vName = v.Name
		}
	}
	order := &provider.Order{
		OrderID:      fmt.Sprintf("DD%d", now.UnixNano()%1e10),
		SessionID:    sid,
		QuoteID:      quoteID,
		Pickup:       q.Pickup,
		Dropoff:      q.Dropoff,
		VehicleClass: q.VehicleClass,
		VehicleName:  vName,
		PriceCents:   q.PriceCents,
		Status:       provider.StatusCreated,
		DistanceM:    0,
		CreatedAt:    now,
		UpdatedAt:    now,
	}
	if route, ok := d.Map.GetRoute(q.RouteID); ok && len(route.Options) > 0 {
		order.DistanceM = route.Options[0].DistanceM
		order.DurationS = route.Options[0].DurationS
	}
	if err := d.Store.InsertOrder(order); err != nil {
		if err == store.ErrConflict {
			// 幂等键（quote_id 唯一）兜底：并发重试返回已有单
			if orders, e := d.Store.ListOrders(sid, "", 5); e == nil {
				for _, o := range orders {
					if o.QuoteID == quoteID {
						return map[string]any{"order": o, "idempotent": true}, nil
					}
				}
			}
		}
		return nil, fmt.Errorf("下单失败: %w", err)
	}
	_ = d.Store.ConsumeQuote(quoteID, now)
	d.Machine.Launch(order)
	d.Audit.OrderEvent("", sid, "order_created",
		fmt.Sprintf("order=%s quote=%s price=%d", order.OrderID, quoteID, order.PriceCents))
	return map[string]any{"order": order}, nil
}

func (d *Deps) getOrder(ctx context.Context, args map[string]any) (any, error) {
	sid, err := sessionFrom(args)
	if err != nil {
		return nil, err
	}
	id, _ := args["order_id"].(string)
	o, err := d.Store.GetOrder(id)
	if err != nil {
		return nil, fmt.Errorf("GUARD:UNKNOWN_ORDER order_id=%q 不存在", id)
	}
	if o.SessionID != sid {
		d.Audit.Guardrail("", sid, "foreign_order", fmt.Sprintf("order=%s", id))
		return nil, fmt.Errorf("GUARD:FOREIGN_ORDER 订单不属于当前会话")
	}
	return map[string]any{"order": o}, nil
}

func (d *Deps) listOrders(ctx context.Context, args map[string]any) (any, error) {
	sid, err := sessionFrom(args)
	if err != nil {
		return nil, err
	}
	status, _ := args["status"].(string)
	limit := 10
	if v, ok := args["limit"].(float64); ok && v > 0 {
		limit = int(v)
	}
	orders, err := d.Store.ListOrders(sid, status, limit)
	if err != nil {
		return nil, err
	}
	if orders == nil {
		orders = []*provider.Order{}
	}
	return map[string]any{"orders": orders}, nil
}

func (d *Deps) cancelOrder(ctx context.Context, args map[string]any) (any, error) {
	sid, err := sessionFrom(args)
	if err != nil {
		return nil, err
	}
	id, _ := args["order_id"].(string)
	reason, _ := args["reason"].(string)
	o, err := d.Store.GetOrder(id)
	if err != nil {
		return nil, fmt.Errorf("GUARD:UNKNOWN_ORDER order_id=%q 不存在", id)
	}
	if o.SessionID != sid {
		d.Audit.Guardrail("", sid, "foreign_order", fmt.Sprintf("cancel order=%s", id))
		return nil, fmt.Errorf("GUARD:FOREIGN_ORDER 订单不属于当前会话")
	}
	o, err = d.Machine.Cancel(id, reason, d.Ride)
	if err != nil {
		return nil, err // GUARD:NOT_CANCELLABLE 原样返回给模型转述
	}
	return map[string]any{"order": o,
		"note": fmt.Sprintf("已取消，取消费 ¥%.2f", float64(o.CancelFeeCents)/100)}, nil
}

func (d *Deps) savedPlaces(ctx context.Context, args map[string]any) (any, error) {
	sid, err := sessionFrom(args)
	if err != nil {
		return nil, err
	}
	places, err := d.Store.ListSavedPlaces(sid)
	if err != nil {
		return nil, err
	}
	return map[string]any{"saved_places": places}, nil
}

// EncodeResult 把 handler 结果编码为 MCP text（JSON 字符串）。
func EncodeResult(v any) (string, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func boolPtr(b bool) *bool { return &b }
