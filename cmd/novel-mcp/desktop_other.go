//go:build !windows

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

func copyText(text string) error {
	candidates := [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}}
	var lastErr error
	for _, args := range candidates {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		if err := cmd.Run(); err == nil {
			return nil
		} else {
			lastErr = fmt.Errorf("%s: %w", args[0], err)
		}
	}
	if lastErr != nil {
		return fmt.Errorf("系统剪贴板工具执行失败: %w", lastErr)
	}
	return errors.New("未找到可用的系统剪贴板工具")
}

func openURL(rawURL string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return startDetached(exec.Command(name, rawURL))
}
