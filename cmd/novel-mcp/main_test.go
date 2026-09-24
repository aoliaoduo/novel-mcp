package main

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"novel-mcp/internal/tui"
)

func TestTokenRotateDocumentedArgumentOrder(t *testing.T) {
	dir := t.TempDir()
	args := []string{
		"token", "rotate",
		"--data", dir,
		"--i-understand-this-invalidates-current-clients",
	}
	if err := run(args); err != nil {
		t.Fatalf("documented token rotate order should work: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "credentials.json")); err != nil {
		t.Fatalf("credentials not written: %v", err)
	}
}

func TestTokenRotateStillRequiresExplicitConfirmation(t *testing.T) {
	err := run([]string{"token", "rotate", "--data", t.TempDir()})
	if err == nil {
		t.Fatal("token rotate without confirmation must fail")
	}
}

func TestStartupModeRoundTrip(t *testing.T) {
	dir := t.TempDir()
	c := defaults()
	c.Data = dir
	c.StartupMode = string(tui.StartupLocal)
	if err := saveStartupConfig(c); err != nil {
		t.Fatal(err)
	}
	got, ok := rememberedStartupMode(dir)
	if !ok || got != string(tui.StartupLocal) {
		t.Fatalf("remembered mode = %q, %v", got, ok)
	}
	loaded, err := loadConfig(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.StartupMode != string(tui.StartupLocal) || loaded.Data != dir {
		t.Fatalf("saved config mismatch: %+v", loaded)
	}
}

func TestUnknownCommandIsUsageError(t *testing.T) {
	err := run([]string{"definitely-not-a-command"})
	var use *usageError
	if !errors.As(err, &use) {
		t.Fatalf("unknown command should be usage error, got %T %v", err, err)
	}
}

func TestUnknownFlagIsUsageError(t *testing.T) {
	err := run([]string{"local", "--not-a-flag"})
	var use *usageError
	if !errors.As(err, &use) {
		t.Fatalf("unknown flag should be usageError, got %T %v", err, err)
	}
}

func TestSubcommandHelpIsNotAnError(t *testing.T) {
	if err := run([]string{"local", "--help"}); err != nil {
		t.Fatalf("local --help should succeed: %v", err)
	}
}
