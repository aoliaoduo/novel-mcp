//go:build !windows

package public

func enableVT() {}

// setTitle 在非 Windows 上是空操作。
func setTitle(string) {}
