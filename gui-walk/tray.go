package main

// 托盘（walk NotifyIcon）：菜单 = 显示主窗口 / 启停连接（随状态换文案）/ 退出；
// 双击唤回主窗口；首次隐藏到托盘弹气泡提示（ui.go hideUI 回调内实现）。

import (
	"github.com/lxn/walk"
)

// setupTray 创建托盘图标与菜单（须在主窗口 Create 之后调用）。
func setupTray(ui *uiState) error {
	ni, err := walk.NewNotifyIcon(ui.mw)
	if err != nil {
		return err
	}
	icon, err := appIcon()
	if err != nil {
		ni.Dispose()
		return err
	}
	ni.SetIcon(icon)
	ni.SetToolTip(siWndTitle)

	// 显示主窗口
	actShow := walk.NewAction()
	actShow.SetText("显示主窗口")
	actShow.Triggered().Attach(func() { ui.app.showMainWindow() })
	_ = ni.ContextMenu().Actions().Add(actShow)

	// 启动·停止连接（文案随状态切换，见 ui.go onState 回调）
	actToggle := walk.NewAction()
	if ui.app.IsRunning() {
		actToggle.SetText("停止连接")
	} else {
		actToggle.SetText("启动连接")
	}
	actToggle.Triggered().Attach(func() { go ui.app.toggleConnection() })
	_ = ni.ContextMenu().Actions().Add(actToggle)
	ui.trayToggle = actToggle

	_ = ni.ContextMenu().Actions().Add(walk.NewSeparatorAction())

	// 检查更新：手动触发 win7 通道版本检查（有新版则下载静默安装）
	actUpdate := walk.NewAction()
	actUpdate.SetText("检查更新")
	actUpdate.Triggered().Attach(func() { go ui.CheckUpdateManual() })
	_ = ni.ContextMenu().Actions().Add(actUpdate)

	_ = ni.ContextMenu().Actions().Add(walk.NewSeparatorAction())

	// 退出：停 Agent + 保存会话 + 真退出（对齐主 GUI beforeClose 语义）
	actExit := walk.NewAction()
	actExit.SetText("退出")
	actExit.Triggered().Attach(func() { ui.exitApp() })
	_ = ni.ContextMenu().Actions().Add(actExit)

	// 单击（左键）唤回主窗口（此版 walk NotifyIcon 无双击事件）
	ni.MouseUp().Attach(func(x, y int, button walk.MouseButton) {
		if button == walk.LeftButton {
			ui.app.showMainWindow()
		}
	})

	ni.SetVisible(true)
	ui.ni = ni
	return nil
}
