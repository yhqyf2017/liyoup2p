//go:build windows

package main

// Windows 开机自启：HKCU Run 注册表项。

import (
	"errors"
	"os"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "LiLink"
)

// autostartEnabled 当前是否已注册开机自启。
func autostartEnabled() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValueName)
	return err == nil
}

// setAutostartEnabled 写入/删除 Run 项；写入时按当前 exe 路径生成命令
// （程序移动/重装后自启入口始终指向最新安装位置）。
func setAutostartEnabled(on bool) error {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return err
	}
	defer k.Close()
	if !on {
		if err := k.DeleteValue(runValueName); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return err
		}
		return nil
	}
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	return k.SetStringValue(runValueName, `"`+exe+`" `+autostartArg)
}
