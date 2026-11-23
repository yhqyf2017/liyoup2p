//go:build windows

package main

// Windows 平台窗口服务：显隐串行化 + 托盘常驻（从 app.go 抽出的 Windows 专属实现）。

import "sync"

// ---------- 窗口显隐串行化 ----------
// 根因（最小化/托盘唤回偶发整体无响应）：
//   1) 前端最小化按钮走 JS WindowMinimise → 跨线程 ShowWindow(SW_MINIMIZE) 真·最小化，
//      触发 WebView2 最小化/还原缩放周期（主线程同步 COM 调用，快速反复时易卡死）；
//   2) 托盘唤回走 wails WindowUnminimise → Form.Restore() 内部是跨线程阻塞
//      SendMessage(WM_SYSCOMMAND, SC_RESTORE)，与最小化交错时互相等待 → 主线程
//      消息泵停摆（IsHungAppWindow=True，实测 >2min 不自愈）。
//   修复（双管齐下，消除全部阻塞调用）：
//   a) 最小化改为「隐藏到托盘」（前端 winMin → App.HideWindow），不再真·最小化，
//      彻底绕开 WebView2 缩放周期；
//   b) 显隐统一改用 ShowWindowAsync（把显示命令投递到主线程消息队列，永不阻塞），
//      不再使用 wails runtime 的跨线程同步 SendMessage；快速连点经队列去抖合并。
type winOp int

const (
	winOpHide winOp = iota
	winOpShow
)

var (
	winOpCh  = make(chan winOp, 16)
	mainHwnd = struct {
		mu sync.Mutex
		h  uintptr
	}{}
)

// requestWinOp 非阻塞提交显隐操作（任意线程/窗口过程内可安全调用，永不阻塞）。
func requestWinOp(op winOp) {
	select {
	case winOpCh <- op:
	default:
		// 队列满：丢弃，最后一次操作会覆盖到目标状态
	}
}

// runWinOpLoop 显隐操作唯一执行点：去抖合并（吸干队列只执行最后一个操作），
// 全部经由 ShowWindowAsync 投递到主线程消息队列，非阻塞、不重入、不并发。
func runWinOpLoop(a *App) {
	for op := range winOpCh {
		last := op
	drain:
		for {
			select {
			case o := <-winOpCh:
				last = o
			default:
				break drain
			}
		}
		mainHwnd.mu.Lock()
		h := mainHwnd.h
		mainHwnd.mu.Unlock()
		if h == 0 {
			h = mainWindowHwnd()
			if h == 0 {
				continue
			}
			mainHwnd.mu.Lock()
			mainHwnd.h = h
			mainHwnd.mu.Unlock()
		}
		switch last {
		case winOpHide:
			winTrace("winop: 执行隐藏 hwnd=0x%x", h)
			a.balloonOnce.Do(func() {
				trayBalloon("励联 LiLink", "已最小化到托盘，后台保持连接")
			})
			procShowWindowAsync.Call(h, swHide)
		case winOpShow:
			winTrace("winop: 执行显示 hwnd=0x%x", h)
			procShowWindowAsync.Call(h, swRestore)
			foregroundMainWindow()
		}
	}
}

func (a *App) hideToTray() {
	requestWinOp(winOpHide)
}

// HideWindow 隐藏主窗口到托盘（前端最小化按钮绑定调用）。
func (a *App) HideWindow() {
	requestWinOp(winOpHide)
}

// showMainWindow 显示并前置主窗口（托盘双击/菜单、二次启动唤起共用）。
// 仅投递请求，实际显隐由 runWinOpLoop 串行执行——窗口/托盘过程内不得同步调 wails runtime。
func (a *App) showMainWindow() {
	if a.ctx == nil {
		return
	}
	requestWinOp(winOpShow)
}

// startWindowServices 启动窗口服务：显隐串行消费循环 + 托盘常驻。
func startWindowServices(a *App) {
	go runWinOpLoop(a)
	startTray(a)
}
