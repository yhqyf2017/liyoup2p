// Package fwutil Windows 防火墙自愈：确保本程序存在入站 UDP 放行规则（跨网打洞必需）。
package fwutil

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const ruleName = "LiYou P2P Agent UDP-In"

// EnsureUDPRule 检查并按需添加入站 UDP 放行规则，返回是否新添加。
// 已存在同名规则则跳过；无管理员权限时返回错误，由调用方记录提示，不阻塞启动。
func EnsureUDPRule() (bool, error) {
	exe, err := os.Executable()
	if err != nil {
		return false, err
	}
	if err := exec.Command("netsh", "advfirewall", "firewall", "show", "rule", "name="+ruleName).Run(); err == nil {
		return false, nil
	}
	out, err := exec.Command("netsh", "advfirewall", "firewall", "add", "rule",
		"name="+ruleName, "dir=in", "action=allow", "protocol=UDP", "program="+exe).CombinedOutput()
	if err != nil {
		return false, fmt.Errorf("添加防火墙规则失败（需管理员权限）：%v：%s", err, strings.TrimSpace(string(out)))
	}
	return true, nil
}
