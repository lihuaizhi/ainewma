// Package retrieve glues the embedder, the vector store, an optional BM25 arm
// and an optional re-ranker into the single retrieval step used by both ask
// and eval.
package retrieve

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/bm25"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/embed"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/rerank"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/store"
)

// Retriever performs retrieval with an optional second stage.
//
// Two modes share the same Options:
//   - vector:   pure ANN search (the lesson3.2 baseline);
//   - hybrid:   BM25 + vector arms fused with RRF, then optional re-ranking.
type Retriever struct {
	store      *store.Store
	embedder   *embed.Client
	collection string
	reranker   rerank.Reranker

	mu      sync.Mutex
	allHits []store.Hit
	bmIndex *bm25.Index
}

// New builds a Retriever. reranker may be nil to skip re-ranking.
func New(st *store.Store, e *embed.Client, collection string, r rerank.Reranker) *Retriever {
	return &Retriever{store: st, embedder: e, collection: collection, reranker: r}
}

// Options controls a single retrieval call.
type Options struct {
	// Filter is a Milvus boolean expression for the vector arm (may be empty).
	Filter string
	// Volume and Formula mirror BuildFilter, but as structured values: the
	// vector arm turns them into a Milvus expression and the BM25 arm applies
	// the same constraint in memory.
	Volume  string
	Formula string
	// Recall is the candidate pool size per retrieval arm.
	Recall int
	// TopK is how many results are returned (and fed to the LLM).
	TopK int
	// Hybrid fuses a BM25 arm with the vector arm using RRF.
	Hybrid bool
	// RRFK is the RRF fusion constant (default 60).
	RRFK int
}

// Retrieve embeds the query, pulls a candidate pool from the vector index (and
// the BM25 index when hybrid), merges the rankings and optionally re-ranks the
// pool down to TopK.
func (r *Retriever) Retrieve(ctx context.Context, query string, opts Options) ([]store.Hit, error) {
	if opts.Recall < opts.TopK {
		opts.Recall = opts.TopK
	}
	if opts.Hybrid {
		return r.retrieveHybrid(ctx, query, opts)
	}
	return r.retrieveVector(ctx, query, opts)
}

// retrieveVector is the pure ANN path (vector -> optional rerank).
func (r *Retriever) retrieveVector(ctx context.Context, query string, opts Options) ([]store.Hit, error) {
	vecs, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embed query: empty vector")
	}

	hits, err := r.store.Search(ctx, r.collection, vecs[0], opts.Recall, opts.Filter)
	if err != nil {
		return nil, err
	}
	if len(hits) == 0 {
		return nil, nil
	}
	return r.finish(ctx, query, hits, opts)
}

// retrieveHybrid runs the vector and BM25 arms over the same chunk surface,
// fuses them with RRF and hands the fused pool to the optional re-ranker.
func (r *Retriever) retrieveHybrid(ctx context.Context, query string, opts Options) ([]store.Hit, error) {
	ix, all, err := r.bm25(ctx)
	if err != nil {
		return nil, fmt.Errorf("build bm25 index: %w", err)
	}

	// BM25 arm: the query is cheap to score locally, so no embedding needed.
	keep := func(d bm25.Doc) bool {
		if opts.Volume != "" && d.Volume != opts.Volume {
			return false
		}
		if opts.Formula != "" && !strings.Contains(d.FormulaText, opts.Formula) {
			return false
		}
		return true
	}
	bmHits := ix.Search(query, opts.Recall, keep)
	bmRank := make([]string, len(bmHits))
	for i, h := range bmHits {
		bmRank[i] = h.Doc.ID
	}

	// Vector arm: embed the query, ANN search, keep only the ids for RRF.
	vecs, err := r.embedder.Embed(ctx, []string{query})
	if err != nil {
		return nil, fmt.Errorf("embed query: %w", err)
	}
	if len(vecs) == 0 {
		return nil, fmt.Errorf("embed query: empty vector")
	}
	vectorHits, err := r.store.Search(ctx, r.collection, vecs[0], opts.Recall, opts.Filter)
	if err != nil {
		return nil, err
	}
	vectorRank := make([]string, len(vectorHits))
	for i, h := range vectorHits {
		vectorRank[i] = h.ID
	}

	// Fuse the two rankings, then map ids back to full hits. Vector-arm hits
	// keep their ANN score; BM25-only hits have no score (0).
	byID := make(map[string]store.Hit, len(all))
	for _, h := range all {
		byID[h.ID] = h
	}
	vecByID := make(map[string]store.Hit, len(vectorHits))
	for _, h := range vectorHits {
		vecByID[h.ID] = h
	}
	fused := rrfFuse([][]string{vectorRank, bmRank}, opts.RRFK)
	merged := make([]store.Hit, 0, len(fused))
	for _, id := range fused {
		if h, ok := vecByID[id]; ok {
			merged = append(merged, h)
			continue
		}
		if h, ok := byID[id]; ok {
			merged = append(merged, h)
		}
	}
	// Keep re-rank cost bounded: the fused pool is capped at the same size as
	// the vector-only candidate pool.
	if len(merged) > opts.Recall {
		merged = merged[:opts.Recall]
	}
	if len(merged) == 0 {
		return nil, nil
	}
	return r.finish(ctx, query, merged, opts)
}

// finish applies the optional re-ranker (or trims to TopK directly).
func (r *Retriever) finish(ctx context.Context, query string, hits []store.Hit, opts Options) ([]store.Hit, error) {
	if r.reranker == nil || r.reranker.Name() == "none" {
		return trim(hits, opts.TopK), nil
	}

	docs := make([]string, len(hits))
	for i, h := range hits {
		docs[i] = h.Text
	}
	order, err := r.reranker.Rerank(ctx, query, docs, opts.TopK)
	if err != nil {
		// Fall back to the fused order rather than failing the whole query.
		return trim(hits, opts.TopK), fmt.Errorf("rerank (fallback to fused order): %w", err)
	}
	reranked := make([]store.Hit, 0, len(order))
	for _, i := range order {
		if i >= 0 && i < len(hits) {
			reranked = append(reranked, hits[i])
		}
	}
	return reranked, nil
}

// bm25 lazily builds (once) the BM25 index over every chunk in the collection.
func (r *Retriever) bm25(ctx context.Context) (*bm25.Index, []store.Hit, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.bmIndex != nil {
		return r.bmIndex, r.allHits, nil
	}
	all, err := r.store.All(ctx, r.collection)
	if err != nil {
		return nil, nil, err
	}
	docs := make([]bm25.Doc, len(all))
	for i, h := range all {
		docs[i] = bm25.Doc{
			ID:          h.ID,
			Text:        h.Text,
			ParentText:  h.ParentText,
			Volume:      h.Volume,
			FormulaText: strings.Join(h.Formulas, ","),
		}
	}
	ix := bm25.New(docs)
	r.allHits = all
	r.bmIndex = ix
	return ix, all, nil
}

// BuildFilter assembles a Milvus expression from the optional volume and
// formula constraints.
func BuildFilter(volume, formula string) string {
	var parts []string
	if volume != "" {
		parts = append(parts, fmt.Sprintf("%s == %q", store.FieldVolume, volume))
	}
	if formula != "" {
		parts = append(parts, fmt.Sprintf("%s like %q", store.FieldFormulaText, "%"+formula+"%"))
	}
	return strings.Join(parts, " && ")
}

func trim(hits []store.Hit, topK int) []store.Hit {
	if topK > 0 && len(hits) > topK {
		return hits[:topK]
	}
	return hits
}
