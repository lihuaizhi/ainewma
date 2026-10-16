package eval

import (
	"context"
	"strings"
	"testing"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/store"
)

// fakeSearcher returns a scripted list of hits per query.
func fakeSearcher(queries map[string][]store.Hit) RetrieveFunc {
	return func(_ context.Context, query string, _ int) ([]store.Hit, error) {
		return queries[query], nil
	}
}

func hit(text string) store.Hit { return store.Hit{Text: text} }

func TestEvaluateRecallAndMRR(t *testing.T) {
	anchored := hit("二六三：太阳病三日，发汗不解……")
	queries := []Query{
		{Query: "q1", AnyOf: []string{"太阳病三日"}},
		{Query: "q2", AnyOf: []string{"never-present-anchor"}},
	}
	searcher := fakeSearcher(map[string][]store.Hit{
		"q1": {hit("无关内容"), anchored}, // relevant at rank 2
		"q2": {hit("无关内容"), hit("无关内容")},
	})

	m, err := Evaluate(context.Background(), searcher, queries, []int{1, 3, 5})
	if err != nil {
		t.Fatal(err)
	}
	if m.N != 2 {
		t.Fatalf("N = %d, want 2", m.N)
	}
	if got := m.RecallAt[1]; got != 0.0 {
		t.Fatalf("R@1 = %v, want 0 (relevant at rank 2)", got)
	}
	if got := m.RecallAt[3]; got != 0.5 {
		t.Fatalf("R@3 = %v, want 0.5", got)
	}
	// q1 contributes 1/2, q2 contributes 0 => MRR = 0.25
	if got := m.MRR; got != 0.25 {
		t.Fatalf("MRR = %v, want 0.25", got)
	}
}

func TestEvaluateSmallToBigParentAnchor(t *testing.T) {
	// For small2big the anchor lives in parent_text even if the child text
	// was split around it.
	q := Query{Query: "q", AnyOf: []string{"大承气汤"}}
	searcher := fakeSearcher(map[string][]store.Hit{
		"q": {{ID: "c", Text: "阳明之为病，胃家实是也。", ParentText: "大承气汤主之。阳明之为病……"}},
	})
	m, err := Evaluate(context.Background(), searcher, []Query{q}, []int{5})
	if err != nil {
		t.Fatal(err)
	}
	if m.RecallAt[5] != 1.0 || m.MRR != 1.0 {
		t.Fatalf("expected hit: %+v", m)
	}
}

func TestBuildQrelsAnchorsPresentInText(t *testing.T) {
	full := "\n 序言部分 \n一五四：太阳病，其脉浮，宜以汗解之。\n三七：阳明病，发热汗出者，此为热越。\n" +
		"七十条是注解，不是编号标签\n"
	qs := BuildQrels(full, 0)
	if len(qs) != 2 {
		t.Fatalf("got %d queries, want 2: %+v", len(qs), qs)
	}
	for _, q := range qs {
		if !strings.Contains(full, q.AnyOf[0]) {
			t.Fatalf("anchor %q not found in source text", q.AnyOf[0])
		}
	}
	// A fake number label "七十条是" must not be extracted as a 条文.
	for _, q := range qs {
		if strings.Contains(q.Query, "七十条") {
			t.Fatalf("commentary leaked into qrels: %q", q.Query)
		}
	}
}
