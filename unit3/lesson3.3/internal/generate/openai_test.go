package generate

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestDeepSeekStream drives the SDK against a mock OpenAI-compatible endpoint
// and verifies the tokens are surfaced and the request is well-formed.
func TestDeepSeekStream(t *testing.T) {
	var gotPath string
	var gotAuth string
	var gotBody []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		gotBody, _ = io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/event-stream")
		chunks := []string{
			`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-chat","choices":[{"index":0,"delta":{"content":"太阳"},"finish_reason":null}]}`,
			`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-chat","choices":[{"index":0,"delta":{"content":"照常"},"finish_reason":null}]}`,
			`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-chat","choices":[{"index":0,"delta":{"content":"升起"},"finish_reason":null}]}`,
			`{"id":"c1","object":"chat.completion.chunk","created":1,"model":"deepseek-chat","choices":[{"index":0,"delta":{},"finish_reason":"stop"}]}`,
		}
		for _, c := range chunks {
			fmt.Fprintf(w, "data: %s\n\n", c)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	defer srv.Close()

	client, err := NewDeepSeek(DeepSeekOptions{APIKey: "sk-test", BaseURL: srv.URL, Model: "deepseek-chat"})
	if err != nil {
		t.Fatal(err)
	}
	var tokens []string
	if err := client.Stream(context.Background(), "你是助手", "问：桂枝汤？", func(s string) {
		tokens = append(tokens, s)
	}); err != nil {
		t.Fatal(err)
	}
	if gotPath != "/chat/completions" {
		t.Fatalf("path = %s, want /chat/completions", gotPath)
	}
	if got := strings.Join(tokens, ""); got != "太阳照常升起" {
		t.Fatalf("tokens = %q, want 太阳照常升起", got)
	}
	if !strings.Contains(string(gotBody), `"deepseek-chat"`) {
		t.Fatalf("request body missing model: %s", gotBody)
	}
	if gotAuth != "Bearer sk-test" {
		t.Fatalf("Authorization = %q, want Bearer sk-test", gotAuth)
	}
}

// TestDeepSeekRequiresKey guards against accidentally generating from an
// anonymous client.
func TestDeepSeekRequiresKey(t *testing.T) {
	if _, err := NewDeepSeek(DeepSeekOptions{BaseURL: "https://api.deepseek.com"}); err == nil {
		t.Fatal("expected error for empty API key")
	}
}

func TestNormalizeOpenAIBaseURL(t *testing.T) {
	for in, want := range map[string]string{
		"https://api.deepseek.com":               "https://api.deepseek.com/",
		"https://api.deepseek.com/":              "https://api.deepseek.com/",
		"https://api.deepseek.com/v1":            "https://api.deepseek.com/v1/",
		"https://api.deepseek.com/v1/":           "https://api.deepseek.com/v1/",
		"http://localhost:8000/chat/completions": "http://localhost:8000/",
	} {
		got, err := normalizeOpenAIBaseURL(in)
		if err != nil {
			t.Fatalf("normalize(%q): %v", in, err)
		}
		if got != want {
			t.Fatalf("normalize(%q) = %q, want %q", in, got, want)
		}
	}
}
