// Package generate is a minimal streaming client for Ollama's chat API.
//
// It is deliberately small: the point of lesson3.2 is the retrieval pipeline,
// and any OpenAI-compatible client (e.g. github.com/openai/openai-go, used in
// unit2) can be swapped in behind the same Stream signature.
package generate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"
)

// Client streams chat completions from a local Ollama server.
type Client struct {
	baseURL string
	model   string
	http    *http.Client
}

// New creates a chat client for model served at baseURL.
func New(baseURL, model string) *Client {
	return &Client{
		baseURL: baseURL,
		model:   model,
		http:    &http.Client{Timeout: 5 * time.Minute},
	}
}

type chatMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type chatRequest struct {
	Model    string         `json:"model"`
	Messages []chatMessage  `json:"messages"`
	Stream   bool           `json:"stream"`
	Options  map[string]any `json:"options,omitempty"`
}

type chatChunk struct {
	Message chatMessage `json:"message"`
	Done    bool        `json:"done"`
	Error   string      `json:"error"`
}

// Stream sends the system+user messages and calls onToken for every token as it
// arrives. It returns when the model is done or the context is cancelled.
func (c *Client) Stream(ctx context.Context, system, user string, onToken func(string)) error {
	reqBody := chatRequest{
		Model:  c.model,
		Stream: true,
		Messages: []chatMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: user},
		},
		// Low temperature: this is a citation-grounded QA task.
		Options: map[string]any{"temperature": 0.2},
	}
	body, err := json.Marshal(reqBody)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return fmt.Errorf("ollama chat status %d: %s", resp.StatusCode, string(msg))
	}

	dec := json.NewDecoder(resp.Body)
	for {
		var chunk chatChunk
		if err := dec.Decode(&chunk); err != nil {
			if errors.Is(err, io.EOF) {
				return nil
			}
			return err
		}
		if chunk.Error != "" {
			return errors.New(chunk.Error)
		}
		if chunk.Message.Content != "" {
			onToken(chunk.Message.Content)
		}
		if chunk.Done {
			return nil
		}
	}
}
