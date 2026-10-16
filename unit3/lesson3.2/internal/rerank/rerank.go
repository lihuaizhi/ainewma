// Package rerank contains optional second-stage re-rankers.
//
// Retrieval first takes a wide candidate pool (e.g. top-30) from the vector
// index, then a re-ranker reorders that pool and keeps the best few. A
// cross-encoder is usually the single biggest quality win in a RAG pipeline.
package rerank

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/bm25"
)

// Reranker reorders docs for a query and returns the indices of the best topN,
// in order. It never changes doc contents.
type Reranker interface {
	Name() string
	Rerank(ctx context.Context, query string, docs []string, topN int) ([]int, error)
}

// New builds a re-ranker by kind: "none", "lexical", "bm25", "tei" or "llama".
// url is only used by the network kinds "tei" and "llama" (e.g. http://localhost:8080).
func New(kind, url string) Reranker {
	switch kind {
	case "lexical":
		return Lexical{}
	case "bm25":
		return Bm25Ranker{}
	case "tei":
		return NewTEI(url)
	case "llama":
		return NewLlama(url)
	default:
		return Noop{}
	}
}

// Noop keeps the original vector order.
type Noop struct{}

func (Noop) Name() string { return "none" }

func (Noop) Rerank(_ context.Context, _ string, docs []string, topN int) ([]int, error) {
	return firstN(len(docs), topN), nil
}

// Lexical is a dependency-free baseline re-ranker. It scores a document by how
// many of the query's character bigrams it contains (plus an exact-match
// bonus). It is not a cross-encoder, but it runs offline and shows how much a
// second stage can help even without a model.
type Lexical struct{}

func (Lexical) Name() string { return "lexical" }

func (Lexical) Rerank(_ context.Context, query string, docs []string, topN int) ([]int, error) {
	qg := bigrams(query)
	type scored struct {
		idx   int
		score float64
	}
	scores := make([]scored, len(docs))
	for i, d := range docs {
		dg := bigrams(d)
		inter := 0
		for g := range qg {
			if dg[g] {
				inter++
			}
		}
		s := 0.0
		if len(qg) > 0 {
			s = float64(inter) / float64(len(qg))
		}
		if query != "" && strings.Contains(d, query) {
			s += 1.0
		}
		scores[i] = scored{idx: i, score: s}
	}
	sort.SliceStable(scores, func(a, b int) bool { return scores[a].score > scores[b].score })
	n := min(topN, len(scores))
	out := make([]int, n)
	for i := 0; i < n; i++ {
		out[i] = scores[i].idx
	}
	return out, nil
}

// Bm25Ranker re-scores the candidate pool with the very same BM25 scorer the
// hybrid retrieval arm uses (internal/bm25), so the "lexical" baseline can be
// upgraded without pulling in a model or a server.
//
// It differs from Lexical in three ways that matter for ranking: it applies
// IDF (a rare match counts more than a ubiquitous character like 之 or 病),
// saturates term frequency (the 100th occurrence barely adds anything) and
// normalises by document length (a long chunk no longer wins just by having
// more text). Lexical, by contrast, is a plain unweighted bigram-overlap ratio.
//
// Like Lexical it is purely lexical: it cannot bridge a semantic gap, so a
// paraphrased query with no literal overlap is still missable.
//
// A re-ranker only ever sees one candidate pool at a time, so the corpus
// statistics (document frequency, average length) are recomputed over the pool
// itself rather than a global index. Pool docs that share no term with the
// query score 0 and are appended after the matches, keeping their input order.
type Bm25Ranker struct{}

func (Bm25Ranker) Name() string { return "bm25" }

func (Bm25Ranker) Rerank(_ context.Context, query string, docs []string, topN int) ([]int, error) {
	// Index the pool using each doc's position as its id, so hits map straight
	// back to indices in the caller's slice.
	pool := make([]bm25.Doc, len(docs))
	for i, d := range docs {
		pool[i] = bm25.Doc{ID: strconv.Itoa(i), Text: d}
	}
	// topN=0 disables BM25's own limit: we want every match, and slice to topN
	// ourselves after appending the non-matches.
	hits := bm25.New(pool).Search(query, 0, nil)

	out := make([]int, 0, len(pool))
	matched := make([]bool, len(pool))
	for _, h := range hits {
		i, err := strconv.Atoi(h.Doc.ID)
		if err != nil || i < 0 || i >= len(pool) || matched[i] {
			continue
		}
		matched[i] = true
		out = append(out, i)
	}
	for i := range pool {
		if !matched[i] {
			out = append(out, i)
		}
	}
	return capTopN(out, topN), nil
}

// TEI calls a HuggingFace text-embeddings-inference server's /rerank endpoint,
// e.g. `text-embeddings-router --model-id BAAI/bge-reranker-v2-m3 --port 8080`.
type TEI struct {
	url  string
	http *http.Client
}

// NewTEI creates a TEI re-ranker. url is the server base URL.
func NewTEI(url string) *TEI {
	return &TEI{url: strings.TrimRight(url, "/"), http: &http.Client{Timeout: 60 * time.Second}}
}

func (t *TEI) Name() string { return "tei" }

type teiRequest struct {
	Query     string   `json:"query"`
	Texts     []string `json:"texts"`
	RawScores bool     `json:"raw_scores"`
}

type teiResponse struct {
	Results []struct {
		Index int     `json:"index"`
		Score float64 `json:"score"`
	} `json:"results"`
}

func (t *TEI) Rerank(ctx context.Context, query string, docs []string, topN int) ([]int, error) {
	body, err := json.Marshal(teiRequest{Query: query, Texts: docs})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, t.url+"/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("tei rerank: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("tei rerank: status %d", resp.StatusCode)
	}

	var out teiResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	sort.SliceStable(out.Results, func(a, b int) bool { return out.Results[a].Score > out.Results[b].Score })
	indices := make([]int, 0, len(out.Results))
	for _, r := range out.Results {
		if r.Index >= 0 && r.Index < len(docs) {
			indices = append(indices, r.Index)
		}
	}
	return capTopN(indices, topN), nil
}

// Llama calls a llama.cpp `llama-server` rerank endpoint, serving the very same
// cross-encoder locally (no Docker, no GPU). Start it with the model in rerank
// mode:
//
//	llama-server --model <reranker.gguf> --pooling rank --rerank --port 8080
//
// The wire format is the Cohere/Jina rerank API, which llama-server implements:
//
//	POST /v1/rerank {"query": "...", "documents": ["...", "..."]}
//	  -> {"results": [{"index": 0, "relevance_score": -0.66}, ...]}
//
// Note: the plain `bge-reranker-v2-m3` GGUF carries no `bert.pooling_type`
// metadata, so `--pooling rank` must be passed explicitly (otherwise the model
// loads as a generation model and aborts).
type Llama struct {
	url  string
	http *http.Client
}

// NewLlama creates a llama-server re-ranker. url is the server base URL.
//
// The timeout is generous: without a GPU a 568M cross-encoder scores roughly
// one (query, doc) pair every ~0.6s, so an 80-doc candidate pool takes ~1min.
func NewLlama(url string) *Llama {
	return &Llama{url: strings.TrimRight(url, "/"), http: &http.Client{Timeout: 300 * time.Second}}
}

func (l *Llama) Name() string { return "llama" }

type llamaRequest struct {
	Query     string   `json:"query"`
	Documents []string `json:"documents"`
}

type llamaResponse struct {
	Results []struct {
		Index          int     `json:"index"`
		RelevanceScore float64 `json:"relevance_score"`
	} `json:"results"`
}

func (l *Llama) Rerank(ctx context.Context, query string, docs []string, topN int) ([]int, error) {
	body, err := json.Marshal(llamaRequest{Query: query, Documents: docs})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, l.url+"/v1/rerank", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := l.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("llama rerank: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("llama rerank: status %d", resp.StatusCode)
	}

	var out llamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	sort.SliceStable(out.Results, func(a, b int) bool { return out.Results[a].RelevanceScore > out.Results[b].RelevanceScore })
	indices := make([]int, 0, len(out.Results))
	for _, r := range out.Results {
		if r.Index >= 0 && r.Index < len(docs) {
			indices = append(indices, r.Index)
		}
	}
	return capTopN(indices, topN), nil
}

func bigrams(s string) map[string]bool {
	r := []rune(strings.TrimSpace(s))
	out := make(map[string]bool, len(r))
	if len(r) == 1 {
		out[string(r)] = true
		return out
	}
	for i := 0; i+1 < len(r); i++ {
		out[string(r[i:i+2])] = true
	}
	return out
}

func firstN(n, topN int) []int {
	if topN > n {
		topN = n
	}
	out := make([]int, topN)
	for i := range out {
		out[i] = i
	}
	return out
}

func capTopN(in []int, topN int) []int {
	if topN < len(in) {
		return in[:topN]
	}
	return in
}
