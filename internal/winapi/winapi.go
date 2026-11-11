//go:build windows

// Package winapi 封装本工具用到的 user32/gdi32/kernel32 系统调用。
package winapi

import (
	"runtime"
	"unsafe"

	"golang.org/x/sys/windows"
)

var (
	modUser32   = windows.NewLazySystemDLL("user32.dll")
	modGdi32    = windows.NewLazySystemDLL("gdi32.dll")
	modKernel32 = windows.NewLazySystemDLL("kernel32.dll")

	procGetSystemMetrics = modUser32.NewProc("GetSystemMetrics")
	procGetDC            = modUser32.NewProc("GetDC")
	procReleaseDC        = modUser32.NewProc("ReleaseDC")
	procSetCursorPos     = modUser32.NewProc("SetCursorPos")

	procCreateCompatibleDC     = modGdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = modGdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject           = modGdi32.NewProc("SelectObject")
	procDeleteDC               = modGdi32.NewProc("DeleteDC")
	procDeleteObject           = modGdi32.NewProc("DeleteObject")
	procBitBlt                 = modGdi32.NewProc("BitBlt")
	procGetDIBits              = modGdi32.NewProc("GetDIBits")

	procSetDpiAwarenessCtx = modUser32.NewProc("SetProcessDpiAwarenessContext")
	procSetProcessDPIAware = modUser32.NewProc("SetProcessDPIAware")
	procGetCursorPos       = modUser32.NewProc("GetCursorPos")
	procGetAsyncKeyState   = modUser32.NewProc("GetAsyncKeyState")

	procSetWindowsHookEx  = modUser32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHook = modUser32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx    = modUser32.NewProc("CallNextHookEx")
	procGetMessage        = modUser32.NewProc("GetMessageW")
	procPostThreadMessage = modUser32.NewProc("PostThreadMessageW")
	procGetCurrentThread  = modKernel32.NewProc("GetCurrentThreadId")
)

// GetSystemMetrics 常量。
const (
	SM_XVIRTUALSCREEN  = 76
	SM_YVIRTUALSCREEN  = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79
)

// BitBlt 光栅操作标志。
const (
	SRCCOPY    = 0x00CC0020
	CAPTUREBLT = 0x40000000
)

// 低级钩子相关常量。
const (
	WH_KEYBOARD_LL = 13
	WH_MOUSE_LL    = 14
	WM_QUIT        = 0x0012
	LLKHF_INJECTED = 0x10
	LLMHF_INJECTED = 0x10
)

// POINT 屏幕坐标。
type POINT struct{ X, Y int32 }

// MSG Windows 消息结构。
type MSG struct {
	Hwnd     uintptr
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       POINT
	LPrivate uintptr
}

// KBDLLHOOKSTRUCT 低级键盘钩子参数。
type KBDLLHOOKSTRUCT struct {
	VkCode    uint32
	ScanCode  uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// MSLLHOOKSTRUCT 低级鼠标钩子参数。
type MSLLHOOKSTRUCT struct {
	Pt        POINT
	MouseData uint32
	Flags     uint32
	Time      uint32
	_         uint32
	ExtraInfo uintptr
}

// BITMAPINFOHEADER DIB 头。
type BITMAPINFOHEADER struct {
	BiSize          uint32
	BiWidth         int32
	BiHeight        int32
	BiPlanes        uint16
	BiBitCount      uint16
	BiCompression   uint32
	BiSizeImage     uint32
	BiXPelsPerMeter int32
	BiYPelsPerMeter int32
	BiClrUsed       uint32
	BiClrImportant  uint32
}

// BITMAPINFO GetDIBits 参数。
type BITMAPINFO struct {
	BmiHeader BITMAPINFOHEADER
	BmiColors [1]uint32
}

// GetSystemMetrics 获取系统度量。
func GetSystemMetrics(n int32) int32 {
	r, _, _ := procGetSystemMetrics.Call(uintptr(n))
	return int32(r)
}

// GetDC 获取屏幕(或窗口)设备上下文，返回 0 表示失败。
func GetDC(hwnd uintptr) uintptr {
	r, _, _ := procGetDC.Call(hwnd)
	return r
}

// ReleaseDC 释放设备上下文。
func ReleaseDC(hwnd, hdc uintptr) {
	procReleaseDC.Call(hwnd, hdc)
}

// SetCursorPos 移动鼠标到屏幕坐标(虚拟桌面坐标系)。
func SetCursorPos(x, y int32) bool {
	r, _, _ := procSetCursorPos.Call(uintptr(x), uintptr(y))
	return r != 0
}

// GetCursorPos 读取当前鼠标屏幕坐标。
func GetCursorPos(pt *POINT) bool {
	r, _, _ := procGetCursorPos.Call(uintptr(unsafe.Pointer(pt)))
	return r != 0
}

// GetAsyncKeyState 查询按键异步状态，最高位(0x8000)表示按下。
func GetAsyncKeyState(vk int32) int16 {
	r, _, _ := procGetAsyncKeyState.Call(uintptr(vk))
	return int16(r)
}

// 常用虚拟键码。
const (
	VK_LBUTTON = 0x01
	VK_F13     = 0x7C
)

// CreateCompatibleDC 创建兼容内存 DC。
func CreateCompatibleDC(hdc uintptr) uintptr {
	r, _, _ := procCreateCompatibleDC.Call(hdc)
	return r
}

// CreateCompatibleBitmap 创建兼容位图。
func CreateCompatibleBitmap(hdc uintptr, w, h int32) uintptr {
	r, _, _ := procCreateCompatibleBitmap.Call(hdc, uintptr(w), uintptr(h))
	return r
}

// SelectObject 选入 GDI 对象，返回旧对象。
func SelectObject(hdc, obj uintptr) uintptr {
	r, _, _ := procSelectObject.Call(hdc, obj)
	return r
}

// DeleteDC 删除 DC。
func DeleteDC(hdc uintptr) { procDeleteDC.Call(hdc) }

// DeleteObject 删除 GDI 对象。
func DeleteObject(obj uintptr) { procDeleteObject.Call(obj) }

// BitBlt 位块传送。
func BitBlt(dst uintptr, x, y, w, h int32, src uintptr, sx, sy int32, rop uint32) bool {
	r, _, _ := procBitBlt.Call(dst, uintptr(x), uintptr(y), uintptr(w), uintptr(h),
		src, uintptr(sx), uintptr(sy), uintptr(rop))
	return r != 0
}

// GetDIBits 将位图导出为 DIB 像素(biHeight 为负时为顶朝下顺序)。
func GetDIBits(hdc, hbmp uintptr, start, lines uint32, bits unsafe.Pointer, bmi *BITMAPINFO) bool {
	const dibRGBColors = 0
	r, _, _ := procGetDIBits.Call(hdc, hbmp, uintptr(start), uintptr(lines),
		uintptr(bits), uintptr(unsafe.Pointer(bmi)), dibRGBColors)
	return r != 0
}

// EnsureDPIAware 设置进程 DPI 感知，保证取到真实分辨率与坐标。
func EnsureDPIAware() {
	if err := procSetDpiAwarenessCtx.Find(); err == nil {
		// DPI_AWARENESS_CONTEXT_PER_MONITOR_AWARE_V2 = -4
		if r, _, _ := procSetDpiAwarenessCtx.Call(^uintptr(3)); r != 0 {
			return
		}
	}
	procSetProcessDPIAware.Call()
}

// SetWindowsHookEx 安装 Windows 钩子。
func SetWindowsHookEx(idHook int32, proc, hmod uintptr, tid uint32) uintptr {
	r, _, _ := procSetWindowsHookEx.Call(uintptr(idHook), proc, hmod, uintptr(tid))
	return r
}

// UnhookWindowsHookEx 卸载钩子。
func UnhookWindowsHookEx(hhk uintptr) bool {
	r, _, _ := procUnhookWindowsHook.Call(hhk)
	return r != 0
}

// CallNextHookEx 传递钩子事件给下一个钩子，返回处理结果。
func CallNextHookEx(ncode int32, wparam, lparam uintptr) uintptr {
	r, _, _ := procCallNextHookEx.Call(0, uintptr(ncode), wparam, lparam)
	return r
}

// GetMessageW 阻塞取消息，返回 -1 出错、0 表示 WM_QUIT。
func GetMessageW(msg *MSG) int32 {
	r, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(msg)), 0, 0, 0)
	return int32(r)
}

// PostThreadMessageW 向指定线程投递消息。
func PostThreadMessageW(tid uint32, msg uint32) {
	procPostThreadMessage.Call(uintptr(tid), uintptr(msg), 0, 0)
}

// GetCurrentThreadId 取当前线程 ID。
func GetCurrentThreadId() uint32 {
	r, _, _ := procGetCurrentThread.Call()
	return uint32(r)
}

// LockOSThreadForHooks 提示调用方本函数所在 goroutine 需要锁定 OS 线程。
// 钩子与消息泵必须运行在安装钩子的同一线程上。
func LockOSThreadForHooks() { runtime.LockOSThread() }

// UnlockOSThreadForHooks 解除 OS 线程锁定。
func UnlockOSThreadForHooks() { runtime.UnlockOSThread() }
