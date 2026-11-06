// Package embed is a minimal client for the local Ollama embedding API.
package embed

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"time"
)

// Client calls Ollama's /api/embed endpoint (bge-m3 by default).
type Client struct {
	baseURL string
	model   string
	batch   int
	http    *http.Client
}

// New creates a Client. batch is the maximum number of texts per request.
func New(baseURL, model string, batch int) *Client {
	if batch <= 0 {
		batch = 16
	}
	return &Client{
		baseURL: baseURL,
		model:   model,
		batch:   batch,
		http:    &http.Client{Timeout: 120 * time.Second},
	}
}

type embedRequest struct {
	Model string   `json:"model"`
	Input []string `json:"input"`
}

type embedResponse struct {
	Embeddings [][]float32 `json:"embeddings"`
	Error      string      `json:"error"`
}

// Embed returns one vector per input text, preserving order.
func (c *Client) Embed(ctx context.Context, texts []string) ([][]float32, error) {
	vectors := make([][]float32, 0, len(texts))
	total := (len(texts) + c.batch - 1) / c.batch
	for start := 0; start < len(texts); start += c.batch {
		end := min(start+c.batch, len(texts))
		batchVectors, err := c.embedBatch(ctx, texts[start:end])
		if err != nil {
			return nil, fmt.Errorf("embed batch [%d:%d]: %w", start, end, err)
		}
		vectors = append(vectors, batchVectors...)
		log.Printf("embed progress: %d/%d batches (%d/%d texts)",
			start/c.batch+1, total, end, len(texts))
	}
	return vectors, nil
}

func (c *Client) embedBatch(ctx context.Context, texts []string) ([][]float32, error) {
	body, err := json.Marshal(embedRequest{Model: c.model, Input: texts})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/embed", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return nil, fmt.Errorf("ollama status %d: %s", resp.StatusCode, string(msg))
	}

	var out embedResponse
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if out.Error != "" {
		return nil, fmt.Errorf("ollama error: %s", out.Error)
	}
	if len(out.Embeddings) != len(texts) {
		return nil, fmt.Errorf("ollama returned %d vectors for %d inputs", len(out.Embeddings), len(texts))
	}
	return out.Embeddings, nil
}
