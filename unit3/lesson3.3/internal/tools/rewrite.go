package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/adapter"
	"github.com/lihuaizhi/ainewma/unit3/lesson3.3/internal/store"
)

type RewriteArgs struct {
	Question string `json:"question"`
	Goal     string `json:"goal,omitempty"`
}

type RewriteResult struct {
	Rewritten []string `json:"rewritten"`
	Reason    string   `json:"reason"`
}

const rewriteSys = `你是一个查询改写助手。根据用户问题改写成更适合古籍（《伤寒论》）检索的查询。

要求：
- 用中文，简洁、精准
- 只返回 JSON 对象：{"rewritten":["q1","q2"], "reason":"..."}
- 返回 1～3 条改写建议
- 优先保留术语（太阳病、中风、伤寒、桂枝汤、麻黄汤、少阳、阳明、太阴、少阴、厥阴等）
- 避免长篇解释`

func RewriteQuery(dec adapter.Streamer, args RewriteArgs) (RewriteResult, error) {
	if dec == nil {
		return RewriteResult{}, fmt.Errorf("llm nil")
	}
	user := fmt.Sprintf(`问题：%s
目标：%s
请改写。`, args.Question, args.Goal)
	var buf strings.Builder
	err := dec.Stream(context.Background(), rewriteSys, user, func(tok string) {
		buf.WriteString(tok)
	})
	if err != nil {
		return RewriteResult{}, err
	}
	res, perr := parseRewrite(buf.String())
	if perr != nil || len(res.Rewritten) == 0 {
		// fallback: keep original question as the only rewrite
		return RewriteResult{Rewritten: []string{args.Question}, Reason: res.Reason}, nil
	}
	return res, nil
}

// parseRewrite extracts the first JSON object from raw LLM output.
func parseRewrite(raw string) (RewriteResult, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return RewriteResult{}, fmt.Errorf("no json object in %q", raw)
	}
	var res RewriteResult
	if err := json.Unmarshal([]byte(raw[start:end+1]), &res); err != nil {
		return RewriteResult{}, err
	}
	return res, nil
}

type GradeArgs struct {
	Question string      `json:"question"`
	Hits     []store.Hit `json:"hits"`
}

type GradeResult struct {
	Score      float64 `json:"score"`
	Sufficient bool    `json:"sufficient"`
	Missing    string  `json:"missing"`
	Reason     string  `json:"reason"`
}

const gradeSys = `你是一个检索质量评估助手。根据用户问题和检索到的片段，判断检索结果是否足以回答问题。

要求：
- 用中文
- 只返回 JSON 对象：{"score":0.0,"sufficient":false,"missing":"...","reason":"..."}
- score 取 0.0～1.0，表示检索结果对回答问题的充分程度
- sufficient 为 true 表示可以据此回答
- missing 描述还缺少的关键信息（若足够则为空字符串）
- reason 简要说明打分理由`

// GradeRelevance asks the decision LLM to score retrieved hits for a question.
func GradeRelevance(dec adapter.Streamer, args GradeArgs) (GradeResult, error) {
	if dec == nil {
		return GradeResult{}, fmt.Errorf("llm nil")
	}
	if len(args.Hits) == 0 {
		return GradeResult{Score: 0, Sufficient: false, Missing: "无检索结果", Reason: "未检索到任何片段"}, nil
	}
	var sb strings.Builder
	fmt.Fprintf(&sb, "问题：%s\n\n检索片段：\n", args.Question)
	for i, h := range args.Hits {
		text := h.Text
		if len(text) > 500 {
			text = text[:500] + "…"
		}
		fmt.Fprintf(&sb, "[%d] (id=%s score=%.3f)\n%s\n\n", i+1, h.ID, h.Score, text)
	}
	sb.WriteString("请评估。")
	var buf strings.Builder
	if err := dec.Stream(context.Background(), gradeSys, sb.String(), func(tok string) {
		buf.WriteString(tok)
	}); err != nil {
		return GradeResult{}, err
	}
	res, perr := parseGrade(buf.String())
	if perr != nil {
		// fallback: treat as sufficient to avoid infinite retry loops
		return GradeResult{Score: 0.5, Sufficient: true, Reason: "解析失败:" + perr.Error()}, nil
	}
	return res, nil
}

func parseGrade(raw string) (GradeResult, error) {
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start < 0 || end <= start {
		return GradeResult{}, fmt.Errorf("no json object in %q", raw)
	}
	var res GradeResult
	if err := json.Unmarshal([]byte(raw[start:end+1]), &res); err != nil {
		return GradeResult{}, err
	}
	return res, nil
}
