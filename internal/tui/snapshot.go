package tui

import (
	"time"

	"novel-mcp/internal/server"
)

// Connection 是连接卡的数据：main 从 Preflight 结果与凭据组装一次，运行中不变。
type Connection struct {
	Version   string
	AccessURL string
	Bearer    string
	BearerOff bool
	HealthURL string
	Note      string // 如“公网 DNS 未检查（解析器不通）”，无则为空
	DataDir   string
}

// ProjectRow 是项目页的一行，来自 server.Projects.List。
type ProjectRow struct {
	ID        string
	Brief     string
	Style     string
	CreatedAt string
	// Chapters 是 chapters/*.md 计数；-1 表示未知（视图里不显示）。
	Chapters        int
	Phase           string
	Flow            string
	CurrentChapter  int
	TotalChapters   int
	PendingRewrites int
}

// Snapshot 是一帧的全部数据。TUI 每 500ms 向 SnapshotFunc 要一帧，
// 组装代价由 main 控制（项目列表节流 5 秒，事件只取最新 60 条）。
type Snapshot struct {
	At        time.Time
	Uptime    time.Duration
	Conn      Connection
	Calls     uint64
	Successes uint64
	Failures  uint64
	Events    []server.Event // 新的在前
	Projects  []ProjectRow
	// ProjectsErr 非空表示项目列表读失败（视图显示错误而不是空列表）。
	ProjectsErr string
}

// SnapshotFunc 由 main 提供：把 Observer 计数、事件环、项目表拼成一帧。
type SnapshotFunc func() Snapshot
