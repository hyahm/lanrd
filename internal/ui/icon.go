//go:build windows

package ui

import (
	"image"
	"image/color"
	"math"
)

// 图标配色(与界面主题一致的青蓝系)。
var (
	iconGradTop = color.RGBA{10, 78, 122, 255}   // 渐变起点: 深海蓝
	iconGradBot = color.RGBA{10, 150, 190, 255}  // 渐变终点: 青蓝
	iconScreen  = color.RGBA{236, 249, 255, 255} // 屏幕底色
	iconInk     = color.RGBA{10, 78, 122, 255}   // 屏内图形
	iconAccent  = color.RGBA{0, 200, 240, 255}   // 信号高亮
)

// iconImage 绘制应用图标(任意尺寸): 圆角渐变方块 + 显示器 + 远程信号波。
func iconImage(size int) *image.RGBA {
	return iconImageOf(size)
}

// IconImage 返回指定尺寸的应用图标(供图标生成工具导出 ICO)。
func IconImage(size int) *image.RGBA { return iconImageOf(size) }

// iconImageOf 在 256 逻辑画布绘制后按需缩放。
func iconImageOf(size int) *image.RGBA {
	const q = 256 // 逻辑坐标按 256 设计，再缩放到目标尺寸
	logical := drawIconLogical()
	if size == q {
		return logical
	}
	out := image.NewRGBA(image.Rect(0, 0, size, size))
	scale := float64(size) / q
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			sx, sy := int(float64(x)/scale), int(float64(y)/scale)
			if sx >= q {
				sx = q - 1
			}
			if sy >= q {
				sy = q - 1
			}
			out.SetRGBA(x, y, logical.RGBAAt(sx, sy))
		}
	}
	return out
}

// drawIconLogical 在 256x256 逻辑画布上绘制图标。
func drawIconLogical() *image.RGBA {
	const q = 256
	img := image.NewRGBA(image.Rect(0, 0, q, q))
	r := 58.0 // 圆角半径
	for y := 0; y < q; y++ {
		for x := 0; x < q; x++ {
			if !inRounded(float64(x), float64(y), q, r) {
				continue
			}
			t := float64(y) / q
			c := lerpColor64(iconGradTop, iconGradBot, t)
			// 左上轻微提亮，增加体积感
			lift := (1 - (float64(x)+float64(y))/(2*q)) * 26
			img.SetRGBA(x, y, liftColor(c, uint8(lift)))
		}
	}
	drawMonitor(img)
	return img
}

// inRounded 点是否在圆角方块内。
func inRounded(x, y, q, r float64) bool {
	if x < 8 || y < 8 || x > q-8 || y > q-8 {
		return false
	}
	x, y = x-8, y-8
	q = q - 16
	cx := math.Max(r, math.Min(x, q-r))
	cy := math.Max(r, math.Min(y, q-r))
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// drawMonitor 绘制显示器(屏幕 + 底座 + 屏内信号波)。
func drawMonitor(img *image.RGBA) {
	const q = 256
	// 屏幕外框
	mx, my, mw, mh := 48.0, 62.0, 160.0, 116.0
	fillRound64(img, mx, my, mw, mh, 14, iconScreen)
	// 底座: 支柱 + 底板
	fillRound64(img, q/2-7, my+mh, 14, 16, 3, iconScreen)
	fillRound64(img, q/2-34, my+mh+14, 68, 10, 5, iconScreen)
	// 屏内: 光标箭头 + 信号波(远程控制寓意)
	drawCursor(img, 92, 92)
	drawSignal(img, 168, 106)
	// 屏幕下缘信号灯
	fillRound64(img, mx+10, my+mh-14, 6, 6, 3, iconAccent)
}

// drawCursor 画屏内小光标箭头。
func drawCursor(img *image.RGBA, ox, oy int) {
	pts := [][2]int{{0, 0}, {0, 34}, {9, 26}, {15, 38}, {21, 35}, {15, 24}, {25, 23}}
	for _, p := range pts {
		dot(img, ox+p[0], oy+p[1], 2.2, iconInk)
		dot(img, ox+p[0]+1, oy+p[1], 2.2, iconInk)
		dot(img, ox+p[0], oy+p[1]+1, 2.2, iconInk)
	}
}

// drawSignal 画信号波(两段圆弧)。
func drawSignal(img *image.RGBA, cx, cy int) {
	for k, rad := range []float64{16, 26} {
		w := 3.4 - float64(k)*0.8
		for a := -0.9; a <= 0.9; a += 0.03 {
			x := cx + int(rad*math.Cos(a-math.Pi/4))
			y := cy + int(rad*math.Sin(a-math.Pi/4))
			dot(img, x, y, w, iconInk)
		}
	}
	dot(img, cx+8, cy+8, 4.2, iconInk)
}

// dot 画实心圆点(抗锯齿边缘)。
func dot(img *image.RGBA, x, y int, r float64, c color.RGBA) {
	rr := r * r
	for dy := -int(r) - 1; dy <= int(r)+1; dy++ {
		for dx := -int(r) - 1; dx <= int(r)+1; dx++ {
			d := float64(dx*dx + dy*dy)
			if d <= rr {
				img.SetRGBA(x+dx, y+dy, c)
			}
		}
	}
}

// fillRound64 填充圆角矩形(float 坐标)。
func fillRound64(img *image.RGBA, x, y, w, h, r float64, c color.RGBA) {
	for py := int(y); py < int(y+h); py++ {
		for px := int(x); px < int(x+w); px++ {
			fx, fy := float64(px)+0.5-x, float64(py)+0.5-y
			cx := math.Max(r, math.Min(fx, w-r))
			cy := math.Max(r, math.Min(fy, h-r))
			dx, dy := fx-cx, fy-cy
			if dx*dx+dy*dy <= r*r {
				img.SetRGBA(px, py, c)
			}
		}
	}
}

// lerpColor64 颜色插值(64 位浮点)。
func lerpColor64(a, b color.RGBA, t float64) color.RGBA {
	return color.RGBA{
		R: uint8(float64(a.R) + (float64(b.R)-float64(a.R))*t),
		G: uint8(float64(a.G) + (float64(b.G)-float64(a.G))*t),
		B: uint8(float64(a.B) + (float64(b.B)-float64(a.B))*t),
		A: 255,
	}
}

// liftColor 提亮。
func liftColor(c color.RGBA, d uint8) color.RGBA {
	lift := func(v uint8) uint8 {
		n := int(v) + int(d)
		if n > 255 {
			return 255
		}
		return uint8(n)
	}
	return color.RGBA{lift(c.R), lift(c.G), lift(c.B), c.A}
}
