package client

import (
	"fmt"
	"image"
	"image/color"
	"os"
	"strings"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
)

// 本地功能热键(不转发给远端)。
const (
	hotkeyLock       = ebiten.KeyF9  // 锁定/解锁远端物理键鼠
	hotkeyFullscreen = ebiten.KeyF11 // 全屏切换
)

// App 控制端 ebiten 界面：显示远端画面并转发本地键鼠。
type App struct {
	cl       *Client
	eimg     *ebiten.Image
	font     font.Face
	selfView bool // 连接的是否本机(画面无限嵌套警告)

	prevKeys map[uint16]bool
	prevBtns [3]bool
	lastPos  [2]int
	lastGood [2]int // 最近一次位于画面区域内的远端坐标
	prevHot  map[ebiten.Key]bool
	locked   bool // 本地记录的锁定状态(以远端广播为准)

	pix        []byte // 上传像素的复用缓冲
	winW, winH int
	scale      float64
	ox, oy     float64
}

// NewApp 创建界面。
func NewApp(cl *Client) *App {
	local, _ := os.Hostname()
	return &App{
		cl:       cl,
		font:     basicfont.Face7x13,
		prevKeys: make(map[uint16]bool),
		prevHot:  make(map[ebiten.Key]bool),
		selfView: local != "" && strings.EqualFold(local, cl.Host()),
	}
}

// Update 每帧调用：应用脏块、处理热键、转发键鼠。
func (a *App) Update() error {
	a.applyDirty()
	a.computeLayout()
	a.handleHotkeys()
	a.forwardInput()
	return nil
}

// applyDirty 将网络线程写入画布的脏块上传到 ebiten 图像。
func (a *App) applyDirty() {
	canvas, dirty := a.cl.TakeDirty()
	if canvas == nil {
		return
	}
	w, h := canvas.Bounds().Dx(), canvas.Bounds().Dy()
	if a.eimg == nil || a.eimg.Bounds().Dx() != w || a.eimg.Bounds().Dy() != h {
		a.eimg = ebiten.NewImage(w, h)
		a.uploadRegion(canvas, dirtyRect{0, 0, w, h})
	}
	for _, r := range dirty {
		a.uploadRegion(canvas, r)
	}
}

// uploadRegion 打包画布子区域像素并上传到 ebiten 图像(复用缓冲，避免每块分配)。
func (a *App) uploadRegion(canvas *image.RGBA, r dirtyRect) {
	if a.eimg == nil {
		return
	}
	need := r.w * r.h * 4
	if cap(a.pix) < need {
		a.pix = make([]byte, need)
	}
	pix := a.pix[:need]
	for row := 0; row < r.h; row++ {
		src := canvas.Pix[(r.y+row)*canvas.Stride+r.x*4:]
		copy(pix[row*r.w*4:(row+1)*r.w*4], src)
	}
	sub := a.eimg.SubImage(image.Rect(r.x, r.y, r.x+r.w, r.y+r.h)).(*ebiten.Image)
	sub.ReplacePixels(pix)
}

// computeLayout 按窗口尺寸计算适配缩放与居中偏移。
func (a *App) computeLayout() {
	w, h := a.cl.Size()
	if w == 0 || h == 0 || a.winW == 0 || a.winH == 0 {
		return
	}
	s := float64(a.winW) / float64(w)
	if s2 := float64(a.winH) / float64(h); s2 < s {
		s = s2
	}
	a.scale = s
	a.ox = (float64(a.winW) - s*float64(w)) / 2
	a.oy = (float64(a.winH) - s*float64(h)) / 2
}

// toRemote 将窗口坐标映射为远端屏幕坐标(已由 pointerPos 取代)。
// 保留映射语义见 pointerPos。

// handleHotkeys 处理 F9 锁定远端键鼠 / F11 全屏。
func (a *App) handleHotkeys() {
	for _, k := range []ebiten.Key{hotkeyLock, hotkeyFullscreen} {
		pressed := ebiten.IsKeyPressed(k)
		if pressed && !a.prevHot[k] {
			switch k {
			case hotkeyLock:
				a.locked = !a.locked
				a.cl.SendInputLock(a.locked)
			case hotkeyFullscreen:
				ebiten.SetFullscreen(!ebiten.IsFullscreen())
			}
		}
		a.prevHot[k] = pressed
	}
}

// forwardInput 检测本地键鼠变化并转发给远端。
// 光标不在画面区域(窗口内画布范围)内时不转发移动，
// 否则钳位到边缘/角落的坐标会把远端光标钉死在角落。
func (a *App) forwardInput() {
	if _, h := a.cl.Size(); h == 0 {
		return
	}
	mx, my := ebiten.CursorPosition()
	rx, ry, inside := a.pointerPos(mx, my)
	anyBtn := a.prevBtns[0] || a.prevBtns[1] || a.prevBtns[2]
	if inside {
		a.lastGood = [2]int{rx, ry}
	}
	if (inside || anyBtn) && (rx != a.lastPos[0] || ry != a.lastPos[1]) {
		a.cl.SendMouseMove(uint16(rx), uint16(ry))
		a.lastPos = [2]int{rx, ry}
	}
	// 按键/滚轮保持状态一致总是转发；光标在画面外时用最近的画面内坐标
	bx, by := rx, ry
	if !inside && !anyBtn {
		bx, by = a.lastGood[0], a.lastGood[1]
	}
	a.forwardButtons(bx, by)
	if _, wy := ebiten.Wheel(); wy != 0 {
		a.cl.SendWheel(int32(wy*120), uint16(bx), uint16(by))
	}
	a.forwardKeys()
}

// pointerPos 将窗口坐标映射为远端坐标，inside 表示光标是否位于画面区域内。
func (a *App) pointerPos(mx, my int) (rx, ry int, inside bool) {
	w, h := a.cl.Size()
	if w == 0 || h == 0 || a.scale <= 0 {
		return 0, 0, false
	}
	fx := (float64(mx) - a.ox) / a.scale
	fy := (float64(my) - a.oy) / a.scale
	inside = fx >= 0 && fy >= 0 && fx <= float64(w-1) && fy <= float64(h-1)
	rx = int(fx + 0.5)
	ry = int(fy + 0.5)
	if rx < 0 {
		rx = 0
	}
	if ry < 0 {
		ry = 0
	}
	if rx > w-1 {
		rx = w - 1
	}
	if ry > h-1 {
		ry = h - 1
	}
	return rx, ry, inside
}

// forwardButtons 转发鼠标按键变化。
func (a *App) forwardButtons(rx, ry int) {
	for i := 0; i < 3; i++ {
		pressed := ebiten.IsMouseButtonPressed(ebiten.MouseButton(i))
		if pressed != a.prevBtns[i] {
			a.cl.SendMouseBtn(uint8(i), pressed, uint16(rx), uint16(ry))
			a.prevBtns[i] = pressed
		}
	}
}

// forwardKeys 转发键盘按下/释放变化(热键除外)。
func (a *App) forwardKeys() {
	pressed := make(map[uint16]bool)
	for k, scan := range keyToScan {
		if k == hotkeyLock || k == hotkeyFullscreen {
			continue
		}
		if ebiten.IsKeyPressed(k) {
			pressed[scan] = true
		}
	}
	for scan := range pressed {
		if !a.prevKeys[scan] {
			a.cl.SendKey(scan, true)
		}
	}
	for scan := range a.prevKeys {
		if !pressed[scan] {
			a.cl.SendKey(scan, false)
		}
	}
	a.prevKeys = pressed
}

// Draw 绘制远端画面与状态指示。
func (a *App) Draw(screen *ebiten.Image) {
	screen.Fill(color.RGBA{20, 20, 24, 255})
	if a.eimg != nil {
		opts := &ebiten.DrawImageOptions{}
		// 1:1 或放大用最近邻保持像素锐利；缩小用双线性避免文字破碎
		if a.scale < 0.999 {
			opts.Filter = ebiten.FilterLinear
		} else {
			opts.Filter = ebiten.FilterNearest
		}
		opts.GeoM.Scale(a.scale, a.scale)
		opts.GeoM.Translate(a.ox, a.oy)
		screen.DrawImage(a.eimg, opts)
	}
	a.drawHud(screen)
}

// drawHud 绘制连接/抢占状态指示。
func (a *App) drawHud(screen *ebiten.Image) {
	w, h := a.cl.Size()
	lines := []string{fmt.Sprintf("RTT %dms  %dx%d  F9=lock F11=fullscreen", a.cl.RTT(), w, h)}
	st := a.cl.Status()
	if st.Locked {
		lines = append(lines, "[Remote input LOCKED - F9 to unlock]")
	}
	if st.Last == 1 && time.Since(time.UnixMilli(int64(st.Since))) < 5*time.Second {
		lines = append(lines, "* Remote user is operating *")
	}
	if a.selfView {
		lines = append(lines, "!! WARNING: connected to THIS machine (mirror loop)")
	}
	if closed, _ := a.cl.Closed(); closed {
		lines = append(lines, "!! DISCONNECTED - close window to exit")
	}
	vector.DrawFilledRect(screen, 4, 4, 340, float32(14*len(lines)+6), color.RGBA{0, 0, 0, 160}, false)
	for i, ln := range lines {
		text.Draw(screen, ln, a.font, 8, 16+i*14, color.RGBA{130, 220, 130, 255})
	}
}

// Layout 返回逻辑画布尺寸并记录窗口尺寸。
func (a *App) Layout(w, h int) (int, int) {
	a.winW, a.winH = w, h
	return w, h
}
