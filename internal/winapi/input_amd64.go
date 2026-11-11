//go:build windows && (amd64 || arm64)

package winapi

import (
	"unsafe"
)

var procSendInput = modUser32.NewProc("SendInput")

// SendInput 输入事件类型。
const (
	INPUT_MOUSE    = 0
	INPUT_KEYBOARD = 1
)

// KEYBDINPUT / MOUSEINPUT 标志。
const (
	MOUSEEVENTF_LEFTDOWN   = 0x0002
	MOUSEEVENTF_LEFTUP     = 0x0004
	MOUSEEVENTF_RIGHTDOWN  = 0x0008
	MOUSEEVENTF_RIGHTUP    = 0x0010
	MOUSEEVENTF_MIDDLEDOWN = 0x0020
	MOUSEEVENTF_MIDDLEUP   = 0x0040
	MOUSEEVENTF_WHEEL      = 0x0800

	KEYEVENTF_EXTENDEDKEY = 0x0001
	KEYEVENTF_KEYUP       = 0x0002
	KEYEVENTF_SCANCODE    = 0x0008
)

// MOUSEINPUT SendInput 鼠标结构。
type MOUSEINPUT struct {
	Dx, Dy    int32
	MouseData uint32
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// KEYBDINPUT SendInput 键盘结构。
type KEYBDINPUT struct {
	Vk        uint16
	Scan      uint16
	Flags     uint32
	Time      uint32
	ExtraInfo uintptr
}

// mouseInput 携带鼠标事件的 INPUT 联合体(amd64/arm64 布局)。
type mouseInput struct {
	Type uint32
	_    uint32
	Mi   MOUSEINPUT
}

// keyboardInput 携带键盘事件的 INPUT 联合体(amd64/arm64 布局)。
type keyboardInput struct {
	Type uint32
	_    uint32
	Ki   KEYBDINPUT
	_    [8]byte
}

// SendMouse 发送单个鼠标输入事件。
func SendMouse(dx, dy, data, flags uint32) bool {
	in := mouseInput{Type: INPUT_MOUSE, Mi: MOUSEINPUT{Dx: int32(dx), Dy: int32(dy), MouseData: data, Flags: flags}}
	// SendInput(cInputs, pInputs, cbSize)
	r, _, _ := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	return r == 1
}

// SendKey 发送单个键盘扫描码事件，scan 的 0x100 位表示扩展键(E0 前缀)。
func SendKey(scan uint16, down bool) bool {
	ki := KEYBDINPUT{Scan: scan & 0xFF, Flags: KEYEVENTF_SCANCODE}
	if scan&0xFF00 != 0 {
		ki.Flags |= KEYEVENTF_EXTENDEDKEY
	}
	if !down {
		ki.Flags |= KEYEVENTF_KEYUP
	}
	in := keyboardInput{Type: INPUT_KEYBOARD, Ki: ki}
	// SendInput(cInputs, pInputs, cbSize)
	r, _, _ := procSendInput.Call(1, uintptr(unsafe.Pointer(&in)), unsafe.Sizeof(in))
	return r == 1
}
