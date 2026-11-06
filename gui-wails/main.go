// LiYou P2P GUI：Wails v2 桌面端（Go 后端 + 内嵌静态前端，单 exe）。
// 支持托盘常驻：关闭/最小化窗口后转入后台运行，从托盘恢复或真正退出。
package main

import (
	"embed"
	"os"
	"os/exec"
	"strings"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"
)

//go:embed all:frontend/dist
var assets embed.FS

// isRelease 构建期注入：正式包用 wails build -ldflags "-s -w -H windowsgui -X main.isRelease=true"。
// 正式包登录/信令固定使用内置平台地址，登录页隐藏服务器地址输入，日志与配置脱敏。
var isRelease = "false"

func main() {
	app := NewApp()
	err := wails.Run(&options.App{
		Title:     "励联 LiLink",
		Width:     960,
		Height:    700,
		MinWidth:  900,
		MinHeight: 640,
		Frameless: true, // 无边框窗口：由前端绘制玻璃材质自定义标题栏（拖拽/最小化/关闭）；v2 默认保留边缘拉伸，无最大化入口
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 244, G: 247, B: 251, A: 255},
		OnStartup:        app.startup,
		OnBeforeClose:    app.beforeClose,
		// 单实例：重复启动时唤起已有窗口并退出新进程；带 --check-update 参数则触发自动升级检查
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "liyou-p2p-gui-9f3c2b7a",
			OnSecondInstanceLaunch: func(d options.SecondInstanceData) {
				for _, arg := range d.Args {
					if strings.EqualFold(strings.TrimSpace(arg), "--check-update") {
						go app.autoCheckUpdate()
						return
					}
				}
				app.showMainWindow()
			},
		},
		Bind: []interface{}{
			app,
		},
		Windows: &windows.Options{
			Theme:                windows.SystemDefault,
			WebviewIsTransparent: false,
			WindowIsTranslucent:  false,
			DisableWindowIcon:    false,
		},
	})
	if err != nil {
		println("启动失败:", err.Error())
	}
	// 兜底：确保托盘图标移除、进程真正结束（托盘退出路径已先停 Agent）
	trayShutdown()
	tray = nil
	// 升级重启：进程退出后再拉起新版主程序（此时单实例锁已释放，规避竞态）
	if app.restartPending {
		if exe, err := os.Executable(); err == nil {
			if err := exec.Command(exe).Start(); err != nil {
				println("新版程序启动失败:", err.Error())
			}
		}
	}
}
