package server

import (
	"errors"
	"path/filepath"
	"testing"
)

// TestDeleteProjectNeedsFreshRevision：删除必须带最新 revision，对得上才删，删完 ID 可重用。
func TestDeleteProjectNeedsFreshRevision(t *testing.T) {
	p, err := NewProjects(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatal(err)
	}
	created, err := p.Create("doomed", "待删", "default")
	if err != nil {
		t.Fatal(err)
	}
	rev := created["revision"].(string)
	// 过期 revision：拒绝，且项目还在。
	out, err := p.Delete("doomed", "0000000000000000000000000000000000000000000000000000000000000000")
	if err != nil {
		t.Fatal(err)
	}
	em, _ := out["error"].(map[string]any)
	if em["code"] != "REVISION_CONFLICT" {
		t.Fatalf("过期 revision 应 REVISION_CONFLICT: %v", out)
	}
	if _, err := p.loadInfo("doomed"); err != nil {
		t.Fatalf("拒绝后项目应还在: %v", err)
	}
	// 最新 revision：删掉，ID 可重用。
	out, err = p.Delete("doomed", rev)
	if err != nil {
		t.Fatal(err)
	}
	res, _ := out["result"].(map[string]any)
	if res["deleted"] != true {
		t.Fatalf("应返回 deleted: %v", out)
	}
	if _, err := p.loadInfo("doomed"); err == nil {
		t.Fatal("删完应读不到")
	}
	if _, err := p.Create("doomed", "重建", "default"); err != nil {
		t.Fatalf("ID 应可重用: %v", err)
	}
}

// TestDeleteProjectNotFound：删不存在的报 PROJECT_NOT_FOUND；空 revision 报 INVALID_REQUEST。
func TestDeleteProjectNotFound(t *testing.T) {
	p, err := NewProjects(filepath.Join(t.TempDir(), "projects"))
	if err != nil {
		t.Fatal(err)
	}
	_, err = p.Delete("ghost", "0000000000000000000000000000000000000000000000000000000000000000")
	var ce *codedError
	if !errors.As(err, &ce) || ce.code != "PROJECT_NOT_FOUND" {
		t.Fatalf("应 PROJECT_NOT_FOUND: %v", err)
	}
	if _, err := p.Create("victim", "待删", "default"); err != nil {
		t.Fatal(err)
	}
	_, err = p.Delete("victim", "")
	if !errors.As(err, &ce) || ce.code != "INVALID_REQUEST" {
		t.Fatalf("空 revision 应 INVALID_REQUEST: %v", err)
	}
}
