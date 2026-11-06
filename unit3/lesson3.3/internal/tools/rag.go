package tools

import (
	"context"
	"fmt"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/adapter"
	l2retrieve "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/retrieve"
	l2store "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/store"
)

type SearchArgs struct {
	Query   string `json:"query"`
	Mode    string `json:"mode,omitempty"`
	TopK    int    `json:"topk,omitempty"`
	Recall  int    `json:"recall,omitempty"`
	Rerank  string `json:"rerank,omitempty"`
	Volume  string `json:"volume,omitempty"`
	Formula string `json:"formula,omitempty"`
}

type SearchResult struct {
	Hits    []l2store.Hit `json:"hits"`
	Count   int           `json:"count"`
	Summary string        `json:"summary"`
}

func SearchKnowledge(a *adapter.Adapter, args SearchArgs) (SearchResult, error) {
	if strings.TrimSpace(args.Query) == "" {
		return SearchResult{}, fmt.Errorf("query empty")
	}
	opts := l2retrieve.Options{
		Filter:  l2retrieve.BuildFilter(args.Volume, args.Formula),
		Volume:  args.Volume,
		Formula: args.Formula,
		Recall:  args.Recall,
		TopK:    args.TopK,
		Hybrid:  args.Mode == "hybrid",
		RRFK:    60,
	}
	if args.Mode == "vector" {
		opts.Hybrid = false
	}
	if args.TopK == 0 {
		opts.TopK = 5
	}
	if args.Recall == 0 {
		opts.Recall = 30
	}
	hits, err := a.Retriever().Retrieve(context.Background(), args.Query, opts)
	if err != nil {
		return SearchResult{}, err
	}
	var sb strings.Builder
	sb.WriteString(fmt.Sprintf("hits=%d", len(hits)))
	if len(hits) > 0 {
		sb.WriteString(fmt.Sprintf(" top1=%s p%d score=%.3f", hits[0].Volume, hits[0].PageNo, hits[0].Score))
	}
	return SearchResult{Hits: hits, Count: len(hits), Summary: sb.String()}, nil
}
