package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"novel-mcp/internal/public"
	"novel-mcp/internal/server"
)

type doctorCheck struct {
	Name   string `json:"name"`
	Status string `json:"status"`
	Detail string `json:"detail"`
	Hint   string `json:"hint,omitempty"`
}

type doctorReport struct {
	Version     string        `json:"version"`
	StartupMode string        `json:"startup_mode,omitempty"`
	Deep        bool          `json:"deep"`
	OK          bool          `json:"ok"`
	Checks      []doctorCheck `json:"checks"`
}

func (r *doctorReport) add(name, status, detail, hint string) {
	r.Checks = append(r.Checks, doctorCheck{Name: name, Status: status, Detail: detail, Hint: hint})
	if status == "fail" {
		r.OK = false
	}
}

// buildDoctorReport 是只读诊断。输出设计为可直接贴给 AI / Issue：
// 不包含数据目录绝对路径、route、Bearer、公网 hostname、小说正文或项目 ID。
func buildDoctorReport(c Config, configPresent, deep bool, configLoadErr ...error) doctorReport {
	r := doctorReport{Version: public.Version, StartupMode: c.StartupMode, Deep: deep, OK: true, Checks: []doctorCheck{}}

	if len(configLoadErr) > 0 && configLoadErr[0] != nil {
		r.add("config", "fail", "配置文件无法读取或解析（路径与原始内容已隐藏）", "检查 config.json 的 JSON 语法和字段名；修正后重新运行 doctor")
	} else if configPresent {
		r.add("config", "ok", "已读取连接配置", "")
	} else {
		r.add("config", "info", "未找到持久化配置；首次运行时这是正常状态", "先运行 novel-mcp local 或 novel-mcp public 完成首次配置")
	}
	if len(configLoadErr) > 0 && configLoadErr[0] != nil {
		r.add("config-safety", "warn", "无法基于损坏配置执行安全组合校验", "先修正配置文件")
	} else if err := c.validate(false); err != nil {
		r.add("config-safety", "fail", "配置安全校验失败："+sanitizeDoctorText(err.Error(), c.Data), "修正配置后再启动服务")
	} else {
		r.add("config-safety", "ok", "监听、认证与公网配置组合有效", "")
	}

	info, err := os.Stat(c.Data)
	switch {
	case os.IsNotExist(err):
		r.add("data", "info", "数据目录尚未初始化", "首次启动会自动创建")
	case err != nil:
		r.add("data", "fail", "数据目录不可访问："+sanitizeDoctorText(err.Error(), c.Data), "检查目录权限")
	case !info.IsDir():
		r.add("data", "fail", "配置的数据位置不是目录", "修正 data 配置")
	default:
		r.add("data", "ok", "数据目录可访问", "")
	}

	credsPath := filepath.Join(c.Data, "credentials.json")
	if _, err := server.LoadCredentials(credsPath); err != nil {
		if os.IsNotExist(err) {
			r.add("credentials", "warn", "尚未生成连接凭据", "启动 local/public/serve 后会自动生成")
		} else {
			r.add("credentials", "fail", "连接凭据不可用（值已隐藏）："+sanitizeDoctorText(err.Error(), c.Data), "不要手工修补 token；必要时使用 token rotate 重新生成")
		}
	} else {
		r.add("credentials", "ok", "route 与 Bearer 格式有效（值已隐藏）", "")
	}

	projectsDir := filepath.Join(c.Data, "projects")
	projectInfo, statErr := os.Stat(projectsDir)
	if os.IsNotExist(statErr) {
		r.add("projects", "info", "尚无项目目录", "创建第一本小说后会出现")
	} else if statErr != nil {
		r.add("projects", "fail", "项目目录不可访问："+sanitizeDoctorText(statErr.Error(), c.Data), "检查目录权限")
	} else if !projectInfo.IsDir() {
		r.add("projects", "fail", "projects 位置不是目录", "不要把普通文件放在 projects 位置")
	} else if projects, newErr := server.NewProjects(projectsDir); newErr != nil {
		r.add("projects", "fail", "项目根不可用："+sanitizeDoctorText(newErr.Error(), c.Data), "检查目录权限与符号链接")
	} else if items, listErr := projects.List(); listErr != nil {
		r.add("projects", "fail", "项目列表读取失败："+sanitizeDoctorText(listErr.Error(), c.Data), "运行 doctor --deep 或人工检查损坏项目")
	} else {
		r.add("projects", "ok", fmt.Sprintf("发现 %d 个可读取项目", len(items)), "")
		if deep {
			failed, warnings := 0, 0
			for _, item := range items {
				out, callErr := projects.Call(context.Background(), item.ID, "verify_project", "", nil)
				if callErr != nil {
					failed++
					continue
				}
				result, _ := out["result"].(map[string]any)
				if result["ok"] != true {
					failed++
				}
				warnings += doctorInt(result["warnings"])
			}
			if failed > 0 {
				r.add("project-integrity", "fail", fmt.Sprintf("完整核验 %d 个项目，其中 %d 个存在错误；未输出项目名或正文", len(items), failed), "在本机对可疑项目调用 MCP verify_project 查看具体 issue")
			} else {
				r.add("project-integrity", "ok", fmt.Sprintf("完整核验 %d 个项目通过；共 %d 条 warning", len(items), warnings), "")
			}
		}
	}

	if public.ProbeRunning(c.Port) {
		r.add("service", "ok", "本机 healthz 可达，服务正在运行", "")
	} else {
		r.add("service", "info", "当前端口没有检测到运行中的 novel-mcp", "需要使用时启动 local 或 public")
	}
	if c.StartupMode == "public" {
		r.add("public-network", "info", "当前配置为公网模式；doctor 不主动访问外网或打印公网地址", "网络/Tailscale 故障请运行 novel-mcp public --dry-run")
	}

	return r
}

func runDoctor(c Config, configPresent, deep, jsonOutput bool, w io.Writer, configLoadErr ...error) error {
	report := buildDoctorReport(c, configPresent, deep, configLoadErr...)
	if jsonOutput {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		if err := enc.Encode(report); err != nil {
			return err
		}
	} else {
		fmt.Fprintf(w, "novel-mcp v%s doctor\n", report.Version)
		fmt.Fprintln(w, "诊断输出已脱敏：不包含 route、Bearer、公网 hostname、项目 ID、小说正文或本机绝对路径。")
		for _, check := range report.Checks {
			mark := "·"
			switch check.Status {
			case "ok":
				mark = "✓"
			case "warn":
				mark = "!"
			case "fail":
				mark = "✗"
			}
			fmt.Fprintf(w, "%s %-18s %s\n", mark, check.Name, check.Detail)
			if check.Hint != "" && check.Status != "ok" {
				fmt.Fprintf(w, "  建议: %s\n", check.Hint)
			}
		}
		if report.OK {
			fmt.Fprintln(w, "结果: 未发现阻断性问题。")
		} else {
			fmt.Fprintln(w, "结果: 发现需要处理的问题。")
		}
	}
	if !report.OK {
		return &displayedError{err: fmt.Errorf("doctor found problems")}
	}
	return nil
}

func sanitizeDoctorText(s, dataDir string) string {
	if dataDir != "" {
		s = strings.ReplaceAll(s, dataDir, "<data>")
	}
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		s = strings.ReplaceAll(s, home, "<home>")
	}
	return strings.ToValidUTF8(s, "\uFFFD")
}

func doctorInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case float64:
		return int(n)
	default:
		return 0
	}
}
