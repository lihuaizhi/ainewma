// Command ingest preprocesses 伤寒论 and loads one chunking configuration into
// Milvus. Run it once per configuration/strategy you want to compare.
//
// Pipeline:
//
//	PDF ──(knowledge PDF reader)────────────> pages
//	    ──(self-implemented chapter split)──> chapters
//	    ──(knowledge FixedSizeChunking + strategy)──> chunks + metadata
//	    ──(Ollama bge-m3)───────────────────> vectors
//	    ──(milvus client/v2)────────────────> collection
//
// Start cmd/milvusd first, then run, e.g.:
//
//	go run ./cmd/ingest -strategy chapter -collection shl_chapter_512
//	go run ./cmd/ingest -strategy fixed  -collection shl_fixed_512
//	go run ./cmd/ingest -strategy small2big -collection shl_s2b_1024_256
//
//	-max-pages 60 builds a smaller, comparable collections for quick eval runs.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"time"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/config"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/embed"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/pipeline"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/store"
)

func main() {
	pdfPath := flag.String("pdf", config.DefaultPDFPath, "path to the 伤寒论 PDF")
	addr := flag.String("addr", config.MilvusAddr, "Milvus gRPC address")
	collection := flag.String("collection", "", fmt.Sprintf("collection name (default: derived from -strategy)"))
	strategy := flag.String("strategy", config.StrategyChapter,
		fmt.Sprintf("chunking strategy: %s | %s | %s",
			config.StrategyFixed, config.StrategyChapter, config.StrategySmallToBig))
	chunkSize := flag.Int("chunk-size", config.ChunkSize, "chunk size in characters (fixed/chapter)")
	overlap := flag.Int("overlap", config.ChunkOverlap, "chunk overlap in characters (fixed/chapter)")
	parentSize := flag.Int("parent-size", config.ParentSize, "parent window size in characters (small2big)")
	childSize := flag.Int("child-size", config.ChildSize, "child chunk size in characters (small2big)")
	childOverlap := flag.Int("child-overlap", config.ChildOverlap, "child chunk overlap (small2big)")
	ollamaURL := flag.String("ollama", config.OllamaBaseURL, "Ollama base URL")
	model := flag.String("model", config.EmbedModel, "embedding model")
	drop := flag.Bool("drop", true, "drop and recreate the collection before inserting")
	maxPages := flag.Int("max-pages", 0, "only process the first N pages (0 = all); keeps configs comparable for eval")
	limit := flag.Int("limit", 0, "only ingest the first N chunks (0 = all)")
	flag.Parse()

	if *strategy != config.StrategyFixed && *strategy != config.StrategyChapter && *strategy != config.StrategySmallToBig {
		log.Fatalf("unknown strategy %q", *strategy)
	}
	if *collection == "" {
		*collection = collectionName(*strategy, *chunkSize, *parentSize, *childSize)
	}

	ctx := context.Background()

	// 1. Extract text from the PDF (knowledge PDF reader) and clean it.
	log.Printf("extracting text from %s ...", *pdfPath)
	pages, err := pipeline.ExtractPages(*pdfPath)
	if err != nil {
		log.Fatalf("extract pdf: %v", err)
	}
	log.Printf("extracted %d pages", len(pages))
	if *maxPages > 0 && *maxPages < len(pages) {
		pages = pages[:*maxPages]
		log.Printf("limited to first %d pages", len(pages))
	}

	// 2. Strategy-aware chunking + metadata.
	opts := pipeline.Options{
		Strategy:     *strategy,
		ChunkSize:    *chunkSize,
		ChunkOverlap: *overlap,
		ParentSize:   *parentSize,
		ChildSize:    *childSize,
		ChildOverlap: *childOverlap,
	}
	chunks := pipeline.BuildChunks(pages, opts)
	if len(chunks) == 0 {
		log.Fatal("no chunks produced")
	}
	log.Printf("produced %d chunks (strategy=%s)", len(chunks), *strategy)
	if *limit > 0 && *limit < len(chunks) {
		chunks = chunks[:*limit]
		log.Printf("limited to first %d chunks", len(chunks))
	}

	// Log how the chunks are distributed over the detected chapters.
	perVolume := make(map[string]int)
	for _, c := range chunks {
		perVolume[c.Volume]++
	}
	for v, n := range perVolume {
		log.Printf("  chapter %q: %d chunks", v, n)
	}

	// 3. Embed every chunk with bge-m3 through Ollama.
	texts := make([]string, len(chunks))
	for i, c := range chunks {
		texts[i] = c.Text
	}
	log.Printf("embedding %d chunks with %s ...", len(texts), *model)
	start := time.Now()
	embedder := embed.New(*ollamaURL, *model, config.EmbedBatch)
	vectors, err := embedder.Embed(ctx, texts)
	if err != nil {
		log.Fatalf("embed: %v", err)
	}
	log.Printf("embedded in %s (dim=%d)", time.Since(start).Round(time.Millisecond), len(vectors[0]))

	// 4. Write into Milvus.
	st, err := store.Open(ctx, *addr)
	if err != nil {
		log.Fatalf("open store: %v", err)
	}
	defer st.Close(ctx)

	if err := st.EnsureCollection(ctx, *collection, config.VectorDim, *drop); err != nil {
		log.Fatalf("ensure collection: %v", err)
	}
	if err := st.Insert(ctx, *collection, config.VectorDim, chunks, vectors); err != nil {
		log.Fatalf("insert: %v", err)
	}
	if err := st.Reload(ctx, *collection); err != nil {
		log.Fatalf("reload: %v", err)
	}

	count, err := st.Count(ctx, *collection)
	if err != nil {
		log.Printf("inserted %d chunks (count unavailable: %v)", len(chunks), err)
		return
	}
	log.Printf("done: %d chunks in collection %q", count, *collection)
}

func collectionName(strategy string, chunkSize, parentSize, childSize int) string {
	if strategy == config.StrategySmallToBig {
		return fmt.Sprintf("shl_s2b_%d_%d", parentSize, childSize)
	}
	return fmt.Sprintf("shl_%s_%d", strategy, chunkSize)
}
