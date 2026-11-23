//go:build darwin

package main

// macOS 开机自启：用户级 LaunchAgent（~/Library/LaunchAgents/com.liyou.lilink.plist）。
// 写入 RunAtLoad=true + 当前可执行文件路径 + --autostart 参数；删除 plist 即取消自启。
// GUI 以普通用户进程运行，无需 root，launchd 在登录时拉起。

import (
	"errors"
	"os"
	"path/filepath"
)

const launchAgentLabel = "com.liyou.lilink"

// launchAgentsDir 当前用户的 LaunchAgents 目录（不存在时按需创建）。
func launchAgentsDir() (string, error) {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return "", errors.New("无法获取用户主目录")
	}
	return filepath.Join(home, "Library", "LaunchAgents"), nil
}

// plistPath LaunchAgent 配置文件路径。
func plistPath() (string, error) {
	dir, err := launchAgentsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, launchAgentLabel+".plist"), nil
}

// autostartEnabled 当前是否已注册 LaunchAgent。
func autostartEnabled() bool {
	p, err := plistPath()
	if err != nil {
		return false
	}
	_, err = os.Stat(p)
	return err == nil
}

// setAutostartEnabled 写入/删除 LaunchAgent plist；写入时按当前程序路径刷新
// （程序移动/重装后自启入口始终指向最新安装位置）。
func setAutostartEnabled(on bool) error {
	p, err := plistPath()
	if err != nil {
		return err
	}
	if !on {
		if err := os.Remove(p); err != nil && !os.IsNotExist(err) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		return err
	}
	// 路径转 XML 转义（macOS 安装路径一般无特殊字符，此处仅防御 & < >）
	esc := xmlEscape
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + esc(launchAgentLabel) + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + esc(exe) + `</string>
		<string>--autostart</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
</dict>
</plist>
`
	return os.WriteFile(p, []byte(plist), 0o644)
}

// xmlEscape 最小 XML 文本转义。
func xmlEscape(s string) string {
	out := make([]byte, 0, len(s))
	for i := 0; i < len(s); i++ {
		switch c := s[i]; c {
		case '&':
			out = append(out, []byte("&amp;")...)
		case '<':
			out = append(out, []byte("&lt;")...)
		case '>':
			out = append(out, []byte("&gt;")...)
		default:
			out = append(out, c)
		}
	}
	return string(out)
}
