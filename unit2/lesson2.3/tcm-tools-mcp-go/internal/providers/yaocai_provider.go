package providers

import "github.com/lihuaizhi/ainewma/unit2/lession2.3/tcm-tools-mcp-go/internal/domain"

// YaocaiSearchOpts 药材搜索选项
type YaocaiSearchOpts struct {
	Query          string
	PropertyFlavor string
	Meridian       interface{} // string or []string
	Efficacy       string
	Indication     string
	SourceType     domain.YaocaiSourceType
	Exact          bool
	Fuzzy          bool
	Page           int
	Limit          int
	SortBy         string // name|pinyin|relevance
	SortOrder      string // asc|desc
}

// YaocaiProvider 药材 Provider 接口
type YaocaiProvider interface {
	GetMeta() ProviderMeta
	Init() error
	Health() bool
	Dispose()
	Search(opts YaocaiSearchOpts) (SearchResult[domain.Yaocai], error)
	GetByID(id string) (*domain.Yaocai, error)
	GetByName(name string) (*domain.Yaocai, error)
}
