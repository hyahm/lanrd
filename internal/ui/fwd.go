//go:build windows

package ui

import (
	"fmt"
	"os"
	"os/exec"
	"strconv"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"

	"lanrd/internal/proxy"
)

// registryOpen 打开 HKCU Run 键(write 为真时请求写权限)。
func registryOpen(write bool) (registry.Key, error) {
	access := uint32(registry.QUERY_VALUE)
	if write {
		access = registry.SET_VALUE
	}
	return registry.OpenKey(registry.CURRENT_USER, runKeyPath, access)
}

// errNotExist 注册表值不存在错误。
var errNotExist = registry.ErrNotExist

// fwdPIDPath 端口映射子进程 PID 文件(与 main.go fwd 子命令一致)。
const fwdPIDPath = "configs/fwd.pid"

// stillActive GetExitCodeProcess 的"仍在运行"退出码(STILL_ACTIVE)。
const stillActive = 259

// setFwdStatus 更新「内网代理」页状态行(线程安全)。
func (a *app) setFwdStatus(format string, args ...any) {
	a.mu.Lock()
	a.fwdStatus = fmt.Sprintf(format, args...)
	a.mu.Unlock()
}

// enabledMaps 返回列表中已勾选(启用)的映射，保持列表顺序。
func (a *app) enabledMaps() []uiMap {
	var out []uiMap
	for _, m := range a.maps {
		if m.Enabled {
			out = append(out, m)
		}
	}
	return out
}

// validateMaps 校验已启用映射: 规则合法且本地端口不重复。
func validateMaps(maps []uiMap) error {
	seen := make(map[int]bool, len(maps))
	for _, m := range maps {
		if _, err := proxy.ParseForward(m.mapArg()); err != nil {
			return fmt.Errorf("端口 %d: %w", m.Local, err)
		}
		if seen[m.Local] {
			return fmt.Errorf("本地端口 %d 重复", m.Local)
		}
		seen[m.Local] = true
	}
	return nil
}

// mapsSummary 生成 "6000→192.168.1.61:3000" 逗号连接的摘要。
func mapsSummary(maps []uiMap) string {
	parts := make([]string, len(maps))
	for i, m := range maps {
		parts[i] = strconv.Itoa(m.Local) + "→" + m.Remote
	}
	return strings.Join(parts, ", ")
}

// toggleFwd 「内网代理」页开关: 未启用则后台启动无界面映射，已启用则停止。
func (a *app) toggleFwd() {
	if a.fwdOn {
		a.stopFwd()
		return
	}
	a.startFwd()
}

// startFwd 拉起无界面直连端口映射子进程(fwd 子命令)，携带列表中全部
// 已勾选的映射规则(每条一个 -fwd 参数，本机直接连接目标，不经被控端)；
// 勾选开机自启时同步写入映射自启项。
func (a *app) startFwd() {
	maps := a.enabledMaps()
	if len(maps) == 0 {
		a.setFwdStatus("请先在列表中勾选至少一条映射")
		return
	}
	if err := validateMaps(maps); err != nil {
		a.setFwdStatus("映射无效: %v", err)
		return
	}
	if err := a.saveUIConfig(); err != nil {
		a.setFwdStatus("保存配置失败: %v", err)
	}
	exe, err := executablePath()
	if err != nil {
		a.setFwdStatus("获取程序路径失败: %v", err)
		return
	}
	args := []string{"fwd"}
	for _, m := range maps {
		args = append(args, "-fwd", m.mapArg())
	}
	cmd := exec.Command(exe, args...)
	cmd.SysProcAttr = hideConsole()
	if err := cmd.Start(); err != nil {
		a.setFwdStatus("启动失败: %v", err)
		return
	}
	a.fwdCmd, a.fwdPID, a.fwdOn = cmd, cmd.Process.Pid, true
	a.fwdBtn.label = "■ 停止端口映射"
	a.fwdBtn.accent = colRed
	a.setFwdStatus("已启动 %d 条映射(直连): %s", len(maps), mapsSummary(maps))
	a.syncFwdAutoStart()
	go func() {
		_ = cmd.Wait()
		a.mu.Lock()
		a.pendingFwdExit = cmd.Process.Pid
		a.mu.Unlock()
	}()
}

// stopFwd 停止端口映射: 优先结束自己拉起的子进程(经退出标志收尾)，
// 否则按 PID 文件结束自启拉起的进程。
func (a *app) stopFwd() {
	if a.fwdCmd != nil {
		_ = a.fwdCmd.Process.Kill()
		_ = removeFwdAutoStart()
		return
	}
	if pid := a.fwdPID; pidAlive(pid) {
		if p, err := os.FindProcess(pid); err == nil {
			_ = p.Kill()
		}
	}
	a.fwdExited(a.fwdPID)
}

// restartFwd 映射运行中重启子进程以应用最新规则: 杀掉旧进程并等待其
// 退出(释放监听端口)后原位拉起新进程；迟到的旧退出通知按 PID 忽略。
func (a *app) restartFwd() {
	if !a.fwdOn {
		return
	}
	old := a.fwdPID
	if a.fwdCmd != nil {
		_ = a.fwdCmd.Process.Kill()
	} else if p, err := os.FindProcess(old); err == nil {
		_ = p.Kill()
	}
	for i := 0; i < 30 && pidAlive(old); i++ { // 最多等 1.5s 释放端口
		time.Sleep(50 * time.Millisecond)
	}
	a.fwdOn = false
	a.startFwd()
}

// fwdExited 子进程退出收尾(须在 UI 线程): 匹配当前 PID 时复位开关、
// 按钮与状态行并移除自启项；迟到的旧进程通知直接忽略。
func (a *app) fwdExited(pid int) {
	if pid != a.fwdPID {
		return
	}
	a.fwdCmd, a.fwdPID, a.fwdOn = nil, 0, false
	a.fwdBtn.label = "▶ 启动端口映射"
	a.fwdBtn.accent = colCyan
	a.fwdBtn.bold = true
	a.setFwdStatus("已停止(连接断开或被停止)")
	_ = removeFwdAutoStart()
}

// pollFwdExit 取出跨线程上报的子进程退出事件并在 UI 线程收尾。
func (a *app) pollFwdExit() {
	a.mu.Lock()
	pid := a.pendingFwdExit
	a.pendingFwdExit = 0
	a.mu.Unlock()
	if pid != 0 {
		a.fwdExited(pid)
	}
}

// healthCheckFwd 校验已启用映射的子进程是否存活(不存活则收尾)。
func (a *app) healthCheckFwd() {
	if a.fwdOn && !pidAlive(a.fwdPID) && a.fwdCmd == nil {
		a.fwdExited(a.fwdPID)
	}
}

// reconcileFwd 启动时恢复映射开关: 自启项存在且子进程存活则视为已启用，
// 否则清理残留自启项。
func (a *app) reconcileFwd() {
	if !fwdAutoStartExists() {
		return
	}
	if pid := readFwdPID(); pidAlive(pid) {
		a.fwdOn, a.fwdPID = true, pid
		a.fwdBtn.label = "■ 停止端口映射"
		a.fwdBtn.accent = colRed
		a.setFwdStatus("运行中(开机自启拉起，PID %d)", pid)
		return
	}
	_ = removeFwdAutoStart()
}

// readFwdPID 读取映射子进程 PID 文件，无文件或非法内容返回 0。
func readFwdPID() int {
	b, err := os.ReadFile(fwdPIDPath)
	if err != nil {
		return 0
	}
	n, _ := strconv.Atoi(strings.TrimSpace(string(b)))
	return n
}

// pidAlive 判断进程是否仍在运行。
func pidAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, uint32(pid))
	if err != nil {
		return false
	}
	defer windows.CloseHandle(h)
	var code uint32
	if err := windows.GetExitCodeProcess(h, &code); err != nil {
		return false
	}
	return code == stillActive
}

// 自启注册表位置(HKCU Run 键，用户登录时启动)。
const (
	runKeyPath    = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValName    = "lanrd"
	runValNameFwd = "lanrd-fwd"
)

// autoStartCommand 返回被控端自启命令行(静默参数)。
func autoStartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return `"` + exe + `" agent -quiet`, nil
}

// autoStartEnabled 返回开机自启是否已开启。
func autoStartEnabled() bool {
	return regValueExists(runValName)
}

// regValueExists 判断 Run 键下指定值是否存在。
func regValueExists(name string) bool {
	k, err := registryOpen(false)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(name)
	return err == nil && v != ""
}

// setAutoStart 开启/关闭开机自启。
func setAutoStart(on bool) error {
	k, err := registryOpen(true)
	if err != nil {
		return fmt.Errorf("lanrd/ui: 打开注册表失败: %w", err)
	}
	defer k.Close()
	if on {
		cmd, err := autoStartCommand()
		if err != nil {
			return err
		}
		return k.SetStringValue(runValName, cmd)
	}
	if err := k.DeleteValue(runValName); err != nil && err != errNotExist {
		return err
	}
	return nil
}

// fwdAutoStartCommand 构造端口映射自启命令行(直连 fwd 子进程 +
// 当前启用的全部映射规则)。
func fwdAutoStartCommand(maps []uiMap) (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	parts := []string{`"` + exe + `"`, "fwd"}
	for _, m := range maps {
		parts = append(parts, "-fwd", m.mapArg())
	}
	return strings.Join(parts, " "), nil
}

// syncFwdAutoStart 映射启用且勾选开机自启时写入映射自启项(携带全部
// 已启用规则)，否则移除。
func (a *app) syncFwdAutoStart() {
	if !a.fwdOn || !a.autoChk.checked {
		_ = removeFwdAutoStart()
		return
	}
	maps := a.enabledMaps()
	if len(maps) == 0 {
		_ = removeFwdAutoStart()
		return
	}
	cmdLine, err := fwdAutoStartCommand(maps)
	if err == nil {
		err = setFwdAutoStart(cmdLine)
	}
	if err != nil {
		a.setFwdStatus("设置开机自启失败: %v", err)
	}
}

// fwdAutoStartExists 端口映射自启项是否存在。
func fwdAutoStartExists() bool { return regValueExists(runValNameFwd) }

// setFwdAutoStart 写入端口映射自启项。
func setFwdAutoStart(cmd string) error {
	k, err := registryOpen(true)
	if err != nil {
		return fmt.Errorf("lanrd/ui: 打开注册表失败: %w", err)
	}
	defer k.Close()
	return k.SetStringValue(runValNameFwd, cmd)
}

// removeFwdAutoStart 移除端口映射自启项(不存在时忽略)。
func removeFwdAutoStart() error {
	k, err := registryOpen(true)
	if err != nil {
		return err
	}
	defer k.Close()
	if err := k.DeleteValue(runValNameFwd); err != nil && err != errNotExist {
		return err
	}
	return nil
}
