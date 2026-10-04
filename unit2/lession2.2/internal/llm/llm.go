// Package llm defines the provider-agnostic request and response types used by
// the HTTP layer. Keeping these types free of SDK structs means callers, and the
// openai-go/v3 adapter, can evolve independently.
package llm

import (
	"context"
	"encoding/json"
)

// Message roles accepted by the chat completions endpoint.
const (
	RoleDeveloper = "developer"
	RoleSystem    = "system"
	RoleUser      = "user"
	RoleAssistant = "assistant"
	RoleTool      = "tool"
)

// Message is a single turn of a conversation. The JSON shape deliberately
// mirrors the OpenAI wire format so requests can be round-tripped untouched.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content"`
	Name       string     `json:"name,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
}

// ToolCall is a function invocation requested by the model. Arguments stay as
// raw JSON because the model, not this service, decides their shape.
type ToolCall struct {
	ID       string       `json:"id,omitempty"`
	Type     string       `json:"type,omitempty"`
	Index    int          `json:"index,omitempty"`
	Function FunctionCall `json:"function"`
}

// FunctionCall carries the function name and its raw JSON arguments.
type FunctionCall struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments"`
}

// Tool exposes a callable function to the model.
type Tool struct {
	Type     string           `json:"type"`
	Function ToolFunctionSpec `json:"function"`
}

// ToolFunctionSpec is the JSON Schema description of a callable function.
type ToolFunctionSpec struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// ResponseFormatType enumerates the output shapes the model may be constrained to.
type ResponseFormatType string

const (
	ResponseFormatText       ResponseFormatType = "text"
	ResponseFormatJSONObject ResponseFormatType = "json_object"
	ResponseFormatJSONSchema ResponseFormatType = "json_schema"
)

// ResponseFormat constrains the model output. Schema is only read for the
// json_schema type.
type ResponseFormat struct {
	Type   ResponseFormatType `json:"type"`
	Schema *JSONSchemaFormat  `json:"json_schema,omitempty"`
}

// JSONSchemaFormat is a Structured Outputs schema definition.
type JSONSchemaFormat struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Schema      json.RawMessage `json:"schema,omitempty"`
	Strict      *bool           `json:"strict,omitempty"`
}

// ChatRequest is a chat completion request.
type ChatRequest struct {
	Model               string          `json:"model,omitempty"`
	Messages            []Message       `json:"messages"`
	Temperature         *float64        `json:"temperature,omitempty"`
	TopP                *float64        `json:"top_p,omitempty"`
	MaxTokens           *int            `json:"max_tokens,omitempty"`
	MaxCompletionTokens *int            `json:"max_completion_tokens,omitempty"`
	Stop                []string        `json:"stop,omitempty"`
	Seed                *int            `json:"seed,omitempty"`
	User                string          `json:"user,omitempty"`
	Tools               []Tool          `json:"tools,omitempty"`
	ToolChoice          json.RawMessage `json:"tool_choice,omitempty"`
	ResponseFormat      *ResponseFormat `json:"response_format,omitempty"`
	Stream              bool            `json:"stream"`
}

// ChatResponse is a normalized, single-choice chat completion. Multi-choice
// requests are served by picking the first choice.
type ChatResponse struct {
	ID           string     `json:"id"`
	Object       string     `json:"object"`
	Created      int64      `json:"created"`
	Model        string     `json:"model"`
	Content      string     `json:"content"`
	Refusal      string     `json:"refusal,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	Usage        Usage      `json:"usage"`
}

// Usage reports token accounting for a single call.
type Usage struct {
	PromptTokens     int64 `json:"prompt_tokens"`
	CompletionTokens int64 `json:"completion_tokens"`
	TotalTokens      int64 `json:"total_tokens"`
}

// StreamEvent is one incremental update forwarded from an upstream stream. Err
// is set on the terminal event when the stream fails; it is never serialized.
type StreamEvent struct {
	Type         string     `json:"type"`
	Delta        string     `json:"delta,omitempty"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason,omitempty"`
	Usage        *Usage     `json:"usage,omitempty"`

	Err error `json:"-"`
}

// ResponsesRequest is a request against the responses endpoint. Input accepts
// the same message shapes as the chat endpoint.
type ResponsesRequest struct {
	Model              string          `json:"model,omitempty"`
	Input              []Message       `json:"input,omitempty"`
	Instructions       string          `json:"instructions,omitempty"`
	PreviousResponseID string          `json:"previous_response_id,omitempty"`
	Temperature        *float64        `json:"temperature,omitempty"`
	TopP               *float64        `json:"top_p,omitempty"`
	MaxOutputTokens    *int            `json:"max_output_tokens,omitempty"`
	Store              *bool           `json:"store,omitempty"`
	User               string          `json:"user,omitempty"`
	Tools              []Tool          `json:"tools,omitempty"`
	ToolChoice         json.RawMessage `json:"tool_choice,omitempty"`
	Text               *ResponsesText  `json:"text,omitempty"`
	Stream             bool            `json:"stream"`
}

// ResponsesText configures output shape and verbosity for the responses
// endpoint.
type ResponsesText struct {
	Verbosity string          `json:"verbosity,omitempty"`
	Format    *ResponseFormat `json:"format,omitempty"`
}

// ResponsesResponse is a normalized, single-choice response object.
type ResponsesResponse struct {
	ID         string               `json:"id"`
	Object     string               `json:"object"`
	Model      string               `json:"model"`
	Status     string               `json:"status"`
	OutputText string               `json:"output_text"`
	Output     []ResponseOutputItem `json:"output"`
	Usage      Usage                `json:"usage"`
}

// ResponseOutputItem is one item of a response output list.
type ResponseOutputItem struct {
	Type      string                `json:"type"`
	ID        string                `json:"id,omitempty"`
	Role      string                `json:"role,omitempty"`
	Status    string                `json:"status,omitempty"`
	CallID    string                `json:"call_id,omitempty"`
	Name      string                `json:"name,omitempty"`
	Arguments string                `json:"arguments,omitempty"`
	Content   []ResponseContentPart `json:"content,omitempty"`
}

// ResponseContentPart is one content block of a message output item.
type ResponseContentPart struct {
	Type    string `json:"type"`
	Text    string `json:"text,omitempty"`
	Refusal string `json:"refusal,omitempty"`
}

// Client is the provider-agnostic surface the HTTP layer depends on.
type Client interface {
	// Chat performs a non-streaming chat completion.
	Chat(ctx context.Context, request ChatRequest) (*ChatResponse, error)
	// StreamChat performs a chat completion and emits incremental events.
	StreamChat(ctx context.Context, request ChatRequest) (<-chan StreamEvent, error)
	// CreateResponse performs a non-streaming responses call.
	CreateResponse(ctx context.Context, request ResponsesRequest) (*ResponsesResponse, error)
	// StreamResponse performs a responses call and emits incremental events.
	StreamResponse(ctx context.Context, request ResponsesRequest) (<-chan StreamEvent, error)
}
