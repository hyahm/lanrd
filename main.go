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
	"strconv"
	"strings"

	"lanrd/internal/client"
	"lanrd/internal/proxy"
)

// version 工具版本。
const version = "0.2.0"

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
	case "fwd":
		runFwd(os.Args[2:])
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
  lanrd fwd [-fwd ...]                            无界面直连端口映射
  lanrd fwd <ip:port> [-token xxx] -fwd ...       经被控端转发(需在线)
  lanrd version                                版本

热键(控制端): F9 锁定/解锁远端物理键鼠  F11 全屏
-fwd   静态端口映射(可重复)，如 -fwd tcp:0.0.0.0:6000=192.168.1.61:3000
       无被控端地址时本机直接连接目标；带地址时目标由被控端内网代连
`, version)
}

// fwdFlags 可重复的 -fwd 转发规则参数。
type fwdFlags []proxy.ForwardRule

// String 实现 flag.Value。
func (f *fwdFlags) String() string {
	parts := make([]string, len(*f))
	for i, r := range *f {
		parts[i] = r.String()
	}
	return strings.Join(parts, ", ")
}

// Set 解析并追加一条规则。
func (f *fwdFlags) Set(s string) error {
	r, err := proxy.ParseForward(s)
	if err != nil {
		return err
	}
	*f = append(*f, r)
	return nil
}

// runClient 启动控制端。兼容 "client <addr> -token x" 与 "client -token x <addr>" 两种参数顺序。
func runClient(args []string) {
	fs := flag.NewFlagSet("client", flag.ExitOnError)
	token := fs.String("token", "", "连接口令")
	var fwds fwdFlags
	fs.Var(&fwds, "fwd", "静态端口映射，可重复: tcp:127.0.0.1:33890=192.168.1.8:3389 或 udp:5353=192.168.1.1:53")
	flags, pos := splitArgs(args, "-token", "-fwd")
	_ = fs.Parse(flags)
	if len(pos) != 1 {
		usage()
		os.Exit(2)
	}
	if err := client.Run(client.Options{
		Addr: pos[0], Token: *token, Forwards: fwds,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "lanrd:", err)
		os.Exit(1)
	}
}

// fwdPIDPath 端口映射子进程 PID 文件(启动器据此停止子进程)。
const fwdPIDPath = "configs/fwd.pid"

// runFwd 启动无界面端口映射(写 PID 文件供启动器停止，须至少一条规则)。
// 无位置参数时为直连模式: 本机直接访问规则目标，不经被控端；
// 带被控端地址时经控制连接由被控端代为转发。
func runFwd(args []string) {
	fs := flag.NewFlagSet("fwd", flag.ExitOnError)
	token := fs.String("token", "", "连接口令")
	var fwds fwdFlags
	fs.Var(&fwds, "fwd", "静态端口映射，可重复: tcp:127.0.0.1:33890=192.168.1.8:3389 或 udp:5353=192.168.1.1:53")
	flags, pos := splitArgs(args, "-token", "-fwd")
	_ = fs.Parse(flags)
	if len(fwds) == 0 || len(pos) > 1 {
		usage()
		os.Exit(2)
	}
	writeFwdPID()
	defer clearFwdPID()
	if len(pos) == 0 { // 直连模式
		if err := proxy.RunDirect(fwds); err != nil {
			fmt.Fprintln(os.Stderr, "lanrd:", err)
			os.Exit(1)
		}
		return
	}
	if err := client.Run(client.Options{
		Addr: pos[0], Token: *token, Forwards: fwds, Headless: true,
	}); err != nil {
		fmt.Fprintln(os.Stderr, "lanrd:", err)
		os.Exit(1)
	}
}

// writeFwdPID 写入当前进程 PID。
func writeFwdPID() {
	_ = os.MkdirAll("configs", 0o755)
	_ = os.WriteFile(fwdPIDPath, []byte(strconv.Itoa(os.Getpid())), 0o644)
}

// clearFwdPID 退出时删除 PID 文件(仅当内容仍是自己的 PID，避免误删接替者)。
func clearFwdPID() {
	b, err := os.ReadFile(fwdPIDPath)
	if err != nil {
		return
	}
	if strings.TrimSpace(string(b)) == strconv.Itoa(os.Getpid()) {
		_ = os.Remove(fwdPIDPath)
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
