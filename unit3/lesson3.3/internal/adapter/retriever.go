package adapter

import (
	"context"

	l2config "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/config"
	l2embed "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/embed"
	l2generate "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/generate"
	l2rerank "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/rerank"
	l2retrieve "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/retrieve"
	l2store "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/store"
)

type Adapter struct {
	st        *l2store.Store
	embedder  *l2embed.Client
	retriever *l2retrieve.Retriever
}

func New(ctx context.Context, addr, ollamaURL, embedModel, collection, rerankKind, rerankURL string) (*Adapter, error) {
	st, err := l2store.Open(ctx, addr)
	if err != nil {
		return nil, err
	}
	embedder := l2embed.New(ollamaURL, embedModel, 1)
	rr := l2rerank.New(rerankKind, rerankURL)
	retriever := l2retrieve.New(st, embedder, collection, rr)
	return &Adapter{st: st, embedder: embedder, retriever: retriever}, nil
}

func (a *Adapter) Close(ctx context.Context)        { a.st.Close(ctx) }
func (a *Adapter) Retriever() *l2retrieve.Retriever { return a.retriever }
func (a *Adapter) Store() *l2store.Store            { return a.st }
func (a *Adapter) Embedder() *l2embed.Client        { return a.embedder }

type Streamer interface {
	Stream(ctx context.Context, system, user string, onToken func(string)) error
}

func NewOllama(baseURL, model string) Streamer {
	if model == "" {
		model = l2config.ChatModel
	}
	return l2generate.New(baseURL, model)
}

func NewDeepSeek(apiKey, baseURL, model string) (Streamer, error) {
	if model == "" {
		model = l2config.DeepSeekModel
	}
	opts := l2generate.DeepSeekOptions{APIKey: apiKey, BaseURL: baseURL, Model: model}
	return l2generate.NewDeepSeek(opts)
}
