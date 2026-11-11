//go:build windows

// Package inject 将控制端发来的键鼠事件注入本机，并通过低级钩子
// 检测远端物理键鼠活动，实现“双方抢占操作”的状态感知与可选屏蔽。
package inject

import (
	"lanrd/internal/winapi"
)

// Injector 键鼠注入器。
type Injector struct{}

// New 创建注入器。
func New() *Injector { return &Injector{} }

// MouseMove 将鼠标移动到虚拟桌面坐标 (vx+x, vy+y)。
func (i *Injector) MouseMove(vx, vy, x, y int) {
	winapi.SetCursorPos(int32(vx+x), int32(vy+y))
}

// MouseButton 按下/释放鼠标按键，btn 取 proto.BtnLeft/BtnRight/BtnMiddle。
func (i *Injector) MouseButton(btn uint8, down bool) {
	var flags uint32
	switch btn {
	case 0:
		flags = winapi.MOUSEEVENTF_LEFTDOWN
		if !down {
			flags = winapi.MOUSEEVENTF_LEFTUP
		}
	case 1:
		flags = winapi.MOUSEEVENTF_RIGHTDOWN
		if !down {
			flags = winapi.MOUSEEVENTF_RIGHTUP
		}
	case 2:
		flags = winapi.MOUSEEVENTF_MIDDLEDOWN
		if !down {
			flags = winapi.MOUSEEVENTF_MIDDLEUP
		}
	default:
		return
	}
	winapi.SendMouse(0, 0, 0, flags)
}

// Wheel 滚轮滚动，delta 单位为 1/120 槽位，正值为向上。
func (i *Injector) Wheel(delta int32) {
	winapi.SendMouse(0, 0, uint32(delta), winapi.MOUSEEVENTF_WHEEL)
}

// Key 按下/释放按键，scan 为 Windows 扫描码，0x0100 位表示扩展键。
func (i *Injector) Key(scan uint16, down bool) {
	winapi.SendKey(scan, down)
}
