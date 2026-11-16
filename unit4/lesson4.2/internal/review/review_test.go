package review

import (
	"strings"
	"testing"
)

const fenced = "以下是评审结果：\n```json\n{\"findings\":[{\"line\":3,\"title\":\"标题\",\"detail\":\"说明\",\"suggestion\":\"修法\"}]}\n```\n"

func TestParsePayloadFenced(t *testing.T) {
	fs, err := ParsePayload(fenced)
	if err != nil {
		t.Fatalf("fenced: %v", err)
	}
	if len(fs) != 1 || fs[0].Line != 3 || fs[0].Title != "标题" {
		t.Fatalf("unexpected findings: %+v", fs)
	}
}

func TestParsePayloadBounding(t *testing.T) {
	// 前后包裹文字时，取首个平衡 {...}
	got, err := ParsePayload(`结果 {"findings":[]} 结束`)
	if err != nil || len(got) != 0 {
		t.Fatalf("want empty findings, got %v err %v", got, err)
	}
	if _, err := ParsePayload("完全不是JSON"); err == nil {
		t.Fatal("want error for non-JSON payload")
	}
}

func TestParsePayloadEmptyList(t *testing.T) {
	fs, err := ParsePayload(`{"findings":[]}`)
	if err != nil || len(fs) != 0 {
		t.Fatalf("want empty, got %v err %v", fs, err)
	}
}

func TestAnnotateLines(t *testing.T) {
	out := AnnotateLines("a\nb\n")
	if !strings.Contains(out, " 1: a\n") || !strings.Contains(out, " 2: b\n") {
		t.Fatalf("annotate: %q", out)
	}
}
