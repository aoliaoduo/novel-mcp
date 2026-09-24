package server

import (
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"sync"
	"time"
	"unicode/utf8"
)

// CallLog 是本机持久化的诊断日志。它只记录调用元数据与参数形状，
// 不记录正文、route、Bearer、Authorization、完整请求体或自由文本值。
// 文件内容是一行一个 JSON 对象，便于人和脚本直接 tail/grep。
type CallLog struct {
	path     string
	mu       sync.Mutex
	maxBytes int64
}

const defaultCallLogMaxBytes int64 = 16 << 20

func CallLogPath(dataDir string) string {
	return filepath.Join(dataDir, "logs", "novel-mcp.log")
}

func NewCallLog(path string) (*CallLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	if err := f.Chmod(0o600); err != nil {
		_ = f.Close()
		return nil, err
	}
	if err := f.Close(); err != nil {
		return nil, err
	}
	return &CallLog{path: path, maxBytes: defaultCallLogMaxBytes}, nil
}

func (l *CallLog) append(event map[string]any) {
	if l == nil {
		return
	}
	raw, err := json.Marshal(event)
	if err != nil {
		return
	}
	raw = append(raw, '\n')
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.maxBytes > 0 {
		if info, err := os.Stat(l.path); err == nil && info.Size()+int64(len(raw)) > l.maxBytes {
			backup := l.path + ".1"
			if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
				slog.Warn("调用日志旧备份删除失败", "module", "server")
			} else if err := os.Rename(l.path, backup); err != nil {
				slog.Warn("调用日志轮换失败", "module", "server")
			}
		}
	}
	f, err := os.OpenFile(l.path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err == nil {
		_, err = f.Write(raw)
		if closeErr := f.Close(); err == nil {
			err = closeErr
		}
	}
	if err != nil {
		// 不把文件名或底层错误文本打到 stderr：它们可能含本机绝对路径。
		slog.Warn("调用日志写入失败", "module", "server")
	}
}

func (l *CallLog) HTTP(event, method string, status int, contentLength int64, originPresent bool, protocol string, latency time.Duration) {
	rec := map[string]any{
		"ts":             time.Now().UTC().Format(time.RFC3339Nano),
		"kind":           "http",
		"event":          event,
		"method":         method,
		"origin_present": originPresent,
		"duration_ms":    latency.Milliseconds(),
	}
	if status > 0 {
		rec["status"] = status
	}
	if contentLength >= 0 {
		rec["content_length"] = contentLength
	}
	if protocol != "" {
		rec["mcp_protocol"] = protocol
	}
	l.append(rec)
}

func (l *CallLog) Tool(tool, project string, args, result map[string]any, ok bool, code, message string, latency time.Duration) {
	rec := map[string]any{
		"ts":          time.Now().UTC().Format(time.RFC3339Nano),
		"kind":        "tool",
		"tool":        tool,
		"ok":          ok,
		"duration_ms": latency.Milliseconds(),
		"args":        summarizeLogMap(args, true),
	}
	if project != "" {
		rec["project"] = project
	}
	if result != nil {
		rec["result"] = summarizeToolResult(result)
	}
	if !ok {
		errInfo := map[string]any{}
		if code != "" {
			errInfo["code"] = code
		}
		if message != "" {
			errInfo["message_chars"] = utf8.RuneCountInString(message)
		}
		if len(errInfo) > 0 {
			rec["error"] = errInfo
		}
	}
	l.append(rec)
}

var logSafeStrings = map[string]bool{
	"type": true, "scale": true, "mode": true, "source": true, "role": true,
	"verdict": true, "agent": true, "flow": true, "format": true, "status": true,
	"action": true,
}

func summarizeLogMap(in map[string]any, args bool) map[string]any {
	out := map[string]any{}
	if in == nil {
		return out
	}
	keys := make([]string, 0, len(in))
	for k := range in {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	out["keys"] = keys
	for _, k := range keys {
		v := in[k]
		switch k {
		case "project":
			// project 单独作为 tool 事件顶层字段；不重复。
			continue
		case "expected_revision":
			if args {
				s, _ := v.(string)
				out["expected_revision_present"] = s != ""
			}
			continue
		case "chapter", "volume", "arc", "from_chapter", "to_chapter", "next_chapter":
			out[k] = v
			continue
		}
		if logSafeStrings[k] {
			if s, ok := v.(string); ok {
				out[k] = s
				continue
			}
		}
		switch x := v.(type) {
		case string:
			out[k+"_chars"] = utf8.RuneCountInString(x)
		case []any:
			out[k+"_count"] = len(x)
		case []string:
			out[k+"_count"] = len(x)
		case map[string]any:
			out[k+"_fields"] = len(x)
		case bool, float64, float32, int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
			out[k] = x
		case nil:
			out[k+"_null"] = true
		default:
			out[k+"_present"] = true
		}
	}
	return out
}

func summarizeToolResult(result map[string]any) map[string]any {
	out := map[string]any{}
	if rev, ok := result["revision"].(string); ok && rev != "" {
		if len(rev) > 12 {
			rev = rev[:12]
		}
		out["revision_prefix"] = rev
	}
	if e, ok := result["error"].(map[string]any); ok {
		if code, _ := e["code"].(string); code != "" {
			out["error_code"] = code
		}
		if recovery, _ := e["recovery"].(map[string]any); recovery != nil {
			if tool, _ := recovery["tool"].(string); tool != "" {
				out["recovery_tool"] = tool
			}
		}
		return out
	}
	payload, _ := result["result"].(map[string]any)
	if payload == nil {
		payload = result
	}
	for k, v := range summarizeLogMap(payload, false) {
		out[k] = v
	}
	if actions, ok := payload["actions"].([]any); ok {
		tools := make([]string, 0, len(actions))
		for _, raw := range actions {
			a, _ := raw.(map[string]any)
			if tool, _ := a["tool"].(string); tool != "" {
				tools = append(tools, tool)
			}
		}
		if len(tools) > 0 {
			out["action_tools"] = tools
		}
	}
	return out
}
