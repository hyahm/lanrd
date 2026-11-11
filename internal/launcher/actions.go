//go:build windows

package launcher

import (
	"context"
	"fmt"
	"log/slog"
	"net"
	"os/exec"
	"strconv"
	"strings"
	"sync"
	"time"

	"lanrd/internal/agent"
	"lanrd/internal/config"
)

// statusStore 收集日志/状态并显示到状态标签(保留最近若干行)。
type statusStore struct {
	mu    sync.Mutex
	hwnd  uintptr
	lines []string
}

// Handle 实现 slog.Handler，把日志追加到状态标签。
func (s *statusStore) Handle(_ context.Context, r slog.Record) error {
	var b strings.Builder
	fmt.Fprintf(&b, "%s %s", r.Time.Format("15:04:05"), r.Message)
	r.Attrs(func(a slog.Attr) bool {
		fmt.Fprintf(&b, " %s=%v", a.Key, a.Value)
		return true
	})
	s.append(b.String())
	return nil
}

// Enabled 始终启用。
func (s *statusStore) Enabled(context.Context, slog.Level) bool { return true }

// WithAttrs 返回自身(简化)。
func (s *statusStore) WithAttrs([]slog.Attr) slog.Handler { return s }

// WithGroup 返回自身(简化)。
func (s *statusStore) WithGroup(string) slog.Handler { return s }

// logf 追加一条自定义状态文本。
func (s *statusStore) logf(format string, args ...any) {
	s.append(time.Now().Format("15:04:05 ") + fmt.Sprintf(format, args...))
}

// append 追加一行并刷新控件文本。
func (s *statusStore) append(line string) {
	s.mu.Lock()
	s.lines = append(s.lines, line)
	if len(s.lines) > 5 {
		s.lines = s.lines[len(s.lines)-5:]
	}
	text := strings.Join(s.lines, "\r\n")
	s.mu.Unlock()
	setWindowText(s.hwnd, text)
}

// startServer 启动/停止被控端(按钮切换)。进程内运行，停止后可再次启动。
func startServer() {
	if g.serverOn {
		stopServer()
		return
	}
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		g.status.logf("配置错误: %v", err)
		return
	}
	log := slog.New(g.status)
	a, err := agent.New(cfg, log)
	if err != nil {
		g.status.logf("被控端启动失败: %v", err)
		return
	}
	g.srv = a
	g.serverOn = true
	g.status.logf("被控端启动中 %s", cfg.Server.Addr)
	setWindowText(g.hServerBtn, "停止服务端")
	go func() {
		if err := a.Run(); err != nil {
			log.Error("被控端退出", "err", err)
		}
	}()
}

// stopServer 停止被控端并恢复按钮(断开监听与在线控制端)。
func stopServer() {
	if g.srv != nil {
		g.srv.Stop()
		g.srv = nil
	}
	g.serverOn = false
	setWindowText(g.hServerBtn, "启动服务端(被控)")
}

// startClient 校验输入并拉起控制端子进程。禁止连接本机：
// 本机运行着服务端时，客户端连自己会形成"无限自嵌套画面"的死循环。
func startClient() {
	ip := windowText(g.hIP)
	port := windowText(g.hPort)
	token := windowText(g.hToken)
	if ip == "" {
		g.status.logf("请填写被控端 IP")
		return
	}
	if isLocalTarget(ip) {
		g.status.logf("已阻止: %s 是本机地址，不能连接自己(画面无限嵌套死循环)", ip)
		return
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		g.status.logf("端口无效: %q", port)
		return
	}
	addr := net.JoinHostPort(ip, port)
	exe, err := executablePath()
	if err != nil {
		g.status.logf("获取程序路径失败: %v", err)
		return
	}
	cmd := exec.Command(exe, "client", addr, "-token", token)
	cmd.SysProcAttr = hideConsole()
	if err := cmd.Start(); err != nil {
		g.status.logf("控制端启动失败: %v", err)
		return
	}
	g.status.logf("控制端已启动 → %s", addr)
	go func() { _ = cmd.Wait() }()
}

// isLocalTarget 判断目标是否本机地址: localhost、环回地址或本机任一网卡 IP。
func isLocalTarget(host string) bool {
	host = strings.TrimSpace(host)
	if host == "" {
		return false
	}
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false // 主机名不判断，视为远端
	}
	if ip.IsLoopback() {
		return true
	}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return false
	}
	for _, a := range addrs {
		if n, ok := a.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}
