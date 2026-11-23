// 自动升级：检查更新（平台 /api/update/latest）→ 下载 zip → SHA256 校验 →
// 停止 Agent → 解压 → 备份替换（失败回滚）→ 重启自身。
// 触发入口：工作页「检查更新」按钮（前端确认）、托盘菜单（弹出确认）、
// 二次实例 --check-update 参数（自动执行，供托盘菜单/自动化测试复用）。
package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	goruntime "runtime"
	"strings"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// appVersion 当前 GUI 版本：发布包用 ldflags -X main.appVersion=x.y.z 注入
// （CI 从 wails.json productVersion 读取；默认值与 wails.json 保持一致，双保险）。
var appVersion = "1.1.12"

// UpdateInfo 检查更新结果（返回给前端）。
type UpdateInfo struct {
	HasUpdate  bool   `json:"has_update"`
	Version    string `json:"version"`
	Notes      string `json:"notes"`
	CurrentVer string `json:"current_version"`
}

// latestInfo 平台 /api/update/latest 的 data 部分。
type latestInfo struct {
	Version    string `json:"version"`
	URL        string `json:"url"`
	SHA256     string `json:"sha256"`
	Notes      string `json:"notes"`
	ReleasedAt string `json:"releasedAt"`
}

// GetVersion 当前版本号（工作页顶栏展示）。
func (a *App) GetVersion() string { return appVersion }

// updateFileLogf 升级关键节点写入文件日志（供排障与自动化验证；目录由平台实现提供）。
func updateFileLogf(format string, args ...any) {
	dir := updateLogDir()
	_ = os.MkdirAll(dir, 0o755)
	f, err := os.OpenFile(filepath.Join(dir, "update.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "%s %s\n", time.Now().Format("2006-01-02 15:04:05.000"), fmt.Sprintf(format, args...))
}

// platformBase 升级检查用的平台 HTTP 根地址（会话地址优先，缺省按构建模式取默认）。
func (a *App) platformBase() string {
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s != nil && s.PlatformURL != "" {
		return strings.TrimRight(s.PlatformURL, "/")
	}
	if releaseMode() {
		return strings.TrimRight(defaultPlatformURL, "/")
	}
	return "http://127.0.0.1:8900"
}

// semverGreater 语义化版本比较：newVer > cur 返回 true（仅支持 x.y.z 数字段）。
func semverGreater(cur, newVer string) bool {
	c := semverParts(cur)
	n := semverParts(newVer)
	for i := 0; i < 3; i++ {
		if n[i] != c[i] {
			return n[i] > c[i]
		}
	}
	return false
}

// semverParts 解析 x.y.z（容忍 v 前缀与结尾非数字），解析失败段记 0。
func semverParts(s string) [3]int {
	var out [3]int
	s = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(s), "v"))
	for i, part := range strings.SplitN(s, ".", 3) {
		if i >= 3 {
			break
		}
		num := 0
		for _, r := range part {
			if r < '0' || r > '9' {
				break
			}
			num = num*10 + int(r-'0')
		}
		out[i] = num
	}
	return out
}

// fetchLatest 拉取平台最新版本信息（按当前系统选择更新通道：Windows→pc / macOS→mac）。
func (a *App) fetchLatest() (*latestInfo, error) {
	ch := "pc"
	if goruntime.GOOS == "darwin" {
		ch = "mac"
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Get(a.platformBase() + "/api/update/latest?ch=" + ch)
	if err != nil {
		return nil, fmt.Errorf("无法连接平台：%w", err)
	}
	defer resp.Body.Close()
	var env struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("平台响应解析失败（HTTP %d）：%w", resp.StatusCode, err)
	}
	if env.Code != 0 {
		return nil, errors.New("平台返回错误：" + env.Msg)
	}
	var li latestInfo
	if err := json.Unmarshal(env.Data, &li); err != nil {
		return nil, fmt.Errorf("版本信息解析失败：%w", err)
	}
	return &li, nil
}

// CheckUpdate 查询平台最新版本（不执行升级）。
func (a *App) CheckUpdate() (*UpdateInfo, error) {
	li, err := a.fetchLatest()
	if err != nil {
		return nil, err
	}
	return &UpdateInfo{
		HasUpdate:  semverGreater(appVersion, li.Version),
		Version:    li.Version,
		Notes:      li.Notes,
		CurrentVer: appVersion,
	}, nil
}

// trayCheckUpdate 托盘菜单触发：有新版弹到主窗口由前端确认，无新版仅提示。
func (a *App) trayCheckUpdate() {
	info, err := a.CheckUpdate()
	if err != nil {
		a.logf("error", "暂时无法检查更新：%s", friendlyErr(err.Error()))
		a.showMainWindow()
		return
	}
	if !info.HasUpdate {
		a.logf("info", "当前已是最新版本（v%s）", appVersion)
		a.showMainWindow()
		return
	}
	a.logf("info", "发现新版本 v%s：%s", info.Version, info.Notes)
	a.showMainWindow()
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "update-available", info)
	}
}

// autoCheckUpdate 二次实例 --check-update 触发：发现新版直接升级（免 UI 确认）。
func (a *App) autoCheckUpdate() {
	info, err := a.CheckUpdate()
	if err != nil {
		a.logf("error", "暂时无法检查更新：%s", friendlyErr(err.Error()))
		updateFileLogf("check error: %v", err)
		a.showMainWindow()
		return
	}
	if !info.HasUpdate {
		a.logf("info", "当前已是最新版本（v%s）", appVersion)
		updateFileLogf("already latest: current=%s latest=%s", appVersion, info.Version)
		a.showMainWindow()
		return
	}
	a.showMainWindow()
	a.logf("info", "发现新版本 v%s：%s", info.Version, info.Notes)
	if err := a.ApplyUpdate(); err != nil {
		a.logf("error", "升级未完成：%s", userErr(err))
		updateFileLogf("auto apply failed: %v", err)
	}
}

// ApplyUpdate 完整升级流程：下载 → 校验 → 平台安装（Windows 自动替换重启 / macOS 引导手动更新）；失败自动回滚或保留原版本。
func (a *App) ApplyUpdate() error {
	li, err := a.fetchLatest()
	if err != nil {
		updateFileLogf("check error: %v", err)
		return err
	}
	if li.Version == "" {
		return errors.New("暂无可用更新")
	}
	if !semverGreater(appVersion, li.Version) {
		a.logf("info", "当前已是最新版本（v%s）", appVersion)
		updateFileLogf("already latest: current=%s latest=%s", appVersion, li.Version)
		return nil
	}
	if li.URL == "" || li.SHA256 == "" {
		return errors.New("更新信息不完整，请稍后重试")
	}
	dlURL := li.URL
	if !strings.HasPrefix(dlURL, "http://") && !strings.HasPrefix(dlURL, "https://") {
		dlURL = a.platformBase() + dlURL
	}
	a.logf("info", "发现新版本 v%s，正在下载…", li.Version)
	updateFileLogf("start update: current=%s target=%s url=%s", appVersion, li.Version, dlURL)

	// 下载（进度按 10% 步进推送日志）
	dir := updateLogDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return errors.New("更新准备失败，请检查磁盘空间后重试")
	}
	zipPath := filepath.Join(dir, "LiLink-update.zip")
	if err := a.downloadFile(dlURL, zipPath); err != nil {
		updateFileLogf("download failed: %v", err)
		return err
	}

	// SHA256 校验（失败即终止，不触碰现有程序）
	got, err := fileSHA256(zipPath)
	if err != nil {
		return errors.New("安装包读取失败，请稍后重试")
	}
	if !strings.EqualFold(got, li.SHA256) {
		_ = os.Remove(zipPath)
		err := errors.New("安装包校验未通过，已放弃本次升级")
		a.logf("error", "%s", err)
		updateFileLogf("sha256 mismatch: expect=%s got=%s", li.SHA256, got)
		return err
	}
	a.logf("info", "正在准备安装…")
	updateFileLogf("sha256 ok")

	// 平台安装：Windows 为解压替换自重启；macOS 为解压校验后引导手动更新
	return a.installUpdate(li, zipPath)
}

// downloadFile 下载文件并按 10% 步进推送进度日志。
func (a *App) downloadFile(url, dest string) error {
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败：%s", friendlyErr(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return errors.New("下载失败，请稍后重试")
	}
	total := resp.ContentLength
	out, err := os.Create(dest)
	if err != nil {
		return errors.New("保存安装包失败，请检查磁盘空间")
	}
	defer out.Close()
	var done int64
	lastStep := -1
	start := time.Now()
	buf := make([]byte, 256*1024)
	for {
		n, rerr := resp.Body.Read(buf)
		if n > 0 {
			if _, werr := out.Write(buf[:n]); werr != nil {
				return errors.New("保存安装包失败，请检查磁盘空间")
			}
			done += int64(n)
			if total > 0 {
				pct := int(done * 100 / total)
				if pct/10 > lastStep {
					lastStep = pct / 10
					a.logf("info", "下载进度 %d%%（%.1f/%.1f MB）", lastStep*10, float64(done)/1048576, float64(total)/1048576)
				}
			}
		}
		if rerr == io.EOF {
			break
		}
		if rerr != nil {
			return errors.New("下载中断，请检查网络后重试")
		}
	}
	a.logf("info", "升级包下载完成（%.1f MB，用时 %d 秒）", float64(done)/1048576, int(time.Since(start).Seconds()))
	return nil
}

// extractZip 解压 zip 到 dest（拒绝压缩包内绝对路径与 .. 穿越）。
func extractZip(zp, dest string) error {
	r, err := zip.OpenReader(zp)
	if err != nil {
		return err
	}
	defer r.Close()
	cleanDest := filepath.Clean(dest)
	for _, f := range r.File {
		name := filepath.Clean(f.Name)
		if strings.HasPrefix(name, "..") || filepath.IsAbs(name) {
			return fmt.Errorf("压缩包内含非法路径 %q", f.Name)
		}
		target := filepath.Join(cleanDest, name)
		if !strings.HasPrefix(target, cleanDest+string(os.PathSeparator)) {
			return fmt.Errorf("压缩包内含非法路径 %q", f.Name)
		}
		if f.FileInfo().IsDir() {
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return err
		}
		rc, err := f.Open()
		if err != nil {
			return err
		}
		out, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
		if err != nil {
			rc.Close()
			return err
		}
		_, err = io.Copy(out, rc)
		out.Close()
		rc.Close()
		if err != nil {
			return err
		}
	}
	return nil
}

// findInDir 在目录（含一级子目录）中查找文件名匹配的普通文件。
func findInDir(dir, name string) (string, error) {
	var found string
	err := filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && strings.EqualFold(info.Name(), name) {
			found = p
			return filepath.SkipAll
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	if found == "" {
		return "", errors.New("not found: " + name)
	}
	return found, nil
}

// fileSHA256 计算文件 SHA256（小写十六进制）。
func fileSHA256(p string) (string, error) {
	f, err := os.Open(p)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

// copyFile 常规文件复制。
func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(dst, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o755)
	if err != nil {
		return err
	}
	defer out.Close()
	_, err = io.Copy(out, in)
	return err
}
