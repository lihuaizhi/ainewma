// continue loop helpers
package agent

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"

	l2prompt "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/prompt"
	l2store "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/store"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/tools"
)

const answerDirectSys = `你是中医古籍《伤寒论》的问答助手。若问题与本书无关，请直接回答；若涉及本书内容，应使用检索工具。

要求：
- 用中文，简洁准确
- 不确定时明确说明不确定
- 不要编造原文条文`

func (l *Loop) think(ctx context.Context, st *State) Thought {
	var buf bytes.Buffer
	buf.WriteString(fmt.Sprintf("问题：%s\n\n", st.Question))
	if len(st.Hits) > 0 {
		buf.WriteString(fmt.Sprintf("当前证据：已命中 %d 条\n", len(st.Hits)))
		for i, h := range st.Hits[:min(3, len(st.Hits))] {
			buf.WriteString(fmt.Sprintf("- [%d] %s p%d score=%.3f: %s\n", i+1, h.Volume, h.PageNo, h.Score, truncate(h.Context(), 120)))
		}
		buf.WriteString("\n")
	}
	if len(st.History) > 0 {
		buf.WriteString("历史（最近3轮）：\n")
		for i := len(st.History) - 1; i >= max(0, len(st.History)-3); i-- {
			t := st.History[i]
			buf.WriteString(fmt.Sprintf("  - tool=%s args=%s -> %s\n", t.Tool, t.Args, t.Result))
		}
		buf.WriteString("\n")
	}
	buf.WriteString("请按 JSON 格式返回决策。")
	var th Thought
	var collected strings.Builder
	l.dec.Stream(ctx, thinkSys, buf.String(), func(tok string) { collected.WriteString(tok) })
	_ = json.Unmarshal([]byte(extractJSON(collected.String())), &th)
	if th.Action == "" {
		th.Action = "tool_call"
		th.Tool = "search_knowledge"
		b, _ := json.Marshal(tools.SearchArgs{Query: st.Question, Mode: "hybrid"})
		th.Args = b
	}
	return th
}

// extractJSON returns the first {...} JSON object in raw (strips markdown
// fences and any surrounding prose the model may emit).
func extractJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return raw
	}
	return raw[start : end+1]
}

func (l *Loop) execTool(ctx context.Context, st *State, th Thought) {
	switch th.Tool {
	case "search_knowledge":
		var args tools.SearchArgs
		_ = json.Unmarshal(th.Args, &args)
		if args.Query == "" {
			args.Query = st.Question
		}
		res, err := tools.SearchKnowledge(l.a, args)
		if err == nil {
			st.Hits = mergeHits(st.Hits, res.Hits)
			l.addTurn(st, th, args, res.Summary)
		} else {
			l.addTurn(st, th, args, "err:"+err.Error())
		}
	case "rewrite_query":
		var args tools.RewriteArgs
		_ = json.Unmarshal(th.Args, &args)
		if args.Question == "" {
			args.Question = st.Question
		}
		res, _ := tools.RewriteQuery(l.dec, args)
		if len(res.Rewritten) > 0 && l.enableRew {
			sa := tools.SearchArgs{Query: res.Rewritten[0], Mode: "hybrid"}
			b, _ := json.Marshal(sa)
			th2 := Thought{Action: "tool_call", Tool: "search_knowledge", Args: b}
			l.execTool(ctx, st, th2)
			l.addTurn(st, th, args, fmt.Sprintf("rewrote->%s", res.Rewritten[0]))
			return
		}
		l.addTurn(st, th, args, res.Reason)
	case "grade_relevance":
		grade, err := tools.GradeRelevance(l.dec, tools.GradeArgs{Question: st.Question, Hits: st.Hits})
		if err != nil {
			l.addTurn(st, th, th.Args, "err:"+err.Error())
			return
		}
		res := fmt.Sprintf("score=%.2f sufficient=%v missing=%s reason=%s", grade.Score, grade.Sufficient, grade.Missing, grade.Reason)
		l.addTurn(st, th, th.Args, res)
		// If evidence is insufficient, try a rewrite + search retry once per iteration.
		if l.enableGrade && !grade.Sufficient && grade.Score < l.gradeThresh && l.enableRew {
			rew, _ := tools.RewriteQuery(l.dec, tools.RewriteArgs{Question: st.Question, Goal: grade.Missing})
			if len(rew.Rewritten) > 0 {
				sa := tools.SearchArgs{Query: rew.Rewritten[0], Mode: "hybrid"}
				b, _ := json.Marshal(sa)
				l.execTool(ctx, st, Thought{Action: "tool_call", Tool: "search_knowledge", Args: b})
				l.addTurn(st, th, tools.RewriteArgs{Question: st.Question, Goal: grade.Missing}, "retry->"+rew.Rewritten[0])
			}
		}
	case "answer_direct":
		var args map[string]string
		_ = json.Unmarshal(th.Args, &args)
		ans := l.answerWithHits(ctx, st)
		if qv, ok := args["question"]; ok && strings.TrimSpace(qv) != "" {
			// keep as is
		}
		l.addTurn(st, th, args, "direct")
		st.Answer = ans
		st.Done = true
	default:
		l.addTurn(st, th, th.Args, "unknown")
	}
}

func (l *Loop) answerWithHits(ctx context.Context, st *State) string {
	llm := l.answerLLM()
	if len(st.Hits) == 0 {
		// No evidence: answer directly (chat / general question).
		var out strings.Builder
		if err := llm.Stream(ctx, answerDirectSys, st.Question, func(tok string) {
			out.WriteString(tok)
		}); err != nil || out.Len() == 0 {
			return "（暂无足够证据）"
		}
		return out.String()
	}
	ctxs := l2prompt.Context(st.Hits)
	var collected strings.Builder
	llm.Stream(ctx, l2prompt.System, l2prompt.User(st.Question, ctxs), func(tok string) {
		collected.WriteString(tok)
	})
	return collected.String()
}

func (l *Loop) addTurn(st *State, th Thought, args any, res string) {
	a, _ := json.Marshal(args)
	st.History = append(st.History, Turn{Thought: th, Tool: th.Tool, Args: string(a), Result: res})
}

func mergeHits(a, b []l2store.Hit) []l2store.Hit {
	seen := make(map[string]struct{}, len(a))
	out := make([]l2store.Hit, 0, len(a)+len(b))
	for _, h := range a {
		if _, ok := seen[h.ID]; !ok {
			seen[h.ID] = struct{}{}
			out = append(out, h)
		}
	}
	for _, h := range b {
		if _, ok := seen[h.ID]; !ok {
			seen[h.ID] = struct{}{}
			out = append(out, h)
		}
	}
	return out
}

func min(x, y int) int {
	if x < y {
		return x
	}
	return y
}
func max(x, y int) int {
	if x > y {
		return x
	}
	return y
}
func truncate(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}
