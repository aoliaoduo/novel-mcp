//go:build windows

package main

import (
	"encoding/binary"
	"fmt"
	"os/exec"

	"golang.org/x/sys/windows"
)

const (
	cfUnicodeText = 13
	gmemMoveable  = 0x0002
	gmemZeroInit  = 0x0040
)

var (
	user32             = windows.NewLazySystemDLL("user32.dll")
	kernel32           = windows.NewLazySystemDLL("kernel32.dll")
	procOpenClipboard  = user32.NewProc("OpenClipboard")
	procCloseClipboard = user32.NewProc("CloseClipboard")
	procEmptyClipboard = user32.NewProc("EmptyClipboard")
	procSetClipboard   = user32.NewProc("SetClipboardData")
	procGlobalAlloc    = kernel32.NewProc("GlobalAlloc")
	procGlobalLock     = kernel32.NewProc("GlobalLock")
	procGlobalUnlock   = kernel32.NewProc("GlobalUnlock")
	procGlobalFree     = kernel32.NewProc("GlobalFree")
)

func copyText(text string) error {
	utf16, err := windows.UTF16FromString(text)
	if err != nil {
		return fmt.Errorf("剪贴板文本包含无效 NUL 字符: %w", err)
	}
	payload := make([]byte, len(utf16)*2)
	for i, unit := range utf16 {
		binary.LittleEndian.PutUint16(payload[i*2:], unit)
	}

	if ok, _, callErr := procOpenClipboard.Call(0); ok == 0 {
		return fmt.Errorf("打开剪贴板失败: %w", callErr)
	}
	defer procCloseClipboard.Call()

	if ok, _, callErr := procEmptyClipboard.Call(); ok == 0 {
		return fmt.Errorf("清空剪贴板失败: %w", callErr)
	}

	hMem, _, callErr := procGlobalAlloc.Call(gmemMoveable|gmemZeroInit, uintptr(len(payload)))
	if hMem == 0 {
		return fmt.Errorf("分配剪贴板内存失败: %w", callErr)
	}
	owned := true
	defer func() {
		if owned {
			procGlobalFree.Call(hMem)
		}
	}()

	ptr, _, callErr := procGlobalLock.Call(hMem)
	if ptr == 0 {
		return fmt.Errorf("锁定剪贴板内存失败: %w", callErr)
	}
	if err := windows.WriteProcessMemory(windows.CurrentProcess(), ptr, &payload[0], uintptr(len(payload)), nil); err != nil {
		procGlobalUnlock.Call(hMem)
		return fmt.Errorf("写入剪贴板内存失败: %w", err)
	}
	procGlobalUnlock.Call(hMem)

	if result, _, callErr := procSetClipboard.Call(cfUnicodeText, hMem); result == 0 {
		return fmt.Errorf("写入剪贴板失败: %w", callErr)
	}
	owned = false // SetClipboardData 成功后，内存所有权交给系统。
	return nil
}

func openURL(rawURL string) error {
	return exec.Command("rundll32.exe", "url.dll,FileProtocolHandler", rawURL).Start()
}
