package rerank

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNoop(t *testing.T) {
	idx, err := (Noop{}).Rerank(context.Background(), "q", []string{"a", "b", "c"}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 1 {
		t.Fatalf("noop order = %v", idx)
	}
}

func TestLexicalPromotesExactMatch(t *testing.T) {
	docs := []string{
		"太阳病，恶寒发热，无汗而喘。", // some overlap with query
		"桂枝汤主之。脉浮，头项强痛。", // exact query contained
		"少阴病，脉微细。",
	}
	query := "脉浮，头项强痛"
	idx, err := (Lexical{}).Rerank(context.Background(), query, docs, 3)
	if err != nil {
		t.Fatal(err)
	}
	if idx[0] != 1 {
		t.Fatalf("exact-match doc should rank first, got order %v", idx)
	}
}

func TestBm25RankerRanksMatchesBeforeRest(t *testing.T) {
	docs := []string{"无关内容甲", "桂枝汤主之", "无关内容乙"}
	idx, err := (Bm25Ranker{}).Rerank(context.Background(), "桂枝汤", docs, 3)
	if err != nil {
		t.Fatal(err)
	}
	// The only matching doc first; the non-matches keep their input order.
	if len(idx) != 3 || idx[0] != 1 || idx[1] != 0 || idx[2] != 2 {
		t.Fatalf("bm25 order = %v", idx)
	}
}

func TestBm25RankerLengthNormalisation(t *testing.T) {
	docs := []string{
		"凡病，若发汗，若吐，若下，若亡血，亡津液，阴阳自和者，必自愈。",
		"桂枝汤主之。",
		"太阳中风，阳浮而阴弱，桂枝汤主之。",
	}
	idx, err := (Bm25Ranker{}).Rerank(context.Background(), "桂枝汤", docs, 3)
	if err != nil {
		t.Fatal(err)
	}
	// A short, focused match should outrank a long doc sharing the same terms,
	// and the non-matching doc sinks to the tail.
	if idx[0] != 1 || idx[2] != 0 {
		t.Fatalf("bm25 order = %v, want [1 2 0]", idx)
	}
}

func TestBm25RankerEmptyQueryKeepsOrder(t *testing.T) {
	docs := []string{"甲", "乙", "丙"}
	idx, err := (Bm25Ranker{}).Rerank(context.Background(), "", docs, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 2 || idx[0] != 0 || idx[1] != 1 {
		t.Fatalf("empty-query order = %v", idx)
	}
}

func TestNewBm25Kind(t *testing.T) {
	if got := New("bm25", "").Name(); got != "bm25" {
		t.Fatalf("New(bm25).Name() = %q", got)
	}
}

func TestTEIParsesRerankResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/rerank" {
			t.Fatalf("wrong path %s", r.URL.Path)
		}
		// TEI returns scores in arbitrary order; the client must sort by score.
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"index": 1, "score": 0.9},
				map[string]any{"index": 0, "score": 0.3},
				map[string]any{"index": 2, "score": 0.5},
			},
		})
	}))
	defer srv.Close()

	tei := NewTEI(srv.URL)
	idx, err := tei.Rerank(context.Background(), "q", []string{"a", "b", "c"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 3 || idx[0] != 1 || idx[1] != 2 || idx[2] != 0 {
		t.Fatalf("tei order = %v", idx)
	}
}

func TestTEIErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "model not loaded", http.StatusServiceUnavailable)
	}))
	defer srv.Close()

	tei := NewTEI(srv.URL)
	if _, err := tei.Rerank(context.Background(), "q", []string{"a"}, 1); err == nil {
		t.Fatal("expected error from TEI")
	}
}

func TestLlamaParsesRerankResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/rerank" {
			t.Fatalf("wrong path %s", r.URL.Path)
		}
		var req llamaRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Query != "q" || len(req.Documents) != 3 {
			t.Fatalf("unexpected request %+v", req)
		}
		// llama-server returns the Cohere/Jina shape in arbitrary order; the
		// client must sort by relevance_score.
		json.NewEncoder(w).Encode(map[string]any{
			"results": []any{
				map[string]any{"index": 1, "relevance_score": -0.6},
				map[string]any{"index": 0, "relevance_score": -5.1},
				map[string]any{"index": 2, "relevance_score": -2.7},
			},
		})
	}))
	defer srv.Close()

	l := NewLlama(srv.URL)
	idx, err := l.Rerank(context.Background(), "q", []string{"a", "b", "c"}, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(idx) != 3 || idx[0] != 1 || idx[1] != 2 || idx[2] != 0 {
		t.Fatalf("llama order = %v", idx)
	}
}

func TestLlamaErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "process has terminated", http.StatusInternalServerError)
	}))
	defer srv.Close()

	l := NewLlama(srv.URL)
	if _, err := l.Rerank(context.Background(), "q", []string{"a"}, 1); err == nil {
		t.Fatal("expected error from llama-server")
	}
}
