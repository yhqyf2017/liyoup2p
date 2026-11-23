// LiYou P2P GUI —— Win7 专用薄壳版（lxn/walk，纯 Win32，无 WebView2 依赖）。
// 核心逻辑移植自 gui-wails 主 GUI：登录+记忆、映射/白名单热管理、启停、托盘、自启、日志。
// 必须用 Go 1.20.x 编译（Go 1.21+ 生成的二进制不支持 Win7）。
package main

import (
	"bytes"
	_ "embed"
	"fmt"
	"image/png"

	"github.com/lxn/walk"
)

// isRelease 构建期注入：正式包固定内置平台地址，登录页隐藏服务器地址输入，日志与配置脱敏。
// 构建：go build -ldflags "-s -w -H windowsgui -X main.isRelease=true -X main.appVersion=1.1.5"
var isRelease = "false"

// appVersion 版本号（构建期可注入；默认值与 build-win7.ps1 的 $Ver 保持一致，双保险）。
var appVersion = "1.1.10"

// iconPNG 应用图标 256px（winres 另以多尺寸 PNG 嵌入 exe 资源作为安装器/任务栏图标源）。
//
//go:embed assets/appicon256.png
var iconPNG []byte

// appIcon 从内嵌 PNG 构造图标（纯 GDI 路径，不依赖 comctl32 v6 的
// LoadIconWithScaleDown——walk 的资源/文件图标加载在部分环境会失败）。
func appIcon() (*walk.Icon, error) {
	im, err := png.Decode(bytes.NewReader(iconPNG))
	if err != nil {
		return nil, err
	}
	return walk.NewIconFromImage(im)
}

func main() {
	// 单实例：已有实例则唤起其主窗口后退出（与主 GUI 共用同一互斥体名，防双开双 GUI）
	if !acquireSingleInstance() {
		activateFirstInstance()
		return
	}
	app := NewApp()
	if err := runUI(app); err != nil {
		fmt.Println("启动失败:", err.Error())
	}
}
