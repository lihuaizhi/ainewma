// cli：打车 Agent 的命令行 REPL（走 gateway 的 REST/SSE，与 Web 前端同一条链路）。
//
//	go run ./cmd/cli
//	go run ./cmd/cli -url http://127.0.0.1:8080 -token dev-token
//
// 会话 id 可用 -session 固定（跨进程复用同一会话，便于演示订单面板联动）。
package main

import (
	"bufio"
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

func main() {
	url := flag.String("url", "http://127.0.0.1:8080", "gateway 地址")
	token := flag.String("token", "", "可选 Bearer（gateway 未开启鉴权则留空）")
	session := flag.String("session", "", "固定会话 id（默认随机）")
	flag.Parse()

	sid := *session
	in := bufio.NewScanner(os.Stdin)

	fmt.Println("🚕 打车 Agent CLI — 输入行程开始，如：从望京SOHO去首都机场T3")
	fmt.Println("   帮助/help · 退出/exit")
	fmt.Println()

	for {
		fmt.Print("你 > ")
		if !in.Scan() {
			return
		}
		msg := strings.TrimSpace(in.Text())
		if msg == "" {
			continue
		}
		switch msg {
		case "exit", "退出", "quit":
			return
		case "help", "帮助":
			fmt.Println("  示例：从望京SOHO去首都机场T3 / 查询订单 / 确认下单 / 取消订单")
			continue
		}

		newSid, err := chat(*url, *token, sid, msg, os.Stdout)
		if err != nil {
			fmt.Println("✗", err)
			continue
		}
		if sid == "" {
			sid = newSid
			fmt.Printf("（会话 %s）\n", sid)
		}
	}
}

// chat POST /v1/chat 并逐事件渲染到 w；返回（新）session_id。
func chat(base, token, sid, msg string, w io.Writer) (string, error) {
	body, _ := json.Marshal(map[string]any{"session_id": sid, "message": msg})
	req, err := http.NewRequest(http.MethodPost, base+"/v1/chat", bytes.NewReader(body))
	if err != nil {
		return sid, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "text/event-stream")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	// 长超时：LLM 多轮工具调用可能较慢
	client := &http.Client{Timeout: 120 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return sid, fmt.Errorf("连接 gateway 失败（是否已启动？）: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		b, _ := io.ReadAll(resp.Body)
		return sid, fmt.Errorf("gateway %d: %s", resp.StatusCode, strings.TrimSpace(string(b)))
	}

	newSid := sid
	toolN := 0
	scan := bufio.NewScanner(resp.Body)
	scan.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	var event, data string
	flush := func() {
		if event == "" || data == "" {
			return
		}
		var payload map[string]any
		if err := json.Unmarshal([]byte(data), &payload); err != nil {
			event, data = "", ""
			return
		}
		switch event {
		case "delta":
			fmt.Fprint(w, str(payload["text"]))
		case "tool_call":
			toolN++
			fmt.Fprintf(w, "  🔧 %s %s\n", str(payload["name"]), briefArgs(payload["args"]))
		case "tool_result":
			ok, _ := payload["ok"].(bool)
			mark := "✓"
			if !ok {
				mark = "✗"
			}
			fmt.Fprintf(w, "    %s %s\n", mark, str(payload["summary"]))
		case "error":
			fmt.Fprintf(w, "\n✗ %s\n", str(payload["message"]))
		case "done":
			if s := str(payload["session_id"]); s != "" {
				newSid = s
			}
			if toolN > 0 {
				fmt.Fprintf(w, "（本轮调用 %d 次工具）\n", toolN)
			}
			fmt.Fprintln(w)
		}
		event, data = "", ""
	}

	for scan.Scan() {
		line := scan.Text()
		switch {
		case strings.HasPrefix(line, "event:"):
			event = strings.TrimSpace(strings.TrimPrefix(line, "event:"))
		case strings.HasPrefix(line, "data:"):
			data = strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		case line == "":
			flush()
		}
	}
	flush() // 兜底
	if err := scan.Err(); err != nil {
		return newSid, fmt.Errorf("读取流失败: %w", err)
	}
	return newSid, nil
}

func str(v any) string { s, _ := v.(string); return s }

func briefArgs(v any) string {
	m, ok := v.(map[string]any)
	if !ok {
		return ""
	}
	b, _ := json.Marshal(m)
	s := string(b)
	if r := []rune(s); len(r) > 100 {
		return string(r[:100]) + "…"
	}
	return s
}
