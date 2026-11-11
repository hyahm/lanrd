//go:build windows

// Package launcher 提供原生 Win32 图形启动界面：
// 一键启动被控端服务，或填写 IP/端口/口令启动控制端。
package launcher

import (
	"fmt"
	"syscall"
	"unsafe"

	"lanrd/internal/agent"

	"golang.org/x/sys/windows"
)

// 控件 ID。
const (
	idBtnServer = 1001
	idBtnClient = 1002
	idLblStatus = 1003
	idEdtIP     = 1011
	idEdtPort   = 1012
	idEdtToken  = 1013
)

// g 界面全局状态(仅在 GUI 线程访问，状态文本除外)。
var g struct {
	hInstance  uintptr
	font       uintptr
	hwnd       uintptr
	hServerBtn uintptr
	hClientBtn uintptr
	hIP        uintptr
	hPort      uintptr
	hToken     uintptr
	hStatus    uintptr
	status     *statusStore
	srv        *agent.Agent // 进程内被控端
	serverOn   bool
}

// wndProc 窗口过程。
func wndProc(hwnd uintptr, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case wmCommand:
		if wp>>16 == bnClicked {
			switch wp & 0xFFFF {
			case idBtnServer:
				startServer()
			case idBtnClient, idOK:
				startClient() // 回车=连接
			case idCancel:
				procPostMessageW.Call(hwnd, wmClose, 0, 0) // Esc=关闭
			}
		}
	case wmDestroy:
		procPostQuitMessage.Call(0)
	}
	r, _, _ := procDefWindowProcW.Call(hwnd, uintptr(msg), wp, lp)
	return r
}

// Run 创建并运行启动界面，窗口关闭后返回。
func Run() error {
	g.hInstance, _, _ = procGetModuleHandleW.Call(0)
	if err := registerClass(); err != nil {
		return err
	}
	if err := createMainUI(); err != nil {
		return err
	}
	// 标准消息循环: TranslateMessage 把按键翻译成字符消息(EDIT 输入必需)，
	// IsDialogMessageW 提供 TAB 焦点切换/回车/Esc。
	var msg MSG
	for {
		r, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if r == 0 || int32(r) == -1 {
			break
		}
		processed, _, _ := procIsDialogMessageW.Call(g.hwnd, uintptr(unsafe.Pointer(&msg)))
		if processed == 0 {
			procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}
	return nil
}

// registerClass 注册主窗口类。
func registerClass() error {
	name, _ := windows.UTF16PtrFromString("lanrdLauncher")
	cursor, _, _ := procLoadCursorW.Call(0, idcArrow)
	wc := WNDCLASSEXW{
		CbSize:        uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		Style:         csHRedraw | csVRedraw,
		LpfnWndProc:   syscall.NewCallback(wndProc),
		HInstance:     g.hInstance,
		HCursor:       cursor,
		HbrBackground: colorBtnface + 1,
		LpszClassName: name,
	}
	r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc)))
	if r == 0 {
		return fmt.Errorf("lanrd/launcher: RegisterClassExW 失败: %w", err)
	}
	return nil
}

// createMainUI 创建主窗口与全部控件。
func createMainUI() error {
	var rc RECT
	procAdjustWindowRect.Call(uintptr(unsafe.Pointer(&rc)), wsOverlappedWindow, 0)
	const cw, ch = 402, 222
	ww, wh := cw+rc.Right-rc.Left, ch+rc.Bottom-rc.Top
	hwnd, err := createWindowEx("lanrdLauncher", "lanrd - 内网远程桌面",
		wsOverlappedWindow, -1, -1, int32(ww), int32(wh), 0, 0)
	if err != nil {
		return fmt.Errorf("lanrd/launcher: 创建窗口失败: %w", err)
	}
	g.hwnd = hwnd
	createFont()
	if err := createControls(); err != nil {
		return err
	}
	procShowWindow.Call(hwnd, swShow)
	return nil
}

// createFont 创建界面字体。
func createFont() {
	face, _ := windows.UTF16PtrFromString("Segoe UI")
	g.font, _, _ = procCreateFontW.Call(
		^uintptr(13), 0, 0, 0, 400, 0, 0, 0, 1, 0, 0, 0, 0,
		uintptr(unsafe.Pointer(face)))
}

// createControls 创建标签/输入框/按钮。
func createControls() error {
	var err error
	if g.hServerBtn, err = ctl("BUTTON", "启动服务端(被控)", wsTabStop, 20, 16, 170, 34, idBtnServer); err != nil {
		return err
	}
	if g.hStatus, err = ctl("STATIC", "服务端未启动", 0, 20, 58, 362, 52, idLblStatus); err != nil {
		return err
	}
	if _, err = ctl("STATIC", "IP", 0, 20, 122, 24, 20, 0); err != nil {
		return err
	}
	if g.hIP, err = ctl("EDIT", "", wsTabStop|wsBorder|esAutoHscscroll, 48, 118, 150, 26, idEdtIP); err != nil {
		return err
	}
	if _, err = ctl("STATIC", "端口", 0, 208, 122, 30, 20, 0); err != nil {
		return err
	}
	if g.hPort, err = ctl("EDIT", "7080", wsTabStop|wsBorder|esAutoHscscroll, 240, 118, 60, 26, idEdtPort); err != nil {
		return err
	}
	if _, err = ctl("STATIC", "口令", 0, 20, 160, 30, 20, 0); err != nil {
		return err
	}
	if g.hToken, err = ctl("EDIT", "", wsTabStop|wsBorder|esAutoHscscroll, 48, 156, 150, 26, idEdtToken); err != nil {
		return err
	}
	if g.hClientBtn, err = ctl("BUTTON", "连接(控制端)", wsTabStop, 208, 152, 174, 34, idBtnClient); err != nil {
		return err
	}
	g.status = &statusStore{hwnd: g.hStatus}
	return nil
}

// ctl 创建单个控件并发置字体。
func ctl(class, text string, style uint32, x, y, w, h, id int32) (uintptr, error) {
	hwnd, err := createWindowEx(class, text, style|wsChild|wsVisible, x, y, w, h, id, g.hwnd)
	if err != nil {
		return 0, err
	}
	if g.font != 0 {
		procSendMessageW.Call(hwnd, wmSetFont, g.font, 1)
	}
	return hwnd, nil
}
