package openai

import (
	"context"
	"errors"
	"fmt"

	"github.com/openai/openai-go/v3/responses"

	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/llm"
)

// CreateResponse performs a blocking responses call and normalizes it.
func (c *Client) CreateResponse(ctx context.Context, request llm.ResponsesRequest) (*llm.ResponsesResponse, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}
	params, err := buildResponsesParams(request, c.defaultModel)
	if err != nil {
		return nil, err
	}

	response, err := c.sdk.Responses.New(ctx, params)
	if err != nil {
		return nil, fmt.Errorf("create response: %w", err)
	}
	return responsesResponseFromSDK(response), nil
}

// StreamResponse opens an upstream responses stream and forwards its events.
func (c *Client) StreamResponse(ctx context.Context, request llm.ResponsesRequest) (<-chan llm.StreamEvent, error) {
	if ctx == nil {
		return nil, errors.New("context must not be nil")
	}
	params, err := buildResponsesParams(request, c.defaultModel)
	if err != nil {
		return nil, err
	}

	stream := c.sdk.Responses.NewStreaming(ctx, params)
	if err := stream.Err(); err != nil {
		_ = stream.Close()
		return nil, fmt.Errorf("start response stream: %w", err)
	}

	events := make(chan llm.StreamEvent)
	go func() {
		defer close(events)
		defer stream.Close()
		for stream.Next() {
			event, err := responsesEventFromSDK(stream.Current())
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
			sendError(ctx, events, fmt.Errorf("read response stream: %w", err))
		}
	}()
	return events, nil
}

// responsesResponseFromSDK flattens the SDK response object. Only items this
// service understands are expanded; anything else is carried as a typed
// placeholder so no information is lost.
func responsesResponseFromSDK(response *responses.Response) *llm.ResponsesResponse {
	if response == nil {
		return &llm.ResponsesResponse{}
	}
	result := &llm.ResponsesResponse{
		ID:         response.ID,
		Object:     string(response.Object),
		Model:      string(response.Model),
		Status:     string(response.Status),
		OutputText: response.OutputText(),
		Usage: llm.Usage{
			PromptTokens:     response.Usage.InputTokens,
			CompletionTokens: response.Usage.OutputTokens,
			TotalTokens:      response.Usage.TotalTokens,
		},
	}
	for _, item := range response.Output {
		result.Output = append(result.Output, convertSDKOutputItem(item))
	}
	return result
}

func convertSDKOutputItem(item responses.ResponseOutputItemUnion) llm.ResponseOutputItem {
	converted := llm.ResponseOutputItem{
		Type:   item.Type,
		ID:     item.ID,
		Role:   item.Role,
		Status: string(item.Status),
	}
	switch item.Type {
	case "message":
		for _, content := range item.Content {
			converted.Content = append(converted.Content, llm.ResponseContentPart{
				Type:    content.Type,
				Text:    content.Text,
				Refusal: content.Refusal,
			})
		}
	case "function_call":
		converted.CallID = item.CallID
		converted.Name = item.Name
		converted.Arguments = item.Arguments.OfString
	}
	return converted
}

// responsesEventFromSDK keeps only the text-delta and function-argument-delta
// events, which are what a token-by-token client needs. Lifecycle events are
// dropped here rather than in the SSE writer.
func responsesEventFromSDK(event responses.ResponseStreamEventUnion) (*llm.StreamEvent, error) {
	switch string(event.Type) {
	case "response.output_text.delta":
		if event.Delta == "" {
			return nil, nil
		}
		return &llm.StreamEvent{Type: "content.delta", Delta: event.Delta}, nil
	case "response.function_call_arguments.delta":
		if event.Delta == "" {
			return nil, nil
		}
		return &llm.StreamEvent{
			Type: "tool_calls.delta",
			ToolCalls: []llm.ToolCall{{
				Type:     "function",
				Function: llm.FunctionCall{Arguments: event.Delta},
			}},
		}, nil
	case "response.completed":
		// The terminal event embeds the whole response object, which is the
		// only place usage totals appear on a streamed responses call.
		completed := responsesResponseFromSDK(&event.Response)
		return &llm.StreamEvent{
			Type:  "finish",
			Usage: &completed.Usage,
		}, nil
	default:
		return nil, nil
	}
}
