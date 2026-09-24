//go:build windows

package main

import (
	"encoding/base64"
	"fmt"
	"os/exec"
)

func copyText(text string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(text))
	cmd := exec.Command("powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"Set-Clipboard -Value ([Text.Encoding]::UTF8.GetString([Convert]::FromBase64String($args[0])))", encoded)
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("写入剪贴板失败: %w (%s)", err, string(out))
	}
	return nil
}

func openURL(rawURL string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", rawURL).Start()
}
