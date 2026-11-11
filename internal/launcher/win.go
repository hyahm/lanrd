//go:build windows

package launcher

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// Win32 常量。
const (
	wmDestroy = 0x0002
	wmClose   = 0x0010
	wmCommand = 0x0111
	wmSetFont = 0x0030
	bnClicked = 0
	swShow    = 5
	csHRedraw = 0x0002
	csVRedraw = 0x0001

	idOK     = 1 // 对话框回车
	idCancel = 2 // 对话框 Esc

	wsOverlappedWindow = 0x00CF0000
	wsChild            = 0x40000000
	wsVisible          = 0x10000000
	wsTabStop          = 0x00010000
	wsBorder           = 0x00800000
	esAutoHscscroll    = 0x0080

	colorBtnface = 16
	idcArrow     = 32512
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modGdi32    = windows.NewLazySystemDLL("gdi32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procRegisterClassExW     = modUser32.NewProc("RegisterClassExW")
	procCreateWindowExW      = modUser32.NewProc("CreateWindowExW")
	procDefWindowProcW       = modUser32.NewProc("DefWindowProcW")
	procDispatchMessageW     = modUser32.NewProc("DispatchMessageW")
	procTranslateMessage     = modUser32.NewProc("TranslateMessage")
	procIsDialogMessageW     = modUser32.NewProc("IsDialogMessageW")
	procGetMessageW          = modUser32.NewProc("GetMessageW")
	procPostQuitMessage      = modUser32.NewProc("PostQuitMessage")
	procPostMessageW         = modUser32.NewProc("PostMessageW")
	procShowWindow           = modUser32.NewProc("ShowWindow")
	procSendMessageW         = modUser32.NewProc("SendMessageW")
	procSetWindowTextW       = modUser32.NewProc("SetWindowTextW")
	procGetWindowTextLengthW = modUser32.NewProc("GetWindowTextLengthW")
	procGetWindowTextW       = modUser32.NewProc("GetWindowTextW")
	procAdjustWindowRect     = modUser32.NewProc("AdjustWindowRect")
	procEnableWindow         = modUser32.NewProc("EnableWindow")
	procLoadCursorW          = modUser32.NewProc("LoadCursorW")
	procCreateFontW          = modGdi32.NewProc("CreateFontW")
	procGetModuleHandleW     = modKernel32.NewProc("GetModuleHandleW")
)

// POINT 屏幕坐标。
type POINT struct{ X, Y int32 }

// MSG Windows 消息。
type MSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uintptr
}

// RECT 矩形。
type RECT struct{ Left, Top, Right, Bottom int32 }

// WNDCLASSEXW 窗口类。
type WNDCLASSEXW struct {
	CbSize        uint32
	Style         uint32
	LpfnWndProc   uintptr
	CbClsExtra    int32
	CbWndExtra    int32
	HInstance     uintptr
	HIcon         uintptr
	HCursor       uintptr
	HbrBackground uintptr
	LpszMenuName  *uint16
	LpszClassName *uint16
	HIconSm       uintptr
}

// createWindowEx 创建窗口/控件。
func createWindowEx(class, text string, style uint32, x, y, w, h, id int32, parent uintptr) (uintptr, error) {
	cls, err := windows.UTF16PtrFromString(class)
	if err != nil {
		return 0, err
	}
	txt, _ := windows.UTF16PtrFromString(text)
	r, _, callErr := procCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(cls)), uintptr(unsafe.Pointer(txt)), uintptr(style),
		uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		parent, uintptr(id), g.hInstance, 0)
	hwnd := uintptr(r)
	if hwnd == 0 {
		return 0, callErr
	}
	return hwnd, nil
}

// windowText 读取控件文本。
func windowText(h uintptr) string {
	r, _, _ := procGetWindowTextLengthW.Call(h)
	n := int32(r)
	if n <= 0 {
		return ""
	}
	buf := make([]uint16, n+1)
	procGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(n+1))
	return windows.UTF16ToString(buf[:n])
}

// setWindowText 设置控件文本(可跨线程调用)。
func setWindowText(h uintptr, text string) {
	t, err := windows.UTF16PtrFromString(text)
	if err != nil {
		return
	}
	procSetWindowTextW.Call(h, uintptr(unsafe.Pointer(t)))
}
