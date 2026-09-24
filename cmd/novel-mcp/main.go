package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"novel-mcp/internal/public"
	"novel-mcp/internal/server"
	"novel-mcp/internal/store"
	"novel-mcp/internal/tui"
)

type usageError struct{ err error }

func (e *usageError) Error() string { return e.err.Error() }
func (e *usageError) Unwrap() error { return e.err }

type displayedError struct{ err error }

func (e *displayedError) Error() string { return e.err.Error() }
func (e *displayedError) Unwrap() error { return e.err }

const usage = `novel-mcp — 供网页 MCP 调用的小说创作服务（核心复用 ainovel-cli）

  novel-mcp serve        [--config <file>] [--host <ip>] [--port <n>] [--data <dir>]
                         [--public-url <https origin>] [--allow-origin <origin>]...
                         [--bearer=<true|false>] [--insecure-allow-open]
  novel-mcp local        [--config <file>] [--port <n>] [--data <dir>] [--no-tui]
  novel-mcp url          [--config <file>] [--public-url <https origin>]
  novel-mcp token rotate --i-understand-this-invalidates-current-clients [--config <file>]
  novel-mcp version
  novel-mcp help            显示本帮助
  novel-mcp prompt          打印连接 MCP 的初始提示词（配置+URL+Bearer+验证步骤）
  novel-mcp public       [同 serve 的 flag] [--tailnet-only] [--serve-port <n>]
                         [--allow-public-url-token-only] [--smoke] [--dry-run] [--no-tui]
  （无参数、直接双击 novel-mcp.exe：首次选择“公网”或“仅本机”，之后记住选择）

默认只监听 127.0.0.1:8765，数据在 ~/.novel-mcp。只有 /mcp/<route> 与 /healthz 存在，
没有控制台、没有管理 API、没有 shell 与任意文件工具。
`

type Config struct {
	Host          string   `json:"host"`
	Port          int      `json:"port"`
	Data          string   `json:"data"`
	PublicURL     string   `json:"public_url"`
	AllowOrigins  []string `json:"allow_origins"`
	RequireBearer *bool    `json:"require_bearer"`
	StartupMode   string   `json:"startup_mode,omitempty"`
}

func defaultDataDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".novel-mcp")
	}
	return filepath.Join(".novel-mcp")
}

func defaults() Config {
	require := true
	return Config{Host: "127.0.0.1", Port: 8765, Data: defaultDataDir(), RequireBearer: &require}
}

// validate 把“公网暴露”做成需要三件齐备才能启动的配置：HTTPS 源、Bearer、
// 明确的跨源白名单。少一件就只能回到回环地址。
func (c Config) validate(allowOpen bool) error {
	ip := net.ParseIP(c.Host)
	if ip == nil {
		return errors.New("host 必须是 IP 字面量，例如 127.0.0.1 或 0.0.0.0")
	}
	if c.Port < 1 || c.Port > 65535 {
		return errors.New("port 必须在 1-65535")
	}
	if c.Data == "" {
		return errors.New("data 不能为空")
	}
	if c.PublicURL != "" {
		if _, err := server.ValidPublicURL(c.PublicURL); err != nil {
			return err
		}
	}
	for _, o := range c.AllowOrigins {
		if !strings.HasPrefix(o, "http://") && !strings.HasPrefix(o, "https://") {
			return fmt.Errorf("allow_origin 必须是带协议的源: %s", o)
		}
	}
	bearer := c.RequireBearer == nil || *c.RequireBearer
	if !ip.IsLoopback() {
		if !bearer {
			return errors.New("非回环地址必须启用 Bearer 认证")
		}
		if c.PublicURL == "" {
			return errors.New("非回环地址必须给出 --public-url，否则 Host 白名单只能靠本机地址")
		}
	}
	if ip.IsUnspecified() && !allowOpen {
		return errors.New("拒绝绑定所有网卡；确认防火墙与 TLS 终结都已处理好后加 --insecure-allow-open")
	}
	return nil
}

func loadConfig(path string) (Config, error) {
	c := defaults()
	if path == "" {
		return c, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return c, err
	}
	dec := json.NewDecoder(strings.NewReader(string(data)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&c); err != nil {
		return c, fmt.Errorf("config %s: %w", path, err)
	}
	return c, nil
}

func rememberedStartupMode(dataDir string) (string, bool) {
	data, err := os.ReadFile(filepath.Join(dataDir, "config.json"))
	if err != nil {
		return "", false
	}
	var cfg struct {
		StartupMode string `json:"startup_mode"`
	}
	if json.Unmarshal(data, &cfg) != nil {
		return "", false
	}
	switch cfg.StartupMode {
	case string(tui.StartupPublic), string(tui.StartupLocal):
		return cfg.StartupMode, true
	default:
		return "", false
	}
}

func saveStartupConfig(c Config) error {
	if err := os.MkdirAll(c.Data, 0o700); err != nil {
		return err
	}
	buf, err := json.MarshalIndent(c, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(c.Data, "config.json"), append(buf, '\n'), 0o600)
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		var shown *displayedError
		if !errors.As(err, &shown) {
			fmt.Fprintln(os.Stderr, "错误:", err)
			var use *usageError
			if errors.As(err, &use) {
				fmt.Fprint(os.Stderr, usage)
			}
			pauseIfInteractive()
		}
		os.Exit(1)
	}
}

// pauseIfInteractive 双击启动时控制台随进程退出立即关闭，不暂停错误一闪而过。
// 只在交互式终端下等回车（ainovel-cli 同款逻辑），管道/CI/服务里直接退出。
func pauseIfInteractive() {
	fi, err := os.Stdin.Stat()
	if err != nil {
		return
	}
	if fi.Mode()&os.ModeCharDevice == 0 {
		return
	}
	fmt.Fprint(os.Stderr, "\n按回车键退出...")
	fmt.Fscanln(os.Stdin)
}

func run(argv []string) error {
	if len(argv) == 0 {
		dataDir := defaultDataDir()
		mode, ok := rememberedStartupMode(dataDir)
		if !ok && tui.Wanted(false, os.Stdout, os.Stdin) {
			chosen, err := tui.ChooseStartupMode(tui.StartupChoiceOptions{})
			if err != nil {
				return err
			}
			if chosen == "" {
				return nil
			}
			mode = string(chosen)
		}
		cmd := "public"
		if mode == string(tui.StartupLocal) {
			cmd = "local"
		}
		argv = []string{cmd}
		if ok {
			argv = append(argv, "--config", filepath.Join(dataDir, "config.json"))
		}
	}
	if argv[0] == "help" || argv[0] == "-h" || argv[0] == "--help" {
		fmt.Println("novel-mcp v" + public.Version)
		fmt.Print(usage)
		return nil
	}
	cmd, args := argv[0], argv[1:]
	// Go flag 在遇到第一个位置参数后停止解析。token 的公开用法是
	// `token rotate --flag`，因此把 rotate 这个固定子命令挪到末尾再交给 FlagSet，
	// 同时继续兼容 `token --flag rotate`。否则帮助里写的安全轮换命令实际上不可用。
	if cmd == "token" && len(args) > 0 && args[0] == "rotate" {
		args = append(append([]string(nil), args[1:]...), "rotate")
	}
	fs := flag.NewFlagSet(cmd, flag.ContinueOnError)
	// 所有 CLI 错误统一由 main 输出，避免 flag 包先打印一遍 usage、main 再重复一遍。
	fs.SetOutput(io.Discard)
	fs.Usage = func() { fmt.Fprint(fs.Output(), usage) }
	configPath := fs.String("config", "", "配置文件（JSON）")
	host := fs.String("host", "", "监听 IP 字面量")
	port := fs.Int("port", 0, "监听端口")
	data := fs.String("data", "", "数据目录")
	publicURL := fs.String("public-url", "", "公网 HTTPS 源，例如 https://xxx.ngrok.app")
	bearer := fs.Bool("bearer", true, "是否要求 Authorization: Bearer；关掉它就是把整条 URL 当凭据")
	allowOpen := fs.Bool("insecure-allow-open", false, "允许绑定 0.0.0.0/::")
	confirm := fs.Bool("i-understand-this-invalidates-current-clients", false, "轮换令牌并确认现有客户端连接全部失效")
	tailnetOnly := fs.Bool("tailnet-only", false, "public：只做 tailscale serve（tailnet 内），不发布公网")
	servePort := fs.Int("serve-port", 443, "public：funnel/serve 的 --https 端口")
	tokenOnly := fs.Bool("allow-public-url-token-only", false, "public：明确同意发布无 Bearer 的公网端点（URL 即凭据）")
	smoke := fs.Bool("smoke", false, "public：经公网 URL 跑轻量冒烟（只读：404/healthz/握手/工具清单）")
	dryRun := fs.Bool("dry-run", false, "public：只读侦察，不写配置、不挂载、不起服务")
	noTUI := fs.Bool("no-tui", false, "public/local：不用 TUI，只打印静态连接卡")
	var origins stringList
	fs.Var(&origins, "allow-origin", "允许跨源访问的网页源，可重复")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Print(usage)
			return nil
		}
		return &usageError{err: err}
	}
	if fs.NArg() > 0 && cmd == "token" {
		if fs.Arg(0) != "rotate" {
			return &usageError{err: errors.New("用法: novel-mcp token rotate --i-understand-this-invalidates-current-clients")}
		}
		if !*confirm {
			return &usageError{err: errors.New("轮换会让所有在用的 MCP URL 立刻失效；显式加 --i-understand-this-invalidates-current-clients")}
		}
	} else if fs.NArg() > 0 {
		return &usageError{err: fmt.Errorf("多余的参数: %s", fs.Arg(0))}
	}

	c, err := loadConfig(*configPath)
	if err != nil {
		return err
	}
	if *host != "" {
		c.Host = *host
	}
	if *port != 0 {
		c.Port = *port
	}
	if *data != "" {
		c.Data = *data
	}
	if *publicURL != "" {
		c.PublicURL = *publicURL
	}
	if len(origins) > 0 {
		c.AllowOrigins = origins
	}
	// 显式给了 --bearer 就以它为准；否则保留配置文件的值；两处都没写时默认开启。
	if flagSet(fs, "bearer") {
		c.RequireBearer = bearer
	}
	if c.RequireBearer == nil {
		c.RequireBearer = ptr(true)
	}
	if err := c.validate(*allowOpen); err != nil {
		return err
	}
	credsFile := filepath.Join(c.Data, "credentials.json")

	switch cmd {
	case "prompt":
		return printPrompt(c.Data)
	case "version":
		fmt.Println("novel-mcp v" + public.Version)
		return nil
	case "url":
		if info, loadErr := public.LoadConnection(c.Data); loadErr == nil && info.PublicURL != "" {
			fmt.Println(info.PublicURL + "/mcp/" + info.Route)
			if info.BearerOff {
				fmt.Fprintln(os.Stderr, "注意: Bearer 已关闭，这串 URL 本身就是凭据，别贴进仓库、截图或聊天记录。")
			}
			return nil
		}
		existing, err := server.LoadCredentials(credsFile)
		if err != nil {
			return fmt.Errorf("还没有可用的连接凭据；先双击启动，或运行 novel-mcp local / public: %w", err)
		}
		fmt.Println(originOf(c) + "/mcp/" + existing.Route)
		if !*c.RequireBearer {
			fmt.Fprintln(os.Stderr, "注意: Bearer 已关闭，这串 URL 本身就是凭据，别贴进仓库、截图或聊天记录。")
		}
		return nil
	case "token":
		_, err := server.WriteCredentials(credsFile, true)
		if err != nil {
			return err
		}
		fmt.Println("已轮换。旧 URL 立刻失效；重启 serve 后，只把新 URL 交给仍需要访问的客户端。")
		return nil
	case "serve":
		srv, ln, addr, creds, err := prepareServe(c, nil)
		if err != nil {
			return err
		}
		fmt.Printf("novel-mcp 已监听 %s\n  数据目录 %s\n  接入地址 %s/mcp/%s\n", addr, c.Data, originOf(c), creds.Route)
		if *c.RequireBearer {
			fmt.Printf("  认证 Bearer %s\n", creds.Bearer)
		} else {
			fmt.Println("  认证 已关闭——这串 URL 本身就是凭据")
		}
		fmt.Println("  （上面是连接凭据，别贴进仓库、截图或聊天记录。）")
		fmt.Println("  停止：按 Ctrl+C，或直接关闭本窗口")
		return serveLoop(srv, ln, nil)
	case "local":
		c.Host = "127.0.0.1"
		c.PublicURL = ""
		c.AllowOrigins = nil
		c.RequireBearer = ptr(true)
		c.StartupMode = string(tui.StartupLocal)
		if public.ProbeRunning(c.Port) {
			if info, err := public.LoadConnection(c.Data); err == nil {
				public.PrintRunningCard(os.Stdout, info)
				pauseIfInteractive()
				return nil
			}
		}
		obs := server.NewObserver()
		var srv *http.Server
		var ln net.Listener
		var creds server.Credentials
		interactive := tui.Wanted(*noTUI, os.Stdout, os.Stdin)
		if interactive {
			startErr := tui.RunStartupTask(tui.StartupTaskOptions{
				Title: "启动 · 仅本机",
				Steps: []tui.StartupStep{
					{ID: "config", Label: "连接配置"},
					{ID: "service", Label: "MCP 服务"},
				},
				Task: func(report func(tui.StartupStep)) error {
					report(tui.StartupStep{ID: "config", Label: "连接配置", State: tui.StartupRunning, Detail: "保存启动模式"})
					if err := saveStartupConfig(c); err != nil {
						return err
					}
					report(tui.StartupStep{ID: "config", Label: "连接配置", State: tui.StartupOK, Detail: filepath.Join(c.Data, "config.json")})
					report(tui.StartupStep{ID: "service", Label: "MCP 服务", State: tui.StartupRunning, Detail: "监听 127.0.0.1"})
					var err error
					srv, ln, _, creds, err = prepareServe(c, obs)
					if err != nil {
						return err
					}
					report(tui.StartupStep{ID: "service", Label: "MCP 服务", State: tui.StartupOK, Detail: fmt.Sprintf("127.0.0.1:%d", c.Port)})
					return nil
				},
			})
			if startErr != nil {
				return &displayedError{err: startErr}
			}
		} else {
			if err := saveStartupConfig(c); err != nil {
				return err
			}
			var err error
			srv, ln, _, creds, err = prepareServe(c, obs)
			if err != nil {
				return err
			}
		}
		origin := originOf(c)
		if interactive {
			return runServiceTUI(srv, ln, c.Data, tui.Connection{
				Version: public.Version, AccessURL: origin + "/mcp/" + creds.Route,
				Bearer: creds.Bearer, HealthURL: origin + "/healthz",
				Note: "仅本机可访问 · 不使用 Tailscale · 不暴露公网", DataDir: c.Data,
			}, obs, nil)
		}
		fmt.Printf("novel-mcp 本机模式已就绪\n  接入地址 %s/mcp/%s\n  Bearer %s\n  停止：按 Ctrl+C\n", origin, creds.Route, creds.Bearer)
		return serveLoop(srv, ln, nil)
	case "public":
		// 双击第二次：已有实例在跑就直接展示连接卡，不走启动流程。
		if !*dryRun && public.ProbeRunning(c.Port) {
			if info, err := public.LoadConnection(c.Data); err == nil && info.PublicURL != "" {
				public.PrintRunningCard(os.Stdout, info)
				pauseIfInteractive()
				return nil
			}
		}
		allow := c.AllowOrigins
		if len(allow) == 0 {
			allow = public.DefaultAllowOrigins
		}
		interactive := tui.Wanted(*noTUI, os.Stdout, os.Stdin) && !*dryRun
		if interactive {
			var ready *public.Ready
			var srv *http.Server
			var ln net.Listener
			var creds server.Credentials
			obs := server.NewObserver()
			tunnelLabel := "Tailscale Funnel"
			if *tailnetOnly {
				tunnelLabel = "Tailnet Serve"
			}
			startErr := tui.RunStartupTask(tui.StartupTaskOptions{
				Title: "启动 · 网页 / 云端客户端",
				Steps: []tui.StartupStep{
					{ID: "tailnet", Label: "Tailscale"},
					{ID: "config", Label: "连接配置"},
					{ID: "tunnel", Label: tunnelLabel},
					{ID: "dns", Label: "公网 DNS"},
					{ID: "service", Label: "MCP 服务"},
				},
				Task: func(report func(tui.StartupStep)) error {
					progress := func(id, label, state, detail string) {
						report(tui.StartupStep{ID: id, Label: label, State: tui.StartupStepState(state), Detail: detail})
					}
					var err error
					ready, err = public.Preflight(public.Options{
						Port: c.Port, ServePort: *servePort, DataDir: c.Data,
						RequireBearer: *c.RequireBearer, Funnel: !*tailnetOnly,
						AllowTokenOnly: *tokenOnly, AllowOrigins: allow,
						Smoke: *smoke, DryRun: false, Out: io.Discard, Progress: progress,
					})
					if err != nil {
						return err
					}
					pc := c
					pc.PublicURL = ready.PublicURL
					pc.AllowOrigins = ready.AllowOrigins
					pc.StartupMode = string(tui.StartupPublic)
					if err := pc.validate(*allowOpen); err != nil {
						return err
					}
					report(tui.StartupStep{ID: "service", Label: "MCP 服务", State: tui.StartupRunning, Detail: "启动本机服务"})
					srv, ln, _, creds, err = prepareServe(pc, obs)
					if err != nil {
						return err
					}
					report(tui.StartupStep{ID: "service", Label: "MCP 服务", State: tui.StartupOK, Detail: fmt.Sprintf("127.0.0.1:%d", c.Port)})
					return nil
				},
			})
			if startErr != nil {
				return &displayedError{err: startErr}
			}
			return runPublicTUI(srv, ln, c.Data, ready, creds, obs, *smoke)
		}

		ready, err := public.Preflight(public.Options{
			Port: c.Port, ServePort: *servePort, DataDir: c.Data,
			RequireBearer: *c.RequireBearer, Funnel: !*tailnetOnly,
			AllowTokenOnly: *tokenOnly, AllowOrigins: allow,
			Smoke: *smoke, DryRun: *dryRun,
		})
		if err != nil {
			return err
		}
		if *dryRun {
			fmt.Println("dry-run 结束：上面是只读侦察的结果，没改任何东西。")
			return nil
		}
		pc := c
		pc.PublicURL = ready.PublicURL
		pc.AllowOrigins = ready.AllowOrigins
		pc.StartupMode = string(tui.StartupPublic)
		if err := pc.validate(*allowOpen); err != nil {
			return err
		}
		obs := server.NewObserver()
		srv, ln, _, creds, err := prepareServe(pc, obs)
		if err != nil {
			return err
		}
		public.PrintAccess(os.Stdout, ready, creds.Route, creds.Bearer)
		var after func()
		if *smoke {
			after = func() {
				fmt.Println("[5] 轻量冒烟（经公网 URL，只读）...")
				if err := public.LightSmoke(os.Stdout, ready.PublicURL, creds.Route, creds.Bearer, 60*time.Second); err != nil {
					fmt.Printf("  smoke 未通过（%v），但服务继续跑。排查（按顺序）：\n", err)
					for _, h := range public.SmokeFailureHints() {
						fmt.Println("  " + h)
					}
				} else {
					fmt.Println("  smoke 通过")
				}
			}
		}
		return serveLoop(srv, ln, after)
	default:
		return &usageError{err: fmt.Errorf("未知命令 %q", cmd)}
	}
}

// prepareServe 完成 serve 的全部准备：数据目录、凭据、projects、监听、handler。
// serve 与 public 共用，行为一致。obs 传 nil 表示不观测（plain serve 用法）。
func prepareServe(c Config, obs *server.Observer) (srv *http.Server, ln net.Listener, addr string, creds server.Credentials, err error) {
	credsFile := filepath.Join(c.Data, "credentials.json")
	if err = os.MkdirAll(c.Data, 0o700); err != nil {
		return nil, nil, "", server.Credentials{}, err
	}
	if creds, err = server.WriteCredentials(credsFile, false); err != nil {
		return nil, nil, "", server.Credentials{}, err
	}
	projects, err := server.NewProjects(filepath.Join(c.Data, "projects"))
	if err != nil {
		return nil, nil, "", server.Credentials{}, err
	}
	addr = net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
	handler := server.NewHTTP(projects, server.HTTPOptions{
		Credentials:   func() (server.Credentials, error) { return server.LoadCredentials(credsFile) },
		Hosts:         hostAllowlist(c, addr),
		Origins:       c.AllowOrigins,
		RequireBearer: *c.RequireBearer,
		Observer:      obs,
	})
	if ln, err = net.Listen("tcp", addr); err != nil {
		return nil, nil, "", server.Credentials{},
			fmt.Errorf("监听 %s 失败（可能已有实例在跑：Get-Process novel-mcp | Stop-Process 先停掉它）: %w", addr, err)
	}
	srv = &http.Server{Addr: addr, Handler: handler, ReadHeaderTimeout: 10 * time.Second, ReadTimeout: time.Minute, WriteTimeout: 2 * time.Minute, IdleTimeout: 2 * time.Minute}
	return srv, ln, addr, creds, nil
}

// serveLoop 起 Serve 协程，先跑 afterStart（public 的 smoke），
// 再阻塞到 Ctrl+C/退出信号做优雅停止。
func serveLoop(srv *http.Server, ln net.Listener, afterStart func()) error {
	return serveLoopStop(srv, ln, afterStart, nil)
}

// serveLoopStop 多一个 stop 通道：TUI 模式 q 退出时走它停服务。
// stop 传 nil 时行为与 serveLoop 完全一致。
func serveLoopStop(srv *http.Server, ln net.Listener, afterStart func(), stop <-chan struct{}) error {
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	if afterStart != nil {
		afterStart()
	}
	shutdown := func() error {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		if err := srv.Shutdown(ctx); err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("优雅停止超时（可能有长调用未返回）: %w", err)
		}
		return nil
	}
	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()
	select {
	case err := <-errc:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	case <-ctx.Done():
		if err := shutdown(); err != nil {
			return err
		}
		fmt.Println("已停止")
		return nil
	case <-stop:
		return shutdown()
	}
}

// runPublicTUI 是双击场景：服务跑后台协程，前台进 TUI。
// q 退出→停服务；服务崩溃→TUI 跟着退并返回服务错误。
func runPublicTUI(srv *http.Server, ln net.Listener, dataDir string, ready *public.Ready, creds server.Credentials, obs *server.Observer, smoke bool) error {
	var afterStart func()
	if smoke {
		afterStart = func() {
			fmt.Println("[5] 轻量冒烟（经公网 URL，只读）...")
			if err := public.LightSmoke(os.Stdout, ready.PublicURL, creds.Route, creds.Bearer, 60*time.Second); err != nil {
				fmt.Printf("  smoke 未通过（%v），但服务继续跑。排查（按顺序）：\n", err)
				for _, h := range public.SmokeFailureHints() {
					fmt.Println("  " + h)
				}
			} else {
				fmt.Println("  smoke 通过")
			}
		}
	}
	return runServiceTUI(srv, ln, dataDir, tui.Connection{
		Version:   public.Version,
		AccessURL: ready.PublicURL + "/mcp/" + creds.Route,
		Bearer:    creds.Bearer,
		BearerOff: !ready.RequireBearer,
		HealthURL: ready.PublicURL + "/healthz",
		Note:      accessNote(ready),
		DataDir:   dataDir,
	}, obs, afterStart)
}

func runServiceTUI(srv *http.Server, ln net.Listener, dataDir string, conn tui.Connection, obs *server.Observer, afterStart func()) error {
	start := time.Now()
	var mu sync.Mutex
	var rows []tui.ProjectRow
	var rowsErr string
	var rowsAt time.Time
	proj, projErr := server.NewProjects(filepath.Join(dataDir, "projects"))
	snap := func() tui.Snapshot {
		calls, successes, failures := obs.Stats.Snapshot()
		mu.Lock()
		if projErr != nil {
			rowsErr = projErr.Error()
		} else if time.Since(rowsAt) > 5*time.Second {
			if list, err := proj.List(); err != nil {
				rows, rowsErr = nil, err.Error()
			} else {
				rows, rowsErr = nil, ""
				for _, p := range list {
					row := tui.ProjectRow{ID: p.ID, Brief: p.Brief, Style: p.Style, CreatedAt: p.CreatedAt, Chapters: countChapters(dataDir, p.ID)}
					st := store.NewStore(filepath.Join(dataDir, "projects", p.ID))
					if progress, err := st.Progress.Load(); err == nil && progress != nil {
						row.Phase = string(progress.Phase)
						row.Flow = string(progress.Flow)
						row.CurrentChapter = progress.CurrentChapter
						row.TotalChapters = progress.TotalChapters
						row.PendingRewrites = len(progress.PendingRewrites)
					}
					rows = append(rows, row)
				}
			}
			rowsAt = time.Now()
		}
		out, outErr := rows, rowsErr
		mu.Unlock()
		return tui.Snapshot{At: time.Now(), Uptime: time.Since(start), Conn: conn,
			Calls: calls, Successes: successes, Failures: failures,
			Events: obs.Log.Recent(60), Projects: out, ProjectsErr: outErr}
	}
	stop := make(chan struct{})
	tuiStop := make(chan struct{})
	errc := make(chan error, 1)
	go func() { errc <- serveLoopStop(srv, ln, nil, stop) }()
	if afterStart != nil {
		afterStart()
	}
	tuiErr := make(chan error, 1)
	go func() {
		tuiErr <- tui.Run(tui.Options{Snapshot: snap, Stop: tuiStop, Copy: copyText, OpenURL: openURL})
	}()
	select {
	case err := <-errc:
		close(tuiStop)
		<-tuiErr
		return err
	case err := <-tuiErr:
		close(tuiStop)
		close(stop)
		serr := <-errc
		if err != nil {
			return err
		}
		return serr
	}
}

// accessNote 是 PrintAccess 里那行环境备注的 TUI 版，两处文案保持一致。
func accessNote(r *public.Ready) string {
	if r.Mode == "funnel" {
		switch {
		case r.PublicOk != nil && *r.PublicOk:
			return "公网可达，仅 443（URL + Bearer 缺一不可）"
		case r.PublicOk != nil && !*r.PublicOk:
			return "公网 DNS 未发布（README 第 5 节有清单）"
		default:
			return "公网 DNS 未检查（解析器不通）"
		}
	}
	return "仅 tailnet 可连（" + r.MagicDNSSuffix + "）；去掉 --tailnet-only 即发布公网"
}

// printPrompt 打印“连接 MCP 的初始提示词”：连接信息 + 可粘贴的客户端配置片段 +
// 一段话的初始提示词。对标 open-bridge 的 prompt 命令：复制即用，不用再翻窗口抄。
func printPrompt(dataDir string) error {
	info, err := public.LoadConnection(dataDir)
	if err != nil {
		return fmt.Errorf("还没有可用的连接凭据；先双击启动，或运行 novel-mcp local / public: %w", err)
	}
	if info.PublicURL == "" {
		return errors.New("还没有可用的连接地址；请先启动服务")
	}
	access := info.PublicURL + "/mcp/" + info.Route
	health := info.PublicURL + "/healthz"
	fmt.Println("MCP 连接信息（填进 AI 客户端的 MCP 服务器配置，类型选 Streamable HTTP）：")
	fmt.Println()
	fmt.Println("  URL: " + access)
	if info.BearerOff {
		fmt.Println("  Bearer 已关闭——这串 URL 本身就是凭据，别外发")
	} else {
		fmt.Println("  Authorization: Bearer " + info.Bearer)
	}
	fmt.Println("  健康检查: " + health + "（浏览器打开验证，应返回 ok）")
	fmt.Println()
	fmt.Println("配置片段（Cherry Studio / Claude Desktop 兼容，照抄即可）：")
	fmt.Println(`  {`)
	fmt.Println(`    "mcpServers": {`)
	fmt.Println(`      "novel-mcp": {`)
	fmt.Println(`        "type": "streamable-http",`)
	fmt.Println(`        "url": "` + access + `",`)
	if info.BearerOff {
		fmt.Println(`        "headers": {}`)
		fmt.Println("  （Bearer 已关闭：片段里不带 headers）")
	} else {
		fmt.Println(`        "headers": { "Authorization": "Bearer ` + info.Bearer + `" }`)
	}
	fmt.Println(`      }`)
	fmt.Println(`    }`)
	fmt.Println(`  }`)
	fmt.Println()
	fmt.Println("初始提示词（一段话，贴进 AI）：")
	fmt.Println("  " + server.ConnectionPrompt(access, info.Bearer, info.BearerOff, health))
	return nil
}

// countChapters 数 chapters/*.md，失败返回 -1（视图里不显示章节数）。
func countChapters(dataDir, id string) int {
	entries, err := os.ReadDir(filepath.Join(dataDir, "projects", id, "chapters"))
	if err != nil {
		return -1
	}
	n := 0
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".md") {
			n++
		}
	}
	return n
}

func originOf(c Config) string {
	if c.PublicURL != "" {
		return strings.TrimSuffix(c.PublicURL, "/")
	}
	return "http://" + net.JoinHostPort(c.Host, strconv.Itoa(c.Port))
}

// hostAllowlist 覆盖本机访问的常见写法与隧道源。只列精确 host:port：
// 通配 Host 会让 DNS 重绑定直接过关。
func hostAllowlist(c Config, addr string) []string {
	hosts := []string{addr, net.JoinHostPort("localhost", strconv.Itoa(c.Port)), net.JoinHostPort("127.0.0.1", strconv.Itoa(c.Port)), net.JoinHostPort("[::1]", strconv.Itoa(c.Port))}
	if c.PublicURL != "" {
		if u, err := url.Parse(c.PublicURL); err == nil {
			hosts = append(hosts, u.Host)
		}
	}
	return hosts
}

type stringList []string

func (s *stringList) String() string { return strings.Join(*s, ",") }
func (s *stringList) Set(v string) error {
	*s = append(*s, strings.TrimSuffix(v, "/"))
	return nil
}

func ptr[T any](v T) *T { return &v }

// flagSet 判断某个 flag 是否被显式写过：FlagSet.Visit 只遍历被设置的项。
func flagSet(fs *flag.FlagSet, name string) bool {
	set := false
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}
