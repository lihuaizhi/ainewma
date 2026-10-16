package retrieve

import "testing"

func TestRRFFuseInterleaves(t *testing.T) {
	got := rrfFuse([][]string{
		{"a", "b", "c"},
		{"c", "a", "d"},
	}, 60)
	// a ranks (1,2), c ranks (3,1) -> both appear twice, a narrowly wins;
	// b and d appear once each.
	if len(got) != 4 || got[0] != "a" || got[1] != "c" {
		t.Fatalf("fused = %v, want [a c ...]", got)
	}
}

func TestRRFFuseDedupes(t *testing.T) {
	got := rrfFuse([][]string{{"x", "x", "y"}, {"y"}}, 60)
	if len(got) != 2 || got[0] != "x" || got[1] != "y" {
		t.Fatalf("fused = %v, want [x y]", got)
	}
}

func TestRRFFuseEmpty(t *testing.T) {
	if got := rrfFuse(nil, 60); len(got) != 0 {
		t.Fatalf("expected no results, got %v", got)
	}
	if got := rrfFuse([][]string{nil, {}}, 60); len(got) != 0 {
		t.Fatalf("expected no results for empty rankings, got %v", got)
	}
}
