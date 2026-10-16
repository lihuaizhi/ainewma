// Command milvusbm25 is a small, self-contained demo of Milvus-native BM25
// full-text retrieval.
//
// The point of the demo is that everything internal/bm25 does by hand --
// tokenising, counting term frequencies, building an inverted index, scoring --
// happens *inside* Milvus here:
//
//	text ──analyzer(jieba)──▶ tokens ──BM25 function──▶ sparse vector
//	                                                        │
//	query text ──same analyzer + BM25──▶ sparse query ──────┴──▶ SPARSE_INVERTED_INDEX (metric BM25)
//
// So the client only ever sends *raw text*: on insert it never builds or ships
// a sparse vector, and on search it never computes a BM25 score. pymilvus calls
// this the "BM25EmbeddingFunction"; in the Go SDK it is a BM25 *function*
// attached to the schema (entity.FunctionTypeBM25).
//
// With -hybrid the demo also builds a dense arm and lets Milvus fuse the two
// arms itself with Reciprocal Rank Fusion (NewRRFReranker), i.e. the server
// replaces our hand-written retrieve.rrfFuse.
//
// It needs a running Milvus (go run ./cmd/milvusd) and nothing else: the
// corpus is embedded below. -hybrid additionally needs Ollama serving bge-m3.
package main

import (
	"context"
	"flag"
	"fmt"
	"log"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/config"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/embed"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

const (
	colID     = "id"
	colText   = "text"
	colSparse = "sparse"
	colDense  = "dense"
)

// A handful of real 伤寒论 clauses: short, dense in terminology, and full of
// near-synonyms (太陽病 / 中風 / 傷寒), which makes the ranking easy to eyeball.
var corpus = []struct{ ID, Text string }{
	{"s1", "太陽之為病，脈浮，頭項強痛而惡寒。"},
	{"s2", "太陽病，發熱，汗出，惡風，脈緩者，名為中風。"},
	{"s3", "太陽病，或已發熱，或未發熱，必惡寒，體痛，嘔逆，脈陰陽俱緊者，名為傷寒。"},
	{"s4", "太陽中風，陽浮而陰弱，嗇嗇惡寒，淅淅惡風，翕翕發熱，鼻鳴乾嘔者，桂枝湯主之。"},
	{"s5", "太陽病，頭痛發熱，身疼腰痛，骨節疼痛，惡風無汗而喘者，麻黃湯主之。"},
	{"s6", "傷寒，脈浮緊，不發汗，因致衄者，麻黃湯主之。"},
	{"s7", "太陽病，項背強几几，反汗出惡風者，桂枝加葛根湯主之。"},
	{"s8", "傷寒表不解，心下有水氣，乾嘔發熱而咳，或渴，或利，或小便不利，小青龍湯主之。"},
	{"s9", "大下之後，復發汗，小便不利者，亡津液故也。勿治之，得小便利，必自愈。"},
	{"s10", "傷寒，服湯藥，下利不止，心下痞硬，服瀉心湯已，復以他藥下之，利不止，禹餘糧丸主之。"},
}

// Example queries: each is a paraphrase-ish bag of words rather than a verbatim
// clause, so a lexical engine has to actually rank, not just substring-match.
var exampleQueries = []string{
	"太陽病 頭痛 發熱 無汗 而喘",
	"汗出 惡風 桂枝湯",
	"心下痞硬 下利不止",
	"小便不利 亡津液",
}

func main() {
	addr := flag.String("addr", config.MilvusAddr, "Milvus gRPC address")
	collection := flag.String("collection", "bm25_demo", "demo collection name")
	topK := flag.Int("k", 5, "top-k hits per query")
	hybrid := flag.Bool("hybrid", false, "also build a dense arm and fuse both arms with Milvus RRF")
	query := flag.String("q", "", "run a single query (raw text) instead of the built-in examples")
	flag.Parse()

	ctx := context.Background()
	cli, err := milvusclient.New(ctx, &milvusclient.ClientConfig{Address: *addr})
	if err != nil {
		log.Fatalf("connect milvus %s: %v", *addr, err)
	}
	defer cli.Close(ctx)

	if err := build(ctx, cli, *collection, *hybrid); err != nil {
		log.Fatal(err)
	}

	queries := exampleQueries
	if *query != "" {
		queries = []string{*query}
	}
	for _, q := range queries {
		if err := runQuery(ctx, cli, *collection, q, *topK, *hybrid); err != nil {
			log.Fatalf("query %q: %v", q, err)
		}
	}
}

// build drops and recreates the demo collection, then inserts the corpus.
func build(ctx context.Context, cli *milvusclient.Client, name string, hybrid bool) error {
	if err := cli.DropCollection(ctx, milvusclient.NewDropCollectionOption(name)); err != nil {
		return fmt.Errorf("drop %s: %w", name, err)
	}

	// One schema, two ways of turning text into a searchable vector:
	//   * the BM25 function fills `sparse` from `text`  (server-side, lexical)
	//   * `dense` is filled by us with bge-m3 embeddings (only with -hybrid)
	schema := entity.NewSchema().WithName(name).
		WithField(entity.NewField().WithName(colID).
			WithDataType(entity.FieldTypeVarChar).WithMaxLength(64).WithIsPrimaryKey(true)).
		// WithEnableAnalyzer turns on Milvus' text analyzer for this field;
		// jieba is the Chinese word-segmenter (the default "standard" one would
		// chop CJK into single characters).
		WithField(entity.NewField().WithName(colText).
			WithDataType(entity.FieldTypeVarChar).WithMaxLength(8192).
			WithEnableAnalyzer(true).
			WithAnalyzerParams(map[string]any{"tokenizer": "jieba"})).
		WithField(entity.NewField().WithName(colSparse).
			WithDataType(entity.FieldTypeSparseVector)).
		// This is the "BM25EmbeddingFunction": input text, output sparse vector.
		WithFunction(entity.NewFunction().WithName("bm25").
			WithType(entity.FunctionTypeBM25).
			WithInputFields(colText).WithOutputFields(colSparse))

	indexes := []milvusclient.CreateIndexOption{
		milvusclient.NewCreateIndexOption(name, colSparse, index.NewSparseInvertedIndex(entity.BM25, 0.2)).
			WithIndexName("sparse_idx"),
	}
	if hybrid {
		schema = schema.WithField(entity.NewField().WithName(colDense).
			WithDataType(entity.FieldTypeFloatVector).WithDim(int64(config.VectorDim)))
		indexes = append(indexes,
			milvusclient.NewCreateIndexOption(name, colDense, index.NewAutoIndex(entity.COSINE)).
				WithIndexName("dense_idx"))
	}

	if err := cli.CreateCollection(ctx,
		milvusclient.NewCreateCollectionOption(name, schema).WithIndexOptions(indexes...)); err != nil {
		return fmt.Errorf("create collection: %w", err)
	}

	ids := make([]string, len(corpus))
	texts := make([]string, len(corpus))
	for i, d := range corpus {
		ids[i], texts[i] = d.ID, d.Text
	}

	insert := milvusclient.NewColumnBasedInsertOption(name).
		WithVarcharColumn(colID, ids).
		// Note: only raw text -- the sparse BM25 vector is produced by Milvus.
		WithVarcharColumn(colText, texts)
	if hybrid {
		vecs, err := embedTexts(ctx, texts)
		if err != nil {
			return err
		}
		insert = insert.WithFloatVectorColumn(colDense, config.VectorDim, vecs)
	}

	if _, err := cli.Insert(ctx, insert); err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	if _, err := cli.Flush(ctx, milvusclient.NewFlushOption(name)); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	if _, err := cli.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(name)); err != nil {
		return fmt.Errorf("load: %w", err)
	}

	mode := "BM25 full-text only"
	if hybrid {
		mode = "BM25 + dense, fused by Milvus RRF"
	}
	fmt.Printf("collection %q ready: %d docs, mode = %s\n\n", name, len(corpus), mode)
	return nil
}

// runQuery searches with the raw query text and prints the ranked hits.
func runQuery(ctx context.Context, cli *milvusclient.Client, name, q string, topK int, hybrid bool) error {
	var (
		rs  []milvusclient.ResultSet
		err error
	)
	if hybrid {
		qv, e := embedTexts(ctx, []string{q})
		if e != nil {
			return e
		}
		// Two arms, each a normal ANN request; Milvus fuses their *ranks*
		// (RRF, k=60) -- the same idea as retrieve.rrfFuse, done server-side.
		rs, err = cli.HybridSearch(ctx, milvusclient.NewHybridSearchOption(name, topK,
			milvusclient.NewAnnRequest(colDense, topK, entity.FloatVector(qv[0])),
			// The sparse arm takes the raw text; Milvus runs the analyzer + BM25.
			milvusclient.NewAnnRequest(colSparse, topK, entity.Text(q)),
		).WithReranker(milvusclient.NewRRFReranker()).WithOutputFields(colID, colText))
	} else {
		rs, err = cli.Search(ctx,
			milvusclient.NewSearchOption(name, topK, []entity.Vector{entity.Text(q)}).
				WithANNSField(colSparse).WithOutputFields(colID, colText))
	}
	if err != nil {
		return err
	}

	fmt.Printf("query: %s\n", q)
	if len(rs) == 0 || rs[0].ResultCount == 0 {
		fmt.Println("  (no hits)")
		fmt.Println()
		return nil
	}
	r := rs[0]
	for i := 0; i < r.ResultCount; i++ {
		id, _ := r.IDs.GetAsString(i)
		text, _ := r.GetColumn(colText).GetAsString(i)
		fmt.Printf("  #%d  score=%.4f  [%s] %s\n", i+1, r.Scores[i], id, truncate(text, 42))
	}
	fmt.Println()
	return nil
}

// embedTexts embeds with the local bge-m3 (only used by -hybrid).
func embedTexts(ctx context.Context, texts []string) ([][]float32, error) {
	client := embed.New(config.OllamaBaseURL, config.EmbedModel, config.EmbedBatch)
	vecs, err := client.Embed(ctx, texts)
	if err != nil {
		return nil, fmt.Errorf("embed (需要本地 Ollama 提供 %s): %w", config.EmbedModel, err)
	}
	return vecs, nil
}

func truncate(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
