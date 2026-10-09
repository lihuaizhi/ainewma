package domain

// YaocaiSourceType 药材来源类型
type YaocaiSourceType string

const (
	YaocaiSourcePlant   YaocaiSourceType = "plant"
	YaocaiSourceAnimal  YaocaiSourceType = "animal"
	YaocaiSourceMineral YaocaiSourceType = "mineral"
	YaocaiSourceOther   YaocaiSourceType = "other"
)

// Yaocai 药材
type Yaocai struct {
	ID               string           `json:"id"`                        // 唯一 ID
	Name             string           `json:"name"`                      // 药材名
	Aliases          []string         `json:"aliases,omitempty"`         // 别名
	Pinyin           string           `json:"pinyin,omitempty"`          // 拼音
	Initials         string           `json:"initials,omitempty"`        // 首字母
	LatinName        string           `json:"latinName,omitempty"`       // 拉丁学名
	PropertyFlavor   string           `json:"propertyFlavor,omitempty"`  // 性味
	Meridian         []string         `json:"meridian,omitempty"`        // 归经
	Efficacy         string           `json:"efficacy,omitempty"`        // 功效
	Indication       string           `json:"indication,omitempty"`      // 主治
	Dosage           string           `json:"dosage,omitempty"`          // 用法用量
	Toxicity         string           `json:"toxicity,omitempty"`        // 毒性
	ProcessingCommon []string         `json:"processingCommon,omitempty"` // 炮制方式
	SourceType       YaocaiSourceType `json:"sourceType,omitempty"`      // 来源类型
	Origin           string           `json:"origin,omitempty"`          // 基原
	Caution          string           `json:"caution,omitempty"`         // 注意事项
	Notes            string           `json:"notes,omitempty"`           // 备注
	Tags             []string         `json:"tags,omitempty"`            // 标签
	ProviderID       string           `json:"providerId,omitempty"`      // 数据来源
}
