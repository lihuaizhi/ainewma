package providers

// ProviderMeta Provider 元信息
type ProviderMeta struct {
	ID           string   `json:"id"`
	Name         string   `json:"name"`
	Version      string   `json:"version,omitempty"`
	Enabled      bool     `json:"enabled"`
	Capabilities []string `json:"capabilities,omitempty"`
}

// SearchResult 分页结果
type SearchResult[T any] struct {
	Items   []T  `json:"items"`
	Total   int  `json:"total"`
	Page    int  `json:"page"`
	Limit   int  `json:"limit"`
	HasMore bool `json:"hasMore,omitempty"`
}
