package server

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type promptSpec struct {
	name        string
	title       string
	description string
	guideRole   string
}

// registerPrompts 把纯提示模板放回 MCP 原生 prompts 语义；novel_guide 工具仍保留，
// 因为很多网页 Host 只实现 tools。两条入口读取同一份内嵌资产，不形成第二套协议。
func registerPrompts(server *mcp.Server) {
	specs := []promptSpec{
		{name: "novel_overview", title: "小说 MCP 使用总览", description: "novel-mcp 的职责边界、revision 与主循环说明", guideRole: "overview"},
		{name: "novel_architect_short", title: "短篇规划协议", description: "短篇/单卷小说的 Architect 规划协议与模板", guideRole: "architect"},
		{name: "novel_architect_long", title: "长篇规划协议", description: "长篇连载的卷弧规划、Story Compass 与扩展协议", guideRole: "architect_long"},
		{name: "novel_writer", title: "章节写作协议", description: "Writer 的逐章写作、回读、检查与提交协议", guideRole: "writer"},
		{name: "novel_editor", title: "审阅编辑协议", description: "Editor 的章节/弧/卷审阅与摘要协议", guideRole: "editor"},
	}
	for _, spec := range specs {
		spec := spec
		server.AddPrompt(&mcp.Prompt{
			Name: spec.name, Title: spec.title, Description: spec.description,
		}, func(_ context.Context, _ *mcp.GetPromptRequest) (*mcp.GetPromptResult, error) {
			guide, _ := guideForRole(spec.guideRole)
			return &mcp.GetPromptResult{
				Description: spec.description,
				Messages: []*mcp.PromptMessage{{
					Role:    mcp.Role("user"),
					Content: &mcp.TextContent{Text: guide},
				}},
			}, nil
		})
	}
}
