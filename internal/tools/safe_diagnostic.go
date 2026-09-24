package tools

import (
	"fmt"
	"strings"
)

func diagnosticWarning(projectDir, scope string, err error) string {
	msg := fmt.Sprintf("%s 读取失败: %v", scope, err)
	if projectDir != "" {
		msg = strings.ReplaceAll(msg, projectDir, "<project>")
	}
	return strings.ToValidUTF8(msg, "\uFFFD")
}
