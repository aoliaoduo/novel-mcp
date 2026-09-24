// Modified for novel-mcp: local module import paths; see UPSTREAM.md.
package assets

import (
	"embed"
	"fmt"
	"sort"
	"strings"

	"novel-mcp/internal/tools"
)

//go:embed prompts/*.md
var promptsFS embed.FS

//go:embed references
var referencesFS embed.FS

//go:embed voice.md
var voiceFS embed.FS

// Prompts 只保留 MCP 原生 Prompts/novel_guide 实际暴露的四个角色协议。
type Prompts struct {
	ArchitectShort string
	ArchitectLong  string
	Writer         string
	Editor         string
}

type Bundle struct {
	References tools.References
	Prompts    Prompts
	Voice      string
}

// Load 只读取编译进二进制的确定性资产，不读取宿主 HOME、cwd 或项目外文件。
func Load(style string) Bundle {
	return Bundle{
		References: loadReferences(style),
		Prompts:    loadPrompts(),
		Voice:      mustRead(voiceFS, "voice.md"),
	}
}

const voicePlaceholder = "{{VOICE}}"

func BuildWriterPrompt(writerPrompt, voice string) string {
	return strings.Replace(writerPrompt, voicePlaceholder, strings.TrimSpace(voice), 1)
}

func StyleNames() []string {
	names := []string{"default"}
	entries, err := referencesFS.ReadDir("references/genres")
	if err != nil {
		return names
	}
	for _, entry := range entries {
		if entry.IsDir() {
			names = append(names, entry.Name())
		}
	}
	sort.Strings(names)
	return names
}

func HasStyle(style string) bool {
	for _, name := range StyleNames() {
		if style == name {
			return true
		}
	}
	return false
}

func loadReferences(style string) tools.References {
	if style == "" {
		style = "default"
	}
	refs := tools.References{
		ChapterGuide:      mustRead(referencesFS, "references/chapter-guide.md"),
		HookTechniques:    mustRead(referencesFS, "references/hook-techniques.md"),
		QualityChecklist:  mustRead(referencesFS, "references/quality-checklist.md"),
		OutlineTemplate:   mustRead(referencesFS, "references/outline-template.md"),
		CharacterTemplate: mustRead(referencesFS, "references/character-template.md"),
		ChapterTemplate:   mustRead(referencesFS, "references/chapter-template.md"),
		Consistency:       mustRead(referencesFS, "references/consistency.md"),
		ContentExpansion:  mustRead(referencesFS, "references/content-expansion.md"),
		DialogueWriting:   mustRead(referencesFS, "references/dialogue-writing.md"),
		LongformPlanning:  mustRead(referencesFS, "references/longform-planning.md"),
		Differentiation:   mustRead(referencesFS, "references/differentiation.md"),
		AntiAITone:        mustRead(referencesFS, "references/anti-ai-tone.md"),
	}
	if style != "default" {
		genreDir := "references/genres/" + style + "/"
		if data, err := referencesFS.ReadFile(genreDir + "style-references.md"); err == nil {
			refs.StyleReference = string(data)
		}
		if data, err := referencesFS.ReadFile(genreDir + "arc-templates.md"); err == nil {
			refs.ArcTemplates = string(data)
		}
	}
	return refs
}

func loadPrompts() Prompts {
	return Prompts{
		ArchitectShort: mustRead(promptsFS, "prompts/architect-short.md"),
		ArchitectLong:  mustRead(promptsFS, "prompts/architect-long.md"),
		Writer:         mustRead(promptsFS, "prompts/writer.md"),
		Editor:         mustRead(promptsFS, "prompts/editor.md"),
	}
}

func mustRead(fs embed.FS, path string) string {
	data, err := fs.ReadFile(path)
	if err != nil {
		panic(fmt.Sprintf("embed read %s: %v", path, err))
	}
	return string(data)
}
