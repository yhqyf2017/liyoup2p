// App 绑定层：登录注册、会话持久化、Agent 子进程管理与日志回收。
package main

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// MapRule 一条端口映射：远端（服务器上）目标端口 → 本地监听端口/地址；
// Host 为选填的目标内网主机（空 = 服务器本机）。
type MapRule struct {
	Host   string `json:"host,omitempty"`
	Remote int    `json:"remote"`
	Local  string `json:"local"`
}

// mapDesc 映射规则的描述串（日志/提示用）：带主机为三段式，否则两段式。
func mapDesc(host string, remote int, local string) string {
	if host != "" {
		return fmt.Sprintf("%s:%d:%s", host, remote, local)
	}
	return fmt.Sprintf("%d:%s", remote, local)
}

// LogEntry 推送给前端的日志条目。
type LogEntry struct {
	Time  string `json:"time"`
	Level string `json:"level"` // info | warn | error
	Text  string `json:"text"`
}

// UserInfo 登录用户信息（来自平台）。
type UserInfo struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	TenantID int64  `json:"tenant_id"`
	Tenant   string `json:"tenant"`
}

// LoginResult 登录+注册设备的结果。
type LoginResult struct {
	PlatformURL string   `json:"platform_url"`
	PlatformWS  string   `json:"platform_ws"`
	User        UserInfo `json:"user"`
	DeviceUID   string   `json:"device_uid"`
	DeviceName  string   `json:"device_name"`
	DeviceRole  string   `json:"device_role"`
	Mappings    []MapRule `json:"mappings,omitempty"`
	Hosts       []string  `json:"hosts,omitempty"` // 服务端：允许访问的内网主机（不含默认本机）
}

// SessionState 持久化到 %ProgramData%\LiYouP2P\gui.json 的登录态。
type SessionState struct {
	PlatformURL string    `json:"platform_url"`
	PlatformWS  string    `json:"platform_ws"`
	User        UserInfo  `json:"user"`
	DeviceUID   string    `json:"device_uid"`
	DeviceName  string    `json:"device_name"`
	DeviceRole  string    `json:"device_role"`
	DeviceToken string    `json:"device_token"`
	Mappings    []MapRule `json:"mappings,omitempty"`
	Hosts       []string  `json:"hosts,omitempty"` // 服务端：允许访问的内网主机（不含默认本机）
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

const (
	logBufSize       = 1000                   // 历史日志环形缓冲封顶
	logFlushBatch    = 50                     // 待推送条数达此值立即合并推送
	logFlushInterval = 250 * time.Millisecond // 否则最长等待时间后合并推送
	logMaxEmitBatch  = 256                    // 单次推送条数封顶：限制单条事件 payload，超出部分下轮继续

	// codeKicked 平台业务码：同端类型已被新登录顶替（账号已在其他设备登录）
	codeKicked = 4016
)

// ---------- 构建模式与地址脱敏 ----------

// defaultPlatformURL 内置默认平台地址：正式包登录与信令固定使用。
const defaultPlatformURL = "http://jyly8888.gnway.vip:58894"

// redactedURL 正式包对外（前端/日志/配置文件）展示的地址占位。
const redactedURL = "服务器"

// releaseMode 是否正式包（ldflags -X main.isRelease=true 注入）。
func releaseMode() bool { return isRelease == "true" }

// serverSanitizer 平台地址替换表：前缀形式（https/wss/http/ws）在参数序上先于裸域名，
// 保证带前缀的完整地址优先整体替换，不会出现「http://服务器」这类残缺形式。
var serverSanitizer = newServerSanitizer()

func newServerSanitizer() *strings.Replacer {
	host := strings.TrimPrefix(strings.TrimPrefix(defaultPlatformURL, "https://"), "http://")
	return strings.NewReplacer(
		"https://"+host, redactedURL,
		"wss://"+host, redactedURL,
		"http://"+host, redactedURL,
		"ws://"+host, redactedURL,
		host, redactedURL,
	)
}

// sanitizeServerText 正式包把文本中的平台地址统一替换为「服务器」；开发模式原样返回。
func sanitizeServerText(s string) string {
	if !releaseMode() || s == "" {
		return s
	}
	return serverSanitizer.Replace(s)
}

// clearKickedSession 会话被其他电脑顶替时清理本机登录态并停掉 Agent，
// 前端收到登录错误提示后回到登录页。
func (a *App) clearKickedSession() {
	a.StopAgent()
	_ = os.Remove(sessionFile())
	a.mu.Lock()
	a.session = nil
	a.mu.Unlock()
	a.logf("warn", "账号已在其他电脑登录，本机会话已被顶替并清除")
}

// App Wails 绑定对象。
type App struct {
	ctx context.Context

	mu      sync.Mutex
	session *SessionState
	cmd     *exec.Cmd
	running bool
	stopping bool
	exiting  bool   // 托盘「退出」/升级重启置位：允许真正关闭进程
	restartPending bool // 升级替换完成：进程退出后拉起新版 exe
	balloonOnce sync.Once // 首次隐藏到托盘的气泡提示（每次进程生命周期一次）
	adminAddr  string // Agent 管理通道实际地址（解析自 Agent 日志，如 127.0.0.1:53210）
	adminToken string // Agent 管理通道鉴权令牌

	logMu      sync.Mutex
	logBuf     []LogEntry  // 历史缓冲（GetLogs 用）
	logPending []LogEntry  // 待推送缓冲（批量合并推前端）
	logTimer   *time.Timer // 惰性推送定时器
}

// NewApp 创建 App。
func NewApp() *App { return &App{} }

// BuildMode 构建模式：release 时前端隐藏服务器地址输入框并固定默认地址，dev 保持可编辑。
func (a *App) BuildMode() string {
	if releaseMode() {
		return "release"
	}
	return "dev"
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	go installWindowHook(a) // 子类化主窗口：拦截最小化 → 隐藏到托盘
	go runWinOpLoop(a)      // 串行消费窗口显隐操作（托盘唤回/最小化隐藏唯一执行点）
	startTray(a)            // 托盘常驻（图标 + 菜单 + 气泡）
}

// beforeClose 窗口关闭钩子：
//   - 托盘「退出」触发（exiting=true）→ 停 Agent、保存配置，返回 false 放行退出；
//   - 其余（点关闭按钮/Alt+F4）→ 隐藏到托盘并拦截关闭，程序转入后台继续运行。
func (a *App) beforeClose(ctx context.Context) bool {
	a.mu.Lock()
	exiting := a.exiting
	a.mu.Unlock()
	if exiting {
		a.StopAgent()
		a.mu.Lock()
		s := a.session
		a.mu.Unlock()
		if s != nil {
			if err := a.saveSession(s); err != nil {
				a.logf("warn", "退出前保存配置失败：%v", err)
			}
		}
		return false
	}
	a.hideToTray()
	return true
}

// ---------- 窗口显隐串行化 ----------
// 根因（托盘唤回偶发卡死）：
//   隐藏路径（winhook SC_MINIMIZE / beforeClose → runtime.WindowHide）与
//   唤回路径（托盘过程 → runtime.WindowUnminimise/Show）在两个 goroutine 上并发执行，
//   winc 层 Restore() 内部是跨线程 SendMessage(WM_SYSCOMMAND, SC_RESTORE)
//   （wails 依赖 winc/form.go:142，阻塞等待主窗口线程），与 Hide 的跨线程
//   ShowWindow(SW_HIDE) 交错时互相等待 → 托盘过程卡死在 Unminimise、主窗口
//   消息泵停摆（IsHungAppWindow=True，实测 >2min 不自愈）。
//   修复：所有 runtime.Window* 调用收敛到 runWinOpLoop 单一 goroutine，调用方
//   （窗口过程/托盘过程/JS 绑定/二次启动）只做非阻塞投递，天然保序且可合并。
type winOp int

const (
	winOpHide winOp = iota
	winOpShow
)

var winOpCh = make(chan winOp, 16)

// requestWinOp 非阻塞提交显隐操作（任意线程/窗口过程内可安全调用，永不阻塞）。
func requestWinOp(op winOp) {
	select {
	case winOpCh <- op:
	default:
		// 队列满：丢弃，最后一次操作会覆盖到目标状态
	}
}

// runWinOpLoop 显隐操作唯一执行点：去抖合并（吸干队列只执行最后一个操作），
// 保证 Hide/Show 不重入、不并发；Show 后强制前置，修复唤回后不在前台的问题。
func runWinOpLoop(a *App) {
	for op := range winOpCh {
		last := op
	drain:
		for {
			select {
			case o := <-winOpCh:
				last = o
			default:
				break drain
			}
		}
		if a.ctx == nil {
			continue
		}
		switch last {
		case winOpHide:
			a.balloonOnce.Do(func() {
				trayBalloon("励联 LiLink", "已最小化到任务栏，后台保持连接")
			})
			runtime.WindowHide(a.ctx)
		case winOpShow:
			runtime.WindowUnminimise(a.ctx)
			runtime.WindowShow(a.ctx)
			foregroundMainWindow()
		}
	}
}

func (a *App) hideToTray() {
	requestWinOp(winOpHide)
}

// showMainWindow 显示并前置主窗口（托盘双击/菜单、二次启动唤起共用）。
// 仅投递请求，实际显隐由 runWinOpLoop 串行执行——窗口/托盘过程内不得同步调 wails runtime。
func (a *App) showMainWindow() {
	if a.ctx == nil {
		return
	}
	requestWinOp(winOpShow)
}

// toggleConnection 托盘「启动连接/停止连接」：与主窗口按钮等效，状态双向同步。
func (a *App) toggleConnection() {
	if a.IsRunning() {
		a.StopAgent()
		return
	}
	a.mu.Lock()
	s := a.session
	a.mu.Unlock()
	if s == nil {
		a.logf("warn", "尚未登录，请先打开主窗口登录后再启动连接")
		a.showMainWindow()
		return
	}
	if err := a.StartAgent(s.DeviceRole, s.Mappings, s.Hosts, "", s.PlatformWS); err != nil {
		a.logf("error", "启动连接失败：%v", err)
		a.showMainWindow() // 前台展示失败原因（错误已进日志面板）
	}
}

// exitFromTray 托盘「退出」：停 Agent、保存配置后真正结束进程。
func (a *App) exitFromTray() {
	a.mu.Lock()
	a.exiting = true
	a.mu.Unlock()
	trayShutdown()       // 先移除托盘图标
	runtime.Quit(a.ctx)  // → beforeClose（exiting=true）→ 停 Agent → 放行退出
}

// emitAgentState 向前端推送连接状态（托盘启停后主窗口按钮同步）。
func (a *App) emitAgentState(running bool) {
	if a.ctx != nil {
		runtime.EventsEmit(a.ctx, "agent-state", running)
	}
}

// ---------- 日志 ----------

// logf 记录一条日志：写入历史缓冲并进入待推送队列。
// 推送按批合并（满 20 条或 150ms 定时到），避免日志风暴时逐条 IPC 冲爆前端。
func (a *App) logf(level, format string, v ...any) {
	entry := LogEntry{
		Time:  time.Now().Format("15:04:05"),
		Level: level,
		Text:  sanitizeServerText(fmt.Sprintf(format, v...)),
	}
	a.logMu.Lock()
	if len(a.logBuf) >= logBufSize {
		a.logBuf = append(a.logBuf[:0], a.logBuf[len(a.logBuf)-logBufSize+1:]...)
	}
	a.logBuf = append(a.logBuf, entry)
	a.logPending = append(a.logPending, entry)
	immediate := len(a.logPending) >= logFlushBatch
	if !immediate && a.logTimer == nil {
		a.logTimer = time.AfterFunc(logFlushInterval, a.flushLogs)
	}
	a.logMu.Unlock()
	if immediate {
		a.flushLogs()
	}
}

// flushLogs 将待推送缓冲合并为一次事件推送（定时器与满批共用）。
// 单次最多推送 logMaxEmitBatch 条（限制 payload 体积），有剩余时保持定时器继续刷。
func (a *App) flushLogs() {
	a.logMu.Lock()
	if a.logTimer != nil {
		a.logTimer.Stop()
		a.logTimer = nil
	}
	batch := a.logPending
	a.logPending = nil
	if len(batch) > logMaxEmitBatch {
		a.logPending = batch[logMaxEmitBatch:]
		batch = batch[:logMaxEmitBatch]
	}
	rest := len(a.logPending) > 0
	if rest && a.logTimer == nil {
		a.logTimer = time.AfterFunc(logFlushInterval, a.flushLogs)
	}
	a.logMu.Unlock()
	if len(batch) > 0 && a.ctx != nil {
		runtime.EventsEmit(a.ctx, "log", batch)
	}
}

// GetLogs 拉取环形缓冲日志。
func (a *App) GetLogs() []LogEntry {
	a.logMu.Lock()
	defer a.logMu.Unlock()
	out := make([]LogEntry, len(a.logBuf))
	copy(out, a.logBuf)
	return out
}

// ---------- 会话持久化 ----------

func sessionFile() string {
	base := os.Getenv("ProgramData")
	if base == "" {
		base = "."
	}
	return filepath.Join(base, "LiYouP2P", "gui.json")
}

func (a *App) saveSession(s *SessionState) error {
	p := sessionFile()
	if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
		return err
	}
	out := *s
	if releaseMode() {
		// 正式包配置文件不落明文平台地址（读回时还原为内置默认）
		if out.PlatformURL != "" {
			out.PlatformURL = redactedURL
		}
		if out.PlatformWS != "" {
			out.PlatformWS = redactedURL
		}
	}
	data, err := json.MarshalIndent(&out, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(p, data, 0o600)
}

// RestoreSession 读取已持久化登录态。
func (a *App) RestoreSession() (*SessionState, error) {
	data, err := os.ReadFile(sessionFile())
	if err != nil {
		return nil, nil
	}
	var s SessionState
	if err := json.Unmarshal(data, &s); err != nil || s.DeviceToken == "" {
		return nil, nil
	}
	// 正式包落盘的地址是「服务器」占位：任何模式下读回都还原为内置默认，保证 Agent 可用
	if s.PlatformURL == "" || s.PlatformURL == redactedURL {
		s.PlatformURL = defaultPlatformURL
	}
	if s.PlatformWS == "" || s.PlatformWS == redactedURL {
		s.PlatformWS = wsURL(defaultPlatformURL)
	}
	a.mu.Lock()
	a.session = &s
	a.mu.Unlock()
	out := s
	if releaseMode() {
		// 正式包下发给前端的会话同样脱敏（界面不展示地址）
		out.PlatformURL = redactedURL
		out.PlatformWS = redactedURL
	}
	return &out, nil
}

// Logout 停止 Agent 并清除本机登录态。
func (a *App) Logout() {
	a.StopAgent()
	_ = os.Remove(sessionFile())
	a.mu.Lock()
	a.session = nil
	a.mu.Unlock()
	a.logf("info", "已退出登录，本机凭据已清除")
}

// ---------- 登录 / 注册 ----------

// normalizeBaseURL 归一化平台地址（补 http://、去尾部斜杠）。
func normalizeBaseURL(u string) (string, error) {
	u = strings.TrimSpace(u)
	if u == "" {
		return "", errors.New("服务器地址不能为空")
	}
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	u = strings.TrimRight(u, "/")
	return u, nil
}

// wsURL 推导信令 WebSocket 地址：http→ws / https→wss，路径 /ws。
func wsURL(base string) string {
	ws := base
	ws = strings.Replace(ws, "https://", "wss://", 1)
	ws = strings.Replace(ws, "http://", "ws://", 1)
	return ws + "/ws"
}

func postJSON(url string, body any, token string) (envelope, error) {
	var env envelope
	buf, err := json.Marshal(body)
	if err != nil {
		return env, err
	}
	req, err := http.NewRequest(http.MethodPost, url, bytes.NewReader(buf))
	if err != nil {
		return env, err
	}
	req.Header.Set("Content-Type", "application/json")
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return env, fmt.Errorf("无法连接平台 %s：%w", url, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return env, fmt.Errorf("平台响应解析失败（HTTP %d）：%w", resp.StatusCode, err)
	}
	return env, nil
}

// Login 登录平台并注册设备，成功后持久化会话。
func (a *App) Login(platformURL, tenant, user, pass, devName, role string, mappings []MapRule) (*LoginResult, error) {
	if releaseMode() {
		platformURL = defaultPlatformURL // 正式包固定内置平台地址，忽略前端传入
	}
	base, err := normalizeBaseURL(platformURL)
	if err != nil {
		return nil, err
	}
	if user == "" || pass == "" {
		return nil, errors.New("账号和密码不能为空")
	}
	if role != "server" && role != "client" {
		return nil, errors.New("请先选择设备角色")
	}

	a.logf("info", "正在登录平台 %s（账号 %s）…", base, user)
	env, err := postJSON(base+"/api/auth/login", map[string]any{
		"username": user, "password": pass, "tenant": tenant, "client_type": "pc",
	}, "")
	if err != nil {
		return nil, err
	}
	if env.Code != 0 {
		if env.Code == codeKicked {
			a.clearKickedSession()
			return nil, errors.New("账号已在其他电脑登录，本机登录已失效，请重新登录")
		}
		return nil, errors.New(env.Msg)
	}
	var loginData struct {
		AccessToken string   `json:"access_token"`
		User        UserInfo `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &loginData); err != nil {
		return nil, fmt.Errorf("登录响应解析失败：%w", err)
	}
	a.logf("info", "登录成功：用户 %s（角色 %s，租户 %s）", loginData.User.Username, loginData.User.Role, loginData.User.Tenant)

	if strings.TrimSpace(devName) == "" {
		host, _ := os.Hostname()
		devName = host
	}
	// 复用本机已注册的设备记录：平台同平台+同租户+同角色时带上原 DeviceUID（注册接口
	// 按 UID 幂等，重签令牌），避免每次登录都新建一条同名设备堆积在设备列表里，
	// 导致客户端自动路由连到未配置白名单的旧设备记录。
	reuseUID := ""
	a.mu.Lock()
	if s := a.session; s != nil && s.DeviceUID != "" &&
		s.PlatformURL == base && s.User.TenantID == loginData.User.TenantID && s.DeviceRole == role {
		reuseUID = s.DeviceUID
	}
	a.mu.Unlock()
	a.logf("info", "正在注册设备「%s」（%s）…", devName, role)
	env, err = postJSON(base+"/api/devices/register", map[string]any{
		"name": devName, "role": role, "tenant_id": loginData.User.TenantID,
		"device_uid": reuseUID,
	}, loginData.AccessToken)
	if err != nil {
		return nil, err
	}
	if env.Code != 0 {
		if env.Code == codeKicked {
			a.clearKickedSession()
			return nil, errors.New("账号已在其他电脑登录，本机登录已失效，请重新登录")
		}
		return nil, errors.New("注册设备失败：" + env.Msg)
	}
	var devData struct {
		DeviceID    int64  `json:"device_id"`
		DeviceUID   string `json:"device_uid"`
		Role        string `json:"role"`
		DeviceToken string `json:"device_token"`
	}
	if err := json.Unmarshal(env.Data, &devData); err != nil {
		return nil, fmt.Errorf("设备注册响应解析失败：%w", err)
	}
	a.logf("info", "设备注册成功：UID %s，已签发 30 天设备令牌", devData.DeviceUID)

	cleanMaps := make([]MapRule, 0, len(mappings))
	for _, m := range mappings {
		if m.Remote > 0 && m.Remote <= 65535 && strings.TrimSpace(m.Local) != "" {
			r := MapRule{Remote: m.Remote, Local: strings.TrimSpace(m.Local)}
			if h := strings.TrimSpace(m.Host); h != "" {
				if err := validateHostInput(h); err != nil {
					return nil, fmt.Errorf("映射目标主机 %q：%v", h, err)
				}
				r.Host = h
			}
			cleanMaps = append(cleanMaps, r)
		}
	}

	s := &SessionState{
		PlatformURL: base,
		PlatformWS:  wsURL(base),
		User:        loginData.User,
		DeviceUID:   devData.DeviceUID,
		DeviceName:  devName,
		DeviceRole:  devData.Role,
		DeviceToken: devData.DeviceToken,
		Mappings:    cleanMaps,
	}
	if err := a.saveSession(s); err != nil {
		a.logf("warn", "会话保存失败：%v", err)
	}
	a.mu.Lock()
	a.session = s
	a.mu.Unlock()
	a.logf("info", "登录完成，设备 %s 已就绪", s.DeviceUID)

	res := &LoginResult{
		PlatformURL: s.PlatformURL,
		PlatformWS:  s.PlatformWS,
		User:        s.User,
		DeviceUID:   s.DeviceUID,
		DeviceName:  s.DeviceName,
		DeviceRole:  s.DeviceRole,
		Mappings:    s.Mappings,
	}
	if releaseMode() {
		res.PlatformURL = redactedURL // 正式包登录结果同样不向前端暴露地址
		res.PlatformWS = redactedURL
	}
	return res, nil
}

// ---------- Agent 子进程 ----------

// agentExe 定位同目录 liyou-agent.exe（环境变量 LIYOU_AGENT_EXE 可显式指定，测试/调试用）。
func agentExe() (string, error) {
	if p := strings.TrimSpace(os.Getenv("LIYOU_AGENT_EXE")); p != "" {
		if _, err := os.Stat(p); err != nil {
			return "", errors.New("未找到 Agent 程序 " + p)
		}
		return p, nil
	}
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	p := filepath.Join(filepath.Dir(exe), "liyou-agent.exe")
	if _, err := os.Stat(p); err != nil {
		return "", errors.New("未找到 Agent 程序 " + p)
	}
	return p, nil
}

// IsRunning Agent 是否在运行。
func (a *App) IsRunning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.running
}

// StartAgent 以指定角色启动 Agent 子进程（日志经管道回收并实时推送前端）。
// 客户端：mappings 生成 --map（带目标主机为三段式「主机:远端:本机」，否则两段式）；
// 服务端：mappings[0].Remote 为申报端口，hosts 为允许访问的内网主机（自动含默认本机，去重）。
func (a *App) StartAgent(role string, mappings []MapRule, hosts []string, deviceToken, platformWS string) error {
	a.mu.Lock()
	if a.running {
		a.mu.Unlock()
		return errors.New("Agent 已在运行，请先停止")
	}
	s := a.session
	a.mu.Unlock()

	if deviceToken == "" && s != nil {
		deviceToken = s.DeviceToken
	}
	if deviceToken == "" {
		return errors.New("缺少设备令牌，请重新登录")
	}
	if platformWS == "" && s != nil {
		platformWS = s.PlatformWS
	}
	if platformWS == "" {
		return errors.New("缺少平台信令地址，请重新登录")
	}
	if releaseMode() {
		platformWS = wsURL(defaultPlatformURL) // 正式包信令固定内置平台地址
	}
	if role != "server" && role != "client" {
		role = "client"
	}

	exe, err := agentExe()
	if err != nil {
		return err
	}

	devName := ""
	if s != nil {
		devName = s.DeviceName
	}
	var serverHosts []string // 服务端本次生效的内网主机列表（持久化同步用）
	args := []string{"--role", role, "--platform", platformWS, "--device-token", deviceToken}
	if devName != "" {
		args = append(args, "--name", devName)
	}
	if role == "client" {
		for _, m := range mappings {
			if m.Remote > 0 && strings.TrimSpace(m.Local) != "" {
				spec := fmt.Sprintf("%d:%s", m.Remote, strings.TrimSpace(m.Local))
				if h := strings.TrimSpace(m.Host); h != "" {
					if err := validateHostInput(h); err != nil {
						return fmt.Errorf("映射目标主机 %q：%v", h, err)
					}
					spec = fmt.Sprintf("%s:%d:%s", h, m.Remote, strings.TrimSpace(m.Local))
				}
				args = append(args, "--map", spec)
			}
		}
		if len(mappings) == 0 {
			return errors.New("客户端模式至少需要一条端口映射")
		}
	} else {
		// 服务端：首条映射的远端端口作为额外申报端口
		var userHosts []string
		if len(mappings) > 0 && mappings[0].Remote > 0 {
			args = append(args, "--port", fmt.Sprintf("%d", mappings[0].Remote))
		}
		// 允许访问的内网主机：用户添加了其他主机才显式传 --hosts（含默认本机，重复去重）
		seen := map[string]bool{"127.0.0.1": true}
		added := false
		args = append(args, "--hosts", "127.0.0.1")
		for _, h := range hosts {
			h = strings.TrimSpace(h)
			if h == "" || seen[strings.ToLower(h)] {
				continue
			}
			if err := validateHostInput(h); err != nil {
				return fmt.Errorf("内网主机 %q：%v", h, err)
			}
			seen[strings.ToLower(h)] = true
			args = append(args, "--hosts", h)
			userHosts = append(userHosts, h)
			added = true
		}
		if !added {
			args = args[:len(args)-2] // 仅默认本机：不传 --hosts，保持 Agent 默认行为
		}
		serverHosts = userHosts
	}

	// 管理通道：GUI 运行中热管理（客户端热增删映射 / 服务端热管理内网主机白名单；
	// 随机令牌鉴权，仅本机访问）
	tok, err := randToken()
	if err != nil {
		return fmt.Errorf("生成管理令牌失败：%w", err)
	}
	args = append(args, "--admin", "127.0.0.1:0", "--admin-token", tok)

	cmd := exec.Command(exe, args...)
	cmd.Dir = filepath.Dir(exe)
	pr, pw, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd.Stdout = pw
	cmd.Stderr = pw // 合并 stderr 到同一管道
	hideWindow(cmd)

	a.logf("info", "正在启动 Agent（角色=%s）…", role)
	if err := cmd.Start(); err != nil {
		pw.Close()
		pr.Close()
		return fmt.Errorf("Agent 启动失败：%w", err)
	}
	pw.Close() // 父进程侧写端关闭，子进程退出后读端自然 EOF
	a.mu.Lock()
	a.cmd = cmd
	a.running = true
	a.stopping = false
	a.adminAddr, a.adminToken = "", "" // 等待 Agent 日志上报管理通道就绪
	a.mu.Unlock()
	traySetRunning(true)       // 托盘菜单/提示同步为「停止连接」
	a.emitAgentState(true)     // 主窗口按钮同步

	// 本次使用的映射/主机列表写入持久化配置，保证 gui.json 与实际一致
	if role == "client" && s != nil && len(mappings) > 0 {
		a.mu.Lock()
		s.Mappings = mappings
		if err := a.saveSession(s); err != nil {
			a.logf("warn", "映射配置保存失败：%v", err)
		}
		a.mu.Unlock()
	} else if role == "server" && s != nil && serverHosts != nil {
		a.mu.Lock()
		s.Hosts = serverHosts
		if err := a.saveSession(s); err != nil {
			a.logf("warn", "内网主机配置保存失败：%v", err)
		}
		a.mu.Unlock()
	}

	go a.pumpLogs(pr)
	go func() {
		waitErr := cmd.Wait()
		a.mu.Lock()
		stopping := a.stopping
		a.stopping = false
		a.running = false
		a.cmd = nil
		a.adminAddr, a.adminToken = "", ""
		a.mu.Unlock()
		traySetRunning(false)   // 托盘菜单/提示同步为「启动连接」
		a.emitAgentState(false) // 主窗口按钮同步
		if !stopping {
			if waitErr != nil {
				a.logf("error", "Agent 进程异常退出：%v", waitErr)
			} else {
				a.logf("info", "Agent 进程已退出")
			}
		}
	}()
	return nil
}

// adminReadyRe 匹配 Agent 日志中的管理通道就绪行，提取实际地址与令牌。
// 兼容客户端（token=…））与服务端（token=…，支持 /hosts …）两种日志格式。
var adminReadyRe = regexp.MustCompile(`管理通道已就绪：http://([0-9a-zA-Z.:]+)（token=([0-9a-fA-F]{16,64})[），]`)

// parseAdminReady 从日志行解析管理通道地址与令牌；非就绪行返回空。
func parseAdminReady(line string) (addr, token string) {
	m := adminReadyRe.FindStringSubmatch(line)
	if m == nil {
		return "", ""
	}
	return m[1], m[2]
}

// pumpLogs 持续读取子进程输出，解析级别后推送前端。
func (a *App) pumpLogs(r io.Reader) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := strings.TrimRight(sc.Text(), "\r\n ")
		if line == "" {
			continue
		}
		if addr, tok := parseAdminReady(line); addr != "" {
			a.mu.Lock()
			a.adminAddr, a.adminToken = addr, tok
			a.mu.Unlock()
		}
		level := "info"
		if strings.Contains(line, "[错误]") || strings.Contains(line, "[Error]") {
			level = "error"
		} else if strings.Contains(line, "[警告]") || strings.Contains(line, "[Warn]") {
			level = "warn"
		}
		a.logf(level, "%s", line)
	}
}

// StopAgent 停止 Agent 子进程。
func (a *App) StopAgent() {
	a.mu.Lock()
	cmd := a.cmd
	wasRunning := a.running
	a.running = false
	a.stopping = true
	a.cmd = nil
	a.adminAddr, a.adminToken = "", ""
	a.mu.Unlock()
	traySetRunning(false)   // 托盘菜单/提示同步为「启动连接」
	a.emitAgentState(false) // 主窗口按钮同步
	if cmd != nil && cmd.Process != nil {
		a.logf("info", "正在停止 Agent…")
		_ = cmd.Process.Kill() // 退出回收由 Wait goroutine 完成
	} else if wasRunning {
		a.logf("info", "Agent 已停止")
	}
}

// OpenPage 用系统默认浏览器打开本地映射页面。
func (a *App) OpenPage(localPort int) error {
	if localPort <= 0 || localPort > 65535 {
		return errors.New("端口非法")
	}
	url := fmt.Sprintf("http://127.0.0.1:%d", localPort)
	a.logf("info", "正在打开 %s", url)
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// ---------- 映射热增删（Agent 管理通道） ----------

// randToken 生成 128 位随机十六进制令牌。
func randToken() (string, error) {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// normalizeRule 校验并规范化一条映射规则（host 选填，填则校验合法性）。
func normalizeRule(host string, remote int, local string) (MapRule, error) {
	host = strings.TrimSpace(host)
	if host != "" {
		if err := validateHostInput(host); err != nil {
			return MapRule{}, fmt.Errorf("目标主机 %q：%v", host, err)
		}
	}
	if remote < 1 || remote > 65535 {
		return MapRule{}, fmt.Errorf("远端端口 %d 非法（1-65535）", remote)
	}
	local = strings.TrimSpace(local)
	if local == "" {
		return MapRule{}, errors.New("本地监听端口不能为空")
	}
	if !strings.Contains(local, ":") {
		p, err := strconv.Atoi(local)
		if err != nil || p < 1 || p > 65535 {
			return MapRule{}, fmt.Errorf("本地端口 %q 非法（1-65535）", local)
		}
	}
	return MapRule{Host: host, Remote: remote, Local: local}, nil
}

// sameRule 判断两条映射是否同一条（主机名不区分大小写，本地地址归一化后比较）。
func sameRule(a, b MapRule) bool {
	return a.Remote == b.Remote &&
		strings.EqualFold(strings.TrimSpace(a.Host), strings.TrimSpace(b.Host)) &&
		normalizeLocal(a.Local) == normalizeLocal(b.Local)
}

// normalizeLocal 本地地址归一化（纯端口补 127.0.0.1，与 Agent 侧规则一致）。
func normalizeLocal(local string) string {
	local = strings.TrimSpace(local)
	if strings.Contains(local, ":") {
		return local
	}
	return "127.0.0.1:" + local
}

// MappingOpResult 映射增删结果。
type MappingOpResult struct {
	Applied bool    `json:"applied"` // 是否已在运行中的 Agent 上即时生效
	Rule    MapRule `json:"rule"`
}

// AddMapping 新增映射规则：Agent 运行中则热生效（隧道保持不断开），否则仅保存到配置。
// host 选填：指定服务端内网目标主机（须在服务端允许列表内）。
func (a *App) AddMapping(host string, remote int, local string) (*MappingOpResult, error) {
	rule, err := normalizeRule(host, remote, local)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	running := a.running
	addr, tok := a.adminAddr, a.adminToken
	s := a.session
	a.mu.Unlock()

	if running {
		if addr == "" {
			return nil, errors.New("Agent 管理通道未就绪，请停止后重新启动连接")
		}
		// Agent 侧负责查重与端口占用校验（重复/冲突会返回明确错误）
		if _, err := adminCall(http.MethodPost, addr, tok, "/maps", map[string]any{
			"host": rule.Host, "remote": rule.Remote, "local": rule.Local,
		}); err != nil {
			return nil, err
		}
	} else if s != nil {
		for _, m := range s.Mappings {
			if sameRule(m, rule) {
				return nil, fmt.Errorf("映射 %s 已存在", mapDesc(rule.Host, rule.Remote, rule.Local))
			}
			if normalizeLocal(m.Local) == normalizeLocal(rule.Local) {
				return nil, fmt.Errorf("本地端口 %s 已被映射 %s 占用", rule.Local, mapDesc(m.Host, m.Remote, m.Local))
			}
		}
	}
	a.updateMappings(func(list []MapRule) []MapRule {
		for _, m := range list {
			if sameRule(m, rule) {
				return list
			}
		}
		return append(list, rule)
	})
	if running {
		a.logf("info", "映射 %s 已即时生效（连接保持不断开）", mapDesc(rule.Host, rule.Remote, rule.Local))
	} else {
		a.logf("info", "映射 %s 已保存，启动连接后生效", mapDesc(rule.Host, rule.Remote, rule.Local))
	}
	return &MappingOpResult{Applied: running, Rule: rule}, nil
}

// RemoveMapping 删除映射规则：Agent 运行中则热删除监听，否则仅从配置移除。
// Agent 中不存在该映射时（如未点过「生效」的草稿行）视为已删除，仅清理配置。
func (a *App) RemoveMapping(host string, remote int, local string) (*MappingOpResult, error) {
	rule, err := normalizeRule(host, remote, local)
	if err != nil {
		return nil, err
	}
	a.mu.Lock()
	running := a.running
	addr, tok := a.adminAddr, a.adminToken
	a.mu.Unlock()

	applied := false
	if running && addr != "" {
		list, err := adminMaps(addr, tok)
		if err != nil {
			return nil, err
		}
		id, found := uint64(0), false
		for _, m := range list {
			if sameRule(MapRule{Host: m.Host, Remote: m.Remote, Local: m.Local}, rule) {
				id, found = m.ID, true
				break
			}
		}
		if found {
			if _, err := adminCall(http.MethodDelete, addr, tok, fmt.Sprintf("/maps/%d", id), nil); err != nil {
				return nil, err
			}
			applied = true
			a.logf("info", "映射 %s 已停止监听", mapDesc(rule.Host, rule.Remote, rule.Local))
		}
	}
	a.updateMappings(func(list []MapRule) []MapRule {
		out := make([]MapRule, 0, len(list))
		for _, m := range list {
			if sameRule(m, rule) {
				continue
			}
			out = append(out, m)
		}
		return out
	})
	return &MappingOpResult{Applied: applied, Rule: rule}, nil
}

// updateMappings 更新持久化配置中的映射列表。
func (a *App) updateMappings(fn func([]MapRule) []MapRule) {
	a.mu.Lock()
	s := a.session
	if s == nil {
		a.mu.Unlock()
		return
	}
	s.Mappings = fn(s.Mappings)
	err := a.saveSession(s)
	a.mu.Unlock()
	if err != nil {
		a.logf("warn", "映射配置保存失败：%v", err)
	}
}

// adminMapItem Agent 映射列表条目。
type adminMapItem struct {
	ID        uint64 `json:"id"`
	Host      string `json:"host"`
	Remote    int    `json:"remote"`
	Local     string `json:"local"`
	Addr      string `json:"addr"`
	Listening bool   `json:"listening"`
}

// adminMaps 拉取 Agent 当前映射列表。
func adminMaps(addr, token string) ([]adminMapItem, error) {
	data, err := adminCall(http.MethodGet, addr, token, "/maps", nil)
	if err != nil {
		return nil, err
	}
	var list []adminMapItem
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("映射列表解析失败：%w", err)
	}
	return list, nil
}

// adminCall 调用 Agent 本地管理 API（Bearer 令牌鉴权）。
func adminCall(method, addr, token, path string, body any) (json.RawMessage, error) {
	var rd io.Reader
	if body != nil {
		buf, err := json.Marshal(body)
		if err != nil {
			return nil, err
		}
		rd = bytes.NewReader(buf)
	}
	req, err := http.NewRequest(method, "http://"+addr+path, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := (&http.Client{Timeout: 5 * time.Second}).Do(req)
	if err != nil {
		return nil, fmt.Errorf("管理通道请求失败：%w", err)
	}
	defer resp.Body.Close()
	var env struct {
		Code int             `json:"code"`
		Msg  string          `json:"msg"`
		Data json.RawMessage `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return nil, fmt.Errorf("管理通道响应解析失败（HTTP %d）", resp.StatusCode)
	}
	if resp.StatusCode != http.StatusOK || env.Code != 0 {
		if env.Msg != "" {
			return nil, errors.New(env.Msg)
		}
		return nil, fmt.Errorf("管理操作失败（HTTP %d）", resp.StatusCode)
	}
	return env.Data, nil
}

// ---------- 内网主机管理（服务端，Agent 管理通道 /hosts） ----------

// HostOpResult 内网主机增删结果。
type HostOpResult struct {
	Applied bool   `json:"applied"` // 是否已在运行中的 Agent 上即时生效
	Host    string `json:"host"`
}

// validateHostInput 校验主机输入：精确 IPv4/IPv6 或主机名；禁空值、0.0.0.0/::、
// 网段与通配写法（与 Agent 侧 config.ValidateHost 规则一致）。
func validateHostInput(host string) error {
	h := strings.TrimSpace(host)
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return errors.New("主机不能为空")
	}
	if len(h) > 253 {
		return errors.New("主机名过长（超过 253 字符）")
	}
	if ip := net.ParseIP(h); ip != nil {
		if ip.IsUnspecified() {
			return errors.New("不允许 0.0.0.0 / :: 等未指定地址")
		}
		return nil
	}
	if strings.Contains(h, ":") {
		return errors.New("格式非法：含冒号但不是合法 IPv6 地址")
	}
	if strings.ContainsAny(h, "*/\\?%!$@#^&|+=[](){}'\" \t") {
		return errors.New("仅支持精确 IP 或主机名，不允许通配或网段写法（如 192.168.5.*）")
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("主机名 %q 非法", h)
		}
		if !isHostAlnum(label[0]) || !isHostAlnum(label[len(label)-1]) {
			return fmt.Errorf("主机名 %q 非法：须以字母数字开头与结尾", h)
		}
		for i := 0; i < len(label); i++ {
			if !isHostAlnum(label[i]) && label[i] != '-' && label[i] != '_' {
				return fmt.Errorf("主机名含非法字符 %q", string(label[i]))
			}
		}
	}
	return nil
}

func isHostAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// AddHost 服务端添加一台允许访问的内网主机：运行中经 admin API 热生效，同时持久化。
// 默认本机 127.0.0.1 恒可用，无需添加。
func (a *App) AddHost(host string) (*HostOpResult, error) {
	host = strings.TrimSpace(host)
	if strings.EqualFold(host, "127.0.0.1") {
		return nil, errors.New("本机 127.0.0.1 默认已允许，无需重复添加")
	}
	if err := validateHostInput(host); err != nil {
		return nil, err
	}
	a.mu.Lock()
	running := a.running
	addr, tok := a.adminAddr, a.adminToken
	a.mu.Unlock()

	if running {
		if addr == "" {
			return nil, errors.New("Agent 管理通道未就绪，请停止后重新启动连接")
		}
		if _, err := adminCall(http.MethodPost, addr, tok, "/hosts", map[string]any{"host": host}); err != nil {
			return nil, err
		}
	}
	a.updateHosts(func(list []string) []string {
		for _, h := range list {
			if strings.EqualFold(h, host) {
				return list
			}
		}
		return append(list, host)
	})
	if running {
		a.logf("info", "内网主机 %s 已允许访问（即时生效，连接保持不断开）", host)
	} else {
		a.logf("info", "内网主机 %s 已保存，启动连接后生效", host)
	}
	return &HostOpResult{Applied: running, Host: host}, nil
}

// RemoveHost 服务端移除一台允许访问的内网主机：运行中经 admin API 热生效，同时清理配置。
// 默认本机 127.0.0.1 不可移除；Agent 中不存在该主机时视为已删除，仅清理配置。
func (a *App) RemoveHost(host string) (*HostOpResult, error) {
	host = strings.TrimSpace(host)
	if host == "" {
		return nil, errors.New("主机不能为空")
	}
	if strings.EqualFold(host, "127.0.0.1") {
		return nil, errors.New("本机 127.0.0.1 为默认允许项，不可移除")
	}
	a.mu.Lock()
	running := a.running
	addr, tok := a.adminAddr, a.adminToken
	a.mu.Unlock()

	applied := false
	if running {
		if addr == "" {
			return nil, errors.New("Agent 管理通道未就绪，请停止后重新启动连接")
		}
		if _, err := adminCall(http.MethodDelete, addr, tok, "/hosts/"+url.PathEscape(host), nil); err != nil {
			return nil, err
		}
		applied = true
		a.logf("info", "内网主机 %s 已移除（连接保持不断开）", host)
	} else {
		a.logf("info", "内网主机 %s 已移除", host)
	}
	a.updateHosts(func(list []string) []string {
		out := make([]string, 0, len(list))
		for _, h := range list {
			if strings.EqualFold(h, host) {
				continue
			}
			out = append(out, h)
		}
		return out
	})
	return &HostOpResult{Applied: applied, Host: host}, nil
}

// updateHosts 更新持久化配置中的内网主机列表。
func (a *App) updateHosts(fn func([]string) []string) {
	a.mu.Lock()
	s := a.session
	if s == nil {
		a.mu.Unlock()
		return
	}
	s.Hosts = fn(s.Hosts)
	err := a.saveSession(s)
	a.mu.Unlock()
	if err != nil {
		a.logf("warn", "内网主机配置保存失败：%v", err)
	}
}
