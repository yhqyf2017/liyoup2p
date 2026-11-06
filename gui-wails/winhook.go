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

	swHide    = 0 // ShowWindowAsync：隐藏窗口
	swRestore = 9 // ShowWindowAsync：还原/显示窗口
)

var (
	procFindWindowW         = user32.NewProc("FindWindowW")
	procSetWindowLongPtr    = user32.NewProc("SetWindowLongPtrW")
	procCallWindowProc      = user32.NewProc("CallWindowProcW")
	procIsWindowVisible     = user32.NewProc("IsWindowVisible")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procShowWindowAsync     = user32.NewProc("ShowWindowAsync")
	procIsHungAppWindow     = user32.NewProc("IsHungAppWindow")
	procEnumWindows         = user32.NewProc("EnumWindows")
	procGetClassNameW       = user32.NewProc("GetClassNameW")
	procGetWindowThreadPid  = user32.NewProc("GetWindowThreadProcessId")
)

// mainWindowHwnd 查找 Wails 主窗口句柄（重试上限约 30s，找不到返回 0）。
// 限定本进程 pid：机器上可能残留其他实例（含挂死僵尸）的同名窗口，
// 找错对象会导致显隐操作作用于别人的窗口（表现为「点了托盘没反应」）。
func mainWindowHwnd() uintptr {
	for i := 0; i < 600; i++ {
		if h := findOwnWailsWindow(); h != 0 {
			return h
		}
		time.Sleep(50 * time.Millisecond)
	}
	return 0
}

// findOwnWailsWindow 枚举顶层窗口，取本进程的 wailsWindow。
func findOwnWailsWindow() uintptr {
	selfPid, _, _ := procGetCurrentPid.Call()
	var found uintptr
	cb := syscall.NewCallback(func(hwnd syscall.Handle, lp uintptr) uintptr {
		var buf [64]uint16
		n, _, _ := procGetClassNameW.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&buf[0])), 64)
		if n == 0 {
			return 1
		}
		name := syscall.UTF16ToString(buf[:n])
		if name != "wailsWindow" {
			return 1
		}
		var pid uint32
		procGetWindowThreadPid.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&pid)))
		if uintptr(pid) != selfPid {
			return 1
		}
		found = uintptr(hwnd)
		return 0 // 找到即停止枚举
	})
	procEnumWindows.Call(cb, 0)
	return found
}

// foregroundMainWindow 将主窗口强制置前台（唤回后焦点归属问题的兜底）。
// 限定本进程 pid，避免误置其他实例（含挂死僵尸）的同名窗口。
func foregroundMainWindow() {
	h := findOwnWailsWindow()
	if h != 0 {
		procSetForegroundWindow.Call(h)
	}
}

// subClassState 子类化后的原窗口过程与回调（包级保存，回调必须存活于进程生命周期）。
var subClassState struct {
	prevWndProc uintptr
	callback    uintptr
}

// installWindowHook 等待 Wails 主窗口创建完成后子类化（重试直到找到，上限 30s）。
func installWindowHook(app *App) {
	hwnd := mainWindowHwnd()
	if hwnd == 0 {
		trayErrf("未找到主窗口，最小化隐藏托盘不生效")
		return
	}

	subClassState.callback = syscall.NewCallback(func(hwnd syscall.Handle, msg uint32, wp, lp uintptr) uintptr {
		// 最小化系统命令 → 隐藏到托盘（不执行真正最小化）。
		// 窗口过程内只做非阻塞投递（requestWinOp），任何跨线程/跨进程/同步等待
		// 都可能造成窗口未响应，实际显隐由 runWinOpLoop 串行 goroutine 执行。
		if msg == wmSyscommand && wp&scMask == scMinimize {
			winTrace("hook: SC_MINIMIZE → 投递隐藏")
			requestWinOp(winOpHide)
			return 0
		}
		if msg == wmSyscommand || msg == wmClose {
			winTrace("hook: msg=0x%04x wp=0x%x", msg, wp)
		}
		ret, _, _ := procCallWindowProc.Call(subClassState.prevWndProc, uintptr(hwnd), uintptr(msg), wp, lp)
		return ret
	})

	prev, _, _ := procSetWindowLongPtr.Call(hwnd, gwlpWndProc, subClassState.callback)
	subClassState.prevWndProc = prev
	app.logf("info", "已启用托盘常驻：关闭或最小化窗口后程序转入后台运行")
}
