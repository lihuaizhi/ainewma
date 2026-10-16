// Package bm25 implements a compact BM25 retrieval index for Chinese text.
//
// Tokenisation is character bigram + unigram based: a dependency-free proxy
// for word segmentation that works on the mixed classical/modern 伤寒论 text.
// The index is built in memory from the rows already stored in Milvus, so the
// lexical arm sees exactly the same chunk surface as the vector arm (both
// score `text`).
package bm25

import (
	"math"
	"sort"
	"unicode"
)

// Doc is one indexed chunk. Text is the field that is also embedded, so the
// lexical score is computed over the same surface as the vector.
type Doc struct {
	ID          string
	Text        string
	ParentText  string
	Volume      string
	FormulaText string
}

// Hit is a scored search result.
type Hit struct {
	Doc   Doc
	Score float64
}

// Index is a single-collection BM25 index. Corpus statistics (document
// frequency, average length) are computed once at build time.
type Index struct {
	docs   []Doc
	terms  []map[string]int // per-doc term frequency
	docLen []int            // per-doc token count (with duplicates)
	df     map[string]int   // term -> number of docs containing it
	avgdl  float64
	k1     float64
	b      float64
}

// New builds an index over docs and freezes the corpus statistics.
func New(docs []Doc) *Index {
	ix := &Index{
		docs:   docs,
		terms:  make([]map[string]int, len(docs)),
		docLen: make([]int, len(docs)),
		df:     make(map[string]int),
		k1:     1.2,
		b:      0.75,
	}
	total := 0
	for i, d := range docs {
		tf := make(map[string]int)
		for _, t := range Tokenize(d.Text) {
			tf[t]++
		}
		ix.terms[i] = tf
		for t := range tf {
			ix.df[t]++
		}
		dl := 0
		for _, f := range tf {
			dl += f
		}
		ix.docLen[i] = dl
		total += dl
	}
	if len(docs) > 0 {
		ix.avgdl = float64(total) / float64(len(docs))
	}
	return ix
}

// Search ranks docs by BM25 score. keep (optional) filters docs before
// scoring, e.g. to honour a volume/formula constraint. topN limits the result
// list (0 disables the limit); every returned doc shares at least one query
// term.
func (ix *Index) Search(query string, topN int, keep func(Doc) bool) []Hit {
	qterms := unique(Tokenize(query))
	if len(qterms) == 0 || len(ix.docs) == 0 {
		return nil
	}
	n := float64(len(ix.docs))
	avgdl := ix.avgdl
	if avgdl <= 0 {
		avgdl = 1
	}

	var hits []Hit
	for i, d := range ix.docs {
		if keep != nil && !keep(d) {
			continue
		}
		tf := ix.terms[i]
		var score float64
		for _, t := range qterms {
			f, ok := tf[t]
			if !ok || f == 0 {
				continue
			}
			df := float64(ix.df[t])
			idf := math.Log(1 + (n-df+0.5)/(df+0.5))
			dl := float64(ix.docLen[i])
			ff := float64(f)
			score += idf * (ff * (ix.k1 + 1)) / (ff + ix.k1*(1-ix.b+ix.b*dl/avgdl))
		}
		if score > 0 {
			hits = append(hits, Hit{Doc: d, Score: score})
		}
	}
	sort.SliceStable(hits, func(a, b int) bool { return hits[a].Score > hits[b].Score })
	if topN > 0 && len(hits) > topN {
		hits = hits[:topN]
	}
	return hits
}

// Tokenize splits Chinese text into character unigrams and bigrams, skipping
// whitespace and replacement characters. Punctuation is kept so that
// "桂枝汤方：…" style phrasing matches as-is.
func Tokenize(s string) []string {
	rs := make([]rune, 0, len(s))
	for _, r := range s {
		if unicode.IsSpace(r) || r == 0xFFFD {
			continue
		}
		rs = append(rs, r)
	}
	if len(rs) == 0 {
		return nil
	}
	out := make([]string, 0, len(rs)*2)
	for _, r := range rs {
		out = append(out, string(r))
	}
	for i := 0; i+1 < len(rs); i++ {
		out = append(out, string(rs[i:i+2]))
	}
	return out
}

func unique(terms []string) []string {
	if len(terms) == 0 {
		return nil
	}
	seen := make(map[string]bool, len(terms))
	out := make([]string, 0, len(terms))
	for _, t := range terms {
		if !seen[t] {
			seen[t] = true
			out = append(out, t)
		}
	}
	return out
}
