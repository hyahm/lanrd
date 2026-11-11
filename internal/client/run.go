package client

import (
	"fmt"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"
)

// Run 连接被控端并启动控制端窗口，阻塞至窗口关闭。
func Run(addr, token string) error {
	cl, err := Dial(addr, token, 5*time.Second)
	if err != nil {
		return fmt.Errorf("连接 %s 失败: %w", addr, err)
	}
	defer cl.Close()

	winW, winH := fitWindow(cl.Size())
	ebiten.SetWindowTitle("lanrd - " + addr)
	ebiten.SetWindowSize(winW, winH)
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeEnabled)
	return ebiten.RunGame(NewApp(cl))
}

// fitWindow 计算默认窗口尺寸：远端分辨率放得下时 1:1 显示(最锐利)，
// 否则缩到本机屏幕的 90%。
func fitWindow(w, h int) (int, int) {
	winW, winH := w, h
	if sw, sh := ebiten.ScreenSizeInFullscreen(); sw > 0 && sh > 0 {
		maxW, maxH := sw*9/10, sh*9/10
		if winW > maxW || winH > maxH {
			s := math.Min(float64(maxW)/float64(w), float64(maxH)/float64(h))
			winW = int(float64(w) * s)
			winH = int(float64(h) * s)
		}
	}
	if winW < 640 || winH < 480 {
		winW, winH = 640, 480
	}
	return winW, winH
}
