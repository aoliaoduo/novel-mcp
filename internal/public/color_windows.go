//go:build windows

package public

import (
	"os"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// enableVT 打开当前控制台的虚拟终端处理，让 legacy conhost 也能解释 ANSI。
// Windows Terminal 本来就支持，这里是无害的重复设置。失败就当没这回事。
func enableVT() {
	for _, f := range []*os.File{os.Stdout, os.Stderr} {
		if !isCharDevice(f) {
			continue
		}
		h := windows.Handle(f.Fd())
		var mode uint32
		if err := windows.GetConsoleMode(h, &mode); err != nil {
			continue
		}
		_ = windows.SetConsoleMode(h, mode|windows.ENABLE_VIRTUAL_TERMINAL_PROCESSING)
	}
}

// setTitle 设置控制台窗口标题（失败就忽略，不影响主流程）。
func setTitle(title string) {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	proc := kernel32.NewProc("SetConsoleTitleW")
	p, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		return
	}
	proc.Call(uintptr(unsafe.Pointer(p)))
}
