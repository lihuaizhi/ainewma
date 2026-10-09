package providers

import (
	"sort"
	"strings"

	"github.com/lihuaizhi/ainewma/unit2/lession2.3/tcm-tools-mcp-go/internal/domain"
	"github.com/lihuaizhi/ainewma/unit2/lession2.3/tcm-tools-mcp-go/internal/utils"
)

type LocalYaocaiProvider struct {
	meta    ProviderMeta
	dataDir string
	data    []domain.Yaocai
}

func NewLocalYaocaiProvider(id, name string, enabled bool, dataDir string) *LocalYaocaiProvider {
	return &LocalYaocaiProvider{
		meta: ProviderMeta{
			ID:           id,
			Name:         name,
			Version:      "0.1.0",
			Enabled:      enabled,
			Capabilities: []string{"search", "getById", "getByName"},
		},
		dataDir: dataDir,
		data:    []domain.Yaocai{},
	}
}

func (p *LocalYaocaiProvider) GetMeta() ProviderMeta {
	return p.meta
}

func (p *LocalYaocaiProvider) Init() error {
	loaded := utils.UnmarshalJSON[domain.Yaocai]("yaocai.json", p.dataDir, []domain.Yaocai{})
	m := map[string]domain.Yaocai{}
	for _, item := range loaded {
		if item.ID != "" {
			m[item.ID] = item
		}
	}
	p.data = make([]domain.Yaocai, 0, len(m))
	for _, v := range m {
		p.data = append(p.data, v)
	}
	return nil
}

func (p *LocalYaocaiProvider) Health() bool { return true }
func (p *LocalYaocaiProvider) Dispose()     { p.data = []domain.Yaocai{} }

func (p *LocalYaocaiProvider) Search(opts YaocaiSearchOpts) (SearchResult[domain.Yaocai], error) {
	list := make([]domain.Yaocai, len(p.data))
	copy(list, p.data)

	if opts.PropertyFlavor != "" {
		pf := utils.NormalizeText(opts.PropertyFlavor)
		tmp := []domain.Yaocai{}
		for _, y := range list {
			if utils.ContainsMatch(utils.NormalizeText(y.PropertyFlavor), pf) {
				tmp = append(tmp, y)
			}
		}
		list = tmp
	}

	if opts.Meridian != nil {
		mArr := utils.ToStringArray(opts.Meridian)
		if len(mArr) > 0 {
			tmp := []domain.Yaocai{}
			for _, y := range list {
				yM := utils.ToStringArray(y.Meridian)
				match := false
				for _, need := range mArr {
					nm := utils.NormalizeText(need)
					for _, has := range yM {
						hm := utils.NormalizeText(has)
						if utils.ContainsMatch(hm, nm) || hm == nm {
							match = true
							break
						}
					}
					if match {
						break
					}
				}
				if match {
					tmp = append(tmp, y)
				}
			}
			list = tmp
		}
	}

	if opts.Efficacy != "" {
		ef := utils.NormalizeText(opts.Efficacy)
		tmp := []domain.Yaocai{}
		for _, y := range list {
			if utils.ContainsMatch(utils.NormalizeText(y.Efficacy), ef) {
				tmp = append(tmp, y)
			}
		}
		list = tmp
	}

	if opts.Indication != "" {
		ind := utils.NormalizeText(opts.Indication)
		tmp := []domain.Yaocai{}
		for _, y := range list {
			if utils.ContainsMatch(utils.NormalizeText(y.Indication), ind) {
				tmp = append(tmp, y)
			}
		}
		list = tmp
	}

	if opts.SourceType != "" {
		st := opts.SourceType
		tmp := []domain.Yaocai{}
		for _, y := range list {
			if y.SourceType == st {
				tmp = append(tmp, y)
			}
		}
		list = tmp
	}

	if opts.Query != "" {
		q := strings.TrimSpace(opts.Query)
		nq := utils.NormalizeText(q)
		if opts.Exact {
			tmp := []domain.Yaocai{}
			for _, y := range list {
				if utils.NormalizeText(y.Name) == nq {
					tmp = append(tmp, y)
					continue
				}
				for _, a := range y.Aliases {
					if utils.NormalizeText(a) == nq {
						tmp = append(tmp, y)
						break
					}
				}
			}
			list = tmp
		} else {
			tmp := []domain.Yaocai{}
			for _, y := range list {
				h := utils.BuildHaystack(y.Name, y.Aliases, y.Pinyin, y.Initials, y.LatinName, y.PropertyFlavor, y.Meridian, y.Efficacy, y.Indication, y.Origin, y.Notes, y.Tags)
				if utils.ContainsMatch(h, nq) {
					tmp = append(tmp, y)
				}
			}
			list = tmp
		}
	}

	sort.Slice(list, func(i, j int) bool {
		if opts.SortBy == "relevance" && opts.Query != "" {
			qn := utils.NormalizeText(opts.Query)
			hi := utils.BuildHaystack(list[i].Name, list[i].Aliases, list[i].Pinyin, list[i].Initials, list[i].LatinName, list[i].Efficacy, list[i].Indication, list[i].Tags)
			hj := utils.BuildHaystack(list[j].Name, list[j].Aliases, list[j].Pinyin, list[j].Initials, list[j].LatinName, list[j].Efficacy, list[j].Indication, list[j].Tags)
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
		} else if opts.SortBy == "pinyin" {
			pi := utils.NormalizeText(list[i].Pinyin)
			if pi == "" {
				pi = utils.NormalizeText(list[i].Name)
			}
			pj := utils.NormalizeText(list[j].Pinyin)
			if pj == "" {
				pj = utils.NormalizeText(list[j].Name)
			}
			if opts.SortOrder == "desc" {
				return pi > pj
			}
			return pi < pj
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
	items := []domain.Yaocai{}
	if start < end {
		items = list[start:end]
	}
	hasMore := end < total

	return SearchResult[domain.Yaocai]{
		Items:   items,
		Total:   total,
		Page:    page,
		Limit:   limit,
		HasMore: hasMore,
	}, nil
}

func (p *LocalYaocaiProvider) GetByID(id string) (*domain.Yaocai, error) {
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

func (p *LocalYaocaiProvider) GetByName(name string) (*domain.Yaocai, error) {
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
