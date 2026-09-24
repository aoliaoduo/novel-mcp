package server

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestProjectRejectsNonCurrentFormat(t *testing.T) {
	for _, tc := range []struct {
		name    string
		content string
	}{
		{name: "missing", content: ""},
		{name: "other-version", content: `{"version":2}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := newProjects(t)
			id := create(t, p, "format-"+tc.name)
			formatPath := filepath.Join(p.dir(id), "meta", "format.json")
			if tc.content == "" {
				if err := os.Remove(formatPath); err != nil {
					t.Fatal(err)
				}
			} else if err := os.WriteFile(formatPath, []byte(tc.content), 0o600); err != nil {
				t.Fatal(err)
			}

			_, err := p.Call(context.Background(), id, "project_status", "", nil)
			if err == nil {
				t.Fatal("非当前项目格式必须直接拒绝")
			}
			if got := codeOf(err); got != "PROJECT_DAMAGED" {
				t.Fatalf("code=%s, want PROJECT_DAMAGED: %v", got, err)
			}
		})
	}
}
