// Package prompt turns retrieved chunks into the messages sent to the LLM.
//
// The context block is numbered so the model can cite sources as [1], [2] and
// so the user can map a claim back to a chapter and page of the book.
package prompt

import (
	"fmt"
	"strings"

	"github.com/lihuaizhi/ainewma/unit3/lesson3.2/internal/store"
)

// System is the fixed behaviour instruction.
const System = `你是《伤寒论》的中医学习助手。请严格依据【参考资料】回答，不要编造原文或方剂组成。
回答要求：
1. 先给出结论，再引用原文佐证；
2. 引用时用 [序号] 标注来源（序号对应【参考资料】的编号）；
3. 如果资料不足以回答，请直说"资料不足以回答"，不要臆测。`

// Context renders hits into a numbered, citable context block. For small-to-big
// chunks the enclosing parent window is used instead of the small child.
func Context(hits []store.Hit) string {
	var b strings.Builder
	for i, h := range hits {
		fmt.Fprintf(&b, "[%d] 篇章：%s｜页码：第%d页", i+1, volume(h.Volume), h.PageNo)
		if len(h.Formulas) > 0 {
			fmt.Fprintf(&b, "｜方剂：%s", strings.Join(h.Formulas, "、"))
		}
		b.WriteString("\n")
		b.WriteString(strings.TrimSpace(h.Context()))
		b.WriteString("\n\n")
	}
	return strings.TrimSpace(b.String())
}

// User builds the user message from the question and the context block.
func User(question, context string) string {
	return fmt.Sprintf("【参考资料】\n%s\n\n【问题】\n%s", context, question)
}

func volume(v string) string {
	if v == "" {
		return "全文"
	}
	return v
}
