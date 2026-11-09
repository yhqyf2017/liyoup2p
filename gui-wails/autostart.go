package main

// 开机自启：HKCU Run 注册表项 + 用户偏好持久化。默认自启动；
// 界面提供开关，偏好落盘以区分「默认开启」与「用户主动关闭」。
// 注册表值携带 --autostart：开机拉起时 GUI 自动恢复登录态并启动连接、隐藏到托盘。

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"

	"golang.org/x/sys/windows/registry"
)

const (
	runKeyPath   = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName = "LiLink"
	autostartArg = "--autostart"
)

// bootAutostart 本次启动是否为开机自启拉起（命令行携带 --autostart）。
var bootAutostart = func() bool {
	for _, a := range os.Args[1:] {
		if a == autostartArg {
			return true
		}
	}
	return false
}()

// settingsPath 本机设置文件：开机自启偏好、登录记忆等（与登录态同目录但独立存盘，注销后保留）。
func settingsPath() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = "."
	}
	return filepath.Join(base, "LiYouP2P", "settings.json")
}

// readSettings 读取设置文件；不存在或损坏时返回空表。
func readSettings() map[string]any {
	data, err := os.ReadFile(settingsPath())
	if err != nil {
		return map[string]any{}
	}
	var m map[string]any
	if json.Unmarshal(data, &m) != nil || m == nil {
		return map[string]any{}
	}
	return m
}

// writeSettings 原子覆盖写设置文件。
func writeSettings(m map[string]any) error {
	p := settingsPath()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, append(b, '\n'), 0o600)
}

// loadAutostartPref 读取用户自启偏好；从未设置过返回 nil（视为默认开启）。
func loadAutostartPref() *bool {
	v, ok := readSettings()["autostart"].(bool)
	if !ok {
		return nil
	}
	return &v
}

func saveAutostartPref(on bool) error {
	m := readSettings()
	m["autostart"] = on
	return writeSettings(m)
}

// autostartRegistered 当前是否已注册开机自启。
func autostartRegistered() bool {
	k, err := registry.OpenKey(registry.CURRENT_USER, runKeyPath, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	_, _, err = k.GetStringValue(runValueName)
	return err == nil
}

// setAutostartRegistry 写入/删除 Run 项；写入时按当前 exe 路径生成命令
// （程序移动/重装后自启入口始终指向最新安装位置）。
func setAutostartRegistry(on bool) error {
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

// ensureAutostart 启动时同步注册表与用户偏好：默认开启（从未设置过即注册），
// 且每次启动都按当前 exe 路径刷新，规避安装位置变更后的失效入口。
func ensureAutostart() error {
	pref := loadAutostartPref()
	if pref == nil || *pref {
		return setAutostartRegistry(true)
	}
	if autostartRegistered() {
		return setAutostartRegistry(false)
	}
	return nil
}

// autostartBoot 开机自启拉起后的自动接管：隐藏到托盘，恢复登录态并自动启动连接
// （与托盘/主界面启动同一链路，状态事件自动同步到前端）。
func (a *App) autostartBoot() {
	a.hideToTray()
	deadline := time.Now().Add(15 * time.Second)
	var s *SessionState
	for time.Now().Before(deadline) {
		s, _ = a.RestoreSession()
		if s != nil {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}
	if s == nil {
		a.logf("warn", "开机自启：未找到已保存的登录态，跳过自动连接")
		return
	}
	if a.IsRunning() {
		return
	}
	if err := a.StartAgent(s.DeviceRole, s.Mappings, s.Hosts, s.DeviceToken, s.PlatformWS); err != nil {
		a.logf("warn", "开机自启：自动连接失败：%v", err)
	} else {
		a.logf("info", "开机自启：已自动恢复连接")
	}
}

// GetAutoStart 当前开机自启开关状态（用户偏好；从未设置过为默认开启）。
func (a *App) GetAutoStart() bool {
	pref := loadAutostartPref()
	return pref == nil || *pref
}

// SetAutoStart 设置开机自启开关：写偏好 + 即时写/删注册表。
func (a *App) SetAutoStart(on bool) error {
	if err := saveAutostartPref(on); err != nil {
		return err
	}
	return setAutostartRegistry(on)
}
