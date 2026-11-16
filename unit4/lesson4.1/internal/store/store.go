// Package store provides an in-memory mock business system for the customer
// service demo: orders, delivery tracking, products and refund policies.
// Everything is canned data so the lesson runs offline against any
// OpenAI-compatible LLM.
package store

import (
	"fmt"
	"strings"
	"sync"
)

// Order is a mock customer order.
type Order struct {
	ID       string
	SKU      string // product name
	Amount   float64
	Status   string // 待付款 / 待发货 / 已发货 / 已签收
	User     string
	Refunded bool
}

// TrackPoint is one entry of a delivery trace.
type TrackPoint struct {
	Time  string
	State string
	Desc  string
}

// Product is a purchasable item shown to the shopping-guide agent.
type Product struct {
	ID       string
	Name     string
	Category string
	Price    float64
}

// Policy is one entry of the refund policy base.
type Policy struct {
	Topic string
	Text  string
}

type memory struct {
	mu        sync.RWMutex
	orders    map[string]*Order
	delivery  map[string][]TrackPoint
	products  map[string]*Product
	policies  []Policy
	refunds   map[string]string // refund id -> order id
	refundSeq int
}

var mem = func() *memory {
	return &memory{
		orders: map[string]*Order{
			"O-1001": {ID: "O-1001", SKU: "无线蓝牙耳机 Air3", Amount: 199, Status: "待发货", User: "张三"},
			"O-1002": {ID: "O-1002", SKU: "电饭煲 F4", Amount: 349, Status: "已发货", User: "李四"},
			"O-1003": {ID: "O-1003", SKU: "运动跑鞋 R2", Amount: 429, Status: "已签收", User: "王五"},
			"O-1004": {ID: "O-1004", SKU: "智能手表 W1", Amount: 599, Status: "已签收", User: "赵六"},
			"O-1005": {ID: "O-1005", SKU: "空气炸锅 A9", Amount: 259, Status: "待付款", User: "张三"},
		},
		delivery: map[string][]TrackPoint{
			"O-1002": {
				{Time: "10-28 09:12", State: "已揽收", Desc: "深圳分拨中心已揽收"},
				{Time: "10-29 22:40", State: "运输中", Desc: "到达长沙转运中心"},
				{Time: "10-30 08:05", State: "派送中", Desc: "派送员老周，电话 138****0002"},
			},
			"O-1003": {
				{Time: "10-25 10:00", State: "已揽收", Desc: "义乌分拨中心已揽收"},
				{Time: "10-26 19:30", State: "运输中", Desc: "到达上海转运中心"},
				{Time: "10-27 14:20", State: "已签收", Desc: "本人签收"},
			},
			"O-1004": {
				{Time: "10-15 11:00", State: "已揽收", Desc: "东莞分拨中心已揽收"},
				{Time: "10-17 16:30", State: "已签收", Desc: "本人签收"},
			},
		},
		products: map[string]*Product{
			"P-2001": {ID: "P-2001", Name: "无线蓝牙耳机 Air3", Category: "数码", Price: 199},
			"P-2002": {ID: "P-2002", Name: "跑步机 T50", Category: "运动器材", Price: 2999},
			"P-2003": {ID: "P-2003", Name: "保温杯 550ml", Category: "生活百货", Price: 59},
			"P-2004": {ID: "P-2004", Name: "机械键盘 K87", Category: "数码", Price: 259},
			"P-2005": {ID: "P-2005", Name: "电煮锅 D1", Category: "厨房电器", Price: 119},
			"P-2006": {ID: "P-2006", Name: "人体工学椅 E500", Category: "办公家具", Price: 899},
		},
		policies: []Policy{
			{Topic: "七天无理由", Text: "自签收之日起 7 天内，商品不影响二次销售的，支持七天无理由退货。"},
			{Topic: "退款时限", Text: "退款审核通过后 1~3 个工作日原路退回；如有运费争议最长 7 个工作日。"},
			{Topic: "特殊类目", Text: "生鲜、定制刻字、贴身衣物等类目不支持七天无理由，仅支持质量问题的退换。"},
			{Topic: "运费规则", Text: "质量问题退货商家承担运费；七天无理由退货用户承担往返运费。"},
			{Topic: "未支付订单", Text: "待付款订单尚未产生交易，无需退款，30 分钟未支付自动关闭。"},
			{Topic: "退货运费险", Text: "参加运费险活动的订单，退货成功后自动理赔 10 元以内运费。"},
		},
		refunds: map[string]string{},
	}
}()

// ErrNotFound is returned when an order or product id does not exist.
type ErrNotFound struct{ What string }

func (e ErrNotFound) Error() string { return e.What }

// orderIDsLocked lists all mock order ids; callers must hold the lock.
func orderIDsLocked() string {
	ids := make([]string, 0, len(mem.orders))
	for id := range mem.orders {
		ids = append(ids, id)
	}
	return strings.Join(ids, ", ")
}

// GetOrder returns a copy of the order for the given id.
func GetOrder(orderID string) (*Order, error) {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	mem.mu.RLock()
	o, ok := mem.orders[orderID]
	mem.mu.RUnlock()
	if !ok {
		mem.mu.RLock()
		hint := orderIDsLocked()
		mem.mu.RUnlock()
		return nil, ErrNotFound{fmt.Sprintf("订单不存在，请联系用户提供有效订单号（本店订单号示例：%s）", hint)}
	}
	cp := *o
	return &cp, nil
}

// GetDelivery returns the delivery trace of an order.
func GetDelivery(orderID string) ([]TrackPoint, error) {
	if _, err := GetOrder(orderID); err != nil {
		return nil, err
	}
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	return mem.delivery[orderID], nil
}

// SubmitRefund validates the business rules and creates a refund ticket.
// It is deliberately strict so the demo can show guard rails: only paid
// orders that were not already refunded can be refunded.
func SubmitRefund(orderID, reason string) (string, error) {
	if strings.TrimSpace(reason) == "" {
		return "", fmt.Errorf("退款原因不能为空，请先向用户确认退款原因")
	}
	mem.mu.Lock()
	defer mem.mu.Unlock()
	o, ok := mem.orders[orderID]
	if !ok {
		return "", ErrNotFound{fmt.Sprintf("订单不存在，本店订单号示例：%s", orderIDsLocked())}
	}
	if o.Status == "待付款" {
		return "", fmt.Errorf("订单 %s 尚未支付，无需退款（按政策自动关闭）", orderID)
	}
	if o.Refunded {
		return "", fmt.Errorf("订单 %s 已退款过，不能重复发起", orderID)
	}
	mem.refundSeq++
	refundID := fmt.Sprintf("R-%d", mem.refundSeq)
	mem.refunds[refundID] = orderID
	o.Refunded = true
	return refundID, nil
}

// SearchProducts filters the catalogue by keyword and optional budget.
// Empty keyword returns up to limit items.
func SearchProducts(keyword string, maxPrice float64, limit int) []*Product {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	out := make([]*Product, 0, len(mem.products))
	for _, p := range mem.products {
		if p.Price > maxPrice {
			continue
		}
		if keyword != "" &&
			!strings.Contains(strings.ToLower(p.Name), strings.ToLower(keyword)) &&
			!strings.Contains(strings.ToLower(p.Category), strings.ToLower(keyword)) {
			continue
		}
		out = append(out, p)
		if limit > 0 && len(out) >= limit {
			break
		}
	}
	return out
}

// GetProduct returns a product by id.
func GetProduct(productID string) (*Product, error) {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	p, ok := mem.products[productID]
	if !ok {
		return nil, ErrNotFound{fmt.Sprintf("商品不存在，请提供有效商品ID（示例：P-2001）")}
	}
	cp := *p
	return &cp, nil
}

// GetPolicy returns the policy text for a topic, or all topics when empty.
func GetPolicy(topic string) []Policy {
	mem.mu.RLock()
	defer mem.mu.RUnlock()
	if topic == "" {
		return mem.policies
	}
	var out []Policy
	for _, p := range mem.policies {
		if strings.Contains(p.Topic, topic) || strings.Contains(topic, p.Topic) {
			out = append(out, p)
		}
	}
	return out
}
