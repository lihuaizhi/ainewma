// Command genqrels builds the retrieval evaluation labels directly from the
// book: every "一五四：..." 条文 label becomes one query whose relevance anchor
// is the leading runes of its statement. Anchoring on stable content (instead
// of chunk ids) keeps the labels valid across chunking strategies.
//
//	go run ./cmd/genqrels -out testdata/qrels.jsonl
//
// The result is machine-produced; inspect it once and hand-add a few
// questions/formula anchors if you want a richer test set.
package main

import (
	"flag"
	"log"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/config"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/eval"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/pipeline"
)

func main() {
	pdfPath := flag.String("pdf", config.DefaultPDFPath, "path to the 伤寒论 PDF")
	out := flag.String("out", config.QrelsPath, "output JSONL file")
	max := flag.Int("max", 0, "maximum number of queries to write (0 = all)")
	maxPages := flag.Int("max-pages", 0, "only read the first N pages (0 = all); keep in sync with ingest -max-pages")
	flag.Parse()

	pages, err := pipeline.ExtractPages(*pdfPath)
	if err != nil {
		log.Fatalf("extract pdf: %v", err)
	}
	if *maxPages > 0 && *maxPages < len(pages) {
		pages = pages[:*maxPages]
		log.Printf("limiting to first %d pages", len(pages))
	}
	var full strings.Builder
	for _, p := range pages {
		full.WriteString(p.Text)
		full.WriteByte('\n')
	}

	queries := eval.BuildQrels(full.String(), *max)
	if len(queries) == 0 {
		log.Fatal("no queries extracted")
	}
	if err := eval.WriteQrels(*out, queries); err != nil {
		log.Fatalf("write qrels: %v", err)
	}
	log.Printf("wrote %d queries to %s", len(queries), *out)
	for i, q := range queries[:min(10, len(queries))] {
		log.Printf("  #%d query=%q anchor=%q", i+1, q.Query, q.AnyOf[0])
	}
}
