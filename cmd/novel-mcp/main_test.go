package main

import (
	"encoding/json"
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

func TestSavedConfigStoresRelativeDataPath(t *testing.T) {
	dir := t.TempDir()
	c := defaults()
	c.Data = dir
	if err := saveStartupConfig(c); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	var disk Config
	if err := json.Unmarshal(raw, &disk); err != nil {
		t.Fatal(err)
	}
	if disk.Data != "." {
		t.Fatalf("stored data = %q, want .", disk.Data)
	}
	loaded, err := loadConfig(filepath.Join(dir, "config.json"))
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Data != dir {
		t.Fatalf("resolved data = %q, want %q", loaded.Data, dir)
	}
}

func TestConfigAllowsWildcardOrigin(t *testing.T) {
	c := defaults()
	c.AllowOrigins = []string{"*"}
	if err := c.validate(false); err != nil {
		t.Fatalf("wildcard origin should validate: %v", err)
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
