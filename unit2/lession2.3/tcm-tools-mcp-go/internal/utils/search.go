package utils

import (
	"strings"
	"unicode"
)

func NormalizeText(s string) string {
	var sb strings.Builder
	for _, r := range s {
		if r == '\u3000' {
			sb.WriteRune(' ')
			continue
		}
		sb.WriteRune(r)
	}
	res := strings.TrimSpace(sb.String())
	res = strings.ToLower(res)
	return res
}

func ContainsMatch(haystack, needle string) bool {
	if needle == "" {
		return true
	}
	if haystack == "" {
		return false
	}
	return strings.Contains(haystack, needle)
}

func ToStringArray(v interface{}) []string {
	if v == nil {
		return []string{}
	}
	switch t := v.(type) {
	case []string:
		res := make([]string, 0, len(t))
		for _, s := range t {
			ss := strings.TrimSpace(s)
			if ss != "" {
				res = append(res, ss)
			}
		}
		return res
	case []interface{}:
		res := make([]string, 0, len(t))
		for _, x := range t {
			if x == nil {
				continue
			}
			ss := strings.TrimSpace(toString(x))
			if ss != "" {
				res = append(res, ss)
			}
		}
		return res
	case string:
		ss := strings.TrimSpace(t)
		if ss == "" {
			return []string{}
		}
		return []string{ss}
	default:
		ss := strings.TrimSpace(toString(t))
		if ss == "" {
			return []string{}
		}
		return []string{ss}
	}
}

func toString(v interface{}) string {
	switch x := v.(type) {
	case string:
		return x
	default:
		return ""
	}
}

func BuildHaystack(fields ...interface{}) string {
	var parts []string
	for _, f := range fields {
		if f == nil {
			continue
		}
		switch t := f.(type) {
		case string:
			if t != "" {
				parts = append(parts, NormalizeText(t))
			}
		case []string:
			for _, s := range t {
				if s != "" {
					parts = append(parts, NormalizeText(s))
				}
			}
		case []interface{}:
			for _, x := range t {
				if x != nil {
					if s, ok := x.(string); ok && s != "" {
						parts = append(parts, NormalizeText(s))
					}
				}
			}
		}
	}
	return strings.Join(parts, " ")
}

func isSpaceOrPunct(r rune) bool {
	return unicode.IsSpace(r) || unicode.IsPunct(r)
}

func ScoreRelevance(haystack, needle string) int {
	if needle == "" || haystack == "" {
		return 0
	}
	h := NormalizeText(haystack)
	n := NormalizeText(needle)
	if n == "" {
		return 0
	}
	score := 0
	if h == n {
		score += 60
	}
	if strings.HasPrefix(h, n) {
		score += 40
	}
	tokens := strings.FieldsFunc(n, isSpaceOrPunct)
	if len(tokens) > 1 {
		for _, t := range tokens {
			if t == "" {
				continue
			}
			if strings.Contains(h, t) {
				score += 15
			}
		}
	} else {
		if strings.Contains(h, n) {
			score += 20
			score += 10
		}
	}
	if score > 100 {
		score = 100
	}
	return score
}
