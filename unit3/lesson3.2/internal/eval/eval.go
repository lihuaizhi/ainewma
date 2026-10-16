// Package eval implements offline retrieval evaluation: qrels, Recall@k and
// MRR. The same code powers cmd/genqrels (building the labels) and cmd/eval
// (scoring one or more collections).
package eval

import (
	"context"
	"sort"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/store"
)

// RetrieveFunc retrieves the top k chunks for a query. Both the vector-only and
// the re-ranked retrievers satisfy this signature.
type RetrieveFunc func(ctx context.Context, query string, k int) ([]store.Hit, error)

// Metrics is the result of evaluating one retriever against a qrels set.
type Metrics struct {
	// N is the number of queries that were evaluated.
	N int
	// RecallAt holds Recall@k for every k that was requested.
	RecallAt map[int]float64
	// MRR is the mean reciprocal rank of the first relevant hit, cut off at
	// the largest requested k.
	MRR float64
}

// Evaluate runs every query through retrieve (once, using the largest k) and
// computes Recall@k and MRR@maxK.
//
// A retrieved chunk counts as relevant when the chunk text (or its parent
// window, for small-to-big) contains any of the query's stable anchors
// (`any_of`). Anchoring on content instead of chunk ids keeps the labels valid
// across chunking strategies.
func Evaluate(ctx context.Context, retrieve RetrieveFunc, queries []Query, ks []int) (Metrics, error) {
	ks = normalizeKs(ks)
	maxK := ks[len(ks)-1]

	m := Metrics{N: len(queries), RecallAt: make(map[int]float64, len(ks))}
	var mrrSum float64
	counts := make(map[int]float64, len(ks))

	for _, q := range queries {
		hits, err := retrieve(ctx, q.Query, maxK)
		if err != nil {
			return Metrics{}, err
		}
		firstRank := 0
		for i, h := range hits {
			if q.Matches(h) {
				firstRank = i + 1
				break
			}
		}
		if firstRank > 0 {
			mrrSum += 1 / float64(firstRank)
			for _, k := range ks {
				if firstRank <= k {
					counts[k]++
				}
			}
		}
	}
	if m.N > 0 {
		m.MRR = mrrSum / float64(m.N)
	}
	for _, k := range ks {
		m.RecallAt[k] = counts[k] / float64(m.N)
	}
	return m, nil
}

// normalizeKs sorts and de-duplicates the requested cut-offs.
func normalizeKs(ks []int) []int {
	if len(ks) == 0 {
		ks = []int{1, 3, 5, 10}
	}
	seen := make(map[int]bool, len(ks))
	out := make([]int, 0, len(ks))
	for _, k := range ks {
		if k <= 0 || seen[k] {
			continue
		}
		seen[k] = true
		out = append(out, k)
	}
	sort.Ints(out)
	if len(out) == 0 {
		out = []int{1, 3, 5, 10}
	}
	return out
}

// Ks renders "1,3,5,10" for a set of cut-offs (for headers).
func Ks(ks []int) string {
	parts := make([]string, len(ks))
	for i, k := range ks {
		parts[i] = itoa(k)
	}
	return strings.Join(parts, ",")
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}
