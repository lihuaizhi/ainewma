package retrieve

import "sort"

// rrfFuse merges ordered result lists (each list of ids, best first) using
// Reciprocal Rank Fusion. k is the fusion constant; 60 is the classic default.
// A doc's fused score is the sum of 1/(k + rank) over every ranking it
// appears in, so a doc that both arms rank highly wins over one ranked highly
// by only a single arm. Ties are broken by first appearance.
func rrfFuse(rankings [][]string, k int) []string {
	if k <= 0 {
		k = 60
	}
	scores := make(map[string]float64)
	order := make(map[string]int)
	var ids []string
	for _, rank := range rankings {
		for i, id := range rank {
			scores[id] += 1 / float64(k+i+1)
			if _, seen := order[id]; !seen {
				order[id] = len(ids)
				ids = append(ids, id)
			}
		}
	}
	sort.SliceStable(ids, func(a, b int) bool {
		if scores[ids[a]] != scores[ids[b]] {
			return scores[ids[a]] > scores[ids[b]]
		}
		return order[ids[a]] < order[ids[b]]
	})
	return ids
}
