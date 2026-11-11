//go:build !windows

package main

import (
	"fmt"
	"os"
)

// runDefault 非 Windows 平台无参数时打印用法。
func runDefault() {
	usage()
	os.Exit(2)
}

// runUI 图形界面仅支持 Windows。
func runUI() {
	fmt.Fprintln(os.Stderr, "lanrd: 图形界面仅支持 Windows")
	os.Exit(1)
}
