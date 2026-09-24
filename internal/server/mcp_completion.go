package server

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const maxCompletionValues = 100

// completionHandler 为 MCP Resource Templates 提供协议原生补全。
// 它只读取 Projects/Progress，不触碰宿主路径，也不调用模型。未知 ref/argument
// 返回空集合而不是猜测，以便不同 Host 可以安全探测 completion 能力。
func completionHandler(p *Projects) func(context.Context, *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
	return func(_ context.Context, req *mcp.CompleteRequest) (*mcp.CompleteResult, error) {
		if req == nil || req.Params == nil || req.Params.Ref == nil {
			return completionResult(nil, 0), nil
		}
		ref := req.Params.Ref
		if ref.Type != "ref/resource" || !knownResourceTemplate(ref.URI) {
			return completionResult(nil, 0), nil
		}

		name, prefix := req.Params.Argument.Name, req.Params.Argument.Value
		switch name {
		case "project":
			items, err := p.List()
			if err != nil {
				return nil, err
			}
			values := make([]string, 0, len(items))
			for _, item := range items {
				values = append(values, item.ID)
			}
			sort.Strings(values)
			return filteredCompletion(values, prefix), nil

		case "source":
			if ref.URI != resourceChapterTextTemplate {
				return completionResult(nil, 0), nil
			}
			return filteredCompletion([]string{"draft", "final"}, prefix), nil

		case "chapter":
			if ref.URI != resourceChapterContextTemplate && ref.URI != resourceChapterTextTemplate {
				return completionResult(nil, 0), nil
			}
			project := completionContextArg(req.Params.Context, "project")
			if !validID(project) {
				return completionResult(nil, 0), nil
			}
			b, err := p.load(project)
			if err != nil {
				// 补全是辅助 UI；上下文里的项目已经过期或不存在时返回空建议，
				// 不把一次输入提示升级成项目读取错误。
				return completionResult(nil, 0), nil
			}
			b.mu.RLock()
			progress, err := b.store.Progress.Load()
			outline, outlineErr := b.store.Outline.LoadOutline()
			b.mu.RUnlock()
			if err != nil || progress == nil {
				if err != nil {
					return nil, err
				}
				return completionResult(nil, 0), nil
			}
			if outlineErr != nil {
				return nil, outlineErr
			}

			// final 资源只对已经提交的章节给建议。
			if ref.URI == resourceChapterTextTemplate && completionContextArg(req.Params.Context, "source") == "final" {
				values := make([]string, 0, len(progress.CompletedChapters))
				for _, chapter := range progress.CompletedChapters {
					if chapter > 0 {
						values = append(values, strconv.Itoa(chapter))
					}
				}
				return filteredNumericCompletion(values, prefix), nil
			}

			// TotalChapters 在长篇分层模式可能包含未展开弧的 EstimatedChapters，
			// domain 明确规定它只是内部容量值，禁止暴露给模型。补全只能来自
			// 已落盘的扁平大纲，以及已经发生/正在发生的章节事实。
			values := make([]string, 0, len(outline)+len(progress.CompletedChapters)+2)
			for _, entry := range outline {
				if entry.Chapter > 0 {
					values = append(values, strconv.Itoa(entry.Chapter))
				}
			}
			for _, chapter := range progress.CompletedChapters {
				if chapter > 0 {
					values = append(values, strconv.Itoa(chapter))
				}
			}
			if progress.CurrentChapter > 0 {
				values = append(values, strconv.Itoa(progress.CurrentChapter))
			}
			if progress.InProgressChapter > 0 {
				values = append(values, strconv.Itoa(progress.InProgressChapter))
			}
			return filteredNumericCompletion(values, prefix), nil
		}
		return completionResult(nil, 0), nil
	}
}

func knownResourceTemplate(uri string) bool {
	switch uri {
	case resourceProjectStatusTemplate, resourceProjectContextTemplate, resourceChapterContextTemplate, resourceChapterTextTemplate:
		return true
	default:
		return false
	}
}

func completionContextArg(ctx *mcp.CompleteContext, name string) string {
	if ctx == nil || ctx.Arguments == nil {
		return ""
	}
	return ctx.Arguments[name]
}

func filteredCompletion(values []string, prefix string) *mcp.CompleteResult {
	matched := make([]string, 0, len(values))
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			matched = append(matched, value)
		}
	}
	return completionResult(matched, len(matched))
}

func filteredNumericCompletion(values []string, prefix string) *mcp.CompleteResult {
	// 数字按数值顺序输出，避免字符串排序把 10 放在 2 前面。
	seen := make(map[int]struct{}, len(values))
	nums := make([]int, 0, len(values))
	for _, value := range values {
		n, err := strconv.Atoi(value)
		if err != nil || n <= 0 || !strings.HasPrefix(value, prefix) {
			continue
		}
		if _, ok := seen[n]; ok {
			continue
		}
		seen[n] = struct{}{}
		nums = append(nums, n)
	}
	sort.Ints(nums)
	matched := make([]string, 0, len(nums))
	for _, n := range nums {
		matched = append(matched, strconv.Itoa(n))
	}
	return completionResult(matched, len(matched))
}

func completionResult(values []string, total int) *mcp.CompleteResult {
	if values == nil {
		values = []string{}
	}
	hasMore := len(values) > maxCompletionValues
	if hasMore {
		values = values[:maxCompletionValues]
	}
	return &mcp.CompleteResult{Completion: mcp.CompletionResultDetails{
		Values: values, Total: total, HasMore: hasMore,
	}}
}
