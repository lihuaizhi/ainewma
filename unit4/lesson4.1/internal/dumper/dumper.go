// Package dumper reproduces lesson3.3's request-dumper at framework level and
// makes it a shared package so both cmd/service and cmd/evalroute can trace
// every LLM request of the customer service cluster.
//
// In a multi-agent system the interesting question is not only "what was sent"
// but "which agent sent it": the chat request JSON callback receives the
// invocation context, so each record is labeled with the invoking agent's
// registered name (客服助手 / 退货助手 / ...). Output:
//
//	-D?      pretty summary of every request to stderr (stdout stays clean,
//	         it still carries the streamed user-visible answer)
//	-file?   one JSON record per LLM call, ready for jq / regression diffs
package dumper

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
	"time"

	"trpc.group/trpc-go/trpc-agent-go/agent"
)

// Dumper records the raw chat requests sent to the LLM.
type Dumper struct {
	mu      sync.Mutex
	count   int
	enabled bool // pretty summary on stderr
	file    *os.File
}

// New creates a dumper. It is active when enabled (stderr summary) or path
// is non-empty (JSONL file).
func New(enabled bool, path string) (*Dumper, error) {
	d := &Dumper{enabled: enabled}
	if path != "" {
		f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o644)
		if err != nil {
			return nil, err
		}
		d.file = f
	}
	return d, nil
}

// Close releases the JSONL file.
func (d *Dumper) Close() error {
	if d == nil || d.file == nil {
		return nil
	}
	return d.file.Close()
}

type record struct {
	Time    string          `json:"time"`
	Seq     int             `json:"seq"`
	Agent   string          `json:"agent"`
	Inv     string          `json:"invocation,omitempty"`
	Request json.RawMessage `json:"request,omitempty"`
	Error   string          `json:"error,omitempty"`
}

// DumpJSON implements openai.ChatRequestJSONCallbackFunc. Signature matches:
// func(ctx context.Context, raw []byte, marshalErr error). The label is the
// AgentName from the invocation context carried by ctx, i.e. which agent in
// the cluster was making the call when this request left the gate.
func (d *Dumper) DumpJSON(ctx context.Context, raw []byte, marshalErr error) {
	if !d.active() {
		return
	}
	who := ""
	invID := ""
	if inv, ok := agent.InvocationFromContext(ctx); ok {
		who = inv.AgentName
		invID = inv.InvocationID
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.count++

	if marshalErr != nil {
		if d.file != nil {
			d.write(record{
				Time: time.Now().Format(time.RFC3339Nano), Seq: d.count,
				Agent: who, Inv: invID, Error: marshalErr.Error(),
			})
		}
		if d.enabled {
			fmt.Fprintf(os.Stderr, "\n===== LLM REQUEST #%d [%s]: marshal error: %v =====\n", d.count, who, marshalErr)
		}
		return
	}

	if d.file != nil {
		d.write(record{Time: time.Now().Format(time.RFC3339Nano), Seq: d.count, Agent: who, Inv: invID, Request: json.RawMessage(raw)})
	}
	if !d.enabled {
		return
	}

	var meta struct {
		Model    string            `json:"model"`
		Stream   bool              `json:"stream"`
		Messages []json.RawMessage `json:"messages"`
		Tools    []struct {
			Function struct {
				Name string `json:"name"`
			} `json:"function"`
		} `json:"tools"`
	}
	_ = json.Unmarshal(raw, &meta)
	names := make([]string, 0, len(meta.Tools))
	for _, t := range meta.Tools {
		names = append(names, t.Function.Name)
	}
	if len(names) == 0 {
		names = []string{"(none)"}
	}

	var pretty bytes.Buffer
	if err := json.Indent(&pretty, raw, "", "  "); err != nil {
		pretty.Write(raw)
	}
	fmt.Fprintf(os.Stderr, "\n===== LLM REQUEST #%d [%s] =====\n", d.count, label(who))
	fmt.Fprintf(os.Stderr, "model: %s stream: %v messages: %d tools: %s\n",
		meta.Model, meta.Stream, len(meta.Messages), strings.Join(names, ", "))
	fmt.Fprintln(os.Stderr, "----- request json -----")
	fmt.Fprintln(os.Stderr, pretty.String())
	fmt.Fprintln(os.Stderr, "===== END LLM REQUEST =====")
}

func (d *Dumper) active() bool {
	return d != nil && (d.enabled || d.file != nil)
}

func (d *Dumper) write(rec record) {
	line, err := json.Marshal(rec)
	if err != nil {
		return
	}
	d.file.Write(append(line, '\n'))
}

// label keeps stderr readable when agent names are empty.
func label(who string) string {
	if who == "" {
		return "?"
	}
	return who
}
