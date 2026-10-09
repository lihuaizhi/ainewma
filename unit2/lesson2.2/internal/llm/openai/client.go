// Package openai adapts the provider-agnostic llm types onto the official
// openai-go/v3 SDK.
package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/option"

	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/llm"
)

// Options configures the SDK client.
type Options struct {
	// APIURL is either a chat completions endpoint or a base URL ending in a
	// version segment such as /v1. Both forms are accepted.
	APIURL string
	// APIKey is sent as a bearer token. It may be empty for local gateways
	// that do not authenticate.
	APIKey string
	// DefaultModel is used when a request omits the model field.
	DefaultModel string
	// HTTPClient overrides the transport. Mainly useful for tests.
	HTTPClient *http.Client
	// ResponseHeaderTimeout bounds how long the upstream may take to send
	// response headers. It must stay zero for long-lived streams.
	ResponseHeaderTimeout time.Duration
	// MaxRetries overrides the SDK retry policy. Zero disables retries.
	MaxRetries int
	// OrgID and ProjectID are optional OpenAI organisation fields.
	OrgID     string
	ProjectID string
}

// Client implements llm.Client on top of openai-go/v3.
type Client struct {
	sdk          *sdk.Client
	defaultModel string
}

// New builds a Client from Options, validating the endpoint up front so that
// misconfiguration fails at startup rather than on the first request.
func New(options Options) (*Client, error) {
	baseURL, err := normalizeBaseURL(options.APIURL)
	if err != nil {
		return nil, err
	}
	defaultModel := strings.TrimSpace(options.DefaultModel)
	if defaultModel == "" {
		return nil, errors.New("default model must not be empty")
	}
	if options.ResponseHeaderTimeout < 0 {
		return nil, errors.New("response header timeout must not be negative")
	}
	if options.MaxRetries < 0 {
		return nil, errors.New("max retries must not be negative")
	}

	httpClient := options.HTTPClient
	if httpClient == nil {
		httpClient = newHTTPClient(options.ResponseHeaderTimeout)
	}

	requestOptions := []option.RequestOption{
		option.WithBaseURL(baseURL),
		option.WithAPIKey(strings.TrimSpace(options.APIKey)),
		option.WithHTTPClient(httpClient),
		option.WithMaxRetries(options.MaxRetries),
	}
	if orgID := strings.TrimSpace(options.OrgID); orgID != "" {
		requestOptions = append(requestOptions, option.WithOrganization(orgID))
	}
	if projectID := strings.TrimSpace(options.ProjectID); projectID != "" {
		requestOptions = append(requestOptions, option.WithProject(projectID))
	}

	client := sdk.NewClient(requestOptions...)
	return &Client{sdk: &client, defaultModel: defaultModel}, nil
}

// normalizeBaseURL converts a full endpoint URL into a base URL that the SDK
// can join request paths onto.
func normalizeBaseURL(rawURL string) (string, error) {
	rawURL = strings.TrimSpace(rawURL)
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		return "", fmt.Errorf("LLM API URL is invalid: %w", err)
	}
	if !strings.EqualFold(parsedURL.Scheme, "http") && !strings.EqualFold(parsedURL.Scheme, "https") {
		return "", errors.New("LLM API URL must use http or https")
	}
	if parsedURL.Host == "" || parsedURL.User != nil || parsedURL.Fragment != "" {
		return "", errors.New("LLM API URL must be an absolute URL without user info or fragment")
	}
	if parsedURL.RawQuery != "" {
		return "", errors.New("LLM API URL must not contain a query string")
	}

	path := strings.TrimRight(parsedURL.Path, "/")
	for _, suffix := range []string{"/chat/completions", "/responses", "/completions"} {
		path = strings.TrimSuffix(path, suffix)
	}
	if path == "" {
		path = "/"
	} else {
		path += "/"
	}
	parsedURL.Path = path
	parsedURL.RawPath = ""
	return parsedURL.String(), nil
}

func newHTTPClient(responseHeaderTimeout time.Duration) *http.Client {
	transport, ok := http.DefaultTransport.(*http.Transport)
	if ok {
		transport = transport.Clone()
	} else {
		transport = &http.Transport{}
	}
	if responseHeaderTimeout > 0 {
		transport.ResponseHeaderTimeout = responseHeaderTimeout
	}
	return &http.Client{Transport: transport}
}

// resolveModel falls back to the configured default model.
func (c *Client) resolveModel(requested string) (string, error) {
	model := strings.TrimSpace(requested)
	if model == "" {
		model = c.defaultModel
	}
	if model == "" {
		return "", errors.New("model must not be empty")
	}
	return model, nil
}

// send forwards one event, reporting false when the caller has gone away so
// that pump loops can exit without leaking a goroutine.
func send(ctx context.Context, events chan<- llm.StreamEvent, event llm.StreamEvent) bool {
	select {
	case events <- event:
		return true
	case <-ctx.Done():
		return false
	}
}

func sendError(ctx context.Context, events chan<- llm.StreamEvent, err error) {
	send(ctx, events, llm.StreamEvent{Err: err})
}

var _ llm.Client = (*Client)(nil)
