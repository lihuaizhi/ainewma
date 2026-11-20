// Package provider 定义打车业务的领��类型与两个 Mock Provider：
// MapProvider（POI 搜索/路线规划）与 RideProvider（计价/车型），
// 外加订单状态机（machine.go）。接口形态对齐真实地图/出行开放平台，
// 后续可替换为高德 API 与真实运力方（方案 §一）。
package provider

import "time"

// POI 地点。
type POI struct {
	ID       string  `json:"id"`
	Name     string  `json:"name"`
	Address  string  `json:"address"`
	Lng      float64 `json:"lng"`
	Lat      float64 `json:"lat"`
	City     string  `json:"city"`
	Category string  `json:"category"`
}

// RouteOption 一条路线方案。
type RouteOption struct {
	Label      string   `json:"label"` // 推荐 | 少拥堵 | 少收费
	DistanceM  int      `json:"distance_m"`
	DurationS  int      `json:"duration_s"`
	TollCents  int      `json:"toll_cents"`
	Steps      []string `json:"steps"`
}

// Route plan_route 的产物：一次规划产出 2-3 条方案。
type Route struct {
	RouteID   string        `json:"route_id"`
	Pickup    POI           `json:"pickup"`
	Dropoff   POI           `json:"dropoff"`
	Options   []RouteOption `json:"options"`
	Departure time.Time     `json:"departure"`
}

// Breakdown 价格明细（分）。surge 单独放在 Quote 上也放一份，方便展示。
type Breakdown struct {
	FlagfallCents int     `json:"flagfall_cents"`
	DistanceCents int     `json:"distance_cents"`
	TimeCents     int     `json:"time_cents"`
	RemoteCents   int     `json:"remote_cents"` // 远途费（>20km 部分）
	TollCents     int     `json:"toll_cents"`
	DiscountCents int     `json:"discount_cents"`
	Surge         float64 `json:"surge"`
}

// Quote estimate_price 的产物：服务端存价，60s TTL。
type Quote struct {
	QuoteID      string    `json:"quote_id"`
	SessionID    string    `json:"session_id"`
	RouteID      string    `json:"route_id"`
	VehicleClass string    `json:"vehicle_class"` // taxi | comfort | premium | pool
	VehicleName  string    `json:"vehicle_name"`
	PriceCents   int       `json:"price_cents"`
	Breakdown    Breakdown `json:"breakdown"`
	Surge        float64   `json:"surge"`
	ExpiresAt    time.Time `json:"expires_at"`
	Confirmed    bool      `json:"confirmed"`

	// 服务端私有字段（不下发给模型/前端）。
	Pickup          POI        `json:"-"`
	Dropoff         POI        `json:"-"`
	ConfirmToken    string     `json:"-"`
	TokenExpiresAt  time.Time  `json:"-"`
	ConsumedAt      *time.Time `json:"-"`
}

// Driver 司机（mock）。
type Driver struct {
	Name   string  `json:"name"`
	Plate  string  `json:"plate"`
	Car    string  `json:"car"`
	Rating float64 `json:"rating"`
	ETAS   int     `json:"eta_s"` // 距上车点秒数
}

// 订单状态。
const (
	StatusCreated   = "created"   // 已下单，待派单
	StatusDispatched = "dispatched" // 派单中
	StatusAccepted  = "accepted"  // 司机已接单
	StatusArriving  = "arriving"  // 前往上车点
	StatusInTrip    = "in_trip"   // 行程中
	StatusCompleted = "completed" // 已完成
	StatusCanceled  = "canceled"  // 已取消
)

// Order 订单（面板与状态机的主体）。
type Order struct {
	OrderID        string    `json:"order_id"`
	SessionID      string    `json:"session_id"`
	QuoteID        string    `json:"quote_id"`
	Pickup         POI       `json:"pickup"`
	Dropoff        POI       `json:"dropoff"`
	VehicleClass   string    `json:"vehicle_class"`
	VehicleName    string    `json:"vehicle_name"`
	PriceCents     int       `json:"price_cents"`
	Status         string    `json:"status"`
	Driver         *Driver   `json:"driver"`
	CancelFeeCents int       `json:"cancel_fee_cents"`
	DistanceM      int       `json:"distance_m"`
	DurationS      int       `json:"duration_s"`
	CreatedAt      time.Time `json:"created_at"`
	UpdatedAt      time.Time `json:"updated_at"`
	// AcceptedAt 接单时刻（免费取消窗口起算）。
	AcceptedAt *time.Time `json:"-"`
}

// OrderEvent 状态机推进事件（gateway 轮询 /internal/events 拿增量后广播前端）。
type OrderEvent struct {
	Seq        int64   `json:"seq"`
	Event      string  `json:"event"` // status_changed
	PrevStatus string  `json:"prev_status"`
	Order      *Order  `json:"order"`
}

// 车型定义。
type VehicleClass struct {
	Class    string  `json:"class"`
	Name     string  `json:"name"`
	Mult     float64 `json:"mult"`     // 整体系数
	Flagfall int     `json:"flagfall"` // 起步价（分）
	IncludeKm float64 `json:"include_km"` // 起步含公里
	DistancePerKm int `json:"distance_per_km"` // 里程价（分/km）
	PerMinute int    `json:"per_minute"`       // 时长费（分/min）
	Surgeable bool   `json:"surgeable"`        // 是否参与动态溢价
}

// VehicleCatalog mock 运力目录。
var VehicleCatalog = []VehicleClass{
	{Class: "taxi", Name: "出租车", Mult: 1.0, Flagfall: 1300, IncludeKm: 3, DistancePerKm: 240, PerMinute: 50, Surgeable: false},
	{Class: "comfort", Name: "快车", Mult: 1.0, Flagfall: 1300, IncludeKm: 3, DistancePerKm: 230, PerMinute: 50, Surgeable: true},
	{Class: "premium", Name: "优享", Mult: 1.35, Flagfall: 1600, IncludeKm: 3, DistancePerKm: 260, PerMinute: 60, Surgeable: true},
	{Class: "pool", Name: "拼车", Mult: 0.75, Flagfall: 1000, IncludeKm: 3, DistancePerKm: 180, PerMinute: 40, Surgeable: false},
}

// FindVehicle 按 class 查车型；未知返回 false（GUARD:UNKNOWN_VEHICLE）。
func FindVehicle(class string) (VehicleClass, bool) {
	for _, v := range VehicleCatalog {
		if v.Class == class {
			return v, true
		}
	}
	return VehicleClass{}, false
}
