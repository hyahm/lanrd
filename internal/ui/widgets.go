//go:build windows

package ui

import (
	"image"
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
)

// pt 鼠标位置。
func pt() (float32, float32) {
	x, y := ebiten.CursorPosition()
	return float32(x), float32(y)
}

// inRect 点是否在矩形内。
func inRect(px, py, x, y, w, h float32) bool {
	return px >= x && px < x+w && py >= y && py < y+h
}

// clicked 本帧是否按下左键。
func clicked() bool { return inpututil.IsMouseButtonJustPressed(ebiten.MouseButtonLeft) }

// drawStr 在指定锚点画文本(v2 默认左上角对齐)。
func drawStr(dst *ebiten.Image, s string, face text.Face, x, y float32, c color.Color) {
	op := &text.DrawOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleWithColor(c)
	text.Draw(dst, s, face, op)
}

// strWidth 测量文本宽度。
func strWidth(s string, face text.Face) float32 {
	w, _ := text.Measure(s, face, 0)
	return float32(w)
}

// drawStrCenter 在矩形内水平垂直居中画文本。
func drawStrCenter(dst *ebiten.Image, s string, face text.Face,
	x, y, w, h float32, c color.Color) {
	tw, th := text.Measure(s, face, 0)
	drawStr(dst, s, face, x+(w-float32(tw))/2, y+(h-float32(th))/2, c)
}

// button 霓虹描边按钮: 悬停提亮 + 外发光过渡动画。
type button struct {
	label  string
	accent color.RGBA // 描边主色
	bold   bool       // 主要动作(更亮)
	hoverT float32    // 悬停过渡 0~1
	pressT float32    // 按下过渡 0~1
}

// update 处理悬停/点击，返回本帧是否被点击。
func (b *button) update(x, y, w, h float32) bool {
	mx, my := pt()
	hover := inRect(mx, my, x, y, w, h)
	target := float32(0)
	if hover {
		target = 1
	}
	b.hoverT += clamp01(target-b.hoverT, 0.12)
	press := hover && ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft)
	b.pressT += clamp01(boolF(press)-b.pressT, 0.35)
	return hover && clicked()
}

// boolF 布尔转 0/1。
func boolF(b bool) float32 {
	if b {
		return 1
	}
	return 0
}

// clamp01 帧步进钳制到 0~1。
func clamp01(d, step float32) float32 {
	if d > step {
		return step
	}
	if d < -step {
		return -step
	}
	return d
}

// draw 绘制按钮。
func (b *button) draw(dst *ebiten.Image, x, y, w, h float32) {
	accent := b.accent
	if accent == (color.RGBA{}) {
		accent = colCyan
	}
	base := color.RGBA{255, 255, 255, 252}
	fill := lerpColor(base, colPanelHi, b.hoverT*0.8)
	fill = lerpColor(fill, color.RGBA{228, 234, 243, 255}, b.pressT)
	if b.hoverT > 0.05 {
		glowRect(dst, x, y, w, h, 5, spreadOf(b.hoverT), accent)
	}
	fillRoundRect(dst, x, y, w, h, 5, fill)
	strokeRoundRect(dst, x, y, w, h, 5, 1.2,
		lerpColor(dim(accent, 120), accent, maxF(b.hoverT, b.boldF())))
	fillRect(dst, x+6, y+h-2.2, w-12, 1.1,
		color.RGBA{accent.R, accent.G, accent.B, uint8(40 + 100*b.hoverT)})
	tc := lerpColor(colTextDim, colText, maxF(b.hoverT, boolF(b.bold)))
	drawStrCenter(dst, b.label, faceText, x, y, w, h, tc)
}

func (b *button) boldF() float32 {
	if b.bold {
		return 0.55
	}
	return 0
}

// spreadOf 悬停发光层数随过渡增强。
func spreadOf(t float32) float32 { return 0.5 + t*2.5 }

// dim 降低 alpha。
func dim(c color.RGBA, a uint8) color.RGBA { return color.RGBA{c.R, c.G, c.B, a} }

// maxF 取大。
func maxF(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// input 下划线霓虹输入框: 焦点呼吸、光标闪烁、占位提示、数字过滤。
type input struct {
	text        string
	focus       bool
	numeric     bool
	placeholder string
	cursor      int // 光标(rune 下标)
	holdBS      int // 退格按住帧数(长按连删)
}

// runes 以 rune 切片返回内容。
func (i *input) runes() []rune { return []rune(i.text) }

// update 处理焦点、字符输入与退格。
func (i *input) update(x, y, w, h float32) bool {
	mx, my := pt()
	if clicked() {
		i.focus = inRect(mx, my, x, y, w, h)
	}
	if !i.focus {
		i.holdBS = 0
		return false
	}
	for _, r := range ebiten.InputChars() {
		if i.numeric && (r < '0' || r > '9') {
			continue
		}
		rs := i.runes()
		i.text = string(append(rs[:i.cursor], append([]rune{r}, rs[i.cursor:]...)...))
		i.cursor++
	}
	bs := ebiten.IsKeyPressed(ebiten.KeyBackspace)
	if bs {
		i.holdBS++
		if inpututil.IsKeyJustPressed(ebiten.KeyBackspace) || i.holdBS > 22 {
			if i.holdBS%2 == 0 || inpututil.IsKeyJustPressed(ebiten.KeyBackspace) {
				if i.cursor > 0 {
					rs := i.runes()
					i.text = string(append(rs[:i.cursor-1], rs[i.cursor:]...))
					i.cursor--
				}
			}
		}
	} else {
		i.holdBS = 0
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowLeft) && i.cursor > 0 {
		i.cursor--
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyArrowRight) && i.cursor < len(i.runes()) {
		i.cursor++
	}
	return true
}

// draw 绘制输入框。
func (i *input) draw(dst *ebiten.Image, x, y, w, h float32, tick int) {
	focusA := uint8(0)
	if i.focus {
		focusA = 205
	}
	fillRect(dst, x, y, w, h, color.RGBA{255, 255, 255, 240})
	if i.focus {
		fillRect(dst, x, y, w, h, color.RGBA{8, 134, 175, 8})
	}
	tc := colText
	if i.text == "" {
		tc = colTextFaint
	}
	s := i.text
	if s == "" {
		s = i.placeholder
	}
	padX, padY := x+8, y+(h-16)/2
	drawStr(dst, s, faceText, padX, padY, tc)
	// 光标
	if i.focus && (tick/28)%2 == 0 {
		rs := i.runes()
		cw := strWidth(string(rs[:min(i.cursor, len(rs))]), faceText)
		fillRect(dst, padX+cw+1, y+5, 1.4, h-10, colCyan)
	}
	// 底部下划线(焦点实线，非焦点淡线)
	lineC := color.RGBA{colCyan.R, colCyan.G, colCyan.B, focusA}
	if !i.focus {
		lineC = color.RGBA{120, 138, 162, 110}
	}
	fillRect(dst, x, y+h, w, 1.5, lineC)
}

// min 取小。
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// pulse 0~1 正弦脉冲。
func pulse(tick int, speed float64) float64 {
	return 0.5 + 0.5*f64sin(tick, speed)
}

// checkbox 霓虹复选框。
type checkbox struct {
	checked bool
	hoverT  float32
}

// update 返回本帧是否切换。
func (c *checkbox) update(x, y float32) bool {
	mx, my := pt()
	hover := inRect(mx, my, x-3, y-3, 150, 22)
	c.hoverT += clamp01(boolF(hover)-c.hoverT, 0.15)
	if hover && clicked() {
		c.checked = !c.checked
		return true
	}
	return false
}

// draw 绘制复选框与文字。
func (c *checkbox) draw(dst *ebiten.Image, x, y float32, label string) {
	const s = float32(15)
	ac := colCyan
	if c.hoverT > 0.05 {
		glowRect(dst, x, y, s, s, 3, spreadOf(c.hoverT)*0.6, ac)
	}
	fillRoundRect(dst, x, y, s, s, 3, color.RGBA{255, 255, 255, 252})
	strokeRoundRect(dst, x, y, s, s, 3, 1.1,
		lerpColor(dim(ac, 130), ac, maxF(c.hoverT, boolF(c.checked))))
	if c.checked {
		fillRoundRect(dst, x+2.5, y+2.5, s-5, s-5, 2, ac)
		fillRect(dst, x+4.5, y+7, 3, 1.8, color.RGBA{255, 255, 255, 255}) // 对勾左半
		fillRect(dst, x+6.5, y+8.6, 4.5, 1.8, color.RGBA{255, 255, 255, 255})
	}
	tc := lerpColor(colTextDim, colText, maxF(c.hoverT, boolF(c.checked)))
	drawStr(dst, label, faceText, x+s+8, y-1, tc)
}

// mapRow 映射列表的一行视图数据。
type mapRow struct {
	local, remote string
	enabled       bool
}

// mapList 映射列表: 行悬停高亮、选中指示、勾选框、滚轮滚动。
type mapList struct {
	sel    int // 选中行(-1 无)
	scroll int // 滚动偏移(像素)
	hover  int // 悬停行(-1 无)
}

// update 处理列表交互，返回被点击勾选的行号(-1 无)。
func (l *mapList) update(x, y, w, h float32, rows int) int {
	l.hover = -1
	mx, my := pt()
	if inRect(mx, my, x, y, w, h) {
		if _, wy := ebiten.Wheel(); wy != 0 {
			l.scroll -= int(wy) * 24
		}
	}
	const rh = float32(30)
	maxScroll := int(float32(rows)*rh-h) - 1
	if maxScroll < 0 {
		maxScroll = 0
	}
	l.scroll = clampInt(l.scroll, 0, maxScroll)
	clickedRow := -1
	if clicked() && inRect(mx, my, x, y, w, h) {
		idx := int((my-y+float32(l.scroll))/rh - 0.15)
		if idx >= 0 && idx < rows {
			l.hover = idx
			l.sel = idx
			// 勾选框区域
			if inRect(mx, my, x+14, y+float32(idx)*rh-float32(l.scroll)+7, 16, 16) {
				clickedRow = idx
			}
		}
	} else if inRect(mx, my, x, y, w, h) {
		idx := int((my-y+float32(l.scroll))/rh - 0.15)
		if idx >= 0 && idx < rows {
			l.hover = idx
		}
	}
	return clickedRow
}

// clampInt 钳制整数范围。
func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// draw 绘制列表面板与行。
func (l *mapList) draw(dst *ebiten.Image, x, y, w, h float32,
	rows []mapRow, tick int) {
	fillRoundRect(dst, x-1, y-1, w+2, h+2, 6, color.RGBA{255, 255, 255, 248})
	strokeRoundRect(dst, x-1, y-1, w+2, h+2, 6, 1.1, color.RGBA{140, 158, 182, 110})
	const rh = float32(30)
	clip := image.Rect(int(x), int(y), int(x+w), int(y+h))
	sub := dst.SubImage(clip).(*ebiten.Image)
	for i, r := range rows {
		ry := y + float32(i)*rh - float32(l.scroll)
		if ry < y-rh || ry > y+h {
			continue
		}
		if i == l.sel {
			fillRect(sub, x+1, ry+1, w-2, rh-2, color.RGBA{8, 134, 175, 16})
			fillRect(sub, x+1, ry+2, 2.5, rh-4, colCyan) // 选中指示条
		} else if i == l.hover {
			fillRect(sub, x+1, ry+1, w-2, rh-2, color.RGBA{32, 48, 72, 10})
		}
		// 勾选框
		cb := dim(colCyan, 140)
		boxX, boxY := x+16, ry+rh/2-7
		fillRoundRect(sub, boxX, boxY, 14, 14, 3, color.RGBA{255, 255, 255, 255})
		strokeRoundRect(sub, boxX, boxY, 14, 14, 3, 1.1, cb)
		if r.enabled {
			fillRoundRect(sub, boxX+2.5, boxY+2.5, 9, 9, 2, colCyan)
			fillRect(sub, boxX+3.8, boxY+6.4, 2.6, 1.6, color.RGBA{255, 255, 255, 255})
			fillRect(sub, boxX+5.8, boxY+7.9, 4.2, 1.6, color.RGBA{255, 255, 255, 255})
		}
		// 文本
		tc := colText
		if !r.enabled {
			tc = colTextFaint
		}
		drawStr(sub, r.local, faceMono, x+42, ry+rh/2-8, tc)
		drawStr(sub, "→", faceText, x+118, ry+rh/2-8, dim(colCyan, 170))
		drawStr(sub, r.remote, faceText, x+136, ry+rh/2-8, tc)
	}
	// 滚动条
	if rowsPx := float32(len(rows)) * rh; rowsPx > h {
		bh := h * h / rowsPx
		by := y + (h-bh)*float32(l.scroll)/float32(rowsPx-h)
		fillRect(dst, x+w-3, by, 2, bh, color.RGBA{120, 138, 162, 140})
	}
}
