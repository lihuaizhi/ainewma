package openai

import (
	"context"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/openai/openai-go/v3"

	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/llm"
)

// Chat performs a blocking chat completion and returns the first choice.
func (c *Client) Chat(ctx context.Context, request llm.ChatRequest) (*llm.ChatResponse, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}
	params, err := buildChatParams(request, c.defaultModel)
	if err != nil {
		return nil, err
	}

	completion, err := c.sdk.Chat.Completions.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("chat completion: %w", err)
	}
	return chatResponseFromSDK(completion)
}

// StreamChat opens an upstream stream and forwards each chunk as a domain
// event. The returned channel is closed when the stream ends, when the
// upstream fails, or when ctx is cancelled.
func (c *Client) StreamChat(ctx context.Context, request llm.ChatRequest) (<-chan llm.StreamEvent, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}
	params, err := buildChatParams(request, c.defaultModel)
	if err != nil {
		return nil, err
	}

	stream := c.sdk.Chat.Completions.NewStreaming(ctx, params)
	if err := stream.Err(); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("start chat stream: %w", err)
	}

	events := make(chan llm.StreamEvent)
	go func() {
		defer close(events)
		defer stream.Close()
		for stream.Next() {
			chunk := stream.Current()
			event, err := chatEventFromChunk(chunk)
			if err != nil {
				if ctx.Err() == nil {
					sendError(ctx, events, err)
				}
				return
			}
			if event == nil {
				continue
			}
			if !send(ctx, events, *event) {
				return
			}
		}
		if err := stream.Err(); err != nil && ctx.Err() == nil {
			sendError(ctx, events, fmt.Errorf("read chat stream: %w", err))
		}
	}()
	return events, nil
}

// chatResponseFromSDK normalizes a completion. When the caller asked for JSON
// output and the model complied, Content is unwrapped so callers receive the
// payload itself rather than a string containing JSON.
func chatResponseFromSDK(completion *sdk.ChatCompletion) (*llm.ChatResponse, error) {
	if completion == nil {
		return nil, errors.New("chat completion is empty")
	}
	if len(completion.Choices) == 0 {
		return nil, errors.New("chat completion returned no choices")
	}
	choice := completion.Choices[0]
	response := &llm.ChatResponse{
		ID:           completion.ID,
		Object:       string(completion.Object),
		Created:      completion.Created,
		Model:        completion.Model,
		Content:      choice.Message.Content,
		Refusal:      choice.Message.Refusal,
		FinishReason: choice.FinishReason,
		Usage: llm.Usage{
			PromptTokens:     completion.Usage.PromptTokens,
			CompletionTokens: completion.Usage.CompletionTokens,
			TotalTokens:      completion.Usage.TotalTokens,
		},
	}

	for index, toolCall := range choice.Message.ToolCalls {
		converted := convertSDKToolCall(toolCall)
		converted.Index = index
		response.ToolCalls = append(response.ToolCalls, converted)
	}
	return response, nil
}

// chatEventFromChunk turns one upstream chunk into a domain event. It returns a
// nil event for chunks that carry no client-visible payload, such as the
// leading role-only delta and usage-only final chunks.
func chatEventFromChunk(chunk sdk.ChatCompletionChunk) (*llm.StreamEvent, error) {
	choices := chunk.Choices
	delta := ""
	var toolCalls []llm.ToolCall
	finishReason := ""
	if len(choices) > 0 {
		delta = choices[0].Delta.Content
		finishReason = choices[0].FinishReason
		for _, toolCall := range choices[0].Delta.ToolCalls {
			toolCalls = append(toolCalls, convertSDKDeltaToolCall(toolCall))
		}
	}

	hasDelta := delta != "" || len(toolCalls) > 0
	hasUsage := chunk.Usage.TotalTokens > 0
	if !hasDelta && finishReason == "" && !hasUsage {
		return nil, nil
	}

	event := &llm.StreamEvent{
		Delta:        delta,
		ToolCalls:    toolCalls,
		FinishReason: finishReason,
	}
	if hasUsage {
		event.Usage = &llm.Usage{
			PromptTokens:     chunk.Usage.PromptTokens,
			CompletionTokens: chunk.Usage.CompletionTokens,
			TotalTokens:      chunk.Usage.TotalTokens,
		}
	}
	event.Type = chatEventType(event)
	return event, nil
}

// chatEventType labels an event so SSE consumers can dispatch on it without
// inspecting which fields happen to be populated.
func chatEventType(event *llm.StreamEvent) string {
	switch {
	case len(event.ToolCalls) > 0:
		return "tool_calls.delta"
	case event.FinishReason != "":
		return "finish"
	case event.Usage != nil:
		return "usage"
	default:
		return "content.delta"
	}
}

// convertSDKToolCall reads a completed tool call from a non-streaming
// response.
func convertSDKToolCall(toolCall sdk.ChatCompletionMessageToolCallUnion) llm.ToolCall {
	callType := strings.TrimSpace(toolCall.Type)
	if callType == "" {
		callType = "function"
	}
	return llm.ToolCall{
		ID:   toolCall.ID,
		Type: callType,
		Function: llm.FunctionCall{
			Name:      toolCall.Function.Name,
			Arguments: toolCall.Function.Arguments,
		},
	}
}

// convertSDKDeltaToolCall reads a partial tool call from a streamed chunk.
// Streamed calls arrive in fragments: the first fragment carries the ID and
// name, later ones only append argument characters. Clients are expected to
// merge fragments by Index.
func convertSDKDeltaToolCall(toolCall sdk.ChatCompletionChunkChoiceDeltaToolCall) llm.ToolCall {
	callType := strings.TrimSpace(toolCall.Type)
	if callType == "" {
		callType = "function"
	}
	return llm.ToolCall{
		ID:    toolCall.ID,
		Type:  callType,
		Index: int(toolCall.Index),
		Function: llm.FunctionCall{
			Name:      toolCall.Function.Name,
			Arguments: toolCall.Function.Arguments,
		},
	}
}
