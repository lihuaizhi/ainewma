// Package httpapi exposes the LLM client over HTTP: JSON for one-shot calls
// and Server-Sent Events for incremental ones.
package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"mime"
	"net/http"
	"strings"

	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/llm"
)

// Handler routes HTTP requests onto an llm.Client.
type Handler struct {
	client       llm.Client
	defaultModel string
	maxBody      int64
	logger       *slog.Logger
	routes       *http.ServeMux
}

// NewHandler builds the HTTP handler. maxBody caps request bodies; zero falls
// back to 1 MiB. defaultModel is applied to requests that omit the model.
func NewHandler(client llm.Client, defaultModel string, maxBody int64) http.Handler {
	if maxBody <= 0 {
		maxBody = 1 << 20
	}
	if strings.TrimSpace(defaultModel) == "" {
		defaultModel = "gpt-4o-mini"
	}
	handler := &Handler{
		client:       client,
		defaultModel: defaultModel,
		maxBody:      maxBody,
		logger:       slog.Default(),
		routes:       http.NewServeMux(),
	}
	handler.routes.HandleFunc("/healthz", handler.health)
	handler.routes.HandleFunc("/v1/chat/completions", handler.chatCompletions)
	handler.routes.HandleFunc("/v1/responses", handler.createResponse)
	handler.routes.HandleFunc("/v1/responses/stream", handler.streamResponse)
	return withRecovery(handler)
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.routes.ServeHTTP(w, r)
}

func (h *Handler) health(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		methodNotAllowed(w, http.MethodGet)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "model": h.defaultModel})
}

// chatCompletions serves both blocking and streaming chat completions. The
// request's stream field selects between JSON and SSE on the same path, which
// mirrors the upstream API.
func (h *Handler) chatCompletions(w http.ResponseWriter, r *http.Request) {
	request, ok := h.decodeChatRequest(w, r)
	if !ok {
		return
	}
	if !h.ready(w) {
		return
	}
	if request.Stream {
		h.streamEvents(w, r, func() (<-chan llm.StreamEvent, error) {
			return h.client.StreamChat(r.Context(), *request)
		})
		return
	}

	response, err := h.client.Chat(r.Context(), *request)
	if err != nil {
		h.upstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// createResponse serves the non-streaming responses endpoint.
func (h *Handler) createResponse(w http.ResponseWriter, r *http.Request) {
	request, ok := h.decodeResponsesRequest(w, r)
	if !ok {
		return
	}
	request.Stream = false
	if !h.ready(w) {
		return
	}
	response, err := h.client.CreateResponse(r.Context(), *request)
	if err != nil {
		h.upstreamError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, response)
}

// streamResponse serves the responses endpoint as SSE regardless of the
// request's stream field.
func (h *Handler) streamResponse(w http.ResponseWriter, r *http.Request) {
	request, ok := h.decodeResponsesRequest(w, r)
	if !ok {
		return
	}
	request.Stream = true
	if !h.ready(w) {
		return
	}
	h.streamEvents(w, r, func() (<-chan llm.StreamEvent, error) {
		return h.client.StreamResponse(r.Context(), *request)
	})
}

func (h *Handler) decodeChatRequest(w http.ResponseWriter, r *http.Request) (*llm.ChatRequest, bool) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return nil, false
	}
	var request llm.ChatRequest
	if !h.decodeJSON(w, r, &request) {
		return nil, false
	}
	if err := validateChatRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	if strings.TrimSpace(request.Model) == "" {
		request.Model = h.defaultModel
	}
	return &request, true
}

func (h *Handler) decodeResponsesRequest(w http.ResponseWriter, r *http.Request) (*llm.ResponsesRequest, bool) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, http.MethodPost)
		return nil, false
	}
	var request llm.ResponsesRequest
	if !h.decodeJSON(w, r, &request) {
		return nil, false
	}
	if err := validateResponsesRequest(request); err != nil {
		writeError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	if strings.TrimSpace(request.Model) == "" {
		request.Model = h.defaultModel
	}
	return &request, true
}

// decodeJSON reads exactly one JSON object from the request body, rejecting
// oversized bodies and trailing content.
func (h *Handler) decodeJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	if !isJSONContentType(r.Header.Get("Content-Type")) {
		writeError(w, http.StatusUnsupportedMediaType, "Content-Type must be application/json")
		return false
	}
	r.Body = http.MaxBytesReader(w, r.Body, h.maxBody)
	decoder := json.NewDecoder(r.Body)
	if err := decoder.Decode(target); err != nil {
		var maxBytesError *http.MaxBytesError
		switch {
		case errors.As(err, &maxBytesError):
			writeError(w, http.StatusRequestEntityTooLarge, "request body is too large")
		case errors.Is(err, io.EOF):
			writeError(w, http.StatusBadRequest, "request body is required")
		default:
			writeError(w, http.StatusBadRequest, "request body is invalid")
		}
		return false
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		writeError(w, http.StatusBadRequest, "request body must contain one JSON object")
		return false
	}
	return true
}

func (h *Handler) ready(w http.ResponseWriter) bool {
	if h.client != nil {
		return true
	}
	h.logger.Error("LLM client is not configured")
	writeError(w, http.StatusInternalServerError, "LLM client is not configured")
	return false
}

// upstreamError reports a failed upstream call. The upstream message is
// deliberately not echoed back: it can contain provider internals.
func (h *Handler) upstreamError(w http.ResponseWriter, err error) {
	if err == nil {
		writeError(w, http.StatusBadGateway, "upstream LLM request failed")
		return
	}
	h.logger.Error("upstream LLM request failed", "error", err)
	writeError(w, http.StatusBadGateway, "upstream LLM request failed")
}

func validateChatRequest(request llm.ChatRequest) error {
	if len(request.Messages) == 0 {
		return errors.New("messages must not be empty")
	}
	for index, message := range request.Messages {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "" {
			return fmt.Errorf("messages[%d].role must not be empty", index)
		}
		switch role {
		case llm.RoleDeveloper, llm.RoleSystem, llm.RoleUser, llm.RoleAssistant:
		case llm.RoleTool:
			if strings.TrimSpace(message.ToolCallID) == "" {
				return fmt.Errorf("messages[%d].tool_call_id must not be empty", index)
			}
		default:
			return fmt.Errorf("messages[%d].role %q is unsupported", index, message.Role)
		}
		if message.Content == "" && len(message.ToolCalls) == 0 {
			return fmt.Errorf("messages[%d].content must not be empty", index)
		}
	}
	for index, tool := range request.Tools {
		if strings.TrimSpace(tool.Function.Name) == "" {
			return fmt.Errorf("tools[%d].function.name must not be empty", index)
		}
	}
	if request.ResponseFormat != nil {
		switch request.ResponseFormat.Type {
		case "", llm.ResponseFormatText, llm.ResponseFormatJSONObject:
		case llm.ResponseFormatJSONSchema:
			if request.ResponseFormat.Schema == nil || strings.TrimSpace(request.ResponseFormat.Schema.Name) == "" {
				return errors.New("response_format.json_schema.name must not be empty for json_schema output")
			}
		default:
			return fmt.Errorf("response_format.type %q is unsupported", request.ResponseFormat.Type)
		}
	}
	return nil
}

func validateResponsesRequest(request llm.ResponsesRequest) error {
	if len(request.Input) == 0 &&
		strings.TrimSpace(request.Instructions) == "" &&
		strings.TrimSpace(request.PreviousResponseID) == "" {
		return errors.New("input, instructions or previous_response_id must not all be empty")
	}
	for index, message := range request.Input {
		role := strings.ToLower(strings.TrimSpace(message.Role))
		if role == "" {
			return fmt.Errorf("input[%d].role must not be empty", index)
		}
		switch role {
		case llm.RoleDeveloper, llm.RoleSystem, llm.RoleUser, llm.RoleAssistant:
		case llm.RoleTool:
			if strings.TrimSpace(message.ToolCallID) == "" {
				return fmt.Errorf("input[%d].tool_call_id must not be empty", index)
			}
		default:
			return fmt.Errorf("input[%d].role %q is unsupported", index, message.Role)
		}
		if message.Content == "" {
			return fmt.Errorf("input[%d].content must not be empty", index)
		}
	}
	for index, tool := range request.Tools {
		if strings.TrimSpace(tool.Function.Name) == "" {
			return fmt.Errorf("tools[%d].function.name must not be empty", index)
		}
	}
	if request.Text != nil && request.Text.Verbosity != "" {
		switch request.Text.Verbosity {
		case "low", "medium", "high":
		default:
			return fmt.Errorf("text.verbosity %q is unsupported", request.Text.Verbosity)
		}
	}
	return nil
}

func isJSONContentType(value string) bool {
	mediaType, _, err := mime.ParseMediaType(value)
	return err == nil && mediaType == "application/json"
}

func withRecovery(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recovered := recover(); recovered != nil {
				slog.Default().Error("panic while handling request", "panic", recovered)
				writeError(w, http.StatusInternalServerError, "internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}
