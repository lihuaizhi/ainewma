package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/lihuaizhi/ainewma/unit2/lession2.2/internal/llm"
)

// sseTerminator is the sentinel that ends an OpenAI-compatible SSE body.
const sseTerminator = "[DONE]"

// streamEvents opens an upstream stream and relays it to the client as
// Server-Sent Events. open is called only after the response is ready to be
// streamed, but before any bytes are written, so that a failure to connect can
// still be reported as a normal HTTP error.
func (h *Handler) streamEvents(w http.ResponseWriter, r *http.Request, open func() (<-chan llm.StreamEvent, error)) {
	events, err := open()
	if err != nil {
		if r.Context().Err() != nil {
			return
		}
		h.logger.Error("start upstream stream", "error", err)
		writeError(w, http.StatusBadGateway, "upstream LLM stream failed")
		return
	}

	setSSEHeaders(w)
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)
	if flusher != nil {
		// Flush the headers immediately so clients start reading before the
		// model produces its first token.
		flusher.Flush()
	}

	for {
		select {
		case <-r.Context().Done():
			return
		case event, ok := <-events:
			if !ok {
				writeSSEData(w, sseTerminator)
				return
			}
			if event.Err != nil {
				if !errors.Is(event.Err, context.Canceled) {
					h.logger.Error("read upstream stream", "error", event.Err)
				}
				if r.Context().Err() != nil {
					// The client hung up; nothing useful left to write.
					return
				}
				writeSSEError(w, "upstream LLM stream failed")
				writeSSEData(w, sseTerminator)
				return
			}
			if err := writeSSEEvent(w, event); err != nil {
				return
			}
			if flusher != nil {
				flusher.Flush()
			}
		}
	}
}

// writeSSEEvent emits one domain event as a named SSE data frame. The event
// name lets browsers and SDK consumers dispatch without parsing the payload.
func writeSSEEvent(w io.Writer, event llm.StreamEvent) error {
	name := event.Type
	if strings.TrimSpace(name) == "" {
		name = "message"
	}
	payload, err := json.Marshal(event)
	if err != nil {
		return fmt.Errorf("encode stream event: %w", err)
	}
	if _, err := fmt.Fprintf(w, "event: %s\n", name); err != nil {
		return err
	}
	return writeSSEData(w, string(payload))
}

// writeSSEData writes a single data frame. Payloads are always emitted on one
// line: json.Marshal never emits raw newlines, and multi-line data would need
// to be split across repeated data fields.
func writeSSEData(w io.Writer, data string) error {
	if _, err := fmt.Fprintf(w, "data: %s\n\n", data); err != nil {
		return err
	}
	return nil
}

func writeSSEError(w io.Writer, message string) {
	payload, _ := json.Marshal(map[string]any{
		"error": map[string]string{"message": message},
	})
	_ = writeSSEData(w, string(payload))
}

// setSSEHeaders disables every buffering layer between this handler and the
// client, otherwise tokens arrive in one batch at the end of the stream.
func setSSEHeaders(w http.ResponseWriter) {
	header := w.Header()
	header.Set("Content-Type", "text/event-stream; charset=utf-8")
	header.Set("Cache-Control", "no-cache")
	header.Set("Connection", "keep-alive")
	header.Set("X-Accel-Buffering", "no")
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

func writeError(w http.ResponseWriter, status int, message string) {
	writeJSON(w, status, map[string]any{
		"error": map[string]string{"message": message},
	})
}

func methodNotAllowed(w http.ResponseWriter, method string) {
	w.Header().Set("Allow", method)
	writeError(w, http.StatusMethodNotAllowed, "method not allowed")
}
