package generate

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"strings"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"
)

// DeepSeek streams chat completions from the official DeepSeek API. DeepSeek
// speaks the OpenAI protocol, so we drive it with the same openai-go SDK that
// unit2 uses, only pointing it at api.deepseek.com and using the DeepSeek
// model family.
type DeepSeek struct {
	client *sdk.Client
	model  string
}

// DeepSeekOptions configures the official DeepSeek API.
type DeepSeekOptions struct {
	// APIKey is the DeepSeek API key (DEEPSEEK_API_KEY).
	APIKey string
	// BaseURL defaults to https://api.deepseek.com.
	BaseURL string
	// Model defaults to deepseek-chat.
	Model string
}

// NewDeepSeek builds a DeepSeek chat client.
func NewDeepSeek(o DeepSeekOptions) (*DeepSeek, error) {
	if strings.TrimSpace(o.APIKey) == "" {
		return nil, errors.New("deepseek: API key is empty (set -api-key or DEEPSEEK_API_KEY)")
	}
	baseURL, err := normalizeOpenAIBaseURL(o.BaseURL)
	if err != nil {
		return nil, err
	}
	model := strings.TrimSpace(o.Model)
	if model == "" {
		model = "deepseek-chat"
	}
	client := sdk.NewClient(
		option.WithBaseURL(baseURL),
		option.WithAPIKey(strings.TrimSpace(o.APIKey)),
		option.WithHeader("Accept", "text/event-stream"),
		option.WithMaxRetries(0),
	)
	return &DeepSeek{client: &client, model: model}, nil
}

// Stream sends the system+user messages and calls onToken for every token as
// it arrives, mirroring the Ollama client's signature.
func (d *DeepSeek) Stream(ctx context.Context, system, user string, onToken func(string)) error {
	params := sdk.ChatCompletionNewParams{
		Model: sdk.ChatModel(d.model),
		Messages: []sdk.ChatCompletionMessageParamUnion{
			sdk.SystemMessage(system),
			sdk.UserMessage(user),
		},
		// Low temperature: citation-grounded QA, not creative writing.
		Temperature: sdk.Float(0.2),
	}

	stream := d.client.Chat.Completions.NewStreaming(ctx, params)
	defer stream.Close()
	for stream.Next() {
		for _, choice := range stream.Current().Choices {
			if choice.Delta.Content != "" {
				onToken(choice.Delta.Content)
			}
		}
	}
	return stream.Err()
}

// normalizeOpenAIBaseURL turns a user-facing base URL into one openai-go can
// append /chat/completions to. Accepts https://api.deepseek.com as well as the
// /v1 and /chat/completions variants.
func normalizeOpenAIBaseURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		rawURL = "https://api.deepseek.com"
	}
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("deepseek: invalid base URL %q: %w", rawURL, err)
	}
	if parsedURL.Scheme != "http" && parsedURL.Scheme != "https" {
		return "", fmt.Errorf("deepseek: base URL must use http or https: %q", rawURL)
	}
	if parsedURL.Host == "" {
		return "", fmt.Errorf("deepseek: base URL has no host: %q", rawURL)
	}
	path := strings.TrimRight(parsedURL.Path, "/")
	path = strings.TrimSuffix(path, "/chat/completions")
	if path == "" {
		path = "/"
	} else {
		path += "/"
	}
	parsedURL.Path = path
	parsedURL.RawPath = ""
	return parsedURL.String(), nil
}
