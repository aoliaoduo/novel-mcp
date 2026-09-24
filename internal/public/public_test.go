package public

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

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

func TestFunnelAlreadyOn(t *testing.T) {
	on := "Funnel on\nhttps://node.tail123.ts.net -> http://127.0.0.1:8765 (tailnet only)"
	if !FunnelAlreadyOn(on, 8765) {
		t.Fatal("should detect mounted funnel")
	}
	if FunnelAlreadyOn(strings.ToLower(on), 8765) == false {
		t.Fatal("match must be case-insensitive like PowerShell -match")
	}
	if FunnelAlreadyOn(on, 9999) {
		t.Fatal("wrong port must not match")
	}
	if FunnelAlreadyOn("no serve config", 8765) {
		t.Fatal("empty status must not match")
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
