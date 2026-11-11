//go:build windows && (amd64 || arm64)

package winapi

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestSetCursorPosRoundTrip 验证鼠标移动注入。
func TestSetCursorPosRoundTrip(t *testing.T) {
	EnsureDPIAware()
	require.True(t, SetCursorPos(500, 500))
	var pt POINT
	require.True(t, GetCursorPos(&pt))
	assert.InDelta(t, 500, pt.X, 2)
	assert.InDelta(t, 500, pt.Y, 2)
}

// TestSendKeyF13 用无害的 F13 键验证键盘注入确实生效。
func TestSendKeyF13(t *testing.T) {
	const scanF13 = 0x64
	require.True(t, SendKey(scanF13, true), "SendInput 按下失败")
	assert.Negative(t, GetAsyncKeyState(VK_F13), "F13 未处于按下状态")
	require.True(t, SendKey(scanF13, false), "SendInput 释放失败")
}

// TestSendMouseButtonReturn 验证鼠标按键事件被系统接受。
func TestSendMouseButtonReturn(t *testing.T) {
	require.True(t, SendMouse(0, 0, 0, MOUSEEVENTF_LEFTDOWN), "SendInput 左键按下失败")
	require.True(t, SendMouse(0, 0, 0, MOUSEEVENTF_LEFTUP), "SendInput 左键释放失败")
	require.True(t, SendMouse(0, 0, 120, MOUSEEVENTF_WHEEL), "SendInput 滚轮失败")
}
