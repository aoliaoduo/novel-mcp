//go:build !windows

package main

import (
	"errors"
	"os/exec"
	"runtime"
	"strings"
)

func copyText(text string) error {
	candidates := [][]string{{"pbcopy"}, {"wl-copy"}, {"xclip", "-selection", "clipboard"}}
	for _, args := range candidates {
		if _, err := exec.LookPath(args[0]); err != nil {
			continue
		}
		cmd := exec.Command(args[0], args[1:]...)
		cmd.Stdin = strings.NewReader(text)
		return cmd.Run()
	}
	return errors.New("未找到可用的系统剪贴板工具")
}

func openURL(rawURL string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, rawURL).Start()
}
