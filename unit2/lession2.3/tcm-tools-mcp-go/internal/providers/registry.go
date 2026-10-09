package providers

import "sync"

type ProviderRegistry struct {
	mu              sync.RWMutex
	fangjiProviders map[string]FangjiProvider
	yaocaiProviders map[string]YaocaiProvider
	defaultFangjiID string
	defaultYaocaiID string
}

func NewProviderRegistry() *ProviderRegistry {
	return &ProviderRegistry{
		fangjiProviders: map[string]FangjiProvider{},
		yaocaiProviders: map[string]YaocaiProvider{},
	}
}

func (r *ProviderRegistry) RegisterFangji(p FangjiProvider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	meta := p.GetMeta()
	if meta.Enabled {
		r.fangjiProviders[meta.ID] = p
	}
	if r.defaultFangjiID == "" && meta.Enabled {
		r.defaultFangjiID = meta.ID
	}
}

func (r *ProviderRegistry) RegisterYaocai(p YaocaiProvider) {
	if p == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	meta := p.GetMeta()
	if meta.Enabled {
		r.yaocaiProviders[meta.ID] = p
	}
	if r.defaultYaocaiID == "" && meta.Enabled {
		r.defaultYaocaiID = meta.ID
	}
}

func (r *ProviderRegistry) SetDefaultFangji(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.fangjiProviders[id]; ok {
		r.defaultFangjiID = id
	}
}

func (r *ProviderRegistry) SetDefaultYaocai(id string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, ok := r.yaocaiProviders[id]; ok {
		r.defaultYaocaiID = id
	}
}

func (r *ProviderRegistry) GetFangji(id ...string) FangjiProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	target := r.defaultFangjiID
	if len(id) > 0 && id[0] != "" {
		target = id[0]
	}
	if p, ok := r.fangjiProviders[target]; ok && p.GetMeta().Enabled {
		return p
	}
	return nil
}

func (r *ProviderRegistry) GetYaocai(id ...string) YaocaiProvider {
	r.mu.RLock()
	defer r.mu.RUnlock()
	target := r.defaultYaocaiID
	if len(id) > 0 && id[0] != "" {
		target = id[0]
	}
	if p, ok := r.yaocaiProviders[target]; ok && p.GetMeta().Enabled {
		return p
	}
	return nil
}

func (r *ProviderRegistry) ListFangji() []ProviderMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]ProviderMeta, 0, len(r.fangjiProviders))
	for _, p := range r.fangjiProviders {
		res = append(res, p.GetMeta())
	}
	return res
}

func (r *ProviderRegistry) ListYaocai() []ProviderMeta {
	r.mu.RLock()
	defer r.mu.RUnlock()
	res := make([]ProviderMeta, 0, len(r.yaocaiProviders))
	for _, p := range r.yaocaiProviders {
		res = append(res, p.GetMeta())
	}
	return res
}

func (r *ProviderRegistry) GetDefaultFangjiID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultFangjiID
}

func (r *ProviderRegistry) GetDefaultYaocaiID() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.defaultYaocaiID
}
