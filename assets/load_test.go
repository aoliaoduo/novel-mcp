// Modified for novel-mcp: local module import paths; see UPSTREAM.md.
package assets

import (
	"strings"
	"testing"
)

func TestBuildWriterPromptInjectsVoice(t *testing.T) {
	protocol := mustRead(promptsFS, "prompts/writer.md")
	voice := mustRead(voiceFS, "voice.md")
	got := BuildWriterPrompt(protocol, voice)
	if strings.Contains(got, voicePlaceholder) {
		t.Fatal("writer voice placeholder was not replaced")
	}
	if !strings.Contains(got, strings.TrimSpace(voice)) {
		t.Fatal("writer prompt missing embedded voice")
	}
}

func TestLoadEmbeddedBundle(t *testing.T) {
	b := Load("fantasy")
	if b.Voice != mustRead(voiceFS, "voice.md") {
		t.Fatal("voice must come from embedded asset")
	}
	if b.References.AntiAITone != mustRead(referencesFS, "references/anti-ai-tone.md") {
		t.Fatal("anti-ai-tone must come from embedded asset")
	}
	if b.References.StyleReference == "" || b.References.ArcTemplates == "" {
		t.Fatal("fantasy genre references must be embedded")
	}
}

func TestStyleNamesComeFromGenreReferences(t *testing.T) {
	got := StyleNames()
	want := []string{"default", "fantasy", "romance", "suspense"}
	if len(got) != len(want) {
		t.Fatalf("styles=%v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("styles=%v, want %v", got, want)
		}
		if !HasStyle(want[i]) {
			t.Fatalf("HasStyle(%q)=false", want[i])
		}
	}
	if HasStyle("unknown") {
		t.Fatal("unknown style must be rejected")
	}
}

func TestCorePromptsOnly(t *testing.T) {
	p := loadPrompts()
	for name, value := range map[string]string{
		"architect_short": p.ArchitectShort,
		"architect_long":  p.ArchitectLong,
		"writer":          p.Writer,
		"editor":          p.Editor,
	} {
		if strings.TrimSpace(value) == "" {
			t.Fatalf("%s prompt is empty", name)
		}
	}
}
