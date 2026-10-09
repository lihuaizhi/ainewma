package domain

// FangjiCompositionItem 方剂组成项
type FangjiCompositionItem struct {
	HerbName   string `json:"herbName"`             // 药材名称
	HerbID     string `json:"herbId,omitempty"`     // 药材 ID
	Dosage     string `json:"dosage,omitempty"`     // 剂量
	Processing string `json:"processing,omitempty"` // 炮制方式
}

// Fangji 方剂
type Fangji struct {
	ID                string                  `json:"id"`                        // 唯一 ID
	Name              string                  `json:"name"`                      // 方剂名
	Aliases           []string                `json:"aliases,omitempty"`         // 别名
	Pinyin            string                  `json:"pinyin,omitempty"`          // 拼音
	Initials          string                  `json:"initials,omitempty"`        // 首字母
	Source            string                  `json:"source,omitempty"`          // 出处
	Category          string                  `json:"category,omitempty"`        // 分类
	Efficacy          string                  `json:"efficacy,omitempty"`        // 功效
	Indication        string                  `json:"indication,omitempty"`      // 主治
	Composition       []FangjiCompositionItem `json:"composition"`               // 组成
	Usage             string                  `json:"usage,omitempty"`           // 用法用量
	Contraindication  string                  `json:"contraindication,omitempty"` // 禁忌
	Notes             string                  `json:"notes,omitempty"`           // 备注
	Tags              []string                `json:"tags,omitempty"`            // 标签
	ProviderID        string                  `json:"providerId,omitempty"`      // 数据来源
}
