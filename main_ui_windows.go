//go:build windows

package main

import (
	"fmt"
	"os"

	"lanrd/internal/ui"
)

// runDefault Windows 下无参数启动图形界面。
func runDefault() {
	if err := ui.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "lanrd:", err)
		os.Exit(1)
	}
}

// runUI 打开图形界面。
func runUI() { runDefault() }
