// 非 Windows 平台（macOS/Linux）无需防火墙自愈：
// macOS 应用防火墙在程序首次监听入站连接时由系统向用户确认，无需预置规则。
//go:build !windows

package fwutil

// EnsureUDPRule 非 Windows 平台为空操作，恒返回未添加、无错误。
func EnsureUDPRule() (bool, error) {
	return false, nil
}
