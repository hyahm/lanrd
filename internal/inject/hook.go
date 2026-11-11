//go:build windows

package inject

import (
	"fmt"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"lanrd/internal/winapi"
)

// 钩子捕获的鼠标消息(仅用于活动检测，不区分具体消息)。
const (
	wmKeyDown    = 0x0100
	wmKeyUp      = 0x0101
	wmSysKeyDown = 0x0104
	wmSysKeyUp   = 0x0105
	wmMouseFirst = 0x0200
	wmMouseLast  = 0x0209
)

// Monitor 通过低级键盘/鼠标钩子监控远端机器前的物理键鼠活动。
type Monitor struct {
	mu        sync.Mutex
	lastLocal int64  // 最近一次物理输入的 Unix 纳秒时间戳
	blocking  bool   // 是否屏蔽物理键鼠
	pumpTid   uint32 // 消息泵线程 ID
	lastCB    uintptr
	mouseCB   uintptr
	kbHook    uintptr
	mouseHook uintptr
	stopped   chan struct{}
}

// StartMonitor 安装低级钩子并启动消息泵，返回停止函数。
func StartMonitor() (*Monitor, func(), error) {
	m := &Monitor{stopped: make(chan struct{})}
	ready := make(chan error, 1)
	go m.pump(ready)
	if err := <-ready; err != nil {
		return nil, func() {}, err
	}
	var once sync.Once
	return m, func() { once.Do(m.stop) }, nil
}

// pump 运行在独立 OS 线程上：安装钩子 -> 消息泵 -> 卸载钩子。
func (m *Monitor) pump(ready chan<- error) {
	winapi.LockOSThreadForHooks()
	defer winapi.UnlockOSThreadForHooks()

	m.lastCB = syscall.NewCallback(m.onKeyboard)
	m.mouseCB = syscall.NewCallback(m.onMouse)
	m.kbHook = winapi.SetWindowsHookEx(winapi.WH_KEYBOARD_LL, m.lastCB, 0, 0)
	m.mouseHook = winapi.SetWindowsHookEx(winapi.WH_MOUSE_LL, m.mouseCB, 0, 0)
	tid := winapi.GetCurrentThreadId()
	m.mu.Lock()
	m.pumpTid = tid
	m.mu.Unlock()
	if m.kbHook == 0 || m.mouseHook == 0 {
		m.uninstall()
		ready <- fmt.Errorf("lanrd/inject: 安装低级钩子失败(键盘=%d 鼠标=%d)", m.kbHook, m.mouseHook)
		close(m.stopped)
		return
	}
	ready <- nil

	var msg winapi.MSG
	for {
		r := winapi.GetMessageW(&msg)
		if r == 0 || r == -1 {
			break
		}
	}
	m.uninstall()
	close(m.stopped)
}

// uninstall 卸载两个低级钩子。
func (m *Monitor) uninstall() {
	if m.kbHook != 0 {
		winapi.UnhookWindowsHookEx(m.kbHook)
		m.kbHook = 0
	}
	if m.mouseHook != 0 {
		winapi.UnhookWindowsHookEx(m.mouseHook)
		m.mouseHook = 0
	}
}

// stop 通知消息泵线程退出并等待清理完成。
func (m *Monitor) stop() {
	m.mu.Lock()
	tid := m.pumpTid
	m.mu.Unlock()
	if tid != 0 {
		winapi.PostThreadMessageW(tid, winapi.WM_QUIT)
	}
	<-m.stopped
}

// onKeyboard 低级键盘钩子回调。
func (m *Monitor) onKeyboard(ncode int32, wparam uintptr, lparam unsafe.Pointer) uintptr {
	if ncode >= 0 {
		switch wparam {
		case wmKeyDown, wmKeyUp, wmSysKeyDown, wmSysKeyUp:
			k := (*winapi.KBDLLHOOKSTRUCT)(lparam)
			if k.Flags&winapi.LLKHF_INJECTED == 0 {
				if m.markLocal() {
					return 1 // 屏蔽物理按键
				}
			}
		}
	}
	return winapi.CallNextHookEx(ncode, wparam, uintptr(lparam))
}

// onMouse 低级鼠标钩子回调。
func (m *Monitor) onMouse(ncode int32, wparam uintptr, lparam unsafe.Pointer) uintptr {
	if ncode >= 0 && wparam >= wmMouseFirst && wparam <= wmMouseLast {
		s := (*winapi.MSLLHOOKSTRUCT)(lparam)
		if s.Flags&winapi.LLMHF_INJECTED == 0 {
			if m.markLocal() {
				return 1
			}
		}
	}
	return winapi.CallNextHookEx(ncode, wparam, uintptr(lparam))
}

// markLocal 记录一次物理输入，返回是否需要屏蔽该输入。
func (m *Monitor) markLocal() bool {
	m.mu.Lock()
	m.lastLocal = time.Now().UnixNano()
	block := m.blocking
	m.mu.Unlock()
	return block
}

// LastLocal 返回最近一次物理输入时间，从未有过输入返回零值。
func (m *Monitor) LastLocal() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return time.Unix(0, m.lastLocal)
}

// SetBlocking 设置是否屏蔽远端物理键鼠。
func (m *Monitor) SetBlocking(b bool) {
	m.mu.Lock()
	m.blocking = b
	m.mu.Unlock()
}

// Blocking 返回当前是否屏蔽远端物理键鼠。
func (m *Monitor) Blocking() bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.blocking
}
