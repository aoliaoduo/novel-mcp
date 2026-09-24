package main

import (
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"time"

	"novel-mcp/internal/public"
	"novel-mcp/internal/server"
	"novel-mcp/internal/tui"
)

func runLocalCommand(c Config, noTUI bool) error {
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
	interactive := tui.Wanted(noTUI, os.Stdout, os.Stdin)
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
}

type publicCommandOptions struct {
	allowOpen   bool
	tailnetOnly bool
	servePort   int
	tokenOnly   bool
	smoke       bool
	dryRun      bool
	noTUI       bool
}

func (o publicCommandOptions) preflight(c Config, allow []string) public.Options {
	return public.Options{
		Port: c.Port, ServePort: o.servePort, DataDir: c.Data,
		RequireBearer: *c.RequireBearer, Funnel: !o.tailnetOnly,
		AllowTokenOnly: o.tokenOnly, AllowOrigins: allow,
		Smoke: o.smoke, DryRun: o.dryRun,
	}
}

func runPublicCommand(c Config, opts publicCommandOptions) error {
	// 双击第二次：已有实例在跑就直接展示连接卡，不走启动流程。
	if !opts.dryRun && public.ProbeRunning(c.Port) {
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
	interactive := tui.Wanted(opts.noTUI, os.Stdout, os.Stdin) && !opts.dryRun
	if interactive {
		return runPublicInteractive(c, allow, opts)
	}

	ready, err := public.Preflight(opts.preflight(c, allow))
	if err != nil {
		return err
	}
	if opts.dryRun {
		fmt.Println("dry-run 结束：上面是只读侦察的结果，没改任何东西。")
		return nil
	}
	pc := publicConfig(c, ready)
	if err := pc.validate(opts.allowOpen); err != nil {
		return err
	}
	obs := server.NewObserver()
	srv, ln, _, creds, err := prepareServe(pc, obs)
	if err != nil {
		return err
	}
	public.PrintAccess(os.Stdout, ready, creds.Route, creds.Bearer)
	return serveLoop(srv, ln, publicSmoke(ready, creds, opts.smoke))
}

func runPublicInteractive(c Config, allow []string, opts publicCommandOptions) error {
	var ready *public.Ready
	var srv *http.Server
	var ln net.Listener
	var creds server.Credentials
	obs := server.NewObserver()
	tunnelLabel := "Tailscale Funnel"
	if opts.tailnetOnly {
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
			preflight := opts.preflight(c, allow)
			preflight.DryRun = false
			preflight.Out = io.Discard
			preflight.Progress = progress
			var err error
			ready, err = public.Preflight(preflight)
			if err != nil {
				return err
			}
			pc := publicConfig(c, ready)
			if err := pc.validate(opts.allowOpen); err != nil {
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
	return runPublicTUI(srv, ln, c.Data, ready, creds, obs, opts.smoke)
}

func publicConfig(c Config, ready *public.Ready) Config {
	c.PublicURL = ready.PublicURL
	c.AllowOrigins = ready.AllowOrigins
	c.StartupMode = string(tui.StartupPublic)
	return c
}

func publicSmoke(ready *public.Ready, creds server.Credentials, enabled bool) func() {
	if !enabled {
		return nil
	}
	return func() {
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
