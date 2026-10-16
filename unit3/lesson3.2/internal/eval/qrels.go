package eval

import (
	"bufio"
	"encoding/json"
	"os"
	"regexp"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/store"
)

// Query is one labelled evaluation example.
type Query struct {
	// Query is what we send to the retriever (usually a 条文 statement).
	Query string `json:"query"`
	// AnyOf are stable content anchors. A retrieved chunk is relevant when its
	// text contains at least one anchor. Anchoring on content (not chunk ids)
	// keeps the labels valid across chunking strategies.
	AnyOf []string `json:"any_of"`
	// Note documents where the label came from (optional).
	Note string `json:"note,omitempty"`
}

// Matches reports whether a hit is relevant to the query.
func (q Query) Matches(h store.Hit) bool {
	text := h.Text
	if h.ParentText != "" {
		text += "\n" + h.ParentText
	}
	for _, a := range q.AnyOf {
		if a != "" && strings.Contains(text, a) {
			return true
		}
	}
	return false
}

// LoadQrels reads one JSON object per line.
func LoadQrels(path string) ([]Query, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	var queries []Query
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		var q Query
		if err := json.Unmarshal([]byte(line), &q); err != nil {
			return nil, err
		}
		if q.Query == "" || len(q.AnyOf) == 0 {
			continue
		}
		queries = append(queries, q)
	}
	return queries, sc.Err()
}

// WriteQrels writes queries as JSONL.
func WriteQrels(path string, queries []Query) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	w := bufio.NewWriter(f)
	enc := json.NewEncoder(w)
	for _, q := range queries {
		if err := enc.Encode(q); err != nil {
			return err
		}
	}
	return w.Flush()
}

// reClauseLabel matches "一五四：" / "二三一:" style 条文 labels in the book.
var reClauseLabel = regexp.MustCompile(`[一二三四五六七八九十百]{1,4}[：:]\s*([^。！？\n「」]{8,80})[。！？]`)

// minAnchorRunes is how many leading runes of a sentence we keep as the anchor.
// Short anchors are robust to chunk boundaries; 12 runes are distinctive enough
// in a classical text.
const minAnchorRunes = 12

// BuildQrels extracts evaluation queries from the cleaned full text of the
// book. Every 条文 label ("一五四：...") yields one query: the statement is the
// query, and its leading runes are the relevance anchor.
//
// The result is cheap, objective and reproducible, which is what you want for
// tuning chunking/rerank; for a production system you would add a hand-checked
// set on top.
func BuildQrels(full string, max int) []Query {
	var queries []Query
	seen := make(map[string]bool)

	for _, m := range reClauseLabel.FindAllStringSubmatch(full, -1) {
		statement := strings.TrimSpace(m[1])
		statement = strings.Trim(statement, "，,、 「」")
		if len([]rune(statement)) < 8 {
			continue
		}
		if strings.Contains(statement, "脉证并治") {
			continue
		}
		// Skip OCR artifacts: control characters, replacement glyphs,
		// embedded colons or half-width question marks.
		if strings.ContainsAny(statement, "\u0000\uFFFD:：?？") {
			continue
		}
		anchor := firstRunes(statement, minAnchorRunes)
		if seen[anchor] {
			continue
		}
		seen[anchor] = true
		queries = append(queries, Query{
			Query: statement,
			AnyOf: []string{anchor},
			Note:  "auto: 条文 label anchor",
		})
		if max > 0 && len(queries) >= max {
			break
		}
	}
	return queries
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}
