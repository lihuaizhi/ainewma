package provider

import (
	"fmt"
	"math"
	"time"
)

// RideProvider 出行能力接口：计价与取消费规则（Mock 实现见本文件）。
type RideProvider interface {
	Estimate(route *Route, vehicleClass string, departure time.Time) (int, Breakdown, float64, bool)
	CancelFee(status string, acceptedAt *time.Time, now time.Time) (int, error)
}

// RideMock 计价引擎：起步价 + 里程 + 时长 + 远途 + 过路费，乘动态溢价与车型系数。
// 公式与常数见方案 §6.2（贴近真实快车计价，面试可讲推导）。
type RideMock struct {
	// FreeWindow 接单后免费取消窗口（按演示倍速缩放，见 cmd/ride-mcp）。
	FreeWindow time.Duration
}

// NewRideMock 构造（freeWindow<=0 时按真实 2 分钟）。
func NewRideMock(freeWindow time.Duration) *RideMock {
	if freeWindow <= 0 {
		freeWindow = 2 * time.Minute
	}
	return &RideMock{FreeWindow: freeWindow}
}

// Estimate 对推荐方案估价。ok=false 表示车型未知（GUARD:UNKNOWN_VEHICLE）。
func (r *RideMock) Estimate(route *Route, vehicleClass string, departure time.Time) (int, Breakdown, float64, bool) {
	v, ok := FindVehicle(vehicleClass)
	if !ok || route == nil || len(route.Options) == 0 {
		return 0, Breakdown{}, 1.0, false
	}
	opt := route.Options[0] // 推荐方案计价
	km := float64(opt.DistanceM) / 1000
	min := float64(opt.DurationS) / 60

	bd := Breakdown{
		FlagfallCents: v.Flagfall,
		DistanceCents: int(math.Max(0, km-v.IncludeKm) * float64(v.DistancePerKm)),
		TimeCents:     int(min * float64(v.PerMinute)),
		RemoteCents:   0,
		TollCents:     opt.TollCents,
		DiscountCents: 0,
	}
	if km > 20 {
		// 远途费：>20km 部分加收 20% 里程价。
		bd.RemoteCents = int((km - 20) * 0.2 * float64(v.DistancePerKm))
	}
	surge := surgeFor(route, departure, v.Surgeable)
	bd.Surge = surge

	subtotal := bd.FlagfallCents + bd.DistanceCents + bd.TimeCents +
		bd.RemoteCents + bd.TollCents
	price := int(math.Round(float64(subtotal) * surge * v.Mult))
	if price < 900 { // 兜底起步消费
		price = 900
	}
	return price, bd, surge, true
}

// surgeFor 动态溢价：枢纽（机场/火车站）1.2、早晚高峰 1.2，取较高者，封顶 1.5；
// 不可溢价车型（出租车/拼车）恒为 1.0。
func surgeFor(route *Route, departure time.Time, surgeable bool) float64 {
	if !surgeable {
		return 1.0
	}
	surge := 1.0
	if isHub(route.Pickup) || isHub(route.Dropoff) {
		surge = 1.2
	}
	h := departure.Hour()
	if (h >= 7 && h <= 9) || (h >= 17 && h <= 19) {
		if 1.2 > surge {
			surge = 1.2
		}
		// 高峰叠加枢纽 = 1.44，按 1.5 封顶展示为 1.4。
		if isHub(route.Pickup) || isHub(route.Dropoff) {
			surge = 1.4
		}
	}
	if surge > 1.5 {
		surge = 1.5
	}
	return surge
}

func isHub(p POI) bool {
	return p.Category == "airport" || p.Category == "railway"
}

// CancelFee 取消费规则（服务端判定，模型只转述）：
//   - 未接单（created/dispatched）：免费
//   - 已接单未上车（accepted/arriving）：免费窗口内免费，之后 ¥5
//   - 行程中/终态：不可取消 → GUARD:NOT_CANCELLABLE
func (r *RideMock) CancelFee(status string, acceptedAt *time.Time, now time.Time) (int, error) {
	switch status {
	case StatusCreated, StatusDispatched:
		return 0, nil
	case StatusAccepted, StatusArriving:
		if acceptedAt == nil || now.Sub(*acceptedAt) < r.FreeWindow {
			return 0, nil
		}
		return 500, nil // ¥5
	default:
		return 0, fmt.Errorf("GUARD:NOT_CANCELLABLE 订单状态为 %s，不支持取消", status)
	}
}
