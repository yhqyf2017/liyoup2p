//go:build darwin

package main

// macOS 平台窗口服务：v1 简化模型——无托盘、无看门狗、无单实例锁。
// 关闭/最小化窗口 → 隐藏（进程常驻，连接不中断）；唤回 → 显示窗口。
// wails runtime 调用内部会投递到主线程执行，任意 goroutine 调用安全。

import "github.com/wailsapp/wails/v2/pkg/runtime"

func (a *App) hideToTray() {
	if a.ctx != nil {
		runtime.WindowHide(a.ctx)
	}
}

// HideWindow 隐藏主窗口（前端最小化按钮绑定调用）。
func (a *App) HideWindow() {
	a.hideToTray()
}

// showMainWindow 显示主窗口（二次启动唤起/自动更新提示共用）。
func (a *App) showMainWindow() {
	if a.ctx == nil {
		return
	}
	runtime.WindowShow(a.ctx)
}

// startWindowServices macOS 无托盘/看门狗，无需启动额外窗口服务。
func startWindowServices(a *App) {}

// winTrace macOS 无看门狗，窗口日志转写日志面板（仅供排障）。
func winTrace(format string, v ...any) {}
