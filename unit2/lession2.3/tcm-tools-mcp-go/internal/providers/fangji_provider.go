package providers

import "github.com/lihuaizhi/ainewma/unit2/lession2.3/tcm-tools-mcp-go/internal/domain"

// FangjiSearchOpts 方剂搜索选项
type FangjiSearchOpts struct {
	Query     string
	HerbName  string
	HerbID    string
	Source    string
	Category  string
	Exact     bool
	Fuzzy     bool
	Page      int
	Limit     int
	SortBy    string // name|source|relevance
	SortOrder string // asc|desc
}

// FangjiProvider 方剂 Provider 接口
type FangjiProvider interface {
	GetMeta() ProviderMeta
	Init() error
	Health() bool
	Dispose()
	Search(opts FangjiSearchOpts) (SearchResult[domain.Fangji], error)
	GetByID(id string) (*domain.Fangji, error)
	GetByName(name string) (*domain.Fangji, error)
}
