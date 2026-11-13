package main

// 自动更新（win7 通道）：启动后台定时检查 + 托盘「检查更新」手动触发。
// 流程：GET /api/update/latest?ch=win7（免鉴权）→ 语义化版本比对 → 下载安装包到临时目录 →
// sha256 校验 → Inno 静默安装（/VERYSILENT /RESTARTAPPLICATIONS，装完自动重启本程序）→ 退出自身。

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	updateCheckInterval = 12 * time.Hour // 自动检查周期
	updateStartupDelay  = 5 * time.Second
	updateHTTPTimeout   = 15 * time.Second // 版本查询超时
)

// latestInfo /api/update/latest 返回的单通道版本信息（字段与平台 updateInfo 一致）。
type latestInfo struct {
	Version string `json:"version"`
	URL     string `json:"url"`
	SHA256  string `json:"sha256"`
	Notes   string `json:"notes"`
}

type latestEnvelope struct {
	Code int        `json:"code"`
	Msg  string     `json:"msg"`
	Data latestInfo `json:"data"`
}

// updateBaseURL 当前生效的平台基地址（会话地址优先；正式包落盘占位还原为内置默认）。
func (a *App) updateBaseURL() string {
	s := a.currentSession()
	u := strings.TrimSpace(s.PlatformURL)
	if u == "" || u == redactedURL {
		u = defaultPlatformURL
	}
	return strings.TrimRight(u, "/")
}

// fetchLatest 拉取 win7 通道最新版本信息（未发布时 Version 为空串）。
func (a *App) fetchLatest() (*latestInfo, error) {
	client := &http.Client{Timeout: updateHTTPTimeout}
	resp, err := client.Get(a.updateBaseURL() + "/api/update/latest?ch=win7")
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	var env latestEnvelope
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, err
	}
	if env.Code != 0 {
		return nil, errors.New(env.Msg)
	}
	return &env.Data, nil
}

// updateLoop 后台自动检查：启动延迟后首轮，之后按周期循环（进程存活期内持续）。
func (a *App) updateLoop() {
	time.Sleep(updateStartupDelay)
	for {
		a.autoUpdate()
		time.Sleep(updateCheckInterval)
	}
}

// autoUpdate 静默自动升级：发现新版本 → 下载校验 → 静默安装 → 退出自身。
func (a *App) autoUpdate() {
	info, err := a.fetchLatest()
	if err != nil {
		a.logf("warn", "检查更新失败：%v", sanitizeServerText(err.Error()))
		return
	}
	if info.Version == "" || !versionNewer(appVersion, info.Version) {
		return
	}
	a.logf("info", "发现新版本 %s（当前 %s），正在后台下载…", info.Version, appVersion)
	if err := a.downloadAndInstall(info); err != nil {
		a.logf("error", "自动更新失败：%v", err)
	}
}

// versionNewer 语义化版本比对（major.minor.patch，忽略前缀 v 与后缀）：latest > cur 时为真。
func versionNewer(cur, latest string) bool {
	c := parseVersion(cur)
	l := parseVersion(latest)
	for i := 0; i < 3; i++ {
		if l[i] != c[i] {
			return l[i] > c[i]
		}
	}
	return false
}

// parseVersion 取版本号前三段数字（非法段按 0）。
func parseVersion(v string) [3]int {
	var out [3]int
	v = strings.TrimPrefix(strings.TrimSpace(v), "v")
	if i := strings.IndexAny(v, "-+"); i >= 0 {
		v = v[:i]
	}
	for i, seg := range strings.Split(v, ".") {
		if i >= 3 {
			break
		}
		n := 0
		for _, ch := range seg {
			if ch < '0' || ch > '9' {
				break
			}
			n = n*10 + int(ch-'0')
		}
		out[i] = n
	}
	return out
}

// downloadAndInstall 下载安装包到临时目录、校验 sha256 后静默安装，随后退出自身。
func (a *App) downloadAndInstall(info *latestInfo) error {
	// 防并发：自动检查与手动检查同时命中时只装一次
	a.mu.Lock()
	if a.updating {
		a.mu.Unlock()
		return nil
	}
	a.updating = true
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		a.updating = false
		a.mu.Unlock()
	}()

	url := info.URL
	if url == "" {
		return errors.New("服务器未返回下载地址")
	}
	if strings.HasPrefix(url, "/") {
		url = a.updateBaseURL() + url
	}

	// 下载到临时目录（先落 .part 再改名，防半包误装）
	dst := filepath.Join(os.TempDir(), "LiLink-Setup-Win7-"+info.Version+".exe")
	part := dst + ".part"
	client := &http.Client{Timeout: 30 * time.Minute}
	resp, err := client.Get(url)
	if err != nil {
		return fmt.Errorf("下载失败：%s", sanitizeServerText(err.Error()))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("下载失败：HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(part)
	if err != nil {
		return fmt.Errorf("创建临时文件失败：%w", err)
	}
	hasher := sha256.New()
	_, err = io.Copy(io.MultiWriter(f, hasher), resp.Body)
	f.Close()
	if err != nil {
		_ = os.Remove(part)
		return fmt.Errorf("下载中断：%w", err)
	}
	if info.SHA256 != "" {
		got := hex.EncodeToString(hasher.Sum(nil))
		if !strings.EqualFold(got, info.SHA256) {
			_ = os.Remove(part)
			return fmt.Errorf("校验失败：sha256 不匹配（期望 %s，实际 %s）", info.SHA256[:12], got[:12])
		}
	}
	if err := os.Rename(part, dst); err != nil {
		_ = os.Remove(part)
		return fmt.Errorf("落盘安装包失败：%w", err)
	}

	a.logf("info", "新版本 %s 下载完成，正在静默安装…", info.Version)

	// 静默安装：装完由 Inno 重启本程序（/RESTARTAPPLICATIONS 配合 /CLOSEAPPLICATIONS）
	cmd := exec.Command(dst, "/VERYSILENT", "/SUPPRESSMSGBOXES", "/NORESTART",
		"/CLOSEAPPLICATIONS", "/RESTARTAPPLICATIONS")
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("启动安装器失败：%w", err)
	}
	// 让安装器先起来，再退出自身（文件被占用时 Inno 会等待/关闭本进程）
	go func() {
		time.Sleep(2 * time.Second)
		a.requestExitForUpdate()
	}()
	return nil
}

// requestExitForUpdate 更新安装前的进程退出：停 Agent + 保存会话 + 结束消息循环（投递主线程）。
func (a *App) requestExitForUpdate() {
	if a.onExitRequest != nil {
		a.onExitRequest()
		return
	}
	// 兜底：UI 回调未注册（极早期）时直接停 Agent 退出
	a.StopAgent()
	os.Exit(0)
}

// CheckUpdateManual 托盘「检查更新」：手动检查并提示结果；有新版且非安装中时执行升级。
func (ui *uiState) CheckUpdateManual() {
	a := ui.app
	info, err := a.fetchLatest()
	switch {
	case err != nil:
		ui.notifyUpdate("检查更新失败：" + sanitizeServerText(err.Error()))
	case info.Version == "":
		ui.notifyUpdate("服务器暂未发布 Win7 版本，当前已是最新")
	case !versionNewer(appVersion, info.Version):
		ui.notifyUpdate(fmt.Sprintf("当前已是最新版本 %s", appVersion))
	default:
		a.mu.Lock()
		busy := a.updating
		a.mu.Unlock()
		if busy {
			ui.notifyUpdate("新版本正在下载安装中，请稍候")
			return
		}
		ui.notifyUpdate(fmt.Sprintf("发现新版本 %s，正在下载安装…", info.Version))
		if err := a.downloadAndInstall(info); err != nil {
			a.logf("error", "更新失败：%v", err)
			ui.notifyUpdate("更新失败：" + err.Error())
		}
	}
}

// notifyUpdate 更新相关提示：托盘气泡（投递主线程，线程安全）。
func (ui *uiState) notifyUpdate(text string) {
	ui.mw.Synchronize(func() {
		if ui.ni != nil {
			ui.ni.ShowInfo("励联 LiLink", text)
		}
	})
}
