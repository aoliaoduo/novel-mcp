package main

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"
	"testing"

	"novel-mcp/internal/server"
)

func TestDoctorReportRedactsSecretsAndPaths(t *testing.T) {
	dir := t.TempDir()
	credsPath := filepath.Join(dir, "credentials.json")
	creds, err := server.WriteCredentials(credsPath, false)
	if err != nil {
		t.Fatal(err)
	}
	c := defaults()
	c.Data = dir
	c.StartupMode = "local"

	report := buildDoctorReport(c, true, false)
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	for _, forbidden := range []string{creds.Route, creds.Bearer, dir} {
		if forbidden != "" && strings.Contains(text, forbidden) {
			t.Fatalf("doctor report leaked sensitive value %q: %s", forbidden, text)
		}
	}
	if !report.OK {
		t.Fatalf("valid local setup should not have blocking doctor failures: %+v", report.Checks)
	}
}

func TestDoctorMissingCredentialsIsWarningNotFailure(t *testing.T) {
	c := defaults()
	c.Data = t.TempDir()
	report := buildDoctorReport(c, false, false)
	if !report.OK {
		t.Fatalf("first-run missing credentials should remain diagnosable without failure: %+v", report.Checks)
	}
	found := false
	for _, check := range report.Checks {
		if check.Name == "credentials" {
			found = true
			if check.Status != "warn" {
				t.Fatalf("credentials status=%q want warn", check.Status)
			}
		}
	}
	if !found {
		t.Fatal("doctor report missing credentials check")
	}
}

func TestDoctorInvalidConfigFailsSafely(t *testing.T) {
	c := defaults()
	c.Data = t.TempDir()
	c.Host = "not-an-ip"
	report := buildDoctorReport(c, true, false)
	if report.OK {
		t.Fatal("invalid host should fail doctor config safety check")
	}
}

func TestDoctorConfigLoadFailureDoesNotEchoOriginalError(t *testing.T) {
	c := defaults()
	c.Data = t.TempDir()
	fakePath := "C:" + string(filepath.Separator) + filepath.Join("Users", "private-marker", "config.json")
	report := buildDoctorReport(c, true, false, fmt.Errorf("config %s: private-marker", fakePath))
	raw, err := json.Marshal(report)
	if err != nil {
		t.Fatal(err)
	}
	text := string(raw)
	if strings.Contains(text, "private-marker") {
		t.Fatalf("doctor echoed raw config error: %s", text)
	}
	if report.OK {
		t.Fatal("config load failure must make doctor fail")
	}
}
