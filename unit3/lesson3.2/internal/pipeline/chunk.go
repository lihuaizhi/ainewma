package pipeline

import (
	"errors"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/config"
	"trpc.group/trpc-go/trpc-agent-go/knowledge/chunking"
)

var (
	errNoContent = errors.New("pipeline: pdf contains no extractable text")

	// reChapter matches the six-meridian chapter headings of 伤寒论.
	reChapter = regexp.MustCompile(`辨[\p{Han}]{2,6}病脉证并治(?:法)?[上中下]?`)
)

// Options controls how the cleaned pages are turned into chunks.
type Options struct {
	Strategy     string
	ChunkSize    int
	ChunkOverlap int
	ParentSize   int
	ChildSize    int
	ChildOverlap int
}

// Chunk is a single retrieval unit together with the metadata of our schema.
type Chunk struct {
	ID         string
	Text       string
	ParentText string
	Volume     string
	ChapterNo  int
	ClauseNo   int64
	Formulas   []string
	PageNo     int64
	ChunkIndex int64
}

type span struct {
	start int // byte offset into the concatenated text
	page  int
}

type chapter struct {
	name       string
	start, end int
}

// BuildChunks turns the cleaned pages into chunks according to opts.Strategy:
//
//   - fixed:     the whole book is cut by size, ignoring chapter boundaries;
//   - chapter:   chapters are cut out first (ourselves), then cut by size;
//   - small2big: children (ChildSize) are indexed, each carrying the enclosing
//     parent window (ParentSize) that will be fed to the LLM.
//
// The actual size-based splitting is delegated to the trpc-agent-go knowledge
// FixedSizeChunking strategy (the "knowledge part"); the chapter segmentation
// and metadata extraction are ours.
func BuildChunks(pages []Page, opts Options) []Chunk {
	if len(pages) == 0 {
		return nil
	}
	full, pageAt := joinPages(pages)

	var chapters []chapter
	if opts.Strategy == config.StrategyFixed {
		chapters = []chapter{{name: "全文", start: 0, end: len(full)}}
	} else {
		chapters = detectChapters(full)
		if len(chapters) == 0 {
			chapters = []chapter{{name: "全文", start: 0, end: len(full)}}
		}
	}

	var chunks []Chunk
	for i, ch := range chapters {
		chapterNo := i + 1
		if opts.Strategy == config.StrategySmallToBig {
			chunks = append(chunks, buildSmallToBig(chapterNo, ch, full, pageAt, opts)...)
			continue
		}
		chunks = append(chunks, buildChapterChunks(chapterNo, ch, full, pageAt, opts)...)
	}

	// Assign stable ids and re-index chunks inside every chapter.
	perChapter := make(map[int]int64)
	for i := range chunks {
		idx := perChapter[chunks[i].ChapterNo]
		perChapter[chunks[i].ChapterNo] = idx + 1
		chunks[i].ChunkIndex = idx
		chunks[i].ID = chapterID(chunks[i].ChapterNo, idx)
	}
	return chunks
}

func joinPages(pages []Page) (string, func(int) int64) {
	var sb strings.Builder
	spans := make([]span, 0, len(pages))
	for _, p := range pages {
		spans = append(spans, span{start: sb.Len(), page: p.No})
		sb.WriteString(p.Text)
		sb.WriteByte('\n')
	}
	full := sb.String()
	pageAt := func(off int) int64 {
		page := int64(0)
		for _, sp := range spans {
			if sp.start > off {
				break
			}
			page = int64(sp.page)
		}
		return page
	}
	return full, pageAt
}

func buildChapterChunks(chapterNo int, ch chapter, full string, pageAt func(int) int64, opts Options) []Chunk {
	chapText := full[ch.start:ch.end]
	var chunks []Chunk
	cursor := ch.start
	for _, text := range chunkText(chapText, ch.name, opts.ChunkSize, opts.ChunkOverlap) {
		off := locate(full, cursor, text)
		cursor = off + 1
		chunks = append(chunks, Chunk{
			Volume:    ch.name,
			ChapterNo: chapterNo,
			ClauseNo:  detectClause(text),
			Text:      text,
			Formulas:  DetectFormulas(text),
			PageNo:    pageAt(off),
		})
	}
	return chunks
}

func buildSmallToBig(chapterNo int, ch chapter, full string, pageAt func(int) int64, opts Options) []Chunk {
	rawText := full[ch.start:ch.end]
	// The knowledge chunker normalizes whitespace before splitting, so parents
	// and the probe search must run against the same normalized text.
	norm := normalizeText(rawText)
	normRunes := []rune(norm)
	parentSize := opts.ParentSize
	if parentSize <= 0 {
		parentSize = config.ParentSize
	}
	nParents := (len(normRunes) + parentSize - 1) / parentSize
	if nParents == 0 {
		return nil
	}
	parentText := func(p int) string {
		lo := p * parentSize
		hi := min(lo+parentSize, len(normRunes))
		return strings.TrimSpace(string(normRunes[lo:hi]))
	}

	var chunks []Chunk
	cursor := 0    // rune offset inside the normalized chapter text
	rawCursor := 0 // best-effort byte offset inside the raw chapter text
	for _, text := range chunkText(norm, ch.name, opts.ChildSize, opts.ChildOverlap) {
		start := indexRunes(normRunes, []rune(firstRunes(text, 24)), cursor)
		if start < 0 {
			start = cursor
		}
		cursor = start + 1
		p := start / parentSize
		if p >= nParents {
			p = nParents - 1
		}
		// Best-effort page attribution (the normalized text shifts byte
		// offsets, so we re-locate the probe in the raw chapter text).
		off := rawCursor
		if probe := firstRunes(text, 24); probe != "" {
			if i := strings.Index(rawText[rawCursor:], probe); i >= 0 {
				off = rawCursor + i
			}
		}
		rawCursor = off + 1
		chunks = append(chunks, Chunk{
			Volume:     ch.name,
			ChapterNo:  chapterNo,
			ClauseNo:   detectClause(text),
			Text:       text,
			ParentText: parentText(p),
			Formulas:   DetectFormulas(text),
			PageNo:     pageAt(ch.start + off),
		})
	}
	return chunks
}

// normalizeText mirrors the knowledge chunker's cleanText so that chunked
// contents stay verbatim substrings of the text we locate parents in.
func normalizeText(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "\r\n", "\n")
	s = strings.ReplaceAll(s, "\r", "\n")
	lines := strings.Split(s, "\n")
	for i, line := range lines {
		lines[i] = strings.TrimSpace(line)
	}
	return strings.Join(lines, "\n")
}

// chunkText delegates size-based splitting to knowledge's FixedSizeChunking.
func chunkText(text, name string, size, overlap int) []string {
	if size <= 0 {
		size = config.ChunkSize
	}
	if overlap < 0 || overlap >= size {
		overlap = size / 8
	}
	strategy := chunking.NewFixedSizeChunking(
		chunking.WithChunkSize(size),
		chunking.WithOverlap(overlap),
	)
	docs, err := strategy.Chunk(newDocument(name, text))
	if err != nil {
		return nil
	}
	out := make([]string, 0, len(docs))
	for _, d := range docs {
		if t := strings.TrimSpace(d.Content); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// detectChapters finds the chapter headings in the body of the book. The table
// of contents (which lists the same headings surrounded by dot leaders) is
// filtered out by looking at the neighbourhood of each match.
func detectChapters(full string) []chapter {
	type match struct {
		name  string
		start int
	}
	var matches []match
	seen := make(map[string]bool)

	for _, loc := range reChapter.FindAllStringIndex(full, -1) {
		name := full[loc[0]:loc[1]]
		if isTOC(full, loc[0], loc[1]) || seen[name] {
			continue
		}
		seen[name] = true
		matches = append(matches, match{name: name, start: loc[0]})
	}
	sort.Slice(matches, func(i, j int) bool { return matches[i].start < matches[j].start })

	chapters := make([]chapter, 0, len(matches))
	for i, m := range matches {
		end := len(full)
		if i+1 < len(matches) {
			end = matches[i+1].start
		}
		chapters = append(chapters, chapter{name: m.name, start: m.start, end: end})
	}
	return chapters
}

func isTOC(full string, start, end int) bool {
	left := max(0, start-30)
	right := min(len(full), end+30)
	dots := strings.Count(full[left:start], ".") + strings.Count(full[end:right], ".")
	return dots >= 6
}

func locate(full string, from int, text string) int {
	probe := firstRunes(text, 24)
	if probe == "" || from >= len(full) {
		return from
	}
	if i := strings.Index(full[from:], probe); i >= 0 {
		return from + i
	}
	return from
}

// indexRunes returns the index of needle in hay starting at from, or -1.
func indexRunes(hay, needle []rune, from int) int {
	if len(needle) == 0 {
		return -1
	}
	for i := max(0, from); i+len(needle) <= len(hay); i++ {
		if string(hay[i:i+len(needle)]) == string(needle) {
			return i
		}
	}
	return -1
}

func firstRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n])
}

func chapterID(chapterNo int, idx int64) string {
	return "shl-" + leftPad(chapterNo, 2) + "-" + leftPad(int(idx), 3)
}

func leftPad(v int, width int) string {
	s := strings.Repeat("0", width) + strconv.Itoa(v)
	return s[len(s)-width:]
}
