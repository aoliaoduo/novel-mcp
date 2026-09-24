// Modified for novel-mcp: LLM-call chain removed (see UPSTREAM.md); Nullable tests only.
package llmcontract

import (
	"testing"

	"github.com/voocel/agentcore/schema"
)

func TestNullableCopies(t *testing.T) {
	orig := schema.String("可空字段")
	out := Nullable(orig)
	got, ok := out["type"].([]string)
	if !ok || len(got) != 2 || got[0] != "string" || got[1] != "null" {
		t.Fatalf("Nullable type = %v", out["type"])
	}
	if orig["type"] != "string" {
		t.Fatalf("Nullable 修改了传入 map: %v", orig["type"])
	}
}

func TestNullableExtendsEnumWithNull(t *testing.T) {
	orig := schema.Enum("可空枚举", "a", "b")
	out := Nullable(orig)
	enum, ok := out["enum"].([]any)
	if !ok || len(enum) != 3 || enum[2] != nil {
		t.Fatalf("Nullable enum = %#v", out["enum"])
	}
	if _, ok := orig["enum"].([]string); !ok {
		t.Fatalf("Nullable 修改了传入 enum: %#v", orig["enum"])
	}
}
