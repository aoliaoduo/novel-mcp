package public

import (
	"os"
	"strings"
)

// colorEnabled 由 AutoColor 在 Preflight 入口处判定一次。
var colorEnabled bool

// AutoColor 判定彩色输出开关：NO_COLOR 关，FORCE_COLOR 开，
// 否则只在 stdout 是终端且 TERM 不是 dumb 时开。Windows 下会先尝试
// 打开控制台的 VT 支持（legacy conhost 需要，Windows Terminal 无害）。
func AutoColor() {
	if off(os.Getenv("NO_COLOR")) {
		colorEnabled = false
		return
	}
	if v := strings.ToLower(strings.TrimSpace(os.Getenv("FORCE_COLOR"))); v == "1" || v == "true" {
		enableVT()
		colorEnabled = true
		return
	}
	if strings.ToLower(strings.TrimSpace(os.Getenv("TERM"))) == "dumb" {
		colorEnabled = false
		return
	}
	colorEnabled = isCharDevice(os.Stdout)
	if colorEnabled {
		enableVT()
	}
}

func off(v string) bool {
	v = strings.ToLower(strings.TrimSpace(v))
	return v != "" && v != "0" && v != "false"
}

func isCharDevice(f *os.File) bool {
	fi, err := f.Stat()
	if err != nil {
		return false
	}
	return fi.Mode()&os.ModeCharDevice != 0
}

const (
	cReset  = "\x1b[0m"
	cBold   = "\x1b[1m"
	cRed    = "\x1b[31m"
	cGreen  = "\x1b[32m"
	cYellow = "\x1b[33m"
	cCyan   = "\x1b[36m"
	cGray   = "\x1b[90m"
)

func paint(code, s string) string {
	if !colorEnabled {
		return s
	}
	return code + s + cReset
}

// Bold/Red/Green/Yellow/Cyan/Gray 按当前开关着色；关闭时原样返回。
func Bold(s string) string   { return paint(cBold, s) }
func Red(s string) string    { return paint(cRed, s) }
func Green(s string) string  { return paint(cGreen, s) }
func Yellow(s string) string { return paint(cYellow, s) }
func Cyan(s string) string   { return paint(cCyan, s) }
func Gray(s string) string   { return paint(cGray, s) }

// padLabel 按显示宽度（CJK 算 2）把标签垫到 w 列。
func padLabel(s string, w int) string {
	n := 0
	for _, r := range s {
		if r > 0xFF {
			n += 2
		} else {
			n++
		}
	}
	for n < w {
		s += " "
		n++
	}
	return s
}
