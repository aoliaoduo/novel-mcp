package public

import (
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"novel-mcp/internal/server"
)

func TestParseTailStatus(t *testing.T) {
	raw := `{"BackendState":"Running","Self":{"DNSName":"node.tail123.ts.net.","TailscaleIPs":["100.1.2.3"]},"Health":["warn: x"],"MagicDNSSuffix":"tail123.ts.net"}`
	st, err := ParseTailStatus([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if st.BackendState != "Running" || st.DNSName != "node.tail123.ts.net" {
		t.Fatalf("got %+v", st)
	}
	if len(st.TailscaleIPs) != 1 || st.MagicDNSSuffix != "tail123.ts.net" || len(st.Health) != 1 {
		t.Fatalf("fields lost: %+v", st)
	}
	if _, err := ParseTailStatus([]byte("not json")); err == nil {
		t.Fatal("malformed JSON should fail")
	}
}

func TestTailnetRecoveryHint(t *testing.T) {
	tests := []struct {
		name string
		st   TailStatus
		want string
	}{
		{"login", TailStatus{BackendState: "NeedsLogin"}, "登录"},
		{"machine auth", TailStatus{BackendState: "NeedsMachineAuth"}, "管理员批准"},
		{"starting", TailStatus{BackendState: "NoState", Health: []string{"Tailscale is starting"}}, "*.tailscale.com"},
		{"generic", TailStatus{BackendState: "Stopped"}, "Running"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := tailnetRecoveryHint(tt.st)
			if !strings.Contains(got, tt.want) {
				t.Fatalf("hint=%q, want substring %q", got, tt.want)
			}
		})
	}
}

func TestTailnetRecoveryHintsArePlatformSpecific(t *testing.T) {
	st := TailStatus{BackendState: "NoState", Health: []string{"Tailscale is starting"}}
	linux := tailnetRecoveryHintForOS(st, "linux")
	if !strings.Contains(linux, "tailscaled") || strings.Contains(linux, "repair.cmd") {
		t.Fatalf("linux hint leaked Windows recovery: %q", linux)
	}
	darwin := tailnetRecoveryHintForOS(st, "darwin")
	if !strings.Contains(darwin, "Tailscale 应用") || strings.Contains(darwin, "repair.cmd") {
		t.Fatalf("darwin hint leaked Windows recovery: %q", darwin)
	}
}

func TestFunnelAlreadyOn(t *testing.T) {
	on := "Funnel on\nhttps://node.tail123.ts.net -> http://127.0.0.1:8765 (tailnet only)"
	if !FunnelAlreadyOn(on, 8765, 443) {
		t.Fatal("should detect mounted funnel")
	}
	if FunnelAlreadyOn(strings.ToLower(on), 8765, 443) == false {
		t.Fatal("match must be case-insensitive like PowerShell -match")
	}
	if FunnelAlreadyOn(on, 9999, 443) {
		t.Fatal("wrong backend port must not match")
	}
	if FunnelAlreadyOn(on, 8765, 8443) {
		t.Fatal("wrong HTTPS port must not match")
	}
	on8443 := "Funnel on\nhttps://node.tail123.ts.net:8443 -> http://127.0.0.1:8765"
	if !FunnelAlreadyOn(on8443, 8765, 8443) {
		t.Fatal("non-default HTTPS port should match")
	}
	if FunnelAlreadyOn("no serve config", 8765, 443) {
		t.Fatal("empty status must not match")
	}
	multi := "Funnel on\nhttps://node.tail123.ts.net:8443 (Funnel on)\n|-- / proxy http://127.0.0.1:9999\nhttps://node.tail123.ts.net (Funnel on)\n|-- / proxy http://127.0.0.1:8765"
	if FunnelAlreadyOn(multi, 8765, 8443) {
		t.Fatal("must not pair an HTTPS port from one mapping with another mapping's backend")
	}
	if !FunnelAlreadyOn(multi, 8765, 443) {
		t.Fatal("should match backend within its own mapping block")
	}
}

func TestPublicBaseURLIncludesNonDefaultHTTPSPort(t *testing.T) {
	if got := publicBaseURL("node.tail123.ts.net", 443); got != "https://node.tail123.ts.net" {
		t.Fatalf("default URL = %q", got)
	}
	if got := publicBaseURL("node.tail123.ts.net", 8443); got != "https://node.tail123.ts.net:8443" {
		t.Fatalf("custom-port URL = %q", got)
	}
}

func TestCheckConsent(t *testing.T) {
	if err := CheckConsent(true, true, false); err != nil {
		t.Fatal("funnel+bearer must pass")
	}
	if err := CheckConsent(true, false, true); err != nil {
		t.Fatal("explicit token-only consent must pass")
	}
	if err := CheckConsent(false, false, false); err != nil {
		t.Fatal("tailnet-only without bearer must pass (原 setup.ps1 行为)")
	}
	if err := CheckConsent(true, false, false); err == nil {
		t.Fatal("public funnel without bearer and without consent must be refused")
	} else if !strings.Contains(err.Error(), "--allow-public-url-token-only") {
		t.Fatalf("refusal must name the consent flag: %v", err)
	}
}

func TestPublicURLShape(t *testing.T) {
	st, _ := ParseTailStatus([]byte(`{"BackendState":"Running","Self":{"DNSName":"a.b.ts.net."}}`))
	if got := "https://" + st.DNSName; got != "https://a.b.ts.net" {
		t.Fatalf("trailing dot must be trimmed: %s", got)
	}
}

func TestPaintRespectsSwitch(t *testing.T) {
	colorEnabled = false
	if got := Green("x"); got != "x" {
		t.Fatalf("disabled must pass through: %q", got)
	}
	colorEnabled = true
	defer func() { colorEnabled = false }()
	if got := Green("x"); got != "\x1b[32mx\x1b[0m" {
		t.Fatalf("enabled must wrap: %q", got)
	}
	if got := padLabel("配置", 8); got != "配置    " {
		t.Fatalf("CJK pad wrong: %q", got)
	}
	if got := padLabel("tailnet", 8); got != "tailnet " {
		t.Fatalf("ascii pad wrong: %q", got)
	}
}

func TestParseDoHResponse(t *testing.T) {
	ips, ok, err := parseDoHResponse([]byte(`{"Status":0,"Answer":[{"data":"1.2.3.4"},{"data":"5.6.7.8"}]}`))
	if err != nil || !ok || len(ips) != 2 || ips[0] != "1.2.3.4" {
		t.Fatalf("got %v %v %v", ips, ok, err)
	}
	_, ok, err = parseDoHResponse([]byte(`{"Status":3}`))
	if err != nil || ok {
		t.Fatalf("NXDOMAIN must be reached-but-not-published: %v %v", ok, err)
	}
	if _, _, err := parseDoHResponse([]byte(`{oops`)); err == nil {
		t.Fatal("malformed must fail")
	}
}

func TestQueryDoHRejectsHTTPErrorStatus(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
		_, _ = w.Write([]byte(`{"Status":3}`))
	}))
	defer ts.Close()
	got := queryDoH(ts.Client(), ts.URL, "node.example")
	if got.reached || got.published {
		t.Fatalf("HTTP error must not count as a reachable DNS answer: %+v", got)
	}
}

func TestCombineDoHResultsPrefersPublishedResolver(t *testing.T) {
	ips, published, reachable := combineDoHResults(
		dohResult{reached: true, published: false},
		dohResult{ips: []string{"1.2.3.4"}, reached: true, published: true},
	)
	if !reachable || !published || len(ips) != 1 || ips[0] != "1.2.3.4" {
		t.Fatalf("combined DoH result = %v %v %v", ips, published, reachable)
	}
}

func TestSnippetDoesNotSplitUTF8(t *testing.T) {
	if got := snippet("甲乙丙", 2); got != "甲乙…" {
		t.Fatalf("snippet = %q", got)
	}
}

func TestNoRedirectClientDoesNotFollow(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/from" {
			http.Redirect(w, r, "/to", http.StatusFound)
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()
	resp, err := noRedirectClient(time.Second).Get(ts.URL + "/from")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Fatalf("redirect followed unexpectedly: %d", resp.StatusCode)
	}
}

func TestProbeConnectionMatchesSavedCredentials(t *testing.T) {
	route := strings.Repeat("a", 64)
	bearer := strings.Repeat("b", 64)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/mcp/"+route {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer "+bearer {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusMethodNotAllowed)
	}))
	defer ts.Close()
	u, err := url.Parse(ts.URL)
	if err != nil {
		t.Fatal(err)
	}
	_, portText, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		t.Fatal(err)
	}
	if !ProbeConnection(port, ConnectionInfo{Route: route, Bearer: bearer}) {
		t.Fatal("saved credentials should match running instance")
	}
	if ProbeConnection(port, ConnectionInfo{Route: route, Bearer: strings.Repeat("c", 64)}) {
		t.Fatal("wrong bearer must not match running instance")
	}
}

func TestProbeRunning(t *testing.T) {
	portOf := func(ts *httptest.Server) int {
		t.Helper()
		u, err := url.Parse(ts.URL)
		if err != nil {
			t.Fatal(err)
		}
		p, err := strconv.Atoi(u.Port())
		if err != nil {
			t.Fatal(err)
		}
		return p
	}
	good := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/healthz" {
			w.Write([]byte(`{"service":"novel-mcp","ok":true}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer good.Close()
	if !ProbeRunning(portOf(good)) {
		t.Fatal("should detect novel-mcp on the port")
	}
	// 别的程序占着端口：body 不对就不能误认。
	wrong := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"service":"something-else"}`))
	}))
	defer wrong.Close()
	if ProbeRunning(portOf(wrong)) {
		t.Fatal("must not mistake another program for novel-mcp")
	}
	// 没东西的端口：2 秒超时内返回 false。
	if ProbeRunning(1) {
		t.Fatal("closed port should return false")
	}
}

func TestLoadConnection(t *testing.T) {
	dir := t.TempDir()
	want, err := server.WriteCredentials(filepath.Join(dir, "credentials.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := `{"public_url":"https://node.tail123.ts.net/","require_bearer":true}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := LoadConnection(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.Route != want.Route || info.Bearer != want.Bearer {
		t.Fatalf("creds mismatch: %+v", info)
	}
	if info.PublicURL != "https://node.tail123.ts.net" || info.BearerOff {
		t.Fatalf("config mismatch: %+v", info)
	}
	// 空目录：报错，调用方回落正常启动流程。
	if _, err := LoadConnection(t.TempDir()); err == nil {
		t.Fatal("missing credentials should fail")
	}
}

func TestLoadConnectionLocalMode(t *testing.T) {
	dir := t.TempDir()
	want, err := server.WriteCredentials(filepath.Join(dir, "credentials.json"), false)
	if err != nil {
		t.Fatal(err)
	}
	cfg := `{"host":"127.0.0.1","port":9876,"startup_mode":"local","require_bearer":true}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := LoadConnection(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.PublicURL != "http://127.0.0.1:9876" || info.Route != want.Route || info.Bearer != want.Bearer || info.BearerOff {
		t.Fatalf("local connection mismatch: %+v", info)
	}
}

func TestLoadConnectionLocalIPv6URL(t *testing.T) {
	dir := t.TempDir()
	if _, err := server.WriteCredentials(filepath.Join(dir, "credentials.json"), false); err != nil {
		t.Fatal(err)
	}
	cfg := `{"host":"::1","port":9876,"startup_mode":"local","require_bearer":true}`
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := LoadConnection(dir)
	if err != nil {
		t.Fatal(err)
	}
	if info.PublicURL != "http://[::1]:9876" {
		t.Fatalf("IPv6 local URL = %q", info.PublicURL)
	}
}

func TestLoadConnectionRejectsMalformedConfig(t *testing.T) {
	dir := t.TempDir()
	if _, err := server.WriteCredentials(filepath.Join(dir, "credentials.json"), false); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(`{`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadConnection(dir); err == nil {
		t.Fatal("malformed config must not yield a reusable connection card")
	}
}

func TestLoadConnectionRejectsInvalidStoredEndpoint(t *testing.T) {
	for name, cfg := range map[string]string{
		"public bad scheme":  `{"public_url":"http://example.com","startup_mode":"public","require_bearer":true}`,
		"local non-loopback": `{"host":"0.0.0.0","port":9876,"startup_mode":"local","require_bearer":true}`,
	} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := server.WriteCredentials(filepath.Join(dir, "credentials.json"), false); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte(cfg), 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadConnection(dir); err == nil {
				t.Fatalf("invalid stored endpoint accepted: %s", cfg)
			}
		})
	}
}

func TestFirstIPv4(t *testing.T) {
	if got := firstIPv4([]string{"fd7a::1", "100.1.2.3"}); got != "100.1.2.3" {
		t.Fatalf("want v4 first, got %s", got)
	}
	if got := firstIPv4([]string{"100.1.2.3"}); got != "100.1.2.3" {
		t.Fatalf("got %s", got)
	}
	if got := firstIPv4(nil); got != "无地址" {
		t.Fatalf("got %s", got)
	}
}
