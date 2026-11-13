package main

// 单实例守卫（简化自 gui-wails/singleton.go）：命名互斥体识别首/次实例；
// 次实例直接唤回首实例主窗口（ShowWindow+SetForegroundWindow，不依赖消息钩子，永不阻塞）。

import (
	"syscall"
	"time"
	"unsafe"

	"github.com/lxn/win"
)

const (
	siMutexName    = "LiYouP2P-GUI-SingleInstance"
	siWndTitle     = "励联 LiLink"
	siNotifyRetry  = 20 // 首实例窗口尚未就绪时重试次数
	siNotifyStepMs = 300
)

var (
	kernel32        = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutex = kernel32.NewProc("CreateMutexW")
)

// acquireSingleInstance 尝试持有单实例互斥锁；返回 false 表示已有实例在运行。
func acquireSingleInstance() bool {
	name, _ := syscall.UTF16PtrFromString(siMutexName)
	h, _, err := procCreateMutex.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		return true // 创建失败按首实例放行，避免互斥体异常导致完全无法启动
	}
	return err != syscall.ERROR_ALREADY_EXISTS
}

// activateFirstInstance 唤回首实例主窗口（按窗口标题查找）后退出。
func activateFirstInstance() {
	title, _ := syscall.UTF16PtrFromString(siWndTitle)
	for i := 0; i < siNotifyRetry; i++ {
		if hwnd := win.FindWindow(nil, title); hwnd != 0 {
			win.ShowWindow(hwnd, win.SW_RESTORE)
			win.SetForegroundWindow(hwnd)
			return
		}
		time.Sleep(siNotifyStepMs * time.Millisecond)
	}
}
