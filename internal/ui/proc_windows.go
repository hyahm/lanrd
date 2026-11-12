//go:build windows

package ui

import (
	"os"
	"syscall"
)

// hideConsole 让子进程(控制端/映射)不弹出额外控制台窗口。
func hideConsole() *syscall.SysProcAttr {
	return &syscall.SysProcAttr{HideWindow: true}
}

// executablePath 返回当前可执行文件路径。
func executablePath() (string, error) { return os.Executable() }
