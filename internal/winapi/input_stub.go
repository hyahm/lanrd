//go:build windows && (386 || arm)

package winapi

import "errors"

// ErrArch64Required 仅支持 64 位 Windows。
var ErrArch64Required = errors.New("lanrd/winapi: 键鼠注入仅支持 64 位 Windows (amd64/arm64)")

// SendMouse 非 64 位平台不可用。
func SendMouse(dx, dy, data, flags uint32) bool { return false }

// SendKey 非 64 位平台不可用。
func SendKey(scan uint16, down bool) bool { return false }
