package openai

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	sdk "github.com/openai/openai-go/v3"
	"github.com/openai/openai-go/v3/responses"
	"github.com/openai/openai-go/v3/shared/constant"

	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/llm"
)

// buildChatParams converts a domain chat request into SDK parameters.
func buildChatParams(request llm.ChatRequest, defaultModel string) (sdk.ChatCompletionNewParams, error) {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = defaultModel
	}
	if model == "" {
		return sdk.ChatCompletionNewParams{}, errors.New("model must not be empty")
	}

	messages := make([]sdk.ChatCompletionMessageParamUnion, 0, len(request.Messages))
	for index, message := range request.Messages {
		converted, err := convertMessage(message)
		if err != nil {
			return sdk.ChatCompletionNewParams{}, fmt.Errorf("messages[%d]: %w", index, err)
		}
		messages = append(messages, converted)
	}
	if len(messages) == 0 {
		return sdk.ChatCompletionNewParams{}, errors.New("messages must not be empty")
	}

	params := sdk.ChatCompletionNewParams{
		Model:    sdk.ChatModel(model),
		Messages: messages,
	}
	if request.Temperature != nil {
		params.Temperature = sdk.Float(*request.Temperature)
	}
	if request.TopP != nil {
		params.TopP = sdk.Float(*request.TopP)
	}
	if request.MaxTokens != nil {
		params.MaxTokens = sdk.Int(int64(*request.MaxTokens))
	}
	if request.MaxCompletionTokens != nil {
		params.MaxCompletionTokens = sdk.Int(int64(*request.MaxCompletionTokens))
	}
	if request.Seed != nil {
		params.Seed = sdk.Int(int64(*request.Seed))
	}
	if request.User != "" {
		params.User = sdk.String(request.User)
	}
	if len(request.Stop) == 1 {
		params.Stop = sdk.ChatCompletionNewParamsStopUnion{OfString: sdk.String(request.Stop[0])}
	} else if len(request.Stop) > 1 {
		params.Stop = sdk.ChatCompletionNewParamsStopUnion{OfStringArray: request.Stop}
	}

	extraFields, err := chatExtraFields(request)
	if err != nil {
		return sdk.ChatCompletionNewParams{}, err
	}
	if len(extraFields) > 0 {
		params.SetExtraFields(extraFields)
	}
	return params, nil
}

// chatExtraFields builds the nested tool and response_format objects. They are
// injected as raw JSON rather than assembled from SDK unions: the shapes are
// deep and mostly pass-through, and this keeps the adapter stable across SDK
// releases that add variants to those unions.
func chatExtraFields(request llm.ChatRequest) (map[string]any, error) {
	fields := make(map[string]any)
	if len(request.Tools) > 0 {
		fields["tools"] = request.Tools
	}
	if len(request.ToolChoice) > 0 {
		choice, err := decodeInto[any](request.ToolChoice)
		if err != nil {
			return nil, fmt.Errorf("tool_choice is invalid: %w", err)
		}
		fields["tool_choice"] = choice
	}
	if request.ResponseFormat != nil {
		format, err := convertResponseFormat(*request.ResponseFormat)
		if err != nil {
			return nil, err
		}
		fields["response_format"] = format
	}
	return fields, nil
}

// convertResponseFormat renders the OpenAI response_format object, validating
// the parts this service understands.
func convertResponseFormat(format llm.ResponseFormat) (map[string]any, error) {
	switch format.Type {
	case "", llm.ResponseFormatText:
		return map[string]any{"type": string(llm.ResponseFormatText)}, nil
	case llm.ResponseFormatJSONObject:
		return map[string]any{"type": string(llm.ResponseFormatJSONObject)}, nil
	case llm.ResponseFormatJSONSchema:
		if format.Schema == nil {
			return nil, errors.New("response_format.json_schema is required when type is json_schema")
		}
		if strings.TrimSpace(format.Schema.Name) == "" {
			return nil, errors.New("response_format.json_schema.name must not be empty")
		}
		payload := map[string]any{"name": format.Schema.Name}
		if format.Schema.Description != "" {
			payload["description"] = format.Schema.Description
		}
		if len(format.Schema.Schema) > 0 {
			schema, err := decodeInto[any](format.Schema.Schema)
			if err != nil {
				return nil, fmt.Errorf("response_format.json_schema.schema is invalid: %w", err)
			}
			payload["schema"] = schema
		}
		if format.Schema.Strict != nil {
			payload["strict"] = *format.Schema.Strict
		}
		return map[string]any{
			"type":        string(llm.ResponseFormatJSONSchema),
			"json_schema": payload,
		}, nil
	default:
		return nil, fmt.Errorf("response_format.type %q is unsupported", format.Type)
	}
}

func convertMessage(message llm.Message) (sdk.ChatCompletionMessageParamUnion, error) {
	role := strings.ToLower(strings.TrimSpace(message.Role))
	switch role {
	case llm.RoleDeveloper:
		param := &sdk.ChatCompletionDeveloperMessageParam{}
		param.Content.OfString = sdk.String(message.Content)
		if message.Name != "" {
			param.Name = sdk.String(message.Name)
		}
		return sdk.ChatCompletionMessageParamUnion{OfDeveloper: param}, nil
	case llm.RoleSystem:
		param := &sdk.ChatCompletionSystemMessageParam{}
		param.Content.OfString = sdk.String(message.Content)
		if message.Name != "" {
			param.Name = sdk.String(message.Name)
		}
		return sdk.ChatCompletionMessageParamUnion{OfSystem: param}, nil
	case llm.RoleUser:
		param := &sdk.ChatCompletionUserMessageParam{}
		param.Content.OfString = sdk.String(message.Content)
		if message.Name != "" {
			param.Name = sdk.String(message.Name)
		}
		return sdk.ChatCompletionMessageParamUnion{OfUser: param}, nil
	case llm.RoleAssistant:
		param := &sdk.ChatCompletionAssistantMessageParam{}
		param.Content.OfString = sdk.String(message.Content)
		if message.Name != "" {
			param.Name = sdk.String(message.Name)
		}
		if len(message.ToolCalls) > 0 {
			converted, err := convertAssistantToolCalls(message.ToolCalls)
			if err != nil {
				return sdk.ChatCompletionMessageParamUnion{}, err
			}
			param.ToolCalls = converted
		}
		return sdk.ChatCompletionMessageParamUnion{OfAssistant: param}, nil
	case llm.RoleTool:
		if strings.TrimSpace(message.ToolCallID) == "" {
			return sdk.ChatCompletionMessageParamUnion{}, errors.New("tool_call_id is required for tool messages")
		}
		param := &sdk.ChatCompletionToolMessageParam{
			ToolCallID: message.ToolCallID,
		}
		param.Content.OfString = sdk.String(message.Content)
		return sdk.ChatCompletionMessageParamUnion{OfTool: param}, nil
	default:
		return sdk.ChatCompletionMessageParamUnion{}, fmt.Errorf("role %q is unsupported", message.Role)
	}
}

func convertAssistantToolCalls(toolCalls []llm.ToolCall) ([]sdk.ChatCompletionMessageToolCallUnionParam, error) {
	converted := make([]sdk.ChatCompletionMessageToolCallUnionParam, 0, len(toolCalls))
	for index, toolCall := range toolCalls {
		callType := strings.TrimSpace(toolCall.Type)
		if callType == "" {
			callType = "function"
		}
		if callType != "function" {
			return nil, fmt.Errorf("tool_calls[%d]: type %q is unsupported", index, callType)
		}
		if strings.TrimSpace(toolCall.Function.Name) == "" {
			return nil, fmt.Errorf("tool_calls[%d]: function.name must not be empty", index)
		}
		arguments := toolCall.Function.Arguments
		if strings.TrimSpace(arguments) == "" {
			arguments = "{}"
		}
		param := &sdk.ChatCompletionMessageFunctionToolCallParam{
			ID:       toolCall.ID,
			Type:     constant.Function("function"),
			Function: sdk.ChatCompletionMessageFunctionToolCallFunctionParam{Name: toolCall.Function.Name, Arguments: arguments},
		}
		converted = append(converted, sdk.ChatCompletionMessageToolCallUnionParam{OfFunction: param})
	}
	return converted, nil
}

// buildResponsesParams converts a domain responses request into SDK parameters.
func buildResponsesParams(request llm.ResponsesRequest, defaultModel string) (responses.ResponseNewParams, error) {
	model := strings.TrimSpace(request.Model)
	if model == "" {
		model = defaultModel
	}
	if model == "" {
		return responses.ResponseNewParams{}, errors.New("model must not be empty")
	}

	params := responses.ResponseNewParams{
		Model: sdk.ResponsesModel(model),
	}
	if len(request.Input) > 0 {
		input, err := convertResponseInput(request.Input)
		if err != nil {
			return responses.ResponseNewParams{}, err
		}
		params.Input = responses.ResponseNewParamsInputUnion{OfInputItemList: input}
	} else if strings.TrimSpace(request.Instructions) == "" && strings.TrimSpace(request.PreviousResponseID) == "" {
		return responses.ResponseNewParams{}, errors.New("input, instructions or previous_response_id must not all be empty")
	}
	if request.Instructions != "" {
		params.Instructions = sdk.String(request.Instructions)
	}
	if request.PreviousResponseID != "" {
		params.PreviousResponseID = sdk.String(request.PreviousResponseID)
	}
	if request.Temperature != nil {
		params.Temperature = sdk.Float(*request.Temperature)
	}
	if request.TopP != nil {
		params.TopP = sdk.Float(*request.TopP)
	}
	if request.MaxOutputTokens != nil {
		params.MaxOutputTokens = sdk.Int(int64(*request.MaxOutputTokens))
	}
	if request.Store != nil {
		params.Store = sdk.Bool(*request.Store)
	}
	if request.User != "" {
		params.User = sdk.String(request.User)
	}

	extraFields := make(map[string]any)
	if len(request.Tools) > 0 {
		extraFields["tools"] = request.Tools
	}
	if len(request.ToolChoice) > 0 {
		choice, err := decodeInto[any](request.ToolChoice)
		if err != nil {
			return responses.ResponseNewParams{}, fmt.Errorf("tool_choice is invalid: %w", err)
		}
		extraFields["tool_choice"] = choice
	}
	if request.Text != nil {
		text, err := convertResponsesText(*request.Text)
		if err != nil {
			return responses.ResponseNewParams{}, err
		}
		extraFields["text"] = text
	}
	if len(extraFields) > 0 {
		params.SetExtraFields(extraFields)
	}
	return params, nil
}

// convertResponseInput maps chat-style messages onto responses input items. The
// tool and function-call items that feed multi-turn tool conversations are
// carried through as raw input items so their exact shape is preserved.
func convertResponseInput(messages []llm.Message) (responses.ResponseInputParam, error) {
	input := make(responses.ResponseInputParam, 0, len(messages))
	for index, message := range messages {
		switch strings.ToLower(strings.TrimSpace(message.Role)) {
		case llm.RoleUser, llm.RoleSystem, llm.RoleDeveloper, llm.RoleAssistant:
			param := &responses.EasyInputMessageParam{
				Role:    responses.EasyInputMessageRole(strings.ToLower(strings.TrimSpace(message.Role))),
				Content: responses.EasyInputMessageContentUnionParam{OfString: sdk.String(message.Content)},
			}
			input = append(input, responses.ResponseInputItemUnionParam{OfMessage: param})
		case llm.RoleTool:
			if strings.TrimSpace(message.ToolCallID) == "" {
				return nil, fmt.Errorf("input[%d]: tool_call_id is required for tool messages", index)
			}
			input = append(input, responses.ResponseInputItemUnionParam{
				OfFunctionCallOutput: &responses.ResponseInputItemFunctionCallOutputParam{
					CallID: message.ToolCallID,
					Output: responses.ResponseInputItemFunctionCallOutputOutputUnionParam{
						OfString: sdk.String(message.Content),
					},
				},
			})
		default:
			return nil, fmt.Errorf("input[%d]: role %q is unsupported", index, message.Role)
		}
	}
	if len(input) == 0 {
		return nil, errors.New("input must not be empty")
	}
	return input, nil
}

func convertResponsesText(text llm.ResponsesText) (map[string]any, error) {
	payload := make(map[string]any)
	if verbosity := strings.TrimSpace(text.Verbosity); verbosity != "" {
		switch verbosity {
		case "low", "medium", "high":
			payload["verbosity"] = verbosity
		default:
			return nil, fmt.Errorf("text.verbosity %q is unsupported", text.Verbosity)
		}
	}
	if text.Format != nil {
		format, err := convertResponseFormat(*text.Format)
		if err != nil {
			return nil, err
		}
		payload["format"] = format
	}
	return payload, nil
}

// decodeInto re-decodes a caller supplied raw value so it can be merged into
// the outgoing request body as plain JSON.
func decodeInto[T any](raw json.RawMessage) (T, error) {
	var value T
	if len(raw) == 0 || strings.TrimSpace(string(raw)) == "null" {
		return value, nil
	}
	if err := json.Unmarshal(raw, &value); err != nil {
		return value, err
	}
	return value, nil
}
