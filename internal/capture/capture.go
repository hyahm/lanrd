//go:build windows

// Package capture 基于 GDI 的虚拟桌面屏幕采集，输出 BGRA 顶朝下像素。
package capture

import (
	"fmt"
	"unsafe"

	"lanrd/internal/winapi"
)

// Capturer 屏幕采集器，覆盖整个虚拟桌面(多显示器合并)。
type Capturer struct {
	vx, vy   int32 // 虚拟桌面原点(可能为负)
	w, h     int32 // 虚拟桌面尺寸
	screenDC uintptr
	memDC    uintptr
	bmp      uintptr
	oldObj   uintptr
	bmi      winapi.BITMAPINFO
	buf      []byte
}

// New 创建采集器并完成首次抓屏，验证环境可用。
func New() (*Capturer, error) {
	winapi.EnsureDPIAware()
	c := &Capturer{}
	c.screenDC = winapi.GetDC(0)
	if c.screenDC == 0 {
		return nil, fmt.Errorf("lanrd/capture: GetDC 失败")
	}
	c.memDC = winapi.CreateCompatibleDC(c.screenDC)
	if c.memDC == 0 {
		winapi.ReleaseDC(0, c.screenDC)
		return nil, fmt.Errorf("lanrd/capture: CreateCompatibleDC 失败")
	}
	if err := c.resize(); err != nil {
		c.Close()
		return nil, err
	}
	return c, nil
}

// Size 返回当前虚拟桌面尺寸。
func (c *Capturer) Size() (w, h int) { return int(c.w), int(c.h) }

// Origin 返回虚拟桌面原点(屏幕坐标，可能为负)。
func (c *Capturer) Origin() (vx, vy int) { return int(c.vx), int(c.vy) }

// resize 读取虚拟桌面尺寸并重建 GDI 资源。
func (c *Capturer) resize() error {
	vx := winapi.GetSystemMetrics(winapi.SM_XVIRTUALSCREEN)
	vy := winapi.GetSystemMetrics(winapi.SM_YVIRTUALSCREEN)
	w := winapi.GetSystemMetrics(winapi.SM_CXVIRTUALSCREEN)
	h := winapi.GetSystemMetrics(winapi.SM_CYVIRTUALSCREEN)
	if w <= 0 || h <= 0 {
		return fmt.Errorf("lanrd/capture: 无效的虚拟桌面尺寸 %dx%d", w, h)
	}
	if c.bmp != 0 {
		winapi.SelectObject(c.memDC, c.oldObj)
		winapi.DeleteObject(c.bmp)
		c.bmp = 0
	}
	c.vx, c.vy, c.w, c.h = vx, vy, w, h
	c.bmp = winapi.CreateCompatibleBitmap(c.screenDC, w, h)
	if c.bmp == 0 {
		return fmt.Errorf("lanrd/capture: CreateCompatibleBitmap 失败")
	}
	c.oldObj = winapi.SelectObject(c.memDC, c.bmp)
	c.bmi = winapi.BITMAPINFO{}
	c.bmi.BmiHeader.BiSize = uint32(unsafe.Sizeof(c.bmi.BmiHeader))
	c.bmi.BmiHeader.BiWidth = w
	c.bmi.BmiHeader.BiHeight = -h // 负值 => 顶朝下
	c.bmi.BmiHeader.BiPlanes = 1
	c.bmi.BmiHeader.BiBitCount = 32
	c.buf = make([]byte, int(w)*int(h)*4)
	return nil
}

// Capture 抓取一帧 BGRA 像素。分辨率变化时自动重建并返回 resized=true。
func (c *Capturer) Capture() (buf []byte, resized bool, err error) {
	cw := winapi.GetSystemMetrics(winapi.SM_CXVIRTUALSCREEN)
	ch := winapi.GetSystemMetrics(winapi.SM_CYVIRTUALSCREEN)
	if cw != c.w || ch != c.h {
		if err := c.resize(); err != nil {
			return nil, false, err
		}
		resized = true
	}
	// 选中位图 -> BitBlt -> 取消选中 -> GetDIBits
	winapi.SelectObject(c.memDC, c.bmp)
	ok := winapi.BitBlt(c.memDC, 0, 0, c.w, c.h, c.screenDC, c.vx, c.vy,
		winapi.SRCCOPY|winapi.CAPTUREBLT)
	winapi.SelectObject(c.memDC, c.oldObj)
	if !ok {
		return nil, false, fmt.Errorf("lanrd/capture: BitBlt 失败")
	}
	if !winapi.GetDIBits(c.screenDC, c.bmp, 0, uint32(c.h), unsafe.Pointer(&c.buf[0]), &c.bmi) {
		return nil, false, fmt.Errorf("lanrd/capture: GetDIBits 失败")
	}
	return c.buf, resized, nil
}

// Close 释放 GDI 资源。
func (c *Capturer) Close() {
	if c.bmp != 0 {
		winapi.SelectObject(c.memDC, c.oldObj)
		winapi.DeleteObject(c.bmp)
		c.bmp = 0
	}
	if c.memDC != 0 {
		winapi.DeleteDC(c.memDC)
		c.memDC = 0
	}
	if c.screenDC != 0 {
		winapi.ReleaseDC(0, c.screenDC)
		c.screenDC = 0
	}
}
