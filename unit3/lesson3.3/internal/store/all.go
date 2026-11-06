package store

import (
	"context"
	"fmt"
	"io"

	"github.com/milvus-io/milvus/client/v2/milvusclient"
)

// All returns every row of a collection. The hybrid retriever uses it to build
// the BM25 arm in memory, so the lexical side scores exactly the same chunk
// texts that were embedded. The query iterator pulls rows in batches, which
// keeps this working for full-book collections too.
func (s *Store) All(ctx context.Context, name string) ([]Hit, error) {
	iter, err := s.cli.QueryIterator(ctx,
		milvusclient.NewQueryIteratorOption(name).
			WithBatchSize(512).
			WithOutputFields(FieldID, FieldText, FieldParentText, FieldVolume, FieldChapterNo, FieldClauseNo, FieldFormula, FieldPageNo))
	if err != nil {
		return nil, fmt.Errorf("all: query iterator: %w", err)
	}

	var out []Hit
	for {
		rs, err := iter.Next(ctx)
		if err == io.EOF {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("all: iterate: %w", err)
		}
		for i := 0; i < rs.Len(); i++ {
			out = append(out, Hit{
				// The query iterator result set has no rs.IDs, so the id comes
				// from the primary-key column instead.
				ID:         stringAt(rs, FieldID, i),
				Text:       stringAt(rs, FieldText, i),
				ParentText: stringAt(rs, FieldParentText, i),
				Volume:     stringAt(rs, FieldVolume, i),
				ChapterNo:  intAt(rs, FieldChapterNo, i),
				ClauseNo:   intAt(rs, FieldClauseNo, i),
				Formulas:   jsonAt(rs, FieldFormula, i),
				PageNo:     intAt(rs, FieldPageNo, i),
			})
		}
	}
	return out, nil
}
