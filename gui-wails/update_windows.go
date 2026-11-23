//go:build windows

package main

// Windows 升级安装：解压 → 校验内容 → 停 Agent → 备份替换（主程序 + Agent）→ 重启；失败自动回滚。

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// updateLogDir Windows 升级工作目录（安装包/日志）。
func updateLogDir() string {
	return filepath.Join(os.Getenv("ProgramData"), "LiLink", "update")
}

// installUpdate 执行安装替换并重启。
func (a *App) installUpdate(li *latestInfo, zipPath string) error {
	dir := filepath.Dir(zipPath)

	// 先解压校验内容，全部就绪后才停 Agent（避免停了连接才发现包不可用）
	stage := filepath.Join(dir, "stage")
	_ = os.RemoveAll(stage)
	if err := extractZip(zipPath, stage); err != nil {
		a.logf("error", "安装包解压失败，已保留当前版本")
		updateFileLogf("unzip failed: %v", err)
		return errors.New("安装包解压失败，已保留当前版本")
	}
	newGUI, err := findInDir(stage, "LiYouP2P.exe")
	if err != nil {
		a.logf("error", "升级包缺少主程序，已放弃升级（未做任何替换）")
		updateFileLogf("zip missing LiYouP2P.exe")
		return errors.New("安装包内容不完整，已放弃本次升级")
	}
	newAgent, err := findInDir(stage, "liyou-agent.exe")
	if err != nil {
		a.logf("error", "安装包内容不完整，已放弃本次升级")
		updateFileLogf("zip missing liyou-agent.exe")
		return errors.New("安装包内容不完整，已放弃本次升级")
	}

	// 内容就绪：停止 Agent 子进程（升级窗口期连接不可用）
	a.StopAgent()

	// 备份替换（主程序 + Agent），失败回滚
	exePath, err := os.Executable()
	if err != nil {
		return err
	}
	exePath, _ = filepath.Abs(exePath)
	backup := exePath + ".old"
	if err := os.Remove(backup); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(exePath, backup); err != nil {
		updateFileLogf("backup rename failed: %v", err)
		return errors.New("升级准备失败，请稍后重试")
	}
	restore := func(why error) error {
		_ = os.Rename(backup, exePath)
		a.logf("error", "升级未完成，已恢复为原版本")
		updateFileLogf("rollback: %v", why)
		return errors.New("升级未完成，已恢复为原版本")
	}
	if err := copyFile(newGUI, exePath); err != nil {
		return restore(fmt.Errorf("替换程序文件失败：%w", err))
	}
	agentPath := filepath.Join(filepath.Dir(exePath), "liyou-agent.exe")
	agentBackup := agentPath + ".old"
	_ = os.Remove(agentBackup)
	agentMoved := false
	if _, err := os.Stat(agentPath); err == nil {
		if err := os.Rename(agentPath, agentBackup); err != nil {
			return restore(fmt.Errorf("替换程序文件失败：%w", err))
		}
		agentMoved = true
	}
	if err := copyFile(newAgent, agentPath); err != nil {
		if agentMoved {
			_ = os.Rename(agentBackup, agentPath)
		}
		return restore(fmt.Errorf("替换程序文件失败：%w", err))
	}
	updateFileLogf("replaced: gui+agent updated to %s, restarting", li.Version)
	a.logf("info", "升级完成，应用即将重启…")

	// 重启：置位退出标记放行 beforeClose，退出后由 main 兜底拉起新进程（规避单实例锁竞态）
	a.mu.Lock()
	a.restartPending = true
	a.exiting = true
	a.mu.Unlock()
	trayShutdown()
	runtime.Quit(a.ctx)
	return nil
}
