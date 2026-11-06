//go:build windows

// 主窗口钩子：通过 SetWindowLongPtrW 子类化 Wails 主窗口，
// 拦截「最小化」系统命令改为隐藏到托盘（Wails v2 无原生最小化钩子）。
package main

import (
	"syscall"
	"time"
	"unsafe"
)

const (
	gwlpWndProc  = ^uintptr(3) // GWLP_WNDPROC = -4（x64 两补码）
	wmSyscommand = 0x0112
	scMinimize   = 0xF020
	wmClose      = 0x0010
	scMask       = 0xFFF0
)

var (
	procFindWindowW      = user32.NewProc("FindWindowW")
	procSetWindowLongPtr = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProc   = user32.NewProc("CallWindowProcW")
	procIsWindowVisible  = user32.NewProc("IsWindowVisible")
)

// subClassState 子类化后的原窗口过程与回调（包级保存，回调必须存活于进程生命周期）。
var subClassState struct {
	prevWndProc uintptr
	callback    uintptr
}

// installWindowHook 等待 Wails 主窗口创建完成后子类化（重试直到找到，上限 30s）。
func installWindowHook(app *App) {
	var hwnd uintptr
	for i := 0; i < 600; i++ {
		name, _ := syscall.UTF16PtrFromString("wailsWindow") // Wails v2 主窗口固定类名
		h, _, _ := procFindWindowW.Call(uintptr(unsafe.Pointer(name)), 0)
		if h != 0 {
			hwnd = h
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if hwnd == 0 {
		trayErrf("未找到主窗口，最小化隐藏托盘不生效")
		return
	}

	subClassState.callback = syscall.NewCallback(func(hwnd syscall.Handle, msg uint32, wp, lp uintptr) uintptr {
		// 最小化系统命令 → 隐藏到托盘（不执行真正最小化）。
		// 窗口过程内不得做任何可能阻塞的调用：hideToTray 里的 Shell_NotifyIconW 是
		// 跨进程交互（Explorer 忙时可能阻塞），放 goroutine 执行，窗口过程立即返回，
		// 避免「偶发窗口未响应」。
		if msg == wmSyscommand && wp&scMask == scMinimize {
			go app.hideToTray()
			return 0
		}
		ret, _, _ := procCallWindowProc.Call(subClassState.prevWndProc, uintptr(hwnd), uintptr(msg), wp, lp)
		return ret
	})

	prev, _, _ := procSetWindowLongPtr.Call(hwnd, gwlpWndProc, subClassState.callback)
	subClassState.prevWndProc = prev
	app.logf("info", "已启用托盘常驻：关闭或最小化窗口后程序转入后台运行")
}
