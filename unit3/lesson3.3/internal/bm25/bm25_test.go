package bm25

import "testing"

func TestTokenize(t *testing.T) {
	got := Tokenize("太阳病之")
	// 4 unigrams + 3 bigrams; whitespace is dropped.
	if len(got) != 7 {
		t.Fatalf("Tokenize len = %d (%v), want 7", len(got), got)
	}
	if got[0] != "太" || got[4] != "太阳" {
		t.Fatalf("unexpected tokens %v", got)
	}
}

func TestTokenizeSkipsWhitespace(t *testing.T) {
	got := Tokenize("太 阳\n病")
	// whitespace dropped: 3 unigrams + 2 bigrams
	if len(got) != 5 {
		t.Fatalf("Tokenize len = %d (%v), want 5 unigrams+bigrams", len(got), got)
	}
}

func TestSearchRanksExactMatchFirst(t *testing.T) {
	docs := []Doc{
		{ID: "a", Text: "太阳病，头痛发热，汗出恶风者，桂枝汤主之。"},
		{ID: "b", Text: "桂枝汤方：桂枝三两 去皮，芍药三两 炙。"},
		{ID: "c", Text: "少阴病，脉微细，但欲寐也。"},
	}
	ix := New(docs)
	hits := ix.Search("桂枝汤方：桂枝三两", 3, nil)
	if len(hits) == 0 {
		t.Fatal("no hits")
	}
	if hits[0].Doc.ID != "b" {
		t.Fatalf("expected doc b first, got %s (score %.4f)", hits[0].Doc.ID, hits[0].Score)
	}
}

func TestSearchKeepFilters(t *testing.T) {
	docs := []Doc{
		{ID: "a", Text: "太阳病，脉浮，头项强痛而恶寒。", Volume: "卷一"},
		{ID: "b", Text: "太阳病，项背强几几。", Volume: "卷二"},
	}
	ix := New(docs)
	hits := ix.Search("太阳病项背强", 5, func(d Doc) bool { return d.Volume == "卷一" })
	if len(hits) != 1 || hits[0].Doc.ID != "a" {
		t.Fatalf("filtered hits = %+v", hits)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	ix := New([]Doc{{ID: "a", Text: "太阳病"}})
	if hits := ix.Search("   \n", 5, nil); len(hits) != 0 {
		t.Fatalf("expected no hits for whitespace query, got %d", len(hits))
	}
}
