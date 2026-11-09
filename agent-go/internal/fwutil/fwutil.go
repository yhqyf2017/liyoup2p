// Package fwutil Windows 防火墙自愈：确保本程序存在入站 UDP 放行规则（跨网打洞必需）。
package fwutil

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

const ruleName = "LiYou P2P Agent UDP-In"

// ruleExists 检查同名防火墙规则是否已存在。
func ruleExists() bool {
	return exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name="+ruleName).Run() == nil
}

// addDirect 当前权限下直接添加（管理员/已放行时成功）。
func addDirect(exe string) error {
	out, err := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+ruleName, "dir=in", "action=allow", "protocol=UDP", "program="+exe).CombinedOutput()
	if err != nil {
		return fmt.Errorf("%v：%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// addElevated 无管理员权限时兜底：弹出 UAC 提权窗口执行同一条 netsh（用户取消则失败）。
// 限时 90 秒，防止提权窗口无人应答时阻塞启动流程。
func addElevated(exe string) error {
	ps := fmt.Sprintf(
		`Start-Process -FilePath netsh -Verb RunAs -Wait -ArgumentList 'advfirewall firewall add rule name="%s" dir=in action=allow protocol=UDP program="%s"'`,
		ruleName, exe)
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "powershell", "-NoProfile", "-NonInteractive", "-Command", ps).CombinedOutput()
	if err != nil {
		return fmt.Errorf("提权添加失败：%v：%s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// EnsureUDPRule 检查并按需添加入站 UDP 放行规则，返回是否新添加。
// 已存在同名规则则跳过；直接添加失败时尝试 UAC 提权自愈（安装包场景通常已由安装器预置规则）。
func EnsureUDPRule() (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	if ruleExists() {
		return false, nil
	}
	if err := addDirect(exe); err == nil {
		return true, nil
	}
	if err := addElevated(exe); err != nil {
		return false, fmt.Errorf("添加防火墙规则失败（需管理员权限）：%v（跨网打洞需要放行入站 UDP）", err)
	}
	if !ruleExists() {
		return false, fmt.Errorf("提权后仍未检测到规则 %s（可能被用户取消）", ruleName)
	}
	return true, nil
}
