// Package exp 复用上游 ainovel-cli/internal/host/exp 的 TXT 排版语义：
// 纯渲染（《书名》→卷分隔→“第 N 章 标题”→正文，剥正文首行重复标题），
// 不写盘、不改 store。txt.go/txt_test.go 与上游同形（仅模块路径不同，
// 见 upstream-manifest.json）；本文件是 novel-mcp 的公开包装。
package exp

import "novel-mcp/internal/domain"

// ChapterTitleIndex 给定章号查标题，缺失返回空串。
type ChapterTitleIndex = chapterTitleIndex

// ChapterLocation 是某章在分层大纲中的归属（只保留导出版式需要的卷信息）。
type ChapterLocation = chapterLocation

// BuildTitleIndex 从扁平大纲建标题索引。
func BuildTitleIndex(outline []domain.OutlineEntry) ChapterTitleIndex {
	return buildTitleIndex(outline)
}

// BuildLocations 按分层大纲的全局章节顺序构造 {chapter -> location}。
func BuildLocations(volumes []domain.VolumeOutline) map[int]ChapterLocation {
	return buildLocations(volumes)
}

// RenderTXT 拼接最终文本。标题/定位缺失即降级：标题缺失只输出“第 N 章”；
// 分层定位缺失就当扁平大纲。
func RenderTXT(novelName string, chapters []int, titleIdx ChapterTitleIndex, locations map[int]ChapterLocation, bodies map[int]string) string {
	return renderTXT(novelName, chapters, titleIdx, locations, bodies)
}
