package agent

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/adapter"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/config"
	l2store "github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/store"
)

type Thought struct {
	Thought     string          `json:"thought"`
	Action      string          `json:"action"`
	Tool        string          `json:"tool,omitempty"`
	Args        json.RawMessage `json:"args,omitempty"`
	FinalAnswer string          `json:"final_answer,omitempty"`
}

type Turn struct {
	Thought Thought
	Tool    string
	Args    string
	Result  string
}

type State struct {
	Question string
	History  []Turn
	Hits     []l2store.Hit
	Iters    int
	Done     bool
	Answer   string
}

type Loop struct {
	a           *adapter.Adapter
	dec         adapter.Streamer
	answer      adapter.Streamer
	maxIters    int
	gradeThresh float64
	enableRew   bool
	enableGrade bool
	verbose     bool
}

func NewLoop(a *adapter.Adapter, dec adapter.Streamer, maxIters int, gradeThresh float64, enableRew, enableGrade, verbose bool) *Loop {
	if maxIters <= 0 {
		maxIters = config.DefaultMaxIters
	}
	if gradeThresh <= 0 {
		gradeThresh = config.DefaultGradeThresh
	}
	return &Loop{a: a, dec: dec, maxIters: maxIters, gradeThresh: gradeThresh, enableRew: enableRew, enableGrade: enableGrade, verbose: verbose}
}

// SetAnswerStreamer sets a separate LLM for final answer generation. When unset,
// the decision streamer is reused for answers.
func (l *Loop) SetAnswerStreamer(s adapter.Streamer) {
	if s != nil {
		l.answer = s
	}
}

// answerLLM returns the streamer used for answer generation.
func (l *Loop) answerLLM() adapter.Streamer {
	if l.answer != nil {
		return l.answer
	}
	return l.dec
}

const thinkSys = `你是一个严谨的中医古籍（《伤寒论》）问答助手。你是 Agentic RAG：只在真正需要证据时才调用工具。

可用工具：
- search_knowledge: 查询伤寒论知识库，获取原文/条文/方剂证据，支持query和formula2个字段参数，query是检索内容，formula是按方剂名
- rewrite_query: 改写问题成更适合检索的表述（提升召回）
- grade_relevance: 判断检索结果是否足够回答问题（0-1 + sufficient）
- answer_direct: 不查直接回答（闲聊/通用）

约束：
- 一次最多调用 1 个工具
- 如果已有足够证据回答问题，直接返回 action=answer 并给出 final_answer
- 如果问题模糊，优先考虑 rewrite_query 或 search_knowledge
- 检索后可用 grade_relevance 判断是否要重查
- 只返回 JSON：{"thought":"...","action":"tool_call|answer|think_more","tool":"...","args":{...},"final_answer":"..."}`

func (l *Loop) Run(ctx context.Context, q string) (string, []l2store.Hit) {
	st := l.RunDetailed(ctx, q)
	return st.Answer, st.Hits
}

// RunDetailed runs the ReAct loop and returns the full state (history, hits,
// iteration count) so callers can compute agent-behaviour metrics.
func (l *Loop) RunDetailed(ctx context.Context, q string) *State {
	st := &State{Question: q, Hits: []l2store.Hit{}}
	for !st.Done && st.Iters < l.maxIters {
		th := l.think(ctx, st)
		if l.verbose {
			fmt.Printf("[THINK] %s %s\n", th.Thought, th.Action)
			if th.Action == "tool_call" {
				fmt.Printf("[ACT] tool=%s args=%s\n", th.Tool, string(th.Args))
			} else if th.Action == "answer" {
				fmt.Printf("[ANSWER] will answer directly\n")
			} else {
				fmt.Printf("[action=%s]", th.Action)
			}
		}
		switch th.Action {
		case "answer":
			if th.FinalAnswer != "" {
				st.Answer = th.FinalAnswer
			} else {
				st.Answer = l.answerWithHits(ctx, st)
			}
			st.Done = true
		case "tool_call":
			l.execTool(ctx, st, th)
			st.Iters++
		default:
			st.Iters++
		}
	}
	if !st.Done {
		st.Answer = l.answerWithHits(ctx, st)
		st.Done = true
	}
	return st
}
