// Package store owns the Milvus schema and all read/write access.
//
// It talks to the Milvus server through the new SDK
// github.com/milvus-io/milvus/client/v2.
package store

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/pipeline"
	"github.com/milvus-io/milvus/client/v2/column"
	"github.com/milvus-io/milvus/client/v2/entity"
	"github.com/milvus-io/milvus/client/v2/index"
	"github.com/milvus-io/milvus/client/v2/milvusclient"
	"google.golang.org/grpc"
	"google.golang.org/grpc/keepalive"
)

// Field names of the collection. Kept as constants so the schema, the insert
// path and the search path cannot drift apart.
const (
	FieldID         = "chunk_id"
	FieldVector     = "vector"
	FieldText       = "text"
	FieldParentText = "parent_text"
	FieldVolume     = "volume"
	FieldChapterNo  = "chapter_no"
	FieldClauseNo   = "clause_no"
	FieldFormula    = "formula"
	FieldPageNo     = "page_no"
	FieldChunkIndex = "chunk_index"

	// FieldFormulaText is a denormalised "桂枝汤,麻黄汤" copy of FieldFormula.
	// milvus-lite cannot evaluate json_contains() on a JSON field, so this
	// scalar field lets us filter with `formula_text like "%桂枝汤%"` locally
	// while FieldFormula keeps the structured array for real Milvus.
	FieldFormulaText = "formula_text"
)

// formulaSeparator joins the formula names stored in FieldFormulaText.
const formulaSeparator = ","

// Store wraps a Milvus client.
type Store struct {
	cli *milvusclient.Client
}

// Open connects to the Milvus (milvus-lite) server at addr.
func Open(ctx context.Context, addr string) (*Store, error) {
	// milvus-lite enforces a strict ping budget; a gentle keepalive avoids
	// `too_many_pings` while the CLI idles during LLM generation.
	cli, err := milvusclient.New(ctx, &milvusclient.ClientConfig{
		Address: addr,
		DialOptions: []grpc.DialOption{
			grpc.WithKeepaliveParams(keepalive.ClientParameters{
				Time:                300 * time.Second,
				Timeout:             20 * time.Second,
				PermitWithoutStream: false,
			}),
		},
	})
	if err != nil {
		return nil, fmt.Errorf("connect milvus: %w", err)
	}
	return &Store{cli: cli}, nil
}

// Close releases the gRPC connection.
func (s *Store) Close(ctx context.Context) {
	_ = s.cli.Close(ctx)
}

// HasCollection reports whether a collection exists.
func (s *Store) HasCollection(ctx context.Context, name string) (bool, error) {
	return s.cli.HasCollection(ctx, milvusclient.NewHasCollectionOption(name))
}

// Schema returns our collection schema.
func Schema(name string, dim int) *entity.Schema {
	return entity.NewSchema().WithName(name).
		WithField(entity.NewField().WithName(FieldID).
			WithDataType(entity.FieldTypeVarChar).WithMaxLength(64).WithIsPrimaryKey(true)).
		WithField(entity.NewField().WithName(FieldVector).
			WithDataType(entity.FieldTypeFloatVector).WithDim(int64(dim))).
		WithField(entity.NewField().WithName(FieldText).
			WithDataType(entity.FieldTypeVarChar).WithMaxLength(8192)).
		WithField(entity.NewField().WithName(FieldParentText).
			WithDataType(entity.FieldTypeVarChar).WithMaxLength(8192)).
		WithField(entity.NewField().WithName(FieldVolume).
			WithDataType(entity.FieldTypeVarChar).WithMaxLength(256)).
		WithField(entity.NewField().WithName(FieldChapterNo).
			WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(FieldClauseNo).
			WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(FieldFormula).
			WithDataType(entity.FieldTypeJSON)).
		WithField(entity.NewField().WithName(FieldFormulaText).
			WithDataType(entity.FieldTypeVarChar).WithMaxLength(512)).
		WithField(entity.NewField().WithName(FieldPageNo).
			WithDataType(entity.FieldTypeInt64)).
		WithField(entity.NewField().WithName(FieldChunkIndex).
			WithDataType(entity.FieldTypeInt64))
}

// EnsureCollection creates the collection if needed. When drop is true an
// existing collection is removed first, which makes ingestion idempotent.
//
// milvus-lite only accepts FLAT / IVF_FLAT / AUTOINDEX for the vector index,
// so we use AUTOINDEX with COSINE (bge-m3 vectors are L2-normalised).
func (s *Store) EnsureCollection(ctx context.Context, name string, dim int, drop bool) error {
	has, err := s.cli.HasCollection(ctx, milvusclient.NewHasCollectionOption(name))
	if err != nil {
		return fmt.Errorf("has collection: %w", err)
	}
	if has {
		if !drop {
			return nil
		}
		if err := s.cli.DropCollection(ctx, milvusclient.NewDropCollectionOption(name)); err != nil {
			return fmt.Errorf("drop collection: %w", err)
		}
	}

	indexOptions := []milvusclient.CreateIndexOption{
		milvusclient.NewCreateIndexOption(name, FieldVector, index.NewAutoIndex(entity.COSINE)).
			WithIndexName("vector_idx"),
	}
	err = s.cli.CreateCollection(ctx,
		milvusclient.NewCreateCollectionOption(name, Schema(name, dim)).WithIndexOptions(indexOptions...))
	if err != nil {
		return fmt.Errorf("create collection: %w", err)
	}
	return s.load(ctx, name)
}

// Insert writes every chunk together with its embedding.
func (s *Store) Insert(ctx context.Context, name string, dim int, chunks []pipeline.Chunk, vectors [][]float32) error {
	if len(chunks) != len(vectors) {
		return fmt.Errorf("chunks (%d) and vectors (%d) length mismatch", len(chunks), len(vectors))
	}

	ids := make([]string, len(chunks))
	texts := make([]string, len(chunks))
	parentTexts := make([]string, len(chunks))
	volumes := make([]string, len(chunks))
	chapterNos := make([]int64, len(chunks))
	clauseNos := make([]int64, len(chunks))
	pageNos := make([]int64, len(chunks))
	chunkIndexes := make([]int64, len(chunks))
	formulaBytes := make([][]byte, len(chunks))
	formulaTexts := make([]string, len(chunks))
	for i, c := range chunks {
		ids[i] = c.ID
		texts[i] = c.Text
		parentTexts[i] = c.ParentText
		volumes[i] = c.Volume
		chapterNos[i] = int64(c.ChapterNo)
		clauseNos[i] = c.ClauseNo
		pageNos[i] = c.PageNo
		chunkIndexes[i] = c.ChunkIndex
		formulas := c.Formulas
		if formulas == nil {
			formulas = []string{}
		}
		b, err := json.Marshal(formulas)
		if err != nil {
			return fmt.Errorf("marshal formulas for %s: %w", c.ID, err)
		}
		formulaBytes[i] = b
		formulaTexts[i] = strings.Join(formulas, formulaSeparator)
	}

	_, err := s.cli.Insert(ctx, milvusclient.NewColumnBasedInsertOption(name).
		WithVarcharColumn(FieldID, ids).
		WithFloatVectorColumn(FieldVector, dim, vectors).
		WithVarcharColumn(FieldText, texts).
		WithVarcharColumn(FieldParentText, parentTexts).
		WithVarcharColumn(FieldVolume, volumes).
		WithInt64Column(FieldChapterNo, chapterNos).
		WithInt64Column(FieldClauseNo, clauseNos).
		WithInt64Column(FieldPageNo, pageNos).
		WithInt64Column(FieldChunkIndex, chunkIndexes).
		WithVarcharColumn(FieldFormulaText, formulaTexts).
		WithColumns(column.NewColumnJSONBytes(FieldFormula, formulaBytes)))
	if err != nil {
		return fmt.Errorf("insert: %w", err)
	}
	return nil
}

// load brings a collection into memory so that it can be searched.
func (s *Store) load(ctx context.Context, name string) error {
	if _, err := s.cli.Flush(ctx, milvusclient.NewFlushOption(name)); err != nil {
		return fmt.Errorf("flush: %w", err)
	}
	if _, err := s.cli.LoadCollection(ctx, milvusclient.NewLoadCollectionOption(name)); err != nil {
		return fmt.Errorf("load collection: %w", err)
	}
	return nil
}

// Reload must be called after Insert before searching.
func (s *Store) Reload(ctx context.Context, name string) error {
	return s.load(ctx, name)
}

// Count returns the number of rows in the collection.
func (s *Store) Count(ctx context.Context, name string) (int64, error) {
	stats, err := s.cli.GetCollectionStats(ctx, milvusclient.NewGetCollectionStatsOption(name))
	if err != nil {
		return 0, err
	}
	raw, ok := stats["row_count"]
	if !ok {
		return 0, fmt.Errorf("row_count missing in collection stats")
	}
	return strconv.ParseInt(raw, 10, 64)
}
