package pipeline

import (
	"regexp"
	"strconv"
	"strings"
	"unicode"

	"trpc.group/trpc-go/trpc-agent-go/knowledge/document"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader"
	pdfreader "trpc.group/trpc-go/trpc-agent-go/knowledge/document/reader/pdf"
)

var (
	// rePageMarker locates the printed page number of every PDF page.
	rePageMarker = regexp.MustCompile(`第\s*(\d+)\s*页`)
	// reDraftJunk removes the "2010-05-09定稿" stamp plus the binary glyph
	// garbage that follows it on every page.
	reDraftJunk = regexp.MustCompile(`2010-05-09定稿[^\p{Han}]*`)
	// pageFurniture are running headers/footers repeated on every page.
	pageFurniture = []string{
		"倪注《伤寒论》",
		"勤求古訓，博采眾方",
		"勤求古训，博采众方",
	}
)

// Page is the cleaned text of a single PDF page.
type Page struct {
	No   int
	Text string
}

// ExtractPages reads the PDF through the trpc-agent-go knowledge PDF reader
// (the "knowledge part" of preprocessing) and returns one cleaned Page per
// printed page. The reader's own chunking is disabled so that we keep full
// control over how the text is split.
func ExtractPages(pdfPath string) ([]Page, error) {
	r := pdfreader.New(reader.WithChunk(false))
	docs, err := r.ReadFromFile(pdfPath)
	if err != nil {
		return nil, err
	}
	if len(docs) == 0 {
		return nil, errNoContent
	}

	raw := docs[0].Content
	locs := rePageMarker.FindAllStringSubmatchIndex(raw, -1)
	if len(locs) == 0 {
		if text := cleanText(raw); text != "" {
			return []Page{{No: 0, Text: text}}, nil
		}
		return nil, errNoContent
	}

	pages := make([]Page, 0, len(locs))
	for i, loc := range locs {
		no, _ := strconv.Atoi(raw[loc[2]:loc[3]])
		start := loc[1]
		end := len(raw)
		if i+1 < len(locs) {
			end = locs[i+1][0]
		}
		if text := cleanText(raw[start:end]); text != "" {
			pages = append(pages, Page{No: no, Text: text})
		}
	}
	return pages, nil
}

func cleanText(s string) string {
	s = strings.ReplaceAll(s, "\uFFFD", "")
	s = reDraftJunk.ReplaceAllString(s, "")
	for _, f := range pageFurniture {
		s = strings.ReplaceAll(s, f, "")
	}

	var b strings.Builder
	for _, line := range strings.Split(s, "\n") {
		line = strings.TrimFunc(line, func(r rune) bool {
			return unicode.IsSpace(r) || unicode.IsControl(r)
		})
		if line == "" {
			continue
		}
		b.WriteString(line)
		b.WriteByte('\n')
	}
	return b.String()
}

// newDocument wraps text into the trpc-agent-go document type so that the
// knowledge chunking strategies can consume it.
func newDocument(name, text string) *document.Document {
	return &document.Document{Name: name, Content: text}
}
