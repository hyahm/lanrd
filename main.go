// lanrd 是一个内网 Windows 远程桌面工具：
//
//	lanrd agent              在被控端启动服务(需在已登录的桌面会话中运行)
//	lanrd client <ip:port>   在控制端连接远端桌面
//
// 双方(远端机器前的用户与控制端)可同时抢占键盘鼠标操作，
// 控制端 F9 可锁定远端物理键鼠，F11 切换全屏。
package main

import (
	"flag"
	"fmt"
	"os"
	"strings"

	"lanrd/internal/client"
)

// version 工具版本。
const version = "0.1.0"

func main() {
	if len(os.Args) < 2 {
		runDefault()
		return
	}
	switch os.Args[1] {
	case "agent":
		runAgent(os.Args[2:])
	case "client":
		runClient(os.Args[2:])
	case "ui":
		runUI()
	case "version", "-v", "--version":
		fmt.Println("lanrd", version)
	default:
		usage()
		os.Exit(2)
	}
}

// usage 打印用法。
func usage() {
	fmt.Fprintf(os.Stderr, `lanrd %s - 内网 Windows 远程桌面

用法:
  lanrd                 打开图形界面(Windows)
  lanrd agent  [-config configs/config.yaml]   被控端
  lanrd client <ip:port> [-token xxx]          控制端
  lanrd version                                版本

热键(控制端): F9 锁定/解锁远端物理键鼠  F11 全屏
`, version)
}

// runClient 启动控制端。兼容 "client <addr> -token x" 与 "client -token x <addr>" 两种参数顺序。
func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	token := fs.String("token", "", "连接口令")
	flags, pos := splitArgs(args, "-token")
	_ = fs.Parse(flags)
	if len(pos) != 1 {
		usage()
		os.Exit(2)
	}
	if err := client.Run(pos[0], *token); err != nil {
		fmt.Fprintln(os.Stderr, "lanrd:", err)
		os.Exit(1)
	}
}

// splitArgs 把带值 flag 与位置参数分开，返回(仅flag参数, 位置参数)。
func splitArgs(args []string, valueFlags ...string) ([]string, []string) {
	var flags, pos []string
	takesValue := func(a string) bool {
		for _, f := range valueFlags {
			if a == f || a == "--"+f {
				return true
			}
		}
		return false
	}
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case takesValue(a):
			flags = append(flags, a)
			if i+1 < len(args) {
				i++
				flags = append(flags, args[i])
			}
		case strings.HasPrefix(a, "-"):
			flags = append(flags, a)
		default:
			pos = append(pos, a)
		}
	}
	return flags, pos
}
