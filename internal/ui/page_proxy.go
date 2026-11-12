//go:build windows

package ui

import (
	"strconv"

	"github.com/hajimehoshi/ebiten/v2"

	"lanrd/internal/proxy"
)

// 页面2(内网代理)布局。
const (
	pxListY = 110
	pxListH = 126
	pxAddY  = 252
	pxAddH  = 30
	pxBtnY  = 296
	pxBtnH  = 34
	pxStatY = 344
)

// updateProxy 处理「内网代理」页交互。
func (a *app) updateProxy(mx, my float32) {
	w := float32(winW - 2*marginX)
	// 列表: 勾选切换即时生效(保存 + 运行中重启)
	if row := a.list.update(marginX, pxListY, w, pxListH, len(a.maps)); row >= 0 && row < len(a.maps) {
		a.maps[row].Enabled = !a.maps[row].Enabled
		if err := a.saveUIConfig(); err != nil {
			a.setFwdStatus("保存配置失败: %v", err)
		}
		if a.fwdOn {
			a.restartFwd()
		}
	}
	a.newLocalIn.update(marginX, pxAddY, 92, pxAddH)
	a.newRemoteIn.update(marginX+104, pxAddY, w-104-104, pxAddH)
	if a.addBtn.update(marginX+w-96, pxAddY, 96, pxAddH) {
		a.addMapFromInputs()
	}
	if a.delBtn.update(marginX, pxBtnY, 170, pxBtnH) {
		a.delSelectedMap()
	}
	if a.fwdBtn.update(marginX+182, pxBtnY, 220, pxBtnH) {
		a.toggleFwd()
	}
}

// drawProxy 绘制「内网代理」页。
func (a *app) drawProxy(screen *ebiten.Image) {
	w := float32(winW - 2*marginX)

	// 列表标题行
	drawStr(screen, "启用", faceSmall, float32(marginX)+16, pxListY-16, colTextDim)
	drawStr(screen, "本地端口", faceSmall, float32(marginX)+42, pxListY-16, colTextDim)
	drawStr(screen, "远程地址 (ip:port)", faceSmall, float32(marginX)+136, pxListY-16, colTextDim)

	rows := make([]mapRow, len(a.maps))
	for i, m := range a.maps {
		rows[i] = mapRow{
			local: strconv.Itoa(m.Local), remote: m.Remote, enabled: m.Enabled,
		}
	}
	a.list.draw(screen, marginX, pxListY, w, pxListH, rows, a.tick)
	if len(a.maps) == 0 {
		drawStrCenter(screen, "列表为空，请在下方添加映射", faceSmall,
			marginX, pxListY, w, pxListH, colTextFaint)
	}

	// 添加行
	a.newLocalIn.draw(screen, marginX, pxAddY, 92, pxAddH, a.tick)
	a.newRemoteIn.draw(screen, marginX+104, pxAddY, w-104-104, pxAddH, a.tick)
	a.addBtn.draw(screen, marginX+w-96, pxAddY, 96, pxAddH)

	// 操作按钮
	a.delBtn.draw(screen, marginX, pxBtnY, 170, pxBtnH)
	a.fwdBtn.draw(screen, marginX+182, pxBtnY, 220, pxBtnH)

	// 状态行
	a.mu.Lock()
	status := a.fwdStatus
	a.mu.Unlock()
	if status == "" {
		status = "未启用"
	}
	dotC := colTextFaint
	if a.fwdOn {
		dotC = lerpColor(dim(colGreen, 100), colGreen, float32(pulse(a.tick, 0.09)))
	}
	fillRoundRect(screen, marginX, pxStatY+5, 8, 8, 4, dotC)
	if a.fwdOn {
		glowRect(screen, marginX-4, pxStatY+1, 12, 12, 6, 2, dotC)
	}
	tc := colTextDim
	if a.fwdOn {
		tc = colGreen
	}
	drawStr(screen, status, faceMono, marginX+18, pxStatY+1, tc)
	drawStr(screen, "勾选=启用该条 · 运行中修改即时生效 · 监听 0.0.0.0 局域网可访问",
		faceSmall, marginX, pxStatY+24, colTextFaint)
}

// addMapFromInputs 校验添加行输入并把新映射追加到列表(默认启用)。
func (a *app) addMapFromInputs() {
	m := uiMap{Remote: a.newRemoteIn.text}
	m.Local, _ = strconv.Atoi(a.newLocalIn.text)
	if m.Local < 1 || m.Local > 65535 || m.Remote == "" {
		a.setFwdStatus("请填写本地端口(1~65535)与远程地址")
		return
	}
	if _, err := proxy.ParseForward(m.mapArg()); err != nil {
		a.setFwdStatus("映射无效: %v", err)
		return
	}
	for _, e := range a.maps {
		if e.Local == m.Local {
			a.setFwdStatus("本地端口 %d 已存在", m.Local)
			return
		}
	}
	m.Enabled = true
	a.maps = append(a.maps, m)
	a.list.scroll = 1 << 30 // 滚到底部显示新行
	a.newLocalIn.text, a.newLocalIn.cursor = "", 0
	a.newRemoteIn.text, a.newRemoteIn.cursor = "", 0
	if err := a.saveUIConfig(); err != nil {
		a.setFwdStatus("保存配置失败: %v", err)
	}
	if a.fwdOn {
		a.restartFwd()
	}
}

// delSelectedMap 删除列表中选中的映射行并保存。
func (a *app) delSelectedMap() {
	i := a.list.sel
	if i < 0 || i >= len(a.maps) {
		a.setFwdStatus("请先在列表中点击选择要删除的映射")
		return
	}
	a.maps = append(a.maps[:i], a.maps[i+1:]...)
	a.list.sel = -1
	if err := a.saveUIConfig(); err != nil {
		a.setFwdStatus("保存配置失败: %v", err)
	}
	if a.fwdOn {
		a.restartFwd()
	}
}
