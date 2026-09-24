package server

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/voocel/agentcore/schema"
	"novel-mcp/internal/errs"
	"novel-mcp/internal/store"
	"novel-mcp/internal/tools"
)

// reopenTool 把上游 ReopenBook（完本书返工）包成 MCP 写工具：上游函数只认
// store，这里补参数解析与结果整形。revision 守卫与一本内串行走 Projects.Call。
type reopenTool struct {
	s *store.Store
}

// NewReopenBookTool 构造完本书返工工具。
func NewReopenBookTool(s *store.Store) Tool {
	return &reopenTool{s: s}
}

func (t *reopenTool) Name() string { return "reopen_book" }
func (t *reopenTool) Description() string {
	return "把已完结的书重开为返工态：phase complete→writing，目标章入 PendingRewrites，flow=rewriting；" +
		"随后按 next_step 派 writer 逐章重写（edit/draft+commit），队列排空后自动重新完结。" +
		"仅 complete 期可调；只能返工已完成章节，新增剧情走篇幅调整。"
}

func (t *reopenTool) Schema() map[string]any {
	return schema.Object(
		schema.Property("chapters", schema.Array("要返工的已完成章节号", schema.Int("章节号"))).Required(),
		schema.Property("reason", schema.String("返工原因，一句话")).Required(),
	)
}

type reopenArgs struct {
	Chapters []int  `json:"chapters"`
	Reason   string `json:"reason"`
}

func (t *reopenTool) Execute(_ context.Context, args json.RawMessage) (json.RawMessage, error) {
	var a reopenArgs
	if err := json.Unmarshal(args, &a); err != nil {
		return nil, fmt.Errorf("invalid args: %w: %w", errs.ErrToolArgs, err)
	}
	if len(a.Chapters) == 0 {
		return nil, fmt.Errorf("chapters 不能为空，需指明要返工的章节: %w", errs.ErrToolArgs)
	}
	for _, ch := range a.Chapters {
		if ch <= 0 {
			return nil, fmt.Errorf("章节号必须 > 0: %w", errs.ErrToolArgs)
		}
	}
	if err := tools.ReopenBook(t.s, a.Chapters, a.Reason); err != nil {
		return nil, err
	}
	p, err := t.s.Progress.Load()
	if err != nil {
		return nil, fmt.Errorf("load progress: %w: %w", errs.ErrStoreRead, err)
	}
	return json.Marshal(map[string]any{
		"reopened":         a.Chapters,
		"reason":           a.Reason,
		"phase":            p.Phase,
		"flow":             p.Flow,
		"pending_rewrites": p.PendingRewrites,
	})
}
