package tools

import (
	"fmt"
	"slices"
	"sync"

	"novel-mcp/internal/domain"
	"novel-mcp/internal/store"
)

// CastIndex 缓存配角投影所需的轻量 ChapterRecord 事实。
// 首次 Snapshot 从全部已完成章节恢复；之后只读取新增章节，重写由 commit_chapter
// 主动刷新。磁盘 chapter_records 仍是唯一事实源，本索引不产生第二份持久化状态。
type CastIndex struct {
	store *store.Store

	mu      sync.Mutex
	records map[int]domain.ChapterRecord
}

func NewCastIndex(st *store.Store) *CastIndex {
	return &CastIndex{store: st}
}

func (i *CastIndex) Snapshot(completedChapters []int) ([]domain.CastEntry, error) {
	i.mu.Lock()
	defer i.mu.Unlock()

	completed, wanted, err := normalizeCompletedChapters(completedChapters)
	if err != nil {
		return nil, err
	}
	if i.records == nil {
		i.records = make(map[int]domain.ChapterRecord, len(completed))
	}

	for chapter := range i.records {
		if _, ok := wanted[chapter]; !ok {
			delete(i.records, chapter)
		}
	}
	for _, chapter := range completed {
		if _, ok := i.records[chapter]; ok {
			continue
		}
		record, err := i.store.ChapterRecords.Load(chapter)
		if err != nil {
			return nil, fmt.Errorf("读取第 %d 章接纳记录: %w", chapter, err)
		}
		if record == nil {
			return nil, fmt.Errorf("第 %d 章缺少接纳记录", chapter)
		}
		i.records[chapter] = slimCastRecord(*record)
	}

	characters, err := i.store.Characters.Load()
	if err != nil {
		return nil, fmt.Errorf("读取核心角色: %w", err)
	}
	records := make([]domain.ChapterRecord, 0, len(completed))
	for _, chapter := range completed {
		records = append(records, i.records[chapter])
	}
	return domain.ProjectCast(records, characters), nil
}

// ChapterCommitted 刷新已经初始化的索引。索引尚未初始化时无需预热；下一次
// Snapshot 会从当前磁盘事实完整恢复。
func (i *CastIndex) ChapterCommitted(chapter int) error {
	i.mu.Lock()
	defer i.mu.Unlock()

	if i.records == nil {
		return nil
	}
	record, err := i.store.ChapterRecords.Load(chapter)
	if err != nil {
		return fmt.Errorf("读取第 %d 章接纳记录: %w", chapter, err)
	}
	if record == nil {
		return fmt.Errorf("第 %d 章缺少接纳记录", chapter)
	}
	i.records[chapter] = slimCastRecord(*record)
	return nil
}

func slimCastRecord(record domain.ChapterRecord) domain.ChapterRecord {
	return domain.ChapterRecord{
		Chapter: record.Chapter,
		Facts: domain.ChapterFacts{
			Characters: slices.Clone(record.Facts.Characters),
			CastIntros: slices.Clone(record.Facts.CastIntros),
		},
	}
}
