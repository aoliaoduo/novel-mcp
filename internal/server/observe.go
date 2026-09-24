package server

import (
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Stats 是进程级的工具调用计数器：并发安全，TUI 每 500ms 快照一次。
type Stats struct {
	Calls     atomic.Uint64
	Successes atomic.Uint64
	Failures  atomic.Uint64
}

// Snapshot 一次读出三个计数（各自原子，相互之间不保证同一时刻）。
func (s *Stats) Snapshot() (calls, successes, failures uint64) {
	return s.Calls.Load(), s.Successes.Load(), s.Failures.Load()
}

// Event 是一次已完成的工具调用记录。只记元数据，不记参数与结果正文：
// 正文可能含小说原文，进内存环与 TUI 都是泄漏面。
type Event struct {
	At      time.Time
	Tool    string
	Project string
	OK      bool
	Latency time.Duration
	Err     string // 失败时一句话摘要，成功时为空
}

// DefaultEventLogCap 存最近 200 次调用：高频写入也不会把内存吃光。
const DefaultEventLogCap = 200

// EventLog 是有界环：写满后覆盖最旧的，Add 永不阻塞。
type EventLog struct {
	mu   sync.Mutex
	buf  []Event
	next int
	n    int
}

func NewEventLog(capacity int) *EventLog {
	if capacity <= 0 {
		capacity = DefaultEventLogCap
	}
	return &EventLog{buf: make([]Event, capacity)}
}

func (l *EventLog) Add(e Event) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	l.buf[l.next] = e
	l.next = (l.next + 1) % len(l.buf)
	if l.n < len(l.buf) {
		l.n++
	}
}

// Recent 返回最新的 limit 条（新的在前）。limit<=0 返回全部。
func (l *EventLog) Recent(limit int) []Event {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if limit <= 0 || limit > l.n {
		limit = l.n
	}
	out := make([]Event, 0, limit)
	for i := 0; i < limit; i++ {
		idx := (l.next - 1 - i + len(l.buf)) % len(l.buf)
		out = append(out, l.buf[idx])
	}
	return out
}

// Observer 把计数器与事件环绑在一起，是 NewMCP 的唯一埋点出口。
// 零值可用（Log 为 nil 时只计数不记事件），Record 对 nil 接收者也是安全的。
type Observer struct {
	Stats   Stats
	Log     *EventLog
	CallLog *CallLog
}

func NewObserver() *Observer { return &Observer{Log: NewEventLog(DefaultEventLogCap)} }

func (o *Observer) Record(tool, project string, ok bool, latency time.Duration, errMsg string) {
	if o == nil {
		return
	}
	o.Stats.Calls.Add(1)
	if ok {
		o.Stats.Successes.Add(1)
	} else {
		o.Stats.Failures.Add(1)
	}
	if o.Log != nil {
		o.Log.Add(Event{At: time.Now(), Tool: tool, Project: project, OK: ok, Latency: latency, Err: errMsg})
	}
}

// shortErr 把错误压成一句话摘要：去换行、压空白、按 rune 截断，
// 保证 TUI 事件行与内存环里都是定长文本。
func shortErr(s string) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > 120 {
		return string(r[:120]) + "…"
	}
	return s
}
