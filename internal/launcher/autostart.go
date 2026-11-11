//go:build windows

package launcher

import (
	"fmt"
	"os"

	"golang.org/x/sys/windows/registry"
)

// 自启注册表位置(HKCU Run 键，用户登录时启动——被控端必须在
// 已登录的桌面会话中运行才能抓屏与注入键鼠)。
const (
	runKeyPath = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValName = "lanrd"
)

// autoStartCommand 返回自启命令行(带引号的当前程序路径 + 静默参数)。
func autoStartCommand() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return `"` + exe + `" agent -quiet`, nil
}

// autoStartEnabled 返回开机自启是否已开启。
func autoStartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetStringValue(runValName)
	return err == nil && v != ""
}

// setAutoStart 开启/关闭开机自启。
func setAutoStart(on bool) error {
	if on {
		cmd, err := autoStartCommand()
		if err != nil {
			return err
		}
		k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
		if err != nil {
			return fmt.Errorf("lanrd/launcher: 打开注册表失败: %w", err)
		}
		defer k.Close()
		return k.SetStringValue(runValName, cmd)
	}
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("lanrd/launcher: 打开注册表失败: %w", err)
	}
	defer k.Close()
	if err := k.DeleteValue(runValName); err != nil && err != registry.ErrNotExist {
		return err
	}
	return nil
}
