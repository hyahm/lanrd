//go:build windows

// Package ui 提供基于 ebiten 的科技感图形启动界面：
// 深空蓝黑底色 + 青色霓虹描边，自绘按钮/输入框/复选框/列表等组件，
// 承载服务端启停、远程连接与端口映射管理等全部启动器功能。
package ui

import (
	"fmt"
	"image/color"
	"math"
	"os"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/text/v2"
	"github.com/hajimehoshi/ebiten/v2/vector"
	"golang.org/x/image/font"
	"golang.org/x/image/font/opentype"
	"golang.org/x/image/font/sfnt"
)

// 浅色简洁主题色板: 云白底 + 单一青蓝强调。
var (
	colBgTop     = color.RGBA{252, 253, 255, 255} // 背景渐变顶
	colBgBottom  = color.RGBA{240, 243, 249, 255} // 背景渐变底
	colPanel     = color.RGBA{255, 255, 255, 250} // 面板填充
	colPanelHi   = color.RGBA{236, 243, 250, 255} // 面板悬停提亮
	colGrid      = color.RGBA{14, 110, 165, 8}    // 背景网格线(极淡)
	colCyan      = color.RGBA{8, 134, 175, 255}   // 强调色: 青蓝
	colCyanMid   = color.RGBA{8, 134, 175, 140}   // 强调色半透明
	colCyanDim   = color.RGBA{8, 134, 175, 60}    // 强调色微淡
	colPurple    = color.RGBA{109, 76, 255, 255}  // 备用辅色
	colText      = color.RGBA{32, 44, 63, 255}    // 主文本
	colTextDim   = color.RGBA{104, 119, 143, 255} // 次文本
	colTextFaint = color.RGBA{152, 166, 188, 255} // 提示文本
	colGreen     = color.RGBA{26, 148, 79, 255}   // 运行/成功
	colRed       = color.RGBA{214, 56, 66, 255}   // 停止/危险
	colAmber     = color.RGBA{204, 116, 12, 255}  // 警示
	opaqueBg     = color.RGBA{255, 255, 255, 255} // 描边内衬不透明底色
)

// 字体(中文优先微软雅黑，等宽用 Consolas，缺失时逐级回退)。
var (
	faceTitle text.Face // 标题
	faceText  text.Face // 正文
	faceSmall text.Face // 辅助小字
	faceMono  text.Face // 等宽(端口/状态)
)

// loadFonts 加载系统字体并派生各号字面。
func loadFonts() error {
	src := loadFontFile(
		`C:\Windows\Fonts\msyh.ttc`,
		`C:\Windows\Fonts\simhei.ttf`,
		`C:\Windows\Fonts\simsun.ttc`,
	)
	if src == nil {
		return fmt.Errorf("lanrd/ui: 未找到可用中文字体")
	}
	title, err := faceOf(src, 21)
	if err != nil {
		return err
	}
	body, err := faceOf(src, 14)
	if err != nil {
		return err
	}
	small, err := faceOf(src, 12)
	if err != nil {
		return err
	}
	faceTitle, faceText, faceSmall = title, body, small
	if monoSrc := loadFontFile(`C:\Windows\Fonts\consola.ttf`); monoSrc != nil {
		mono, err := faceOf(monoSrc, 13)
		if err != nil {
			return err
		}
		// Consolas 无中文字形，缺失字符回退中文字体，避免日志乱码。
		mf, err := text.NewMultiFace(mono, body)
		if err != nil {
			return err
		}
		faceMono = mf
	} else {
		faceMono = body
	}
	return nil
}

// loadFontFile 依序尝试读取字体文件，返回首个成功内容。
func loadFontFile(paths ...string) []byte {
	for _, p := range paths {
		if b, err := os.ReadFile(p); err == nil {
			return b
		}
	}
	return nil
}

// faceOf 从字体数据派生指定字号字面(自动兼容 ttc 集合)。
func faceOf(data []byte, size float64) (text.Face, error) {
	f, err := sfnt.Parse(data)
	if err != nil {
		col, cerr := sfnt.ParseCollection(data)
		if cerr != nil {
			return nil, fmt.Errorf("lanrd/ui: 字体解析失败: %w", err)
		}
		if f, err = col.Font(0); err != nil {
			return nil, err
		}
	}
	gox, err := opentype.NewFace(f, &opentype.FaceOptions{
		Size: size, DPI: 72, Hinting: font.HintingFull,
	})
	if err != nil {
		return nil, err
	}
	return text.NewGoXFace(gox), nil
}

// fillRect 填充矩形。
func fillRect(dst *ebiten.Image, x, y, w, h float32, c color.RGBA) {
	vector.DrawFilledRect(dst, x, y, w, h, c, true)
}

// fillRoundRect 填充圆角矩形(矩形 + 四角圆拼合)。
func fillRoundRect(dst *ebiten.Image, x, y, w, h, r float32, c color.RGBA) {
	if w < 2*r || h < 2*r {
		fillRect(dst, x, y, w, h, c)
		return
	}
	vector.DrawFilledRect(dst, x+r, y, w-2*r, h, c, true)
	vector.DrawFilledRect(dst, x, y+r, r, h-2*r, c, true)
	vector.DrawFilledRect(dst, x+w-r, y+r, r, h-2*r, c, true)
	for _, p := range [][2]float32{
		{x + r, y + r}, {x + w - r, y + r},
		{x + r, y + h - r}, {x + w - r, y + h - r},
	} {
		vector.DrawFilledCircle(dst, p[0], p[1], r, c, true)
	}
}

// glowRect 矩形外柔和投影(浅色主题用 elevation 替代霓虹光):
// 多层外扩、alpha 逐层衰减、略微下移模拟受光。
func glowRect(dst *ebiten.Image, x, y, w, h, r, spread float32, c color.RGBA) {
	for i := spread; i >= 1; i-- {
		a := uint8(float32(26) * (1 - i/(spread+1)))
		k := i * 1.2
		fillRoundRect(dst, x-k, y-k+1.5, w+2*k, h+2*k, r+k,
			color.RGBA{30, 45, 70, a})
	}
}

// strokeRoundRect 圆角矩形描边: 外层描边色实心 + 内层不透明底色，
// 内层之上再叠半透明面板色由调用者完成。
func strokeRoundRect(dst *ebiten.Image, x, y, w, h, r, t float32, c color.RGBA) {
	fillRoundRect(dst, x, y, w, h, r, c)
	fillRoundRect(dst, x+t, y+t, w-2*t, h-2*t, r-t/2, opaqueBg)
}

// hline 强调下划线: 宽淡 + 细亮双层(浅色下收敛)。
func hline(dst *ebiten.Image, x, y, w float32, c color.RGBA) {
	fillRect(dst, x, y-2, w, 4, color.RGBA{c.R, c.G, c.B, 26})
	fillRect(dst, x, y-0.5, w, 1.8, c)
}

// lerp 线性插值。
func lerp(a, b, t float32) float32 { return a + (b-a)*t }

// f64sin 正弦(供脉冲动画)。
func f64sin(tick int, speed float64) float64 {
	return math.Sin(float64(tick) * speed)
}

// lerpColor 颜色线性插值。
func lerpColor(a, b color.RGBA, t float32) color.RGBA {
	return color.RGBA{
		R: uint8(lerp(float32(a.R), float32(b.R), t)),
		G: uint8(lerp(float32(a.G), float32(b.G), t)),
		B: uint8(lerp(float32(a.B), float32(b.B), t)),
		A: uint8(lerp(float32(a.A), float32(b.A), t)),
	}
}

// newBackground 预渲染背景: 轻微垂直渐变 + 极淡网格。
func newBackground(w, h int) *ebiten.Image {
	img := ebiten.NewImage(w, h)
	for y := 0; y < h; y++ {
		fillRect(img, 0, float32(y), float32(w), 1,
			lerpColor(colBgTop, colBgBottom, float32(y)/float32(h)))
	}
	for x := 0; x < w; x += 36 {
		fillRect(img, float32(x), 0, 1, float32(h), colGrid)
	}
	for y := 0; y < h; y += 36 {
		fillRect(img, 0, float32(y), float32(w), 1, colGrid)
	}
	return img
}
