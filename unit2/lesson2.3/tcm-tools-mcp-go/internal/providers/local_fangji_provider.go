package providers

import (
	"sort"
	"strings"

	"github.com/lihuaizhi/ainewma/unit2/lession2.3/tcm-tools-mcp-go/internal/domain"
	"github.com/lihuaizhi/ainewma/unit2/lession2.3/tcm-tools-mcp-go/internal/utils"
)

type LocalFangjiProvider struct {
	meta    ProviderMeta
	dataDir string
	data    []domain.Fangji
}

func NewLocalFangjiProvider(id, name string, enabled bool, dataDir string) *LocalFangjiProvider {
	return &LocalFangjiProvider{
		meta: ProviderMeta{
			ID:           id,
			Name:         name,
			Version:      "0.1.0",
			Enabled:      enabled,
			Capabilities: []string{"search", "getById", "getByName"},
		},
		dataDir: dataDir,
		data:    []domain.Fangji{},
	}
}

func (p *LocalFangjiProvider) GetMeta() ProviderMeta {
	return p.meta
}

func (p *LocalFangjiProvider) Init() error {
	loaded := utils.UnmarshalJSON[domain.Fangji]("fangji.json", p.dataDir, []domain.Fangji{})
	m := map[string]domain.Fangji{}
	for _, item := range loaded {
		if item.ID != "" {
			m[item.ID] = item
		}
	}
	p.data = make([]domain.Fangji, 0, len(m))
	for _, v := range m {
		p.data = append(p.data, v)
	}
	return nil
}

func (p *LocalFangjiProvider) Health() bool { return true }
func (p *LocalFangjiProvider) Dispose()     { p.data = []domain.Fangji{} }

func (p *LocalFangjiProvider) Search(opts FangjiSearchOpts) (SearchResult[domain.Fangji], error) {
	list := make([]domain.Fangji, len(p.data))
	copy(list, p.data)

	if opts.HerbName != "" {
		hn := utils.NormalizeText(opts.HerbName)
		tmp := []domain.Fangji{}
		for _, f := range list {
			for _, c := range f.Composition {
				if utils.ContainsMatch(utils.NormalizeText(c.HerbName), hn) {
					tmp = append(tmp, f)
					break
				}
			}
		}
		list = tmp
	}

	if opts.HerbID != "" {
		hid := strings.TrimSpace(opts.HerbID)
		tmp := []domain.Fangji{}
		for _, f := range list {
			for _, c := range f.Composition {
				if c.HerbID == hid {
					tmp = append(tmp, f)
					break
				}
			}
		}
		list = tmp
	}

	if opts.Source != "" {
		s := utils.NormalizeText(opts.Source)
		tmp := []domain.Fangji{}
		for _, f := range list {
			if utils.ContainsMatch(utils.NormalizeText(f.Source), s) {
				tmp = append(tmp, f)
			}
		}
		list = tmp
	}

	if opts.Category != "" {
		c := utils.NormalizeText(opts.Category)
		tmp := []domain.Fangji{}
		for _, f := range list {
			if utils.ContainsMatch(utils.NormalizeText(f.Category), c) {
				tmp = append(tmp, f)
			}
		}
		list = tmp
	}

	if opts.Query != "" {
		q := strings.TrimSpace(opts.Query)
		nq := utils.NormalizeText(q)
		if opts.Exact {
			tmp := []domain.Fangji{}
			for _, f := range list {
				if utils.NormalizeText(f.Name) == nq {
					tmp = append(tmp, f)
					continue
				}
				for _, a := range f.Aliases {
					if utils.NormalizeText(a) == nq {
						tmp = append(tmp, f)
						break
					}
				}
			}
			list = tmp
		} else {
			tmp := []domain.Fangji{}
			for _, f := range list {
				h := utils.BuildHaystack(f.Name, f.Aliases, f.Pinyin, f.Initials, f.Source, f.Category, f.Efficacy, f.Indication, f.Notes, f.Tags)
				if utils.ContainsMatch(h, nq) {
					tmp = append(tmp, f)
				}
			}
			list = tmp
		}
	}

	sort.Slice(list, func(i, j int) bool {
		if opts.SortBy == "relevance" && opts.Query != "" {
			qn := utils.NormalizeText(opts.Query)
			hi := utils.BuildHaystack(list[i].Name, list[i].Aliases, list[i].Pinyin, list[i].Initials, list[i].Source, list[i].Efficacy, list[i].Indication, list[i].Tags)
			hj := utils.BuildHaystack(list[j].Name, list[j].Aliases, list[j].Pinyin, list[j].Initials, list[j].Source, list[j].Efficacy, list[j].Indication, list[j].Tags)
			si := utils.ScoreRelevance(hi, qn)
			sj := utils.ScoreRelevance(hj, qn)
			if si != sj {
				return si > sj
			}
		} else if opts.SortBy == "name" {
			ni := utils.NormalizeText(list[i].Name)
			nj := utils.NormalizeText(list[j].Name)
			if opts.SortOrder == "desc" {
				return ni > nj
			}
			return ni < nj
		} else if opts.SortBy == "source" {
			si := utils.NormalizeText(list[i].Source)
			sj := utils.NormalizeText(list[j].Source)
			if si == sj {
				ni := utils.NormalizeText(list[i].Name)
				nj := utils.NormalizeText(list[j].Name)
				if opts.SortOrder == "desc" {
					return ni > nj
				}
				return ni < nj
			}
			if opts.SortOrder == "desc" {
				return si > sj
			}
			return si < sj
		}
		ni2 := utils.NormalizeText(list[i].Name)
		nj2 := utils.NormalizeText(list[j].Name)
		if opts.SortOrder == "desc" {
			return ni2 > nj2
		}
		return ni2 < nj2
	})

	total := len(list)
	page := opts.Page
	if page < 1 {
		page = 1
	}
	limit := opts.Limit
	if limit < 1 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	start := (page - 1) * limit
	end := start + limit
	if start > total {
		start = total
	}
	if end > total {
		end = total
	}
	items := []domain.Fangji{}
	if start < end {
		items = list[start:end]
	}
	hasMore := end < total

	return SearchResult[domain.Fangji]{
		Items:   items,
		Total:   total,
		Page:    page,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (p *LocalFangjiProvider) GetByID(id string) (*domain.Fangji, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	for i := range p.data {
		if p.data[i].ID == id {
			v := p.data[i]
			return &v, nil
		}
	}
	return nil, nil
}

func (p *LocalFangjiProvider) GetByName(name string) (*domain.Fangji, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, nil
	}
	n := utils.NormalizeText(name)
	for i := range p.data {
		if utils.NormalizeText(p.data[i].Name) == n {
			v := p.data[i]
			return &v, nil
		}
		for _, a := range p.data[i].Aliases {
			if utils.NormalizeText(a) == n {
				v := p.data[i]
				return &v, nil
			}
		}
	}
	return nil, nil
}
