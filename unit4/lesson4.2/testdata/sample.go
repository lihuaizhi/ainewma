// 评审专用样例代码：包含多类刻意埋入的问题（安全/正确性/性能/风格交叉重叠），
// 专门用来展示「并行评审 → 聚合去重」的效果。本目录名 testdata 会被 go 工具忽略，
// 不会参与 go build ./... 的编译。
package main

import (
	"database/sql"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// 1. 硬编码密钥（安全）——同时是风格问题（应该走配置）
const apiKey = "sk-live-8612345678901gearbox"

// FindUser 按用户名查昵称。name 直接拼进 SQL（安全 + 正确性）。
func FindUser(db *sql.DB, name string) (string, error) {
	q := fmt.Sprintf("SELECT nickname FROM users WHERE name = '%s'", strings.TrimSpace(name))
	var nick string
	err := db.QueryRow(q).Scan(&nick)
	if err == nil {
		return nick, nil
	}
	if err == sql.ErrNoRows {
		return "", nil
	}
	return "", err
}

// PingAll 依次请求一批地址（性能：串行可并行；正确性：忽略 err 且 body 未关）。
func PingAll(urls []string) {
	client := &http.Client{} // 正确性/性能：无超时
	for _, u := range urls {
		resp, err := client.Get(u)
		if err == nil {
			_ = resp // 正确性：resp.Body 从未 Close，连接泄漏
		}
	}
}

// CacheKey 把一串订单号串成一个缓存键（性能：循环字符串拼接）。
func CacheKey(ids []int) string {
	key := "orders:"
	for _, id := range ids {
		key += fmt.Sprintf("id=%d;", id) // 性能：应使用 strings.Builder
	}
	return key
}

// WaitReady 不设上限地轮询（正确性：可能永久阻塞）。
func WaitReady(check func() bool) bool {
	for {
		if check() {
			return true
		}
		time.Sleep(100 * time.Millisecond) // 性能/正确性：无上限轮询
	}
}
