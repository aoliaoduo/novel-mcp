// Package public 是一键公网链的 Go 实现（原 scripts/tailscale/*.ps1 已合并至此并移除）：
//
//	tailnet 检查（不通则拉起托盘进程）→ 写 config.json → tailscale funnel/serve
//	挂载（已挂好就不动）→ 公网 DNS 检查 → 本进程直接 serve → 打印 ACCESS URL。
//
// 双击 novel-mcp.exe（无参数）= 跑这条链；日常行为与原 .lnk 链 -Funnel -WithBearer
// -SkipSmoke 对齐：默认 funnel + Bearer，不跑 smoke（--smoke 可开轻量版）。
package public

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"novel-mcp/internal/server"
)

// Version 是 exe 版本号（横幅/标题栏/version 命令共用），别名 server.Version。
// 版本号唯一源头在 server 包：public 依赖 server，mcp 握手也必须用同一值。
const Version = server.Version

// DefaultAllowOrigins 与原 setup.ps1 的 -AllowOrigins 默认值一致。
var DefaultAllowOrigins = []string{
	"https://arena.ai",
	"https://www.arena.ai",
	"https://lmarena.ai",
	"https://www.lmarena.ai",
}

const (
	defaultTailscaleExe = `C:\Program Files\Tailscale\tailscale.exe`
	defaultTailscaleIPN = `C:\Program Files\Tailscale\tailscale-ipn.exe`

	// dohTimeout 是单个 DoH 解析器的超时。超时只算“未知/跳过”，不算未发布，
	// 所以可以比原 ps1 的 12 秒激进：双发并行，最坏也只挡 5 秒。
	dohTimeout = 5 * time.Second
)

// Options 是 Preflight 的输入，由 main 的 flag 组装。
type Options struct {
	Port           int
	ServePort      int // funnel/serve 的 --https 端口，默认 443
	DataDir        string
	RequireBearer  bool
	Funnel         bool // false = 仅 tailnet（tailscale serve）
	AllowTokenOnly bool // --allow-public-url-token-only：公网无 Bearer 的明确同意
	AllowOrigins   []string
	Smoke          bool   // 轻量公网 smoke（默认关，与原 .lnk 链 -SkipSmoke 对齐）
	DryRun         bool   // 只读侦察：不写配置、不挂载、不起服务
	TailscaleExe   string // 为空则自动找
	Out            io.Writer
	Progress       func(id, label, state, detail string)
}

// Ready 是 Preflight 完成后的待命状态；main 凭它起 serve。
type Ready struct {
	DryRun         bool
	Mode           string // "funnel" 或 "serve"
	DNS            string
	MagicDNSSuffix string
	PublicURL      string
	PublicOk       *bool // funnel 下 DNS 检查结论；nil=未知（非 funnel/解析器不可达）
	TailscaleExe   string
	Host           string
	Port           int
	DataDir        string
	AllowOrigins   []string
	RequireBearer  bool
	ConfigPath     string
	StartedAt      time.Time
}

// TailStatus 是 tailscale status --json 里用到的子集。
type TailStatus struct {
	BackendState   string
	DNSName        string
	TailscaleIPs   []string
	Health         []string
	MagicDNSSuffix string
}

// ParseTailStatus 解析 tailscale status --json。
func ParseTailStatus(data []byte) (TailStatus, error) {
	var raw struct {
		BackendState string `json:"BackendState"`
		Self         struct {
			DNSName      string   `json:"DNSName"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
		Health         []string `json:"Health"`
		MagicDNSSuffix string   `json:"MagicDNSSuffix"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return TailStatus{}, err
	}
	return TailStatus{
		BackendState:   raw.BackendState,
		DNSName:        strings.TrimSuffix(raw.Self.DNSName, "."),
		TailscaleIPs:   raw.Self.TailscaleIPs,
		Health:         raw.Health,
		MagicDNSSuffix: raw.MagicDNSSuffix,
	}, nil
}

// tailnetRecoveryHint 把 Tailscale 状态压成用户可执行的下一步。
// 不回显 DNSName/IP/本机路径，适合直接出现在错误页或复制给支持人员。
func tailnetRecoveryHint(st TailStatus) string {
	switch st.BackendState {
	case "NeedsLogin":
		return "Tailscale 尚未登录；打开 Tailscale 客户端完成登录后重试"
	case "NeedsMachineAuth":
		return "当前设备等待 tailnet 管理员批准；批准后重试"
	}
	health := strings.ToLower(strings.Join(st.Health, " "))
	if st.BackendState == "NoState" || strings.Contains(health, "starting") || strings.Contains(health, "control") {
		return "Tailscale 仍在启动或无法连接控制面；若使用 TUN/系统代理，请让 *.tailscale.com、*.tailscale.io、*.ts.net 直连，再重试；仍失败再运行 scripts\\tailscale-repair.cmd"
	}
	return "Tailscale 未就绪；先打开桌面客户端确认节点为 Running，仍失败再运行 scripts\\tailscale-repair.cmd"
}

// FunnelAlreadyOn 判断 funnel status 输出是否已正确挂载到 127.0.0.1:port。
// 与原 setup.ps1 的 $alreadyOn 同逻辑：本地已有正确挂载就不重跑，避免重启公网 DNS 发布。
func FunnelAlreadyOn(output string, port int) bool {
	return strings.Contains(strings.ToLower(output), "funnel on") &&
		strings.Contains(output, fmt.Sprintf("127.0.0.1:%d", port))
}

// CheckConsent 与原 setup.ps1 第 2 步同语义：公网无 Bearer 必须明确同意。
func CheckConsent(funnel, bearer, tokenOnly bool) error {
	if funnel && !bearer && !tokenOnly {
		return errors.New("拒绝：公网 funnel 且无 Bearer 会发布一个全网凭一条 URL 就能写的端点，" +
			"而 URL 会漏进代理日志、浏览器历史和截图；要继续请显式加 --allow-public-url-token-only，" +
			"否则保持 Bearer 开启（默认）")
	}
	return nil
}

func (o Options) out() io.Writer {
	if o.Out != nil {
		return o.Out
	}
	return os.Stdout
}

func (o Options) report(id, label, state, detail string) {
	if o.Progress != nil {
		o.Progress(id, label, state, detail)
	}
}

// ---- 紧凑步骤行：✓/!/✗ + 8 列对齐的标签 + 值 ----

func stepLine(w io.Writer, mark, label, value string) {
	fmt.Fprintf(w, "%s %s %s\n", mark, padLabel(label, 8), value)
}

func okLine(w io.Writer, label, value string)   { stepLine(w, Green("✓"), label, value) }
func warnLine(w io.Writer, label, value string) { stepLine(w, Yellow("!"), label, value) }

type cmdRes struct {
	out  string
	code int
}

func runCmd(timeout time.Duration, name string, args ...string) (string, int) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	out, err := cmd.CombinedOutput()
	code := 0
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			code = ee.ExitCode()
		} else {
			code = -1
		}
	}
	return string(out), code
}

func runCmdAsync(timeout time.Duration, name string, args ...string) <-chan cmdRes {
	ch := make(chan cmdRes, 1)
	go func() {
		out, code := runCmd(timeout, name, args...)
		ch <- cmdRes{out: out, code: code}
	}()
	return ch
}

func snippet(s string, n int) string {
	s = strings.TrimSpace(s)
	if len(s) > n {
		return s[:n] + "…"
	}
	return s
}

func resolveTailscale(override string) (string, error) {
	if override != "" {
		if _, err := os.Stat(override); err != nil {
			return "", fmt.Errorf("指定的 tailscale 不可读 %s: %w", override, err)
		}
		return override, nil
	}
	if p, err := exec.LookPath("tailscale"); err == nil {
		return p, nil
	}
	if _, err := os.Stat(defaultTailscaleExe); err == nil {
		return defaultTailscaleExe, nil
	}
	return "", errors.New("找不到 tailscale.exe；请先安装 Tailscale 桌面客户端")
}

func resolveIPN(tsExe string) string {
	if dir := filepath.Dir(tsExe); dir != "" && dir != "." {
		if cand := filepath.Join(dir, "tailscale-ipn.exe"); fileExists(cand) {
			return cand
		}
	}
	if fileExists(defaultTailscaleIPN) {
		return defaultTailscaleIPN
	}
	return ""
}

func fileExists(p string) bool {
	fi, err := os.Stat(p)
	return err == nil && !fi.IsDir()
}

func tailStatus(tsExe string) (TailStatus, string, error) {
	out, code := runCmd(15*time.Second, tsExe, "status", "--json")
	if code != 0 {
		return TailStatus{}, out, fmt.Errorf("tailscale status 退出码 %d: %s", code, snippet(out, 300))
	}
	st, err := ParseTailStatus([]byte(out))
	if err != nil {
		return TailStatus{}, out, fmt.Errorf("解析 tailscale status 失败: %v: %s", err, snippet(out, 300))
	}
	return st, out, nil
}

// Preflight 执行公网链的前半段（tailnet→配置→挂载→DNS），返回 Ready 供 main 起 serve。
// DryRun 下只做只读侦察，不写配置、不挂载。慢调用都并行化：status 与挂载状态双发，
// DoH 双解析器双发，整体最坏只比单次 DoH 超时多一点。
func Preflight(o Options) (*Ready, error) {
	AutoColor()
	w := o.out()
	mode := "serve"
	if o.Funnel {
		mode = "funnel"
	}
	title := Bold("novel-mcp") + Gray(" v"+Version+"  一键公网")
	if o.DryRun {
		title += Yellow("（dry-run：只读）")
	}
	setTitle("novel-mcp v" + Version + " 公网服务")
	fmt.Fprintln(w, title)
	o.report("tailnet", "Tailscale", "running", "检查登录与节点状态")

	// ---- tailnet：status 与挂载状态双发 ----
	tsExe, err := resolveTailscale(o.TailscaleExe)
	if err != nil {
		return nil, err
	}
	statusCh := runCmdAsync(15*time.Second, tsExe, "status", "--json")
	mountCh := runCmdAsync(15*time.Second, tsExe, mode, "status")
	sr, mr := <-statusCh, <-mountCh
	st, statusErr := ParseTailStatus([]byte(sr.out))
	if sr.code != 0 || statusErr != nil {
		st = TailStatus{}
		statusErr = errors.New("status 调用失败")
	}
	if statusErr != nil || st.BackendState != "Running" {
		state := st.BackendState
		if statusErr != nil {
			state = "status 调用失败"
		}
		if o.DryRun {
			warnLine(w, "tailnet", fmt.Sprintf("未就绪（%s）；正式跑会尝试拉起托盘进程", state))
			o.report("tailnet", "Tailscale", "warn", "未就绪："+state)
			return dryReady(o, mode, tsExe), nil
		}
		if st.BackendState == "NeedsLogin" || st.BackendState == "NeedsMachineAuth" {
			return nil, errors.New(tailnetRecoveryHint(st))
		}
		stepLine(w, Gray("…"), "tailnet", fmt.Sprintf("未就绪（%s），拉起托盘进程（免 UAC）…", state))
		ipn := resolveIPN(tsExe)
		if ipn == "" {
			return nil, fmt.Errorf("tailnet 未就绪且找不到 tailscale-ipn.exe；请双击 scripts\\tailscale-repair.cmd（会请求管理员权限）修一次再来")
		}
		_ = exec.Command(ipn).Start()
		deadline := time.Now().Add(45 * time.Second)
		for time.Now().Before(deadline) {
			time.Sleep(3 * time.Second)
			if st2, _, err2 := tailStatus(tsExe); err2 == nil && st2.BackendState == "Running" {
				st = st2
				statusErr = nil
				break
			}
		}
		if statusErr != nil || st.BackendState != "Running" {
			return nil, errors.New(tailnetRecoveryHint(st))
		}
		// daemon 刚起来，之前并行的挂载状态已过期，重查一次。
		mr.out, mr.code = runCmd(15*time.Second, tsExe, mode, "status")
	}
	if st.DNSName == "" {
		return nil, errors.New("本节点没有 MagicDNS 名；请到 tailnet 管理后台打开 MagicDNS")
	}
	publicURL := "https://" + st.DNSName
	okLine(w, "tailnet", st.DNSName+" ("+firstIPv4(st.TailscaleIPs)+")")
	o.report("tailnet", "Tailscale", "ok", st.DNSName)

	// ---- 同意检查 ----
	if err := CheckConsent(o.Funnel, o.RequireBearer, o.AllowTokenOnly); err != nil {
		return nil, err
	}

	// ---- 配置 ----
	o.report("config", "连接配置", "running", "准备本机配置")
	cfgPath := filepath.Join(o.DataDir, "config.json")
	if o.DryRun {
		okLine(w, "配置", fmt.Sprintf("会写 %s（本次不写）", cfgPath))
		o.report("config", "连接配置", "ok", "dry-run：未写入")
	} else {
		if err := os.MkdirAll(o.DataDir, 0o700); err != nil {
			return nil, err
		}
		cfg := map[string]any{
			"host":           "127.0.0.1",
			"port":           o.Port,
			"data":           o.DataDir,
			"public_url":     publicURL,
			"allow_origins":  o.AllowOrigins,
			"require_bearer": o.RequireBearer,
			"startup_mode":   "public",
		}
		buf, _ := json.MarshalIndent(cfg, "", "  ")
		if err := os.WriteFile(cfgPath, append(buf, '\n'), 0o600); err != nil {
			return nil, err
		}
		okLine(w, "配置", cfgPath)
		o.report("config", "连接配置", "ok", cfgPath)
	}

	ready := &Ready{
		Mode: mode, DNS: st.DNSName, MagicDNSSuffix: st.MagicDNSSuffix,
		PublicURL: publicURL, TailscaleExe: tsExe,
		Host: "127.0.0.1", Port: o.Port, DataDir: o.DataDir,
		AllowOrigins: o.AllowOrigins, RequireBearer: o.RequireBearer,
		ConfigPath: cfgPath, StartedAt: time.Now(),
	}

	// ---- funnel/serve 挂载 ----
	target := fmt.Sprintf("http://127.0.0.1:%d", o.Port)
	tunnelLabel := "Tailnet Serve"
	if o.Funnel {
		tunnelLabel = "Tailscale Funnel"
	}
	o.report("tunnel", tunnelLabel, "running", "连接到 "+target)
	if o.Funnel && FunnelAlreadyOn(mr.out, o.Port) {
		okLine(w, "funnel", fmt.Sprintf("已挂载 %s，不动", target))
		o.report("tunnel", tunnelLabel, "ok", "已挂载")
	} else if o.DryRun {
		okLine(w, mode, fmt.Sprintf("正式跑会执行 tailscale %s --bg --https=%d %s", mode, o.ServePort, target))
		o.report("tunnel", tunnelLabel, "ok", "dry-run：未修改")
	} else {
		stepLine(w, Gray("…"), mode, "挂载中…")
		res, code := runCmd(60*time.Second, tsExe, mode, "--bg", fmt.Sprintf("--https=%d", o.ServePort), target)
		lower := strings.ToLower(res)
		failed := code != 0
		if o.Funnel {
			failed = failed || strings.Contains(lower, "not enabled") || strings.Contains(lower, "not permitted") ||
				strings.Contains(lower, "denied") || strings.Contains(lower, "failed to")
		}
		if failed {
			warnLine(w, mode, "没干净地生效：先查 HTTPS 证书（管理后台 → DNS），开了等一分钟再跑")
			o.report("tunnel", tunnelLabel, "warn", "挂载结果需要检查")
		} else {
			okLine(w, mode, fmt.Sprintf("已挂载 %s", target))
			o.report("tunnel", tunnelLabel, "ok", "已挂载")
		}
	}

	// ---- 公网 DNS 检查（仅 funnel）----
	if o.Funnel {
		o.report("dns", "公网 DNS", "running", "检查公网解析")
		ips, ok, reachable := publicDNS(st.DNSName)
		if !reachable {
			stepLine(w, Gray("·"), "公网 DNS", "跳过检查（解析器不通，不影响使用）")
			o.report("dns", "公网 DNS", "warn", "解析器不可达，跳过检查")
		} else if ok {
			b := true
			ready.PublicOk = &b
			okLine(w, "公网 DNS", "可解析："+strings.Join(ips, ", "))
			o.report("dns", "公网 DNS", "ok", strings.Join(ips, ", "))
		} else {
			b := false
			ready.PublicOk = &b
			warnLine(w, "公网 DNS", "未发布：等≤10分钟（别重跑）；还 NXDOMAIN 就退出重登 Tailscale")
			o.report("dns", "公网 DNS", "warn", "尚未发布；通常等待几分钟即可")
		}
	} else {
		o.report("dns", "公网 DNS", "ok", "仅 tailnet 模式，无需公网解析")
	}
	return ready, nil
}

func dryReady(o Options, mode, tsExe string) *Ready {
	return &Ready{DryRun: true, Mode: mode, TailscaleExe: tsExe,
		Host: "127.0.0.1", Port: o.Port, DataDir: o.DataDir,
		AllowOrigins: o.AllowOrigins, RequireBearer: o.RequireBearer,
		ConfigPath: filepath.Join(o.DataDir, "config.json"), StartedAt: time.Now()}
}

// dohResult 是一次 DoH 查询的结果。
type dohResult struct {
	ips       []string
	published bool
	reached   bool
}

// parseDoHResponse 解析 application/dns-json 应答。
func parseDoHResponse(data []byte) (ips []string, published bool, err error) {
	var body struct {
		Status int `json:"Status"`
		Answer []struct {
			Data string `json:"data"`
		} `json:"Answer"`
	}
	if err := json.Unmarshal(data, &body); err != nil {
		return nil, false, err
	}
	if body.Status == 0 && len(body.Answer) > 0 {
		for _, a := range body.Answer {
			ips = append(ips, a.Data)
		}
		return ips, true, nil
	}
	return nil, false, nil
}

func queryDoH(client *http.Client, base, dns string) dohResult {
	req, err := http.NewRequest("GET", base+"?name="+dns+"&type=A", nil)
	if err != nil {
		return dohResult{}
	}
	req.Header.Set("Accept", "application/dns-json")
	resp, err := client.Do(req)
	if err != nil {
		return dohResult{}
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return dohResult{}
	}
	ips, published, err := parseDoHResponse(body)
	if err != nil {
		return dohResult{}
	}
	return dohResult{ips: ips, published: published, reached: true}
}

// publicDNS 经公网 DoH 查 A 记录。双解析器并行，任取一个可达的结果
// （查的是同一份公网 DNS，结论一致是常态）。返回（地址列表，可解析，解析器可达）。
func publicDNS(dns string) ([]string, bool, bool) {
	client := &http.Client{Timeout: dohTimeout}
	ch := make(chan dohResult, 2)
	go func() { ch <- queryDoH(client, "https://1.1.1.1/dns-query", dns) }()
	go func() { ch <- queryDoH(client, "https://dns.google/resolve", dns) }()
	for i := 0; i < 2; i++ {
		if r := <-ch; r.reached {
			return r.ips, r.published, true
		}
	}
	return nil, false, false
}

// PrintAccess 打印最终的接入卡片。
func PrintAccess(w io.Writer, r *Ready, route, bearer string) {
	rule := Gray(strings.Repeat("─", 46))
	accessURL := r.PublicURL + "/mcp/" + route
	fmt.Fprintln(w, rule)
	fmt.Fprintf(w, "  %s  %s\n", Gray("ACCESS URL"), Green(accessURL))
	if r.RequireBearer {
		fmt.Fprintf(w, "  %s      %s\n", Gray("BEARER"), Cyan(bearer))
		fmt.Fprintf(w, "  %s    %s\n", Gray("健康检查"), r.PublicURL+"/healthz（浏览器打开验证）")
		fmt.Fprintf(w, "  %s\n", Gray("客户端：URL 填地址栏，Bearer 填 Authorization 请求头"))
	} else {
		fmt.Fprintf(w, "  %s      %s\n", Gray("BEARER"), Yellow("已关闭——这串 URL 本身就是凭据"))
		fmt.Fprintf(w, "  %s    %s\n", Gray("健康检查"), r.PublicURL+"/healthz（浏览器打开验证）")
	}
	fmt.Fprintln(w, rule)
	if r.Mode == "funnel" {
		switch {
		case r.PublicOk != nil && *r.PublicOk:
			fmt.Fprintf(w, "  %s\n", Gray("公网可达，仅 443（URL + Bearer 缺一不可）"))
		case r.PublicOk != nil && !*r.PublicOk:
			fmt.Fprintf(w, "  %s\n", Gray("公网 DNS 未发布（README 第 5 节有清单）"))
		default:
			fmt.Fprintf(w, "  %s\n", Gray("公网 DNS 未检查（解析器不通）"))
		}
		fmt.Fprintf(w, "  %s\n", Gray("撤销暴露：tailscale funnel --https=443 off"))
	} else {
		fmt.Fprintf(w, "  %s\n", Gray("仅 tailnet 可连（"+r.MagicDNSSuffix+"）；去掉 --tailnet-only 即发布公网"))
		fmt.Fprintf(w, "  %s\n", Gray("撤销暴露：tailscale serve --https=443 off"))
	}
	elapsed := ""
	if !r.StartedAt.IsZero() {
		elapsed = fmt.Sprintf("就绪 %.1fs · ", time.Since(r.StartedAt).Seconds())
	}
	if r.RequireBearer {
		fmt.Fprintf(w, "  %s\n", Gray(elapsed+"URL 与 Bearer 是凭据，别贴仓库/截图/聊天记录"))
	} else {
		fmt.Fprintf(w, "  %s\n", Gray(elapsed+"这串 URL 就是全部凭据，别外发"))
	}
	fmt.Fprintf(w, "  %s\n", Gray("停止：按 Ctrl+C，或直接关闭本窗口（挂载保留）"))
}

// bearerTransport 给外发请求加 Authorization 头（与 tailnet-smoke.go 同构）。
type bearerTransport struct {
	token string
	base  http.RoundTripper
}

func (t bearerTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	r = r.Clone(r.Context())
	if t.token != "" {
		r.Header.Set("Authorization", "Bearer "+t.token)
	}
	base := t.base
	if base == nil {
		base = http.DefaultTransport
	}
	return base.RoundTrip(r)
}

// LightSmoke 经公网 URL 做只读冒烟：错路由 404、healthz、MCP 握手、工具清单。
// 失败只返回错误（调用方打印警示但继续服务），不写任何数据。
func LightSmoke(out io.Writer, baseURL, route, bearer string, timeout time.Duration) error {
	if len(route) != 64 {
		return fmt.Errorf("route 长度应为 64，实得 %d", len(route))
	}
	plain := &http.Client{Timeout: timeout}
	// 错路由必须 404
	resp, err := plain.Get(baseURL + "/mcp/" + strings.Repeat("0", 64))
	if err != nil {
		return fmt.Errorf("公网不通：%v", err)
	}
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	code := resp.StatusCode
	resp.Body.Close()
	if code != 404 {
		return fmt.Errorf("错误路由应 404，实得 %d", code)
	}
	fmt.Fprintf(out, "  %s 错误路由 404\n", Green("✓"))
	// healthz
	resp, err = plain.Get(baseURL + "/healthz")
	if err != nil {
		return fmt.Errorf("healthz 不通：%v", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
	resp.Body.Close()
	if resp.StatusCode != 200 || !strings.Contains(string(body), `"ok":true`) {
		return fmt.Errorf("healthz 异常：%d %s", resp.StatusCode, snippet(string(body), 200))
	}
	fmt.Fprintf(out, "  %s healthz %s\n", Green("✓"), strings.TrimSpace(string(body)))
	// MCP 握手 + 工具清单
	client := mcp.NewClient(&mcp.Implementation{Name: "novel-mcp-smoke", Version: "1.0.0"}, nil)
	tr := &mcp.StreamableClientTransport{Endpoint: baseURL + "/mcp/" + route}
	tr.HTTPClient = &http.Client{Transport: bearerTransport{token: bearer}, Timeout: timeout}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	session, err := client.Connect(ctx, tr, nil)
	if err != nil {
		return fmt.Errorf("MCP 握手失败：%v", err)
	}
	defer session.Close()
	fmt.Fprintf(out, "  %s MCP 握手完成（instructions 长度 %d）\n", Green("✓"), len(session.InitializeResult().Instructions))
	ctx2, cancel2 := context.WithTimeout(context.Background(), timeout)
	defer cancel2()
	res, err := session.ListTools(ctx2, nil)
	if err != nil {
		return fmt.Errorf("工具清单失败：%v", err)
	}
	want := []string{"list_projects", "create_project", "novel_guide", "project_status", "verify_project", "export_book",
		"novel_context", "save_book", "save_foundation", "audit_foundation", "plan_chapter",
		"draft_chapter", "read_chapter", "check_consistency", "commit_chapter"}
	seen := map[string]bool{}
	for _, t := range res.Tools {
		seen[t.Name] = true
	}
	var missing []string
	for _, name := range want {
		if !seen[name] {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return fmt.Errorf("缺少工具：%v（共看到 %d 个）", missing, len(res.Tools))
	}
	fmt.Fprintf(out, "  %s 工具清单 %d 个，关键工具齐全\n", Green("✓"), len(res.Tools))
	return nil
}

// SmokeFailureHints 是 smoke 失败时的排查提示（与原 setup.ps1 同序）。
func SmokeFailureHints() []string {
	return []string{
		"1) 端点还没发布（看上面 DNS 检查说什么）",
		"2) 本机 TUN 模式代理（Clash Party / mihomo）拦截了 ts.net 流量",
		"3) tailnet ACL 拒绝本设备，或 tailnet 没开 Funnel",
	}
}

// ConnectionInfo 是已在运行实例的连接信息（双击第二次时展示用）。
type ConnectionInfo struct {
	PublicURL string
	Route     string
	Bearer    string
	BearerOff bool
}

// LoadConnection 从数据目录读凭据与 public_url。读不到就报错，
// 调用方回落到正常启动流程（会在监听时给出端口占用的明确错误）。
func LoadConnection(dataDir string) (ConnectionInfo, error) {
	var info ConnectionInfo
	creds, err := server.LoadCredentials(filepath.Join(dataDir, "credentials.json"))
	if err != nil {
		return info, err
	}
	info.Route, info.Bearer = creds.Route, creds.Bearer
	if buf, err := os.ReadFile(filepath.Join(dataDir, "config.json")); err == nil {
		var cfg struct {
			PublicURL     string `json:"public_url"`
			RequireBearer *bool  `json:"require_bearer"`
			StartupMode   string `json:"startup_mode"`
			Host          string `json:"host"`
			Port          int    `json:"port"`
		}
		if json.Unmarshal(buf, &cfg) == nil {
			info.PublicURL = strings.TrimSuffix(cfg.PublicURL, "/")
			if info.PublicURL == "" && cfg.StartupMode == "local" {
				host := cfg.Host
				if host == "" {
					host = "127.0.0.1"
				}
				port := cfg.Port
				if port == 0 {
					port = 8765
				}
				info.PublicURL = fmt.Sprintf("http://%s:%d", host, port)
			}
			if cfg.RequireBearer != nil && !*cfg.RequireBearer {
				info.BearerOff = true
			}
		}
	}
	return info, nil
}

// ProbeRunning 探测本机 port 上是否已跑着 novel-mcp：能连上且 /healthz
// 自报 service=novel-mcp 才算，避免把别的程序误认成自己。
func ProbeRunning(port int) bool {
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d/healthz", port))
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 4096))
	if err != nil {
		return false
	}
	return resp.StatusCode == 200 && strings.Contains(string(body), `"service":"novel-mcp"`)
}

// PrintRunningCard 打印“已在运行”连接卡：用户双击第二次时看到的不再是报错，
// 而是直接可用的连接信息。
func PrintRunningCard(w io.Writer, info ConnectionInfo) {
	fmt.Fprintln(w, Bold("novel-mcp")+Gray(" v"+Version+"  已在运行"))
	rule := Gray(strings.Repeat("─", 46))
	fmt.Fprintln(w, rule)
	fmt.Fprintf(w, "  %s  %s\n", Gray("ACCESS URL"), Green(info.PublicURL+"/mcp/"+info.Route))
	if info.BearerOff {
		fmt.Fprintf(w, "  %s      %s\n", Gray("BEARER"), Yellow("已关闭——这串 URL 本身就是凭据"))
	} else {
		fmt.Fprintf(w, "  %s      %s\n", Gray("BEARER"), Cyan(info.Bearer))
		fmt.Fprintf(w, "  %s\n", Gray("客户端：URL 填地址栏，Bearer 填 Authorization 请求头"))
	}
	fmt.Fprintf(w, "  %s    %s\n", Gray("健康检查"), info.PublicURL+"/healthz（浏览器打开验证）")
	fmt.Fprintln(w, rule)
	fmt.Fprintf(w, "  %s\n", Gray("服务正在运行（可能没有窗口，看不见它很正常）"))
	fmt.Fprintf(w, "  %s\n", Gray("想重开：PowerShell 跑 Get-Process novel-mcp | Stop-Process，再双击"))
}

// firstIPv4 取第一个 IPv4 用于展示（IPv6 太长且没人抄它）。
func firstIPv4(ips []string) string {
	for _, ip := range ips {
		if !strings.Contains(ip, ":") {
			return ip
		}
	}
	if len(ips) > 0 {
		return ips[0]
	}
	return "无地址"
}
