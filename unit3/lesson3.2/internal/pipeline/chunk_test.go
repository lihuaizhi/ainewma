package pipeline

import (
	"strings"
	"testing"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/config"
)

// fakePages builds synthetic pages with real chapter headings so that chapter
// detection has something to find. The 条文 body lives inside the first
// chapter's span, mirroring the real book (text before the first heading, e.g.
// the 序, is intentionally dropped).
func fakePages() []Page {
	body := "辨太阳病脉证并治法上 \n" +
		"一五四：太阳病，其脉浮，宜以汗解之。\n" +
		"太阳之为病，脉浮，头项强痛而恶寒。\n" +
		"太阳中风，阳浮而阴弱，啬啬恶寒，淅淅恶风，翕翕发热，鼻鸣干呕者，桂枝汤主之。\n" +
		"桂枝汤方：桂枝三两，芍药三两，甘草二两，生姜三两，大枣十二枚。\n再标" +
		"辨阳明病脉证并治 \n" +
		"阳明之为病，胃家实是也。大承气汤主之。\n" +
		"调胃承气汤主之。肚子胀满，大便不通。\n" +
		strings.Repeat("反覆练习，熟能生巧，勤求古训。", 400)
	return []Page{{No: 12, Text: body}}
}

func TestBuildChunksStrategies(t *testing.T) {
	pages := fakePages()

	strategyTests := map[string]Options{
		"fixed": {
			Strategy:     config.StrategyFixed,
			ChunkSize:    100,
			ChunkOverlap: 20,
		},
		"chapter": {
			Strategy:     config.StrategyChapter,
			ChunkSize:    100,
			ChunkOverlap: 20,
		},
		"small2big": {
			Strategy:     config.StrategySmallToBig,
			ParentSize:   200,
			ChildSize:    60,
			ChildOverlap: 10,
		},
	}

	for name, opts := range strategyTests {
		chunks := BuildChunks(pages, opts)
		if len(chunks) == 0 {
			t.Fatalf("%s: no chunks", name)
		}

		ids := make(map[string]bool, len(chunks))
		for i, c := range chunks {
			if ids[c.ID] {
				t.Fatalf("%s: duplicate id %q", name, c.ID)
			}
			ids[c.ID] = true
			if c.Text == "" {
				t.Fatalf("%s: chunk %d empty text", name, i)
			}
			if c.ChapterNo < 1 {
				t.Fatalf("%s: chunk %d chapter_no %d", name, i, c.ChapterNo)
			}
			if c.PageNo < 1 {
				t.Fatalf("%s: chunk %d page_no %d", name, i, c.PageNo)
			}
		}

		if name == "small2big" {
			for i, c := range chunks {
				if c.ParentText == "" {
					t.Fatalf("small2big: chunk %d empty parent_text", i)
				}
				// The parent window must contain the child's beginning.
				if !strings.Contains(c.ParentText, firstRunes(c.Text, 12)) {
					t.Fatalf("small2big: chunk %d parent does not contain child start: %q vs %q",
						i, firstRunes(c.Text, 12), firstRunes(c.ParentText, 12))
				}
			}
		}
	}
}

func TestBuildChunksPreservesText(t *testing.T) {
	pages := fakePages()
	full, _ := joinPages(pages)

	opts := Options{Strategy: config.StrategyChapter, ChunkSize: 100, ChunkOverlap: 20}
	chunks := BuildChunks(pages, opts)

	prefix := "太阳之为病"
	found := false
	for _, c := range chunks {
		if strings.Contains(c.Text, prefix) {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("chapter strategy lost the opening 条文")
	}
	if strings.Count(full, "调胃承气汤") < 1 {
		t.Fatalf("fixture lost text")
	}
}
