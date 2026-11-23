//go:build darwin

package main

// macOS 全自动升级：解压校验 → 停 Agent → 备份替换当前 .app → 原地 exec 重启新版；
// 任一步失败自动回滚或保留原版本。无法定位 .app 包时（如裸二进制调试运行）回退手动引导。
// 替换合法性：Gatekeeper 只拦「首次打开」，覆盖替换已放行过的 app 不重新触发安全提示；
// 下载解压的文件不带隔离标记（API 下载不参与 LaunchServices），xattr 清理仅为保险。

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// updateLogDir macOS 升级工作目录（安装包/日志）。
func updateLogDir() string {
	base, err := os.UserConfigDir()
	if err != nil || base == "" {
		base = "."
	}
	return filepath.Join(base, "LiYouP2P", "update")
}

// installUpdate 执行全自动替换并重启。
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
	newApp, err := findAppBundle(stage)
	if err != nil {
		a.logf("error", "安装包缺少应用程序文件，已放弃本次升级")
		updateFileLogf("zip missing LiLink.app")
		return errors.New("安装包内容不完整，已放弃本次升级")
	}

	// 当前运行的可执行文件 → 所在 .app 包根；无法定位则回退手动引导
	exePath, err := os.Executable()
	if err != nil {
		exePath = os.Args[0]
	}
	exePath, _ = filepath.Abs(exePath)
	bundleRoot := appBundleRoot(exePath)
	if bundleRoot == "" {
		updateFileLogf("darwin: no .app bundle around %s, fallback to manual", exePath)
		return a.installUpdateManual(li, newApp)
	}

	// 内容就绪：停止 Agent 子进程（升级窗口期连接不可用）
	a.StopAgent()

	// 备份旧包 → 拷贝新包 → 失败回滚
	backup := bundleRoot + ".old"
	_ = os.RemoveAll(backup)
	if err := os.Rename(bundleRoot, backup); err != nil {
		updateFileLogf("backup rename failed: %v", err)
		return errors.New("升级准备失败，请稍后重试")
	}
	restore := func(why error) error {
		_ = os.RemoveAll(bundleRoot)
		_ = os.Rename(backup, bundleRoot)
		a.logf("error", "升级未完成，已恢复为原版本")
		updateFileLogf("rollback: %v", why)
		return errors.New("升级未完成，已恢复为原版本")
	}
	if err := copyDir(newApp, bundleRoot); err != nil {
		return restore(fmt.Errorf("替换程序失败：%w", err))
	}
	// 清除可能携带的隔离标记（保险；无 quarantine 时命令静默成功）
	_ = exec.Command("xattr", "-cr", bundleRoot).Run()
	_ = os.RemoveAll(backup)
	updateFileLogf("replaced: app updated to %s, restarting", li.Version)
	a.logf("info", "升级完成，应用即将重启…")

	// 原地重启：以新版二进制替换当前进程镜像（窗口/连接状态随之复位，无需用户手动打开）
	exeNew := filepath.Join(bundleRoot, "Contents", "MacOS", filepath.Base(exePath))
	if _, err := os.Stat(exeNew); err == nil {
		a.mu.Lock()
		a.exiting = true
		a.mu.Unlock()
		trayShutdown()
		if err := syscall.Exec(exeNew, os.Args, os.Environ()); err == nil {
			return nil // exec 成功则当前进程已被替换，不会到达
		}
	}
	// exec 失败（极罕见）：走正常退出，由用户手动重新打开
	a.mu.Lock()
	a.exiting = true
	a.mu.Unlock()
	trayShutdown()
	runtime.Quit(a.ctx)
	return nil
}

// installUpdateManual 回退路径：无法定位 .app 包时，打开访达引导用户手动拖入。
func (a *App) installUpdateManual(li *latestInfo, newApp string) error {
	stage := filepath.Dir(newApp)
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

// appBundleRoot 由可执行文件路径向上定位所在 .app 包根（Contents/MacOS/exe → …/xxx.app）；
// 不在 .app 内时返回空串。
func appBundleRoot(exePath string) string {
	dir := filepath.Dir(exePath)  // …/xxx.app/Contents/MacOS
	dir = filepath.Dir(dir)       // …/xxx.app/Contents
	bundle := filepath.Dir(dir)   // …/xxx.app
	if name := filepath.Base(bundle); name == ".app" || !strings.HasSuffix(name, ".app") {
		return ""
	}
	if st, err := os.Stat(bundle); err != nil || !st.IsDir() {
		return ""
	}
	return bundle
}

// findAppBundle 在 stage 目录（含子目录）中查找 LiLink.app 应用包目录，返回其路径。
func findAppBundle(stage string) (string, error) {
	var found string
	_ = filepath.Walk(stage, func(p string, info os.FileInfo, err error) error {
		if err != nil || found != "" {
			if found != "" {
				return filepath.SkipAll
			}
			return nil
		}
		if info.IsDir() && info.Name() == "LiLink.app" {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if found == "" {
		return "", errors.New("LiLink.app not found")
	}
	return found, nil
}

// copyDir 递归拷贝目录树（用于整包替换 .app；app 包几十 MB，直拷即可）。
func copyDir(src, dst string) error {
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, p)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, info.Mode().Perm()|0o755)
		}
		if !info.Mode().IsRegular() {
			return nil // 跳过符号链接等特殊文件（app 包内无此依赖）
		}
		return copyFile(p, target)
	})
}
