//go:build darwin

package main

// macOS 升级安装：v1 采用引导手动更新——解压校验包含新版 LiLink.app 后，
// 在访达中展示新版所在目录并提示用户拖入「应用程序」完成替换。
// 不做运行中自动替换：.app 受 Gatekeeper 签名约束，运行中的 app 也无法自替换。

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
)

// updateLogDir macOS 升级工作目录（安装包/日志）。
func updateLogDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, "LiYouP2P", "update")
}

// installUpdate 解压校验并引导用户手动完成更新（当前连接保持不断开）。
func (a *App) installUpdate(li *latestInfo, zipPath string) error {
	dir := filepath.Dir(zipPath)

	// 解压校验：必须包含 LiLink.app 才算有效升级包
	stage := filepath.Join(dir, "stage")
	_ = os.RemoveAll(stage)
	if err := extractZip(zipPath, stage); err != nil {
		a.logf("error", "安装包解压失败，已保留当前版本")
		updateFileLogf("unzip failed: %v", err)
		return errors.New("安装包解压失败，已保留当前版本")
	}
	if !hasAppBundle(stage) {
		a.logf("error", "安装包缺少应用程序文件，已放弃本次升级")
		updateFileLogf("zip missing LiLink.app")
		return errors.New("安装包内容不完整，已放弃本次升级")
	}

	// 在访达中展示新版所在目录，引导用户拖入「应用程序」替换
	if err := exec.Command("open", stage).Start(); err != nil {
		updateFileLogf("open stage failed: %v", err)
		a.logf("info", "新版本已下载完成（v%s）", li.Version)
		a.logf("warn", "未能自动打开下载目录，请在下载目录中找到新版本完成更新")
		return nil
	}
	updateFileLogf("darwin manual install: opened stage=%s target=%s", stage, li.Version)
	a.logf("info", "新版本已下载完成（v%s）", li.Version)
	a.logf("info", "请在弹出的窗口中，将「励联 LiLink」拖入「应用程序」文件夹完成更新")
	return nil
}

// hasAppBundle 检查 stage 目录（含子目录）中是否存在 LiLink.app 应用包目录。
func hasAppBundle(stage string) bool {
	found := false
	_ = filepath.Walk(stage, func(p string, info os.FileInfo, err error) error {
		if err != nil || found {
			return filepath.SkipAll
		}
		if info.IsDir() && info.Name() == "LiLink.app" {
			found = true
			return filepath.SkipAll
		}
		return nil
	})
	return found
}
