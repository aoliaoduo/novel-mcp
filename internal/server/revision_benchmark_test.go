package server

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRevisionDigestStableWithStreamingHash(t *testing.T) {
	dir := t.TempDir()
	files := map[string]string{
		"a.txt":              "alpha\n",
		"chapters/01.md":     "第一章正文\n",
		"meta/progress.json": `{"phase":"writing"}`,
	}
	for rel, content := range files {
		path := filepath.Join(dir, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// 复刻旧实现的摘要算法，锁住“流式读取只是降低峰值内存，不改变 revision”。
	outer := sha256.New()
	for _, rel := range []string{"a.txt", "chapters/01.md", "meta/progress.json"} {
		data, err := os.ReadFile(filepath.Join(dir, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(outer, "%s\x00%x\n", rel, sha256.Sum256(data))
	}
	want := hex.EncodeToString(outer.Sum(nil))
	got, err := revision(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got != want {
		t.Fatalf("revision digest changed: got=%s want=%s", got, want)
	}
}

func BenchmarkRevision(b *testing.B) {
	for _, chapters := range []int{100, 500, 1000, 3000} {
		b.Run(fmt.Sprintf("chapters_%d", chapters), func(b *testing.B) {
			dir := b.TempDir()
			body := strings.Repeat("正文基准数据", 2048)
			for i := 1; i <= chapters; i++ {
				path := filepath.Join(dir, "chapters", fmt.Sprintf("%04d.md", i))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				if _, err := revision(dir); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkScan(b *testing.B) {
	for _, chapters := range []int{100, 500, 1000} {
		b.Run(fmt.Sprintf("chapters_%d", chapters), func(b *testing.B) {
			dir := b.TempDir()
			body := strings.Repeat("正文基准数据", 512)
			for i := 1; i <= chapters; i++ {
				path := filepath.Join(dir, "chapters", fmt.Sprintf("%04d.md", i))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					b.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
					b.Fatal(err)
				}
			}
			b.ResetTimer()
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				if err := scan(dir); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
