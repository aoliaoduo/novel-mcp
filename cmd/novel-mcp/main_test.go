package main

import (
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"novel-mcp/internal/public"
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

func TestConfigRejectsTrailingJSON(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	if err := os.WriteFile(path, []byte(`{"port":8765} {"host":"0.0.0.0"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := loadConfig(path); err == nil {
		t.Fatal("config with a second JSON value must be rejected")
	}
}

func TestOriginNormalization(t *testing.T) {
	got, err := normalizeOrigin(" https://example.com/ ")
	if err != nil {
		t.Fatal(err)
	}
	if got != "https://example.com" {
		t.Fatalf("normalized origin = %q", got)
	}
	for _, bad := range []string{"ftp://example.com", "https://example.com/path", "https://user@example.com", "https://example.com?q=1", "https://:443", "https://example.com:", "https://example.com:0", "https://example.com:65536"} {
		if _, err := normalizeOrigin(bad); err == nil {
			t.Fatalf("invalid origin accepted: %s", bad)
		}
	}
}

func TestHostAllowlistIncludesIPv6Loopback(t *testing.T) {
	c := defaults()
	c.Port = 8765
	hosts := hostAllowlist(c, "127.0.0.1:8765")
	for _, host := range hosts {
		if host == "[::1]:8765" {
			return
		}
	}
	t.Fatalf("IPv6 loopback host missing from %#v", hosts)
}

func TestReserveServeListenerActuallyReservesPort(t *testing.T) {
	c := defaults()
	c.Port = 0
	ln, _, err := reserveServeListener(c)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	if _, err := net.Listen("tcp", ln.Addr().String()); err == nil {
		t.Fatal("second listener unexpectedly bound reserved port")
	}
}

func TestPublicConfigForcesReadyLoopback(t *testing.T) {
	c := defaults()
	c.Host = "0.0.0.0"
	got := publicConfig(c, &public.Ready{Host: "127.0.0.1", Port: 9000, PublicURL: "https://example.test", AllowOrigins: []string{"*"}})
	if got.Host != "127.0.0.1" || got.Port != 9000 {
		t.Fatalf("public config did not adopt ready listener: %+v", got)
	}
}

func TestAccessNoteUsesConfiguredHTTPSPort(t *testing.T) {
	ok := true
	note := accessNote(&public.Ready{Mode: "funnel", ServePort: 8443, PublicOk: &ok})
	if !strings.Contains(note, "8443") || strings.Contains(note, "仅 443") {
		t.Fatalf("custom HTTPS port missing from TUI note: %q", note)
	}
}

func TestLocalAndPublicRejectCustomHost(t *testing.T) {
	for _, cmd := range []string{"local", "public"} {
		err := run([]string{cmd, "--host", "0.0.0.0", "--no-tui"})
		var use *usageError
		if !errors.As(err, &use) {
			t.Fatalf("%s custom host should be a usage error, got %T %v", cmd, err, err)
		}
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
