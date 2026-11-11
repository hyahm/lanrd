//go:build !windows

package main

import (
	"fmt"
	"os"
)

// runAgent 被控端仅支持 Windows。
func runAgent(args []string) {
	fmt.Fprintln(os.Stderr, "lanrd: agent(被控端)仅支持 Windows，当前系统可用 client 连接 Windows 被控端")
	os.Exit(1)
}
