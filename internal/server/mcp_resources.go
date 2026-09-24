package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

const resourceJSON = "application/json"

const (
	resourceProjectStatusTemplate  = "novel://project/{project}/status"
	resourceProjectContextTemplate = "novel://project/{project}/context"
	resourceChapterContextTemplate = "novel://project/{project}/context/{chapter}"
	resourceChapterTextTemplate    = "novel://project/{project}/chapter/{chapter}/{source}"
)

func resourceText(uri, mime, text string) *mcp.ReadResourceResult {
	return &mcp.ReadResourceResult{Contents: []*mcp.ResourceContents{{
		URI: uri, MIMEType: mime, Text: text,
	}}}
}

func marshalResourceJSON(uri string, v any) (*mcp.ReadResourceResult, error) {
	raw, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return nil, err
	}
	return resourceText(uri, resourceJSON, string(raw)), nil
}

func resourceProjectID(u *url.URL) (string, bool) {
	if u == nil || u.Scheme != "novel" || u.Host != "project" {
		return "", false
	}
	parts := strings.Split(strings.Trim(u.Path, "/"), "/")
	if len(parts) == 0 || !validID(parts[0]) {
		return "", false
	}
	return parts[0], true
}

func resourceCall(ctx context.Context, p *Projects, uri, project, name string, args map[string]any) (*mcp.ReadResourceResult, error) {
	out, err := p.Call(ctx, project, name, "", args)
	if err != nil {
		var ce *codedError
		if errors.As(err, &ce) && ce.code == "PROJECT_NOT_FOUND" {
			return nil, mcp.ResourceNotFoundError(uri)
		}
		return nil, err
	}
	return marshalResourceJSON(uri, out)
}

// registerResources 暴露“读事实”的 MCP 原生资源。它们不映射宿主文件路径，
// 只接受已经存在的 project ID / chapter / source，并复用 Projects.Call 的安全门、
// 一本内串行和 revision 读取逻辑。现有读工具继续保留给只支持 tools 的 Host。
func registerResources(server *mcp.Server, p *Projects) {
	server.AddResource(&mcp.Resource{
		URI: "novel://projects", Name: "projects", Title: "小说项目列表",
		Description: "当前数据根下的小说项目元数据；不包含宿主路径", MIMEType: resourceJSON,
	}, func(_ context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		items, err := p.List()
		if err != nil {
			return nil, err
		}
		return marshalResourceJSON(req.Params.URI, map[string]any{"projects": items})
	})

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: resourceProjectStatusTemplate, Name: "project_status", Title: "小说项目状态",
		Description: "项目进度、基础设定缺项、pending commit 与一致性告警", MIMEType: resourceJSON,
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		u, err := url.Parse(req.Params.URI)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		project, ok := resourceProjectID(u)
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if !ok || len(parts) != 2 || parts[1] != "status" {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		return resourceCall(ctx, p, req.Params.URI, project, "project_status", nil)
	})

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: resourceProjectContextTemplate, Name: "project_context", Title: "小说全局上下文",
		Description: "novel_context 的全局只读视图，适合规划与恢复", MIMEType: resourceJSON,
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		u, err := url.Parse(req.Params.URI)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		project, ok := resourceProjectID(u)
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if !ok || len(parts) != 2 || parts[1] != "context" {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		return resourceCall(ctx, p, req.Params.URI, project, "novel_context", map[string]any{})
	})

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: resourceChapterContextTemplate, Name: "chapter_context", Title: "章节写作上下文",
		Description: "指定章节的 novel_context 只读视图", MIMEType: resourceJSON,
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		u, err := url.Parse(req.Params.URI)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		project, ok := resourceProjectID(u)
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if !ok || len(parts) != 3 || parts[1] != "context" {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		chapter, err := strconv.Atoi(parts[2])
		if err != nil || chapter <= 0 || chapter > 10000 {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		return resourceCall(ctx, p, req.Params.URI, project, "novel_context", map[string]any{"chapter": chapter})
	})

	server.AddResourceTemplate(&mcp.ResourceTemplate{
		URITemplate: resourceChapterTextTemplate, Name: "chapter_text", Title: "章节原文",
		Description: "读取指定章节的 final 终稿或 draft 草稿；不暴露磁盘路径", MIMEType: "text/markdown",
	}, func(ctx context.Context, req *mcp.ReadResourceRequest) (*mcp.ReadResourceResult, error) {
		u, err := url.Parse(req.Params.URI)
		if err != nil {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		project, ok := resourceProjectID(u)
		parts := strings.Split(strings.Trim(u.Path, "/"), "/")
		if !ok || len(parts) != 4 || parts[1] != "chapter" {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		chapter, err := strconv.Atoi(parts[2])
		if err != nil || chapter <= 0 || chapter > 10000 || (parts[3] != "final" && parts[3] != "draft") {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		// Resource 保持纯读取语义，并直接返回 markdown 正文；这里走 Store 的只读加载，
		// 同时持有 book.mu 与写工具串行化，避免读取到同一本书的中间写入状态。
		b, err := p.load(project)
		if err != nil {
			var ce *codedError
			if errors.As(err, &ce) && ce.code == "PROJECT_NOT_FOUND" {
				return nil, mcp.ResourceNotFoundError(req.Params.URI)
			}
			return nil, err
		}
		b.mu.RLock()
		defer b.mu.RUnlock()
		var content string
		if parts[3] == "final" {
			content, err = b.store.Drafts.LoadChapterText(chapter)
		} else {
			content, err = b.store.Drafts.LoadDraft(chapter)
		}
		if err != nil {
			return nil, err
		}
		if content == "" {
			return nil, mcp.ResourceNotFoundError(req.Params.URI)
		}
		return resourceText(req.Params.URI, "text/markdown", content), nil
	})
}

func resourceURI(project string, chapter int, source string) string {
	return fmt.Sprintf("novel://project/%s/chapter/%d/%s", project, chapter, source)
}

func projectStatusResourceURI(project string) string {
	return fmt.Sprintf("novel://project/%s/status", project)
}

func projectContextResourceURI(project string) string {
	return fmt.Sprintf("novel://project/%s/context", project)
}

func chapterContextResourceURI(project string, chapter int) string {
	return fmt.Sprintf("novel://project/%s/context/%d", project, chapter)
}

// actionResourceURI 返回该动作对应的规范 MCP Resource（如果存在）。它表达的是
// 动作读取或产出的同一工件，不是执行动作本身的替代品。
func actionResourceURI(project, tool string, args map[string]any) string {
	switch tool {
	case "project_status":
		return projectStatusResourceURI(project)
	case "novel_context":
		if chapter := intArg(args, "chapter", 0); chapter > 0 {
			return chapterContextResourceURI(project, chapter)
		}
		return projectContextResourceURI(project)
	case "read_chapter":
		chapter := intArg(args, "chapter", 0)
		source, _ := args["source"].(string)
		if chapter > 0 && (source == "draft" || source == "final") {
			return resourceURI(project, chapter, source)
		}
	case "draft_chapter", "edit_chapter":
		if chapter := intArg(args, "chapter", 0); chapter > 0 {
			return resourceURI(project, chapter, "draft")
		}
	case "commit_chapter":
		if chapter := intArg(args, "chapter", 0); chapter > 0 {
			return resourceURI(project, chapter, "final")
		}
	}
	return ""
}

func enrichToolResourceURI(project, tool string, args map[string]any, result any) {
	view, ok := result.(map[string]any)
	if !ok {
		return
	}
	if uri := actionResourceURI(project, tool, args); uri != "" {
		view["resource_uri"] = uri
	}
}
