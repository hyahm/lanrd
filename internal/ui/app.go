//go:build windows

package ui

import (
	"image"
	"image/color"
	"os/exec"
	"sync"

	"github.com/hajimehoshi/ebiten/v2"

	"lanrd/internal/agent"
)

// 窗口与布局常量(逻辑像素)。
const (
	winW, winH = 540, 430
	marginX    = 28
	headerY    = 26
	tabY       = 68
	pageY      = 104
	bottomY    = winH - 46
)

// app 图形启动器主体: 组件在 UI 线程访问；跨 goroutine 状态经 mu 保护。
type app struct {
	tick int
	page int // 0 远程桌面 1 内网代理
	bg   *ebiten.Image

	// 「远程桌面」页
	ipIn, portIn, tokenIn input
	srvBtn, cliBtn        button
	autoChk               checkbox
	status                *statusStore

	// 「内网代理」页
	maps                    []uiMap
	list                    mapList
	newLocalIn, newRemoteIn input
	addBtn, delBtn, fwdBtn  button

	// 运行状态(仅 UI 线程)
	srv      *agent.Agent
	serverOn bool
	fwdCmd   *exec.Cmd
	fwdPID   int
	fwdOn    bool

	// 跨 goroutine 状态
	mu             sync.Mutex
	fwdStatus      string
	pendingFwdExit int
}

// Run 创建并运行图形启动界面，窗口关闭后返回。
func Run() error {
	if err := loadFonts(); err != nil {
		return err
	}
	a := &app{status: &statusStore{}}
	a.initWidgets()
	a.bg = newBackground(winW, winH)
	ebiten.SetWindowSize(winW, winH)
	ebiten.SetWindowTitle("lanrd - 内网远程桌面")
	ebiten.SetWindowResizingMode(ebiten.WindowResizingModeDisabled)
	ebiten.SetWindowIcon([]image.Image{
		iconImage(256), iconImage(48), iconImage(32), iconImage(16),
	})
	return ebiten.RunGame(a)
}

// initWidgets 初始化组件初值(配置恢复)。
func (a *app) initWidgets() {
	cfg := loadUIConfig()
	a.ipIn = input{text: cfg.Connect.IP, placeholder: "被控端 IP"}
	a.portIn = input{text: cfg.Connect.Port, placeholder: "端口", numeric: true}
	a.tokenIn = input{text: cfg.Connect.Token, placeholder: "口令(无则留空)"}
	a.srvBtn = button{label: "▶ 启动服务端(被控)", accent: colGreen}
	a.cliBtn = button{label: "连接(控制端) ➔", accent: colCyan, bold: true}
	if autoStartEnabled() {
		a.autoChk.checked = true
	}

	a.maps = cfg.Proxy.Maps
	a.list.sel = -1
	a.list.scroll = 0
	a.newLocalIn = input{placeholder: "端口", numeric: true}
	a.newRemoteIn = input{placeholder: "远程地址 ip:port"}
	a.addBtn = button{label: "+ 添加"}
	a.delBtn = button{label: "− 删除选中", accent: colAmber}
	a.fwdBtn = button{label: "▶ 启动端口映射", accent: colCyan, bold: true}

	a.reconcileFwd() // 恢复开机自启拉起的映射状态
}

// Layout 实现 ebiten.Game。
func (a *app) Layout(_, _ int) (int, int) { return winW, winH }

// Update 实现 ebiten.Game: 动画计数、事件轮询与两页交互。
func (a *app) Update() error {
	a.tick++
	a.pollFwdExit()
	if a.tick%60 == 0 {
		a.healthCheckFwd()
	}
	mx, my := pt()
	a.updateTabs(mx, my)
	if a.page == 0 {
		a.updateDesktop(mx, my)
	} else {
		a.updateProxy(mx, my)
	}
	// 公共底部: 开机自启
	if a.autoChk.update(marginX, bottomY) {
		a.toggleAutoStart()
	}
	return nil
}

// updateTabs 处理顶部 Tab 切换(点击)。
func (a *app) updateTabs(mx, my float32) {
	if clicked() {
		for i := 0; i < 2; i++ {
			tx := tabX(i)
			if inRect(mx, my, tx, tabY-16, tabW(), 34) {
				a.page = i
			}
		}
	}
}

// tabNames Tab 标签文案。
var tabNames = []string{"◈ 远程桌面", "◈ 内网代理"}

// tabX 第 i 个 Tab 的横坐标。
func tabX(i int) float32 {
	return float32(marginX) + float32(i)*(tabW()+28)
}

// tabW 单个 Tab 宽度。
func tabW() float32 { return 132 }

// Draw 实现 ebiten.Game: 背景 → 标题 → Tab → 页面 → 公共底栏。
func (a *app) Draw(screen *ebiten.Image) {
	screen.DrawImage(a.bg, nil)
	a.drawHeader(screen)
	a.drawTabs(screen)
	if a.page == 0 {
		a.drawDesktop(screen)
	} else {
		a.drawProxy(screen)
	}
	a.autoChk.draw(screen, marginX, bottomY, "开机自动启动服务端(登录时后台运行)")
	a.drawFooter(screen)
}

// drawHeader 绘制标题区: LOGO、副标题与运行状态点。
func (a *app) drawHeader(screen *ebiten.Image) {
	drawStr(screen, "LANRD", faceTitle, marginX, headerY, colCyan)
	subX := marginX + strWidth("LANRD", faceTitle) + 14
	drawStr(screen, "内网远程桌面 · 端口映射", faceSmall, subX, headerY+8, colTextDim)
	// 运行状态点
	on := a.serverOn || a.fwdOn
	c := colTextFaint
	if on {
		p := float32(pulse(a.tick, 0.05))
		c = lerpColor(dim(colGreen, 120), colGreen, p)
		if a.serverOn && a.fwdOn {
			c = lerpColor(dim(colCyan, 120), colCyan, p)
		}
	}
	cx := float32(winW - marginX - 66)
	glowRect(screen, cx-4, headerY+5, 12, 12, 6, 2.5, c)
	fillRoundRect(screen, cx, headerY+9, 8, 8, 4, c)
	label := "OFFLINE"
	if on {
		label = "ONLINE"
	}
	drawStr(screen, label, faceMono, cx+18, headerY+7,
		lerpColor(colTextFaint, colText, boolF(on)))
}

// drawTabs 绘制顶部 Tab: 激活项文字高亮 + 发光下划线。
func (a *app) drawTabs(screen *ebiten.Image) {
	for i := 0; i < 2; i++ {
		tx := tabX(i)
		active := a.page == i
		tc := lerpColor(colTextDim, colText, boolF(active))
		if active {
			tc = colCyan
		}
		drawStr(screen, tabNames[i], faceText, tx, tabY, tc)
		if active {
			w := strWidth(tabNames[i], faceText)
			hline(screen, tx, tabY+26, w, colCyan)
		}
	}
	// Tab 分隔线
	fillRect(screen, marginX, tabY+30, winW-2*marginX, 1, color.RGBA{140, 158, 182, 120})
}

// drawFooter 绘制底部版本信息。
func (a *app) drawFooter(screen *ebiten.Image) {
	s := "lanrd v0.2.0"
	drawStr(screen, s, faceSmall,
		float32(winW-marginX)-strWidth(s, faceSmall), float32(bottomY), colTextFaint)
}
