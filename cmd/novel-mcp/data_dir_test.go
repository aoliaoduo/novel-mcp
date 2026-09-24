package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDataDirForExecutableUsesPortableMarker(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "novel-mcp.exe")
	if err := os.WriteFile(filepath.Join(dir, portableMarkerName), []byte("portable\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got := dataDirForExecutable(exe, filepath.Join(dir, "home"))
	want := filepath.Join(dir, "data")
	if got != want {
		t.Fatalf("portable data dir = %q, want %q", got, want)
	}
}

func TestDataDirForExecutableFallsBackToHome(t *testing.T) {
	dir := t.TempDir()
	home := filepath.Join(dir, "home")
	got := dataDirForExecutable(filepath.Join(dir, "novel-mcp.exe"), home)
	want := filepath.Join(home, ".novel-mcp")
	if got != want {
		t.Fatalf("default data dir = %q, want %q", got, want)
	}
}

func TestPortableConfigDataDirRebasesMovedBundle(t *testing.T) {
	dataDir := filepath.Join(t.TempDir(), "data")
	got, ok := portableConfigDataDir(filepath.Join(dataDir, "config.json"), dataDir)
	if !ok || got != dataDir {
		t.Fatalf("portable config data dir = %q, %v; want %q, true", got, ok, dataDir)
	}
	if _, ok := portableConfigDataDir(filepath.Join(t.TempDir(), "other.json"), dataDir); ok {
		t.Fatal("unrelated config must not be rebound to portable data dir")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, filepath.Join(dataDir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if got, ok := portableConfigDataDir(rel, dataDir); !ok || got != dataDir {
		t.Fatalf("relative portable config = %q, %v; want %q, true", got, ok, dataDir)
	}
}
