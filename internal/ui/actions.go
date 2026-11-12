//go:build windows

package ui

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

// statusLines 状态区保留的最大行数。
const statusLines = 6

// statusStore 收集日志/状态并缓存最近若干行(线程安全，Draw 时取快照)。
type statusStore struct {
	mu    sync.Mutex
	lines []string
}

// Handle 实现 slog.Handler，把日志追加到状态区。
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

// append 追加一行并截断。
func (s *statusStore) append(line string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.lines = append(s.lines, line)
	if len(s.lines) > statusLines {
		s.lines = s.lines[len(s.lines)-statusLines:]
	}
}

// snapshot 返回当前状态行副本。
func (s *statusStore) snapshot() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]string, len(s.lines))
	copy(out, s.lines)
	return out
}

// startServer 启动/停止被控端(按钮切换)。进程内运行，停止后可再次启动。
func (a *app) startServer() {
	if a.serverOn {
		a.stopServer()
		return
	}
	cfg, err := config.Load(config.DefaultPath())
	if err != nil {
		a.status.logf("配置错误: %v", err)
		return
	}
	log := slog.New(a.status)
	srv, err := agent.New(cfg, log)
	if err != nil {
		a.status.logf("被控端启动失败: %v", err)
		return
	}
	a.srv = srv
	a.serverOn = true
	a.srvBtn.label = "■ 停止服务端(被控)"
	a.srvBtn.accent = colRed
	a.status.logf("被控端启动中 %s", cfg.Server.Addr)
	go func() {
		if err := srv.Run(); err != nil {
			log.Error("被控端退出", "err", err)
		}
	}()
}

// stopServer 停止被控端并恢复按钮(断开监听与在线控制端)。
func (a *app) stopServer() {
	if a.srv != nil {
		a.srv.Stop()
		a.srv = nil
	}
	a.serverOn = false
	a.srvBtn.label = "▶ 启动服务端(被控)"
	a.srvBtn.accent = colGreen
}

// startClient 校验输入并拉起控制端子进程。禁止连接本机：
// 本机运行着服务端时，客户端连自己会形成"无限自嵌套画面"的死循环。
func (a *app) startClient() {
	ip, port, token := a.ipIn.text, a.portIn.text, a.tokenIn.text
	if ip == "" {
		a.status.logf("请填写被控端 IP")
		return
	}
	if isLocalTarget(ip) {
		a.status.logf("已阻止: %s 是本机地址，不能连接自己(画面无限嵌套死循环)", ip)
		return
	}
	n, err := strconv.Atoi(port)
	if err != nil || n < 1 || n > 65535 {
		a.status.logf("端口无效: %q", port)
		return
	}
	addr := net.JoinHostPort(ip, port)
	if err := a.saveUIConfig(); err != nil {
		a.status.logf("保存配置失败: %v", err)
	}
	exe, err := executablePath()
	if err != nil {
		a.status.logf("获取程序路径失败: %v", err)
		return
	}
	cmd := exec.Command(exe, "client", addr, "-token", token)
	cmd.SysProcAttr = hideConsole()
	if err := cmd.Start(); err != nil {
		a.status.logf("控制端启动失败: %v", err)
		return
	}
	a.status.logf("控制端已启动 → %s", addr)
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
	for _, ad := range addrs {
		if n, ok := ad.(*net.IPNet); ok && n.IP.Equal(ip) {
			return true
		}
	}
	return false
}

// toggleAutoStart 复选框切换开机自启(写/删 HKCU Run 注册表)；
// 开启时若端口映射正在启用，则映射也一并开机自启。
func (a *app) toggleAutoStart() {
	on := a.autoChk.checked
	if err := setAutoStart(on); err != nil {
		a.status.logf("设置自启失败: %v", err)
		return
	}
	if on {
		a.syncFwdAutoStart()
		a.status.logf("已开启开机自启(下次登录生效)")
	} else {
		_ = removeFwdAutoStart()
		a.status.logf("已关闭开机自启")
	}
}
