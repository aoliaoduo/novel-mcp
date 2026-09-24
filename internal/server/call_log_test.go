package server

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestCallLogRedactsCreativeTextAndCredentials(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "novel-mcp.log")
	log, err := NewCallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	args := map[string]any{
		"project":           "test-book",
		"chapter":           7,
		"mode":              "write",
		"content":           "PRIVATE-NOVEL-TEXT",
		"title":             "PRIVATE-TITLE",
		"expected_revision": strings.Repeat("a", 64),
	}
	result := map[string]any{
		"project":  "test-book",
		"revision": strings.Repeat("b", 64),
		"result": map[string]any{
			"chapter": 7, "written": true, "word_count": 1200,
			"summary": "PRIVATE-SUMMARY",
		},
	}
	log.Tool("draft_chapter", "test-book", args, result, true, "", "", 15*time.Millisecond)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	for _, secret := range []string{"PRIVATE-NOVEL-TEXT", "PRIVATE-TITLE", "PRIVATE-SUMMARY", strings.Repeat("a", 64), strings.Repeat("b", 64)} {
		if strings.Contains(s, secret) {
			t.Fatalf("调用日志泄漏敏感值 %q: %s", secret, s)
		}
	}
	for _, want := range []string{`"tool":"draft_chapter"`, `"project":"test-book"`, `"chapter":7`, `"content_chars":18`, `"title_chars":13`, `"word_count":1200`, `"revision_prefix":"bbbbbbbbbbbb"`} {
		if !strings.Contains(s, want) {
			t.Fatalf("调用日志缺 %s: %s", want, s)
		}
	}
}

func TestCallLogHTTPDoesNotNeedPathOrAuth(t *testing.T) {
	path := filepath.Join(t.TempDir(), "logs", "novel-mcp.log")
	log, err := NewCallLog(path)
	if err != nil {
		t.Fatal(err)
	}
	log.HTTP("auth_failed", "POST", 401, 123, true, currentMCPProtocolVersion, time.Millisecond)
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	s := string(raw)
	if !strings.Contains(s, `"event":"auth_failed"`) || !strings.Contains(s, `"status":401`) {
		t.Fatalf("HTTP 诊断信息不完整: %s", s)
	}
	if strings.Contains(strings.ToLower(s), "authorization") || strings.Contains(s, "/mcp/") {
		t.Fatalf("HTTP 日志不应包含认证头或 route: %s", s)
	}
}
