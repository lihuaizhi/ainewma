package store

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

// Hit is one search result.
type Hit struct {
	ID         string
	Text       string
	ParentText string
	Volume     string
	ChapterNo  int64
	ClauseNo   int64
	Formulas   []string
	PageNo     int64
	Score      float32
}

// Context returns the text used to build the LLM prompt: the parent window for
// small-to-big chunks, otherwise the chunk itself.
func (h Hit) Context() string {
	if h.ParentText != "" {
		return h.ParentText
	}
	return h.Text
}

// Search runs an ANN search over the vector field. filter is a Milvus boolean
// expression (may be empty), e.g. `volume == "辨太阳病脉证并治法上"` or
// `formula_text like "%桂枝汤%"`.
func (s *Store) Search(ctx context.Context, name string, vec []float32, topK int, filter string) ([]Hit, error) {
	opt := milvusclient.NewSearchOption(name, topK, []entity.Vector{entity.FloatVector(vec)}).
		WithOutputFields(FieldText, FieldParentText, FieldVolume, FieldChapterNo, FieldClauseNo, FieldFormula, FieldPageNo)
	if filter != "" {
		opt = opt.WithFilter(filter)
	}

	resultSets, err := s.cli.Search(ctx, opt)
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}

	var hits []Hit
	for _, rs := range resultSets {
		for i := 0; i < rs.Len(); i++ {
			hits = append(hits, Hit{
				ID:         idAt(rs, i),
				Text:       stringAt(rs, FieldText, i),
				ParentText: stringAt(rs, FieldParentText, i),
				Volume:     stringAt(rs, FieldVolume, i),
				ChapterNo:  intAt(rs, FieldChapterNo, i),
				ClauseNo:   intAt(rs, FieldClauseNo, i),
				Formulas:   jsonAt(rs, FieldFormula, i),
				PageNo:     intAt(rs, FieldPageNo, i),
				Score:      rs.Scores[i],
			})
		}
	}
	return hits, nil
}

func idAt(rs milvusclient.ResultSet, i int) string {
	if rs.IDs == nil {
		return ""
	}
	if v, err := rs.IDs.Get(i); err == nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func stringAt(rs milvusclient.ResultSet, field string, i int) string {
	col := rs.GetColumn(field)
	if col == nil {
		return ""
	}
	if v, err := col.Get(i); err == nil {
		if s, ok := v.(string); ok {
			return s
		}
	}
	return ""
}

func intAt(rs milvusclient.ResultSet, field string, i int) int64 {
	col := rs.GetColumn(field)
	if col == nil {
		return 0
	}
	if v, err := col.Get(i); err == nil {
		if n, ok := v.(int64); ok {
			return n
		}
	}
	return 0
}

func jsonAt(rs milvusclient.ResultSet, field string, i int) []string {
	col := rs.GetColumn(field)
	if col == nil {
		return nil
	}
	v, err := col.Get(i)
	if err != nil {
		return nil
	}
	var raw []byte
	switch t := v.(type) {
	case []byte:
		raw = t
	case string:
		raw = []byte(t)
	default:
		return nil
	}
	var formulas []string
	if err := json.Unmarshal(raw, &formulas); err != nil {
		return nil
	}
	return formulas
}
