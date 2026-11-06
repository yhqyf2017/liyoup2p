//go:build windows

// Windows 托盘常驻：手写 Shell_NotifyIcon + win32 syscall（免第三方依赖）。
// 提供：托盘图标（内嵌 ico）、右键菜单（显示主窗口/启动·停止连接/退出）、
// 双击恢复窗口、首次隐藏气泡提示、连接状态同步（菜单项与悬浮提示动态更新）。
package main

import (
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	goruntime "runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

//go:embed trayicon.ico
var trayIconData []byte

const (
	trayCallbackMsg = 0x8100 // 托盘回调消息（WM_APP 区段）
	wmTrayShow      = 0x8101 // 跨线程请求：显示主窗口
	wmTrayToggle    = 0x8102 // 跨线程请求：启动/停止连接（测试与程序化触发用）
	wmTrayQuit      = 0x8103 // 跨线程请求：真正退出
	wmTrayUpdate    = 0x8104 // 跨线程请求：检查更新

	menuShow     = 2001 // 托盘菜单 ID：显示主窗口
	menuToggle   = 2002 // 托盘菜单 ID：启动/停止连接
	menuQuit     = 2003 // 托盘菜单 ID：退出
	menuUpdate   = 2004 // 托盘菜单 ID：检查更新
	menuSepFirst = 2005 // 分隔线占位 ID

	nimAdd    = 0
	nimModify = 1
	nimDelete = 2

	nifMessage = 0x01
	nifIcon    = 0x02
	nifTip     = 0x04
	nifInfo    = 0x10

	niifInfo = 0x01 // 气泡带信息图标

	imageIcon      = 1
	lrLoadFromFile = 0x10
	lrDefaultSize  = 0x40

	mfString    = 0x00
	mfSeparator = 0x800

	tpmRightButton = 0x02
	tpmNotify      = 0x00
	tpmReturnCmd   = 0x0100
	tpmNonNotify   = 0x0080

	wmLButtonDblClk = 0x0203
	wmRButtonUp     = 0x0205
	wmNull          = 0x0000

	hwndMessage = ^uintptr(2) // HWND_MESSAGE = (HWND)-3
)

var (
	user32                 = syscall.NewLazyDLL("user32.dll")
	shell32                = syscall.NewLazyDLL("shell32.dll")
	procRegisterClassExW   = user32.NewProc("RegisterClassExW")
	procCreateWindowExW    = user32.NewProc("CreateWindowExW")
	procDefWindowProcW     = user32.NewProc("DefWindowProcW")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostMessageW       = user32.NewProc("PostMessageW")
	procLoadImageW         = user32.NewProc("LoadImageW")
	procDestroyIcon        = user32.NewProc("DestroyIcon")
	procCreatePopupMenu    = user32.NewProc("CreatePopupMenu")
	procAppendMenuW        = user32.NewProc("AppendMenuW")
	procModifyMenuW        = user32.NewProc("ModifyMenuW")
	procTrackPopupMenuEx   = user32.NewProc("TrackPopupMenuEx")
	procDestroyMenu        = user32.NewProc("DestroyMenu")
	procGetCursorPos       = user32.NewProc("GetCursorPos")
	procSetForegroundWnd   = user32.NewProc("SetForegroundWindow")
	procShellNotifyIconW   = shell32.NewProc("Shell_NotifyIconW")
	procGetModuleHandleW32 = kernel32Tray.NewProc("GetModuleHandleW")
)

var kernel32Tray = syscall.NewLazyDLL("kernel32.dll")

type notifyIconData struct {
	CbSize           uint32
	HWnd             syscall.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	HIcon            syscall.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         syscall.GUID
	HBalloonIcon     syscall.Handle
}

type trayWndClassEx struct {
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

type trayPoint struct {
	X, Y int32
}

type trayMsg struct {
	HWnd     syscall.Handle
	Message  uint32
	WParam   uintptr
	LParam   uintptr
	Time     uint32
	Pt       trayPoint
	LPrivate int32
}

// trayManager 托盘单例：消息窗口与菜单在其专属 goroutine（OS 线程）上创建与轮询。
type trayManager struct {
	app     *App
	mu      sync.Mutex
	hwnd    syscall.Handle
	hMenu   uintptr
	hIcon   syscall.Handle
	nid     notifyIconData
	running bool
	ready   bool
}

var tray *trayManager

// trayErrf 托盘错误写入日志文件（windowsgui 无控制台，stderr 不可见）。
func trayErrf(format string, args ...interface{}) {
	msg := fmt.Sprintf(format, args...)
	dir := os.Getenv("ProgramData")
	if dir == "" {
		return
	}
	path := filepath.Join(dir, "LiYouP2P", "tray.log")
	_ = os.MkdirAll(filepath.Dir(path), 0755)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), msg)
}

// startTray 启动托盘 goroutine（失败仅记录，不影响主程序）。
func startTray(app *App) {
	tray = &trayManager{app: app}
	go tray.run()
}

// traySetRunning 同步连接状态到托盘（菜单文案 + 悬浮提示）。
func traySetRunning(running bool) {
	if tray == nil {
		return
	}
	tray.setRunning(running)
}

// trayBalloon 显示托盘气泡通知。
func trayBalloon(title, text string) {
	if tray == nil {
		return
	}
	tray.balloon(title, text)
}

// trayShutdown 移除托盘图标并清理（进程退出前调用，可重复调用）。
func trayShutdown() {
	if tray == nil {
		return
	}
	tray.shutdown()
}

// trayPost 以消息方式跨线程触发托盘动作（自动化测试/程序化控制入口）。
func trayPost(msg uint32) {
	if tray == nil {
		return
	}
	tray.mu.Lock()
	hwnd := tray.hwnd
	tray.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(uintptr(hwnd), uintptr(msg), 0, 0)
	}
}

func (t *trayManager) run() {
	goruntime.LockOSThread()
	defer goruntime.UnlockOSThread()

	className, _ := syscall.UTF16FromString("LiYouTrayWnd")
	windowName, _ := syscall.UTF16FromString("LiYouTrayWnd")
	hInstance, _, _ := procGetModuleHandleW32.Call(0)

	wc := &trayWndClassEx{
		Size:      uint32(unsafe.Sizeof(trayWndClassEx{})),
		WndProc:   syscall.NewCallback(t.wndProc),
		Instance:  syscall.Handle(hInstance),
		ClassName: &className[0],
	}
	if r, _, err := procRegisterClassExW.Call(uintptr(unsafe.Pointer(wc))); r == 0 {
		trayErrf("托盘注册窗口类失败 err=%v", err)
		return
	}
	hwnd, _, err := procCreateWindowExW.Call(
		0,
		uintptr(unsafe.Pointer(&className[0])),
		uintptr(unsafe.Pointer(&windowName[0])),
		0, 0, 0, 0, 0,
		hwndMessage, 0, hInstance, 0,
	)
	if hwnd == 0 {
		trayErrf("托盘创建消息窗口失败 err=%v", err)
		return
	}

	hIcon := t.loadIcon()
	t.mu.Lock()
	t.hwnd = syscall.Handle(hwnd)
	t.hIcon = syscall.Handle(hIcon)
	t.nid = notifyIconData{
		CbSize:           uint32(unsafe.Sizeof(t.nid)),
		HWnd:             syscall.Handle(hwnd),
		UID:              1,
		UFlags:           nifMessage | nifIcon | nifTip,
		UCallbackMessage: trayCallbackMsg,
		HIcon:            syscall.Handle(hIcon),
	}
	setUTF16(t.nid.SzTip[:], "励联 LiLink")
	t.mu.Unlock()

	if r, _, err2 := procShellNotifyIconW.Call(nimAdd, uintptr(unsafe.Pointer(&t.nid))); r == 0 {
		trayErrf("托盘图标添加失败（Shell_NotifyIconW）err=%v", err2)
		return
	}

	t.mu.Lock()
	t.hMenu = t.buildMenuLocked()
	ready := t.hMenu != 0
	t.ready = ready
	running := t.running
	t.mu.Unlock()
	t.setRunning(running) // 初始化菜单文案与悬浮提示
	trayErrf("托盘就绪 hwnd=%d hMenu=%d hIcon=%d", hwnd, t.hMenu, hIcon)

	// 消息循环
	var m trayMsg
	for {
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		switch int32(ret) {
		case -1, 0:
			t.cleanup()
			return
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}

// wndProc 托盘消息窗口回调：处理托盘图标鼠标事件与跨线程控制消息。
func (t *trayManager) wndProc(hwnd syscall.Handle, msg uint32, wp, lp uintptr) uintptr {
	switch msg {
	case trayCallbackMsg:
		switch lp {
		case wmLButtonDblClk: // 双击托盘：显示主窗口
			t.app.showMainWindow()
		case wmRButtonUp: // 右键：弹出菜单
			t.showMenu(hwnd)
		}
		return 0
	case wmTrayShow:
		t.app.showMainWindow()
		return 0
	case wmTrayToggle:
		t.app.toggleConnection()
		return 0
	case wmTrayQuit:
		t.app.exitFromTray()
		return 0
	case wmTrayUpdate:
		go t.app.trayCheckUpdate()
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wp, lp)
	return ret
}

// buildMenuLocked 创建托盘右键菜单（仅托盘线程调用）。
func (t *trayManager) buildMenuLocked() uintptr {
	hMenu, _, err := procCreatePopupMenu.Call()
	if hMenu == 0 {
		trayErrf("托盘菜单创建失败 err=%v", err)
		return 0
	}
	show, _ := syscall.UTF16FromString("显示主窗口")
	procAppendMenuW.Call(hMenu, mfString, menuShow, uintptr(unsafe.Pointer(&show[0])))
	toggle, _ := syscall.UTF16FromString("停止连接")
	procAppendMenuW.Call(hMenu, mfString, menuToggle, uintptr(unsafe.Pointer(&toggle[0])))
	procAppendMenuW.Call(hMenu, mfSeparator, menuSepFirst, 0)
	update, _ := syscall.UTF16FromString("检查更新")
	procAppendMenuW.Call(hMenu, mfString, menuUpdate, uintptr(unsafe.Pointer(&update[0])))
	quit, _ := syscall.UTF16FromString("退出")
	procAppendMenuW.Call(hMenu, mfString, menuQuit, uintptr(unsafe.Pointer(&quit[0])))
	return hMenu
}

// showMenu 弹出托盘右键菜单（须先 SetForegroundWindow，否则菜单点击外部不消失）。
func (t *trayManager) showMenu(hwnd syscall.Handle) {
	t.mu.Lock()
	hMenu := t.hMenu
	ready := t.ready
	t.mu.Unlock()
	if !ready || hMenu == 0 {
		return
	}
	var pt trayPoint
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
	procSetForegroundWnd.Call(uintptr(hwnd))
	id, _, _ := procTrackPopupMenuEx.Call(
		hMenu,
		tpmReturnCmd|tpmRightButton|tpmNonNotify,
		uintptr(pt.X), uintptr(pt.Y), uintptr(hwnd), 0,
	)
	procPostMessageW.Call(uintptr(hwnd), wmNull, 0, 0)
	switch id {
	case menuShow:
		t.app.showMainWindow()
	case menuToggle:
		t.app.toggleConnection()
	case menuQuit:
		t.app.exitFromTray()
	case menuUpdate:
		go t.app.trayCheckUpdate()
	}
}

// setRunning 同步连接状态：切换菜单项文案并更新悬浮提示。
func (t *trayManager) setRunning(running bool) {
	t.mu.Lock()
	runningChanged := t.running != running
	t.running = running
	hMenu := t.hMenu
	t.mu.Unlock()
	if !runningChanged || hMenu == 0 {
		return
	}
	text := "停止连接"
	if !running {
		text = "启动连接"
	}
	label, _ := syscall.UTF16FromString(text)
	procModifyMenuW.Call(hMenu, menuToggle, mfString, menuToggle, uintptr(unsafe.Pointer(&label[0])))
	t.setTooltipLocked("励联 LiLink - " + map[bool]string{true: "连接运行中", false: "已停止"}[running])
}

// setTooltipLocked 更新托盘悬浮提示（NIM_MODIFY）。
func (t *trayManager) setTooltipLocked(tip string) {
	t.mu.Lock()
	if t.hwnd == 0 {
		t.mu.Unlock()
		return
	}
	nid := t.nid
	t.mu.Unlock()
	nid.UFlags = nifTip
	setUTF16(nid.SzTip[:], tip)
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// balloon 显示气泡通知（首次隐藏到托盘时提示用户后台仍在运行）。
func (t *trayManager) balloon(title, text string) {
	t.mu.Lock()
	if t.hwnd == 0 {
		t.mu.Unlock()
		return
	}
	nid := t.nid
	t.mu.Unlock()
	nid.UFlags = nifInfo
	nid.DwInfoFlags = niifInfo
	setUTF16(nid.SzInfo[:], text)
	setUTF16(nid.SzInfoTitle[:], title)
	procShellNotifyIconW.Call(nimModify, uintptr(unsafe.Pointer(&nid)))
}

// shutdown 移除托盘图标、销毁菜单与图标（幂等）。
func (t *trayManager) shutdown() {
	t.mu.Lock()
	hwnd, hMenu, hIcon := t.hwnd, t.hMenu, t.hIcon
	t.hwnd, t.hMenu, t.hIcon = 0, 0, 0
	t.mu.Unlock()
	if hwnd != 0 {
		t.nid.HWnd = hwnd
		procShellNotifyIconW.Call(nimDelete, uintptr(unsafe.Pointer(&t.nid)))
		procPostMessageW.Call(uintptr(hwnd), wmNull, 0, 0)
	}
	if hMenu != 0 {
		procDestroyMenu.Call(hMenu)
	}
	if hIcon != 0 {
		procDestroyIcon.Call(uintptr(hIcon))
	}
}

// cleanup 消息循环退出后的兜底清理。
func (t *trayManager) cleanup() {
	t.shutdown()
}

// loadIcon 从内嵌 ico 字节加载 HICON（落临时文件后 LoadImage，兼容多尺寸/PNG 压缩 ico）。
func (t *trayManager) loadIcon() uintptr {
	f, err := os.CreateTemp("", "liyou-tray-*.ico")
	if err != nil {
		return 0
	}
	path := f.Name()
	_, werr := f.Write(trayIconData)
	cerr := f.Close()
	if werr != nil || cerr != nil {
		os.Remove(path)
		return 0
	}
	defer os.Remove(path)
	p16, _ := syscall.UTF16PtrFromString(path)
	h, _, _ := procLoadImageW.Call(0, uintptr(unsafe.Pointer(p16)), imageIcon, 0, 0, lrLoadFromFile|lrDefaultSize)
	return h
}

// setUTF16 写入定长 UTF-16 数组（按字符截断，保证 NUL 结尾）。
func setUTF16(dst []uint16, s string) {
	runes := []rune(s)
	if len(runes) > len(dst)-1 {
		runes = runes[:len(dst)-1]
	}
	for i, r := range runes {
		dst[i] = uint16(r)
	}
	dst[len(runes)] = 0
}
