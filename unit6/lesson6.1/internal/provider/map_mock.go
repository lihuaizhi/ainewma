package provider

import (
	"fmt"
	"math"
	"strings"
	"sync"
	"time"
)

// MapProvider 地图能力接口（Mock 实现见 MapMock；真实实现可换高德 Web 服务）。
type MapProvider interface {
	SearchPOI(keyword, city string, limit int) []POI
	GetPOI(id string) (POI, bool)
	PlanRoute(pickup, dropoff POI, departure time.Time) *Route
	GetRoute(id string) (*Route, bool)
}

// poiSeed 种子数据：北京主要机场/车站/商圈/写字楼/小区（真实近似坐标）。
var poiSeed = []POI{
	{ID: "poi_cap_t3", Name: "首都机场T3航站楼", Address: "首都机场路", Lng: 116.584, Lat: 40.052, City: "北京", Category: "airport"},
	{ID: "poi_cap_t2", Name: "首都机场T2航站楼", Address: "首都机场路", Lng: 116.590, Lat: 40.068, City: "北京", Category: "airport"},
	{ID: "poi_daxing", Name: "大兴国际机场", Address: "大兴区榆垡镇", Lng: 116.410, Lat: 39.509, City: "北京", Category: "airport"},
	{ID: "poi_south_rail", Name: "北京南站", Address: "丰台区永外大街", Lng: 116.378, Lat: 39.865, City: "北京", Category: "railway"},
	{ID: "poi_beijing_rail", Name: "北京站", Address: "东城区毛家湾胡同甲13号", Lng: 116.427, Lat: 39.902, City: "北京", Category: "railway"},
	{ID: "poi_west_rail", Name: "北京西站", Address: "丰台区广莲路甲2号", Lng: 116.322, Lat: 39.895, City: "北京", Category: "railway"},
	{ID: "poi_wangjing_soho", Name: "望京SOHO", Address: "朝阳区望京街10号", Lng: 116.461, Lat: 39.996, City: "北京", Category: "office"},
	{ID: "poi_wangjing_xiyuan", Name: "望京西园四区", Address: "朝阳区望京西园", Lng: 116.455, Lat: 40.004, City: "北京", Category: "residential"},
	{ID: "poi_oliolive", Name: "融科橄榄城", Address: "朝阳区望京东园四区", Lng: 116.472, Lat: 40.001, City: "北京", Category: "residential"},
	{ID: "poi_guomao", Name: "国贸写字楼", Address: "朝阳区建国门外大街1号", Lng: 116.461, Lat: 39.909, City: "北京", Category: "office"},
	{ID: "poi_china_zun", Name: "中国尊", Address: "朝阳区光华路", Lng: 116.464, Lat: 39.912, City: "北京", Category: "office"},
	{ID: "poi_sanlitun", Name: "三里屯太古里", Address: "朝阳区三里屯路19号", Lng: 116.455, Lat: 39.937, City: "北京", Category: "mall"},
	{ID: "poi_xidan", Name: "西单大悦城", Address: "西城区西单北大街131号", Lng: 116.373, Lat: 39.907, City: "北京", Category: "mall"},
	{ID: "poi_tiananmen", Name: "天安门广场", Address: "东城区长安街", Lng: 116.397, Lat: 39.908, City: "北京", Category: "landmark"},
	{ID: "poi_financial_st", Name: "金融街购物中心", Address: "西城区金城坊街2号", Lng: 116.366, Lat: 39.912, City: "北京", Category: "mall"},
	{ID: "poi_zhongguancun", Name: "中关村鼎好大厦", Address: "海淀区中关村大街", Lng: 116.316, Lat: 39.983, City: "北京", Category: "office"},
	{ID: "poi_baidu", Name: "百度大厦", Address: "海淀区上地信息路", Lng: 116.310, Lat: 40.043, City: "北京", Category: "office"},
	{ID: "poi_tencent_bj", Name: "腾讯北京总部", Address: "海淀区西北旺东路", Lng: 116.305, Lat: 40.043, City: "北京", Category: "office"},
	{ID: "poi_jd_zz", Name: "京东总部", Address: "大兴区经海路", Lng: 116.506, Lat: 39.795, City: "北京", Category: "office"},
	{ID: "poi_shangdi", Name: "上地七街", Address: "海淀区上地七街", Lng: 116.310, Lat: 40.042, City: "北京", Category: "residential"},
	{ID: "poi_huilongguan", Name: "回龙观东大街", Address: "昌平区回龙观", Lng: 116.333, Lat: 40.070, City: "北京", Category: "residential"},
	{ID: "poi_tiantongyuan", Name: "天通苑北", Address: "昌平区天通苑", Lng: 116.417, Lat: 40.078, City: "北京", Category: "residential"},
	{ID: "poi_tongzhou_wanda", Name: "通州万达广场", Address: "通州区新华西街", Lng: 116.663, Lat: 39.902, City: "北京", Category: "mall"},
	{ID: "poi_yizhuang", Name: "亦庄创意生活广场", Address: "大兴区宏达北路", Lng: 116.506, Lat: 39.799, City: "北京", Category: "mall"},
	{ID: "poi_cao_yang", Name: "朝阳大悦城", Address: "朝阳区朝阳北路101号", Lng: 116.486, Lat: 39.926, City: "北京", Category: "mall"},
	{ID: "poi_wangfujing", Name: "王府井百货大楼", Address: "东城区王府井大街253号", Lng: 116.410, Lat: 39.909, City: "北京", Category: "mall"},
	{ID: "poi_galaxy_so", Name: "银河SOHO", Address: "东城区小街口", Lng: 116.428, Lat: 39.919, City: "北京", Category: "office"},
	{ID: "poi_cap_hotel", Name: "首都机场朗豪酒店", Address: "首都机场四纬路", Lng: 116.590, Lat: 40.045, City: "北京", Category: "hotel"},
	{ID: "poi_center_hotel", Name: "国贸大酒店", Address: "朝阳区建国门外大街1号", Lng: 116.459, Lat: 39.908, City: "北京", Category: "hotel"},
	{ID: "poi_babaop", Name: "宝钞胡同", Address: "东城区宝钞胡同", Lng: 116.406, Lat: 39.942, City: "北京", Category: "residential"},
	{ID: "poi_shichahai", Name: "什刹海", Address: "西城区前海西街", Lng: 116.384, Lat: 39.934, City: "北京", Category: "landmark"},
	{ID: "poi_olympic", Name: "奥体中心", Address: "朝阳区安定路", Lng: 116.396, Lat: 39.982, City: "北京", Category: "landmark"},
	{ID: "poi_qianmen", Name: "前门大街", Address: "东城区前门大街", Lng: 116.398, Lat: 39.893, City: "北京", Category: "landmark"},
	{ID: "poi_cbd_couple", Name: "建外SOHO", Address: "朝阳区东三环中路", Lng: 116.458, Lat: 39.904, City: "北京", Category: "office"},
	{ID: "poi_wj_north", Name: "望京北园", Address: "朝阳区望京北园", Lng: 116.466, Lat: 40.014, City: "北京", Category: "residential"},
	{ID: "poi_shanghai_hub", Name: "上海虹桥火车站", Address: "闵行区申贵路1500号", Lng: 121.320, Lat: 31.194, City: "上海", Category: "railway"},
	{ID: "poi_shanghai_air", Name: "上海浦东国际机场", Address: "浦东新区迎宾大道", Lng: 121.805, Lat: 31.143, City: "上海", Category: "airport"},
	{ID: "poi_shanghai_bd", Name: "上海中心大厦", Address: "浦东新区陆家嘴环路", Lng: 121.505, Lat: 31.235, City: "上海", Category: "office"},
	{ID: "poi_shanghai_njw", Name: "南京路步行街", Address: "黄浦区南京东路", Lng: 121.480, Lat: 31.234, City: "上海", Category: "mall"},
}

// MapMock 纯计算 + 种子数据的 MapProvider。
type MapMock struct {
	mu    sync.Mutex
	routes map[string]*Route // route_id → Route（供 estimate_price 取用）
	seq   int
}

// NewMapMock 构造 mock 地图。
func NewMapMock() *MapMock {
	return &MapMock{routes: map[string]*Route{}}
}

// SearchPOI 关键词匹配 name/address/category/别名；支持"首都机场"命中 T3/T2。
func (m *MapMock) SearchPOI(keyword, city string, limit int) []POI {
	kw := strings.ToLower(strings.TrimSpace(keyword))
	if limit <= 0 || limit > 10 {
		limit = 5
	}
	var out []POI
	for _, p := range poiSeed {
		if city != "" && p.City != city {
			continue
		}
		if kw == "" {
			out = append(out, p)
			continue
		}
		hay := strings.ToLower(p.Name + " " + p.Address + " " + p.Category + " " + aliasOf(p))
		if strings.Contains(hay, kw) || fuzzyScore(kw, p.Name) > 0 {
			out = append(out, p)
		}
		if len(out) >= limit {
			break
		}
	}
	return out
}

// aliasOf 中文别名，提升模糊命中（"首都机场"→T3；"南站"→北京南站等）。
func aliasOf(p POI) string {
	switch p.ID {
	case "poi_cap_t3":
		return "首都机场 北京首都国际机场 T3 航站楼"
	case "poi_cap_t2":
		return "首都机场 T2"
	case "poi_daxing":
		return "大兴机场 新机场"
	case "poi_south_rail":
		return "南站 北京南"
	case "poi_beijing_rail":
		return "北京站 老火车站"
	case "poi_west_rail":
		return "西站"
	case "poi_wangjing_soho":
		return "望京"
	case "poi_guomao":
		return "国贸 CBD"
	}
	return ""
}

// fuzzyScore 极简模糊：关键词的每个字都在名字里即算命中（去空格后）。
func fuzzyScore(kw, name string) int {
	kw = strings.ReplaceAll(kw, " ", "")
	if kw == "" || len([]rune(kw)) < 2 {
		return 0
	}
	n := 0
	for _, r := range kw {
		if strings.ContainsRune(name, r) {
			n++
		}
	}
	if n == len([]rune(kw)) {
		return 1
	}
	return 0
}

// PlanRoute 计算 3 条方案：推荐（均衡）/ 少拥堵（更快更贵）/ 少收费（绕行免过路费）。
// 距离 = Haversine × 路网系数 1.35；时长按时段速度换算。
func (m *MapMock) PlanRoute(pickup, dropoff POI, departure time.Time) *Route {
	if departure.IsZero() {
		departure = time.Now()
	}
	base := haversineM(pickup.Lat, pickup.Lng, dropoff.Lat, dropoff.Lng) * 1.35
	speed := speedKmh(departure) // 平峰 35 / 早晚高峰 22 / 夜间 45

	pickRoute := func(label string, dist float64, spd float64, toll bool) RouteOption {
		km := dist / 1000
		dur := int(km / spd * 3600)
		if dur < 300 {
			dur = 300 // 至少 5 分钟
		}
		opt := RouteOption{
			Label:     label,
			DistanceM: int(dist),
			DurationS: dur,
			Steps:     buildSteps(pickup, dropoff, label),
		}
		if toll {
			opt.TollCents = tollFor(km)
		}
		return opt
	}

	opts := []RouteOption{
		pickRoute("推荐", base, speed, true),
		pickRoute("少拥堵", base*1.08, speed*1.18, true),
		pickRoute("少收费", base*1.12, speed*0.95, false),
	}

	m.mu.Lock()
	m.seq++
	rid := fmt.Sprintf("rt_%d_%d", time.Now().UnixNano()%1e7, m.seq)
	r := &Route{RouteID: rid, Pickup: pickup, Dropoff: dropoff, Options: opts, Departure: departure}
	m.routes[rid] = r
	// 控制缓存规模
	if len(m.routes) > 256 {
		for k := range m.routes {
			delete(m.routes, k)
			break
		}
	}
	m.mu.Unlock()
	return r
}

// GetPOI 按 id 取地点。
func (m *MapMock) GetPOI(id string) (POI, bool) {
	for _, p := range poiSeed {
		if p.ID == id {
			return p, true
		}
	}
	return POI{}, false
}

// GetRoute 取缓存路线（estimate_price 用）。
func (m *MapMock) GetRoute(id string) (*Route, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.routes[id]
	return r, ok
}

func haversineM(lat1, lng1, lat2, lng2 float64) float64 {
	const R = 6371000.0
	toRad := func(d float64) float64 { return d * math.Pi / 180 }
	dLat := toRad(lat2 - lat1)
	dLng := toRad(lng2 - lng1)
	a := math.Sin(dLat/2)*math.Sin(dLat/2) +
		math.Cos(toRad(lat1))*math.Cos(toRad(lat2))*math.Sin(dLng/2)*math.Sin(dLng/2)
	return 2 * R * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// speedKmh 时段速度：早晚高峰 22、平峰 35、夜间 45 km/h。
func speedKmh(t time.Time) float64 {
	h := t.Hour()
	if (h >= 7 && h <= 9) || (h >= 17 && h <= 19) {
		return 22
	}
	if h >= 23 || h <= 5 {
		return 45
	}
	return 35
}

// tollFor 过路费：20km 内免，之后每 10km +6 元（机场高速整段 10 元）。
func tollFor(km float64) int {
	if km <= 20 {
		return 0
	}
	extra := int((km - 20) / 10)
	return (extra + 1) * 600
}

func buildSteps(pickup, dropoff POI, label string) []string {
	return []string{
		fmt.Sprintf("从%s出发", pickup.Name),
		fmt.Sprintf("途经城市主干道（%s方案）", label),
		fmt.Sprintf("到达%s", dropoff.Name),
	}
}
