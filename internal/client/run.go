package client

import (
	"fmt"
	"math"
	"time"

	"github.com/hajimehoshi/ebiten/v2"

	"lanrd/internal/proxy"
)

// Options 控制端启动选项。
type Options struct {
	Addr     string              // 被控端地址 ip:port
	Token    string              // 连接口令
	Forwards []proxy.ForwardRule // 静态端口映射规则
	Headless bool                // 无界面模式: 仅端口映射，不开桌面窗口
}

// Run 按选项连接被控端并启动控制端窗口，阻塞至窗口关闭或(无界面)连接断开。
func Run(o Options) error {
	cl, err := Dial(o.Addr, o.Token, 5*time.Second)
	if err != nil {
		return fmt.Errorf("连接 %s 失败: %w", o.Addr, err)
	}
	defer cl.Close()
	if len(o.Forwards) > 0 {
		if err := cl.EnableProxy(o.Forwards); err != nil {
			return fmt.Errorf("启动内网代理失败: %w", err)
		}
	}
	if o.Headless {
		for {
			closed, _ := cl.Closed()
			if closed {
				return nil
			}
			time.Sleep(300 * time.Millisecond)
		}
	}

	winW, winH := fitWindow(cl.Size())
	ebiten.SetWindowTitle("lanrd - " + o.Addr)
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
