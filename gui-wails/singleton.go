//go:build windows

// 自研单实例（替换 wails SingleInstanceLock）：
// wails 的单实例通知用阻塞 SendMessage——首实例主线程挂死时，所有后续启动
// 会永久卡死在 SendMessage 上，成为无窗口、无托盘的僵尸进程（用户观感即
// 「点什么都不响应」）。
// 本实现三重保障：
//  1. 自有互斥锁识别首/次实例，与 wails 内部逻辑完全解耦；
//  2. 次实例用 SendMessageTimeout 通知首实例，超时上限 3s，永不无限阻塞；
//  3. 探测到首实例挂死时强杀（含子进程）并自动接管，保证用户随时能拉起界面。
package main

import (
	"encoding/json"
	"os"
	"os/exec"
	goruntime "runtime"
	"syscall"
	"time"
	"unsafe"
)

const (
	siMutexName      = "LiYouP2P-GUI-SingleInstance"
	siWndClass       = "LiYouP2PSINotifyWnd"
	siCopyDataMagic  = 0x4C50324B // 自定义 dwData 魔数："LP2K"
	siProbeTimeoutMs = 3000       // 探测首实例消息泵超时
	siMutexWaitTotal = 8 * time.Second
	siMutexWaitStep  = 200 * time.Millisecond
	smtoAbortIfHung  = 0x0002
)

var (
	kernel32SI              = syscall.NewLazyDLL("kernel32.dll")
	procCreateMutexW        = kernel32SI.NewProc("CreateMutexW")
	procSendMessageTimeoutW = user32.NewProc("SendMessageTimeoutW")
	procFindWindowExW       = user32.NewProc("FindWindowExW")
	procEnumWindowsSI       = user32.NewProc("EnumWindows")
	procGetClassNameSI      = user32.NewProc("GetClassNameW")
	procGetWindowPidSI      = user32.NewProc("GetWindowThreadProcessId")
	procGetCurrentPid       = kernel32SI.NewProc("GetCurrentProcessId")
)

var siApp *App // 次实例消息回调所需的 App 引用

// setupSingleInstance 单实例入口：main() 在 wails.Run 之前调用（同步，最长约 30s）。
func setupSingleInstance(app *App) {
	siApp = app
	for attempt := 0; attempt < 3; attempt++ {
		if siAcquireMutex() {
			go siRunEventWindow()
			return
		}
		// 已有实例：先尝试正常唤起
		if siNotifyFirst() {
			os.Exit(0)
		}
		// 唤起失败：首实例挂死 → 强杀（含 Agent 子进程）后重试拿锁
		if pid := siFirstPid(); pid != 0 {
			trayErrf("单实例：已有实例无响应(pid=%d)，自动结束并接管", pid)
			killProcessTree(pid)
		}
		if siWaitMutexFree() {
			continue
		}
		trayErrf("单实例：旧实例锁未释放，强制按首实例启动")
		go siRunEventWindow()
		return
	}
	trayErrf("单实例：多次尝试失败，按首实例启动")
	go siRunEventWindow()
}

// siAcquireMutex 尝试持有单实例互斥锁（句柄保持打开直至进程退出，无需显式释放）。
func siAcquireMutex() bool {
	name, _ := syscall.UTF16PtrFromString(siMutexName)
	h, _, err := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if h == 0 {
		trayErrf("单实例：创建互斥锁失败 err=%v", err)
		return false
	}
	// CreateMutex 成功但 GetLastError==ERROR_ALREADY_EXISTS：锁被别人持有
	if err == syscall.ERROR_ALREADY_EXISTS {
		return false // h 保持打开无碍：内核对象仍由持有者维护
	}
	return true
}

// siWaitMutexFree 等待旧实例的互斥锁随进程终止释放。
func siWaitMutexFree() bool {
	deadline := time.Now().Add(siMutexWaitTotal)
	for time.Now().Before(deadline) {
		if siAcquireMutex() {
			return true
		}
		time.Sleep(siMutexWaitStep)
	}
	return false
}

// siNotifyFirst 通知首实例唤起主窗口（携带本进程命令行参数，支持 --check-update）。
// 返回 false 表示首实例无响应或找不到。
func siNotifyFirst() bool {
	hwnd := siFindFirstWindow()
	if hwnd == 0 {
		return false
	}
	payload, _ := json.Marshal(os.Args[1:])
	arr, _ := syscall.UTF16FromString("") // 占位，WM_COPYDATA 用字节缓冲
	_ = arr
	type copyData struct {
		dwData uintptr
		cbData uint32
		lpData uintptr
	}
	cd := copyData{dwData: siCopyDataMagic, cbData: uint32(len(payload)), lpData: uintptr(unsafe.Pointer(&payload[0]))}
	var res uintptr
	ret, _, _ := procSendMessageTimeoutW.Call(
		hwnd, 0x004A /*WM_COPYDATA*/, 0, uintptr(unsafe.Pointer(&cd)),
		smtoAbortIfHung, siProbeTimeoutMs, uintptr(unsafe.Pointer(&res)),
	)
	return ret != 0
}

// siFindFirstWindow 查找首实例的 SI 通知窗口（限定本机任意实例即可）。
func siFindFirstWindow() uintptr {
	name, _ := syscall.UTF16PtrFromString(siWndClass)
	h, _, _ := procFindWindowExW.Call(0, 0, uintptr(unsafe.Pointer(name)), 0)
	return h
}

// siFirstPid 取首实例 pid。
func siFirstPid() uint32 {
	hwnd := siFindFirstWindow()
	if hwnd == 0 {
		return 0
	}
	var pid uint32
	procGetWindowPidSI.Call(hwnd, uintptr(unsafe.Pointer(&pid)))
	return pid
}

// siWndProc SI 通知窗口过程：接收次实例的 WM_COPYDATA（唤起/检查更新）。
// 数据缓冲仅在调用期间有效，必须在过程内同步消费。
func siWndProc(hwnd syscall.Handle, msg uint32, wp, lp uintptr) uintptr {
	if msg == 0x004A { // WM_COPYDATA
		type copyData struct {
			dwData uintptr
			cbData uint32
			lpData uintptr
		}
		if lp != 0 {
			cd := (*copyData)(unsafe.Pointer(lp))
			if cd.dwData == siCopyDataMagic && cd.cbData > 0 && cd.lpData != 0 {
				var args []string
				if err := json.Unmarshal(unsafe.Slice((*byte)(unsafe.Pointer(cd.lpData)), cd.cbData), &args); err == nil {
					winTrace("si: 收到次实例通知 args=%v", args)
					isUpdate := false
					for _, a := range args {
						if a == "--check-update" {
							isUpdate = true
						}
					}
					if isUpdate {
						go siApp.autoCheckUpdate()
					} else {
						siApp.showMainWindow()
					}
				}
			}
		}
		return 1 // 已处理
	}
	if msg == 0x0002 { // WM_DESTROY
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wp, lp)
	return ret
}

var siWndProcCb = syscall.NewCallback(siWndProc)

// siRunEventWindow 创建 SI 通知窗口并跑消息循环（专属 OS 线程，与托盘同模式）。
func siRunEventWindow() {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()

	className, _ := syscall.UTF16FromString(siWndClass)
	hInstance, _, _ := procGetModuleHandleW32.Call(0)

	type wndClassEx struct {
		Size       uint32
		Style      uint32
		WndProc    uintptr
		ClsExtra   int32
		WndExtra   int32
		Instance   syscall.Handle
		Icon       syscall.Handle
		Cursor     syscall.Handle
		Background syscall.Handle
		MenuName   *uint16
		ClassName  *uint16
		IconSm     syscall.Handle
	}
	wc := &wndClassEx{
		Size:      uint32(unsafe.Sizeof(wndClassEx{})),
		WndProc:   siWndProcCb,
		Instance:  syscall.Handle(hInstance),
		ClassName: &className[0],
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(wc))); r == 0 {
		trayErrf("SI 窗口类注册失败 err=%v", err)
		return
	}
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(&className[0])), uintptr(unsafe.Pointer(&className[0])),
		0, 0, 0, 0, 0,
		hwndMessage, 0, hInstance, 0,
	)
	if hwnd == 0 {
		trayErrf("SI 通知窗口创建失败 err=%v", err)
		return
	}
	trayErrf("SI 通知窗口就绪 hwnd=%d", hwnd)

	var m trayMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(ret) <= 0 {
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// killProcessTree 强制结束进程及其全部子进程（Agent 子进程一并回收）。
func killProcessTree(pid uint32) {
	cmd := exec.Command("taskkill", "/PID", itoa(int(pid)), "/T", "/F")
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true}
	if err := cmd.Run(); err != nil {
		trayErrf("taskkill pid=%d 失败 err=%v", pid, err)
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	return string(b[i:])
}
