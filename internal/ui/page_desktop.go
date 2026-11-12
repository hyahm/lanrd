//go:build windows

package ui

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
)

// colLogPanel 日志面板底色。
var colLogPanel = color.RGBA{255, 255, 255, 248}

// 页面1(远程桌面)布局。
const (
	deskBtnY  = 110
	deskBtnH  = 40
	deskInLbl = 170
	deskInY   = 186
	deskInH   = 30
	deskLogY  = 234
	deskLogH  = 116
)

// updateDesktop 处理「远程桌面」页交互。
func (a *app) updateDesktop(mx, my float32) {
	w := float32(winW - 2*marginX)
	half := (w - 12) / 2
	if a.srvBtn.update(marginX, deskBtnY, half, deskBtnH) {
		a.startServer()
	}
	if a.cliBtn.update(marginX+half+12, deskBtnY, half, deskBtnH) {
		a.startClient()
	}
	a.ipIn.update(marginX, deskInY, 214, deskInH)
	a.portIn.update(marginX+226, deskInY, 84, deskInH)
	a.tokenIn.update(marginX+322, deskInY, winW-2*marginX-322, deskInH)
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) || inpututil.IsKeyJustPressed(ebiten.KeyNumpadEnter) {
		a.startClient() // 回车=连接
	}
}

// drawDesktop 绘制「远程桌面」页。
func (a *app) drawDesktop(screen *ebiten.Image) {
	w := float32(winW - 2*marginX)
	half := (w - 12) / 2
	a.srvBtn.draw(screen, marginX, deskBtnY, half, deskBtnH)
	a.cliBtn.draw(screen, marginX+half+12, deskBtnY, half, deskBtnH)

	// 输入区标签
	drawStr(screen, "被控端 IP", faceSmall, marginX, deskInLbl, colTextDim)
	drawStr(screen, "端口", faceSmall, marginX+226, deskInLbl, colTextDim)
	drawStr(screen, "口令", faceSmall, marginX+322, deskInLbl, colTextDim)
	a.ipIn.draw(screen, marginX, deskInY, 214, deskInH, a.tick)
	a.portIn.draw(screen, marginX+226, deskInY, 84, deskInH, a.tick)
	a.tokenIn.draw(screen, marginX+322, deskInY, winW-2*marginX-322, deskInH, a.tick)

	// 状态日志面板
	x, y := float32(marginX), float32(deskLogY)
	pw, ph := w, float32(deskLogH)
	fillRoundRect(screen, x-1, y-1, pw+2, ph+2, 6, colLogPanel)
	strokeRoundRect(screen, x-1, y-1, pw+2, ph+2, 6, 1.1, color.RGBA{140, 158, 182, 110})
	drawStr(screen, "▸ SYSTEM LOG", faceMono, x+10, y+8, dim(colCyan, 190))
	fillRect(screen, x+10, y+26, pw-20, 1, color.RGBA{140, 158, 182, 80})
	lines := a.status.snapshot()
	for i, ln := range lines {
		drawStr(screen, ln, faceMono, x+10, y+34+float32(i)*15, colTextDim)
	}
	if len(lines) == 0 {
		drawStr(screen, "等待操作…  服务端未启动", faceMono, x+10, y+34, colTextFaint)
	}
}
