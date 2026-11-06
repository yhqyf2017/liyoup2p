// Package mobile 励友 P2P 移动端绑定层（gomobile 导出面，仅客户端角色）。
//
// 复用 internal/* 网络栈（ws 信令、UDP 打洞、smux 多路复用、AES-GCM 可靠层、中继兜底），
// 不重写任何网络协议。跨语言边界只用基础类型与 JSON 字符串，规避 gomobile 对
// map / struct 切片的限制；日志经 LogListener 回调推给 Flutter。
package mobile

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"liyou-agent/internal/client"
	"liyou-agent/internal/config"
	"liyou-agent/internal/logx"
	"liyou-agent/internal/ws"
)

// LogListener 日志回调接口（Flutter 侧实现，OnLog 在 Go 子线程被调用，须尽快返回）。
type LogListener interface {
	OnLog(level, text string)
}

var (
	mu       sync.Mutex
	cancel   context.CancelFunc
	gen      int
	cl       *client.Client
	wc       *ws.Client
	deviceUID string
	logMu    sync.Mutex
	logSink  LogListener
	tapOnce  sync.Once
)

// SetLogCallback 注册 Go 日志回调（传 nil 取消）。重复注册覆盖前次。
func SetLogCallback(l LogListener) {
	logMu.Lock()
	logSink = l
	logMu.Unlock()
	tapOnce.Do(installLogTap)
}

// installLogTap 把进程 stdout/stderr 重定向到管道并逐行回调。
// logx 底层是 fmt.Println/os.Stderr，换掉 os.Stdout/os.Stderr 即可统一接管。
func installLogTap() {
	r, w, err := os.Pipe()
	if err != nil {
		return
	}
	os.Stdout = w
	os.Stderr = w
	go func() {
		sc := bufio.NewScanner(r)
		sc.Buffer(make([]byte, 64*1024), 1024*1024)
		for sc.Scan() {
			line := strings.TrimRight(sc.Text(), "\r\n ")
			if line == "" {
				continue
			}
			level := "info"
			switch {
			case strings.Contains(line, "[错误]"):
				level = "error"
			case strings.Contains(line, "[警告]"):
				level = "warn"
			}
			logMu.Lock()
			sink := logSink
			logMu.Unlock()
			if sink != nil {
				sink.OnLog(level, line)
			}
		}
	}()
}

// Login 登录平台并注册客户端设备（role=client）。
// 流程：POST /api/auth/login 换 access_token → POST /api/devices/register 签发设备令牌。
// 成功返回 JSON：{platformUrl, platformWs, username, tenant, deviceUid, deviceName, deviceToken}。
func Login(platformURL, tenant, user, pass, devName string) (string, error) {
	base, err := normalizeBaseURL(platformURL)
	if err != nil {
		return "", err
	}
	if user == "" || pass == "" {
		return "", errors.New("账号和密码不能为空")
	}
	if strings.TrimSpace(devName) == "" {
		devName = "Android设备"
	}

	env, err := postJSON(base+"/api/auth/login", map[string]any{
		"username": user, "password": pass, "tenant": tenant, "client_type": "mobile",
	}, "")
	if err != nil {
		return "", err
	}
	if env.Code != 0 {
		if env.Code == 4016 { // 同端类型已被新登录顶替
			return "", errors.New("账号已在其他手机登录，请重新登录")
		}
		return "", errors.New(env.Msg)
	}
	var loginData struct {
		AccessToken string `json:"access_token"`
		User        struct {
			ID       int64  `json:"id"`
			Username string `json:"username"`
			TenantID int64  `json:"tenant_id"`
			Tenant   string `json:"tenant"`
		} `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &loginData); err != nil {
		return "", fmt.Errorf("登录响应解析失败：%w", err)
	}

	mu.Lock()
	reuseUID := deviceUID
	mu.Unlock()
	env, err = postJSON(base+"/api/devices/register", map[string]any{
		"name": devName, "role": "client", "tenant_id": loginData.User.TenantID,
		"device_uid": reuseUID,
	}, loginData.AccessToken)
	if err != nil {
		return "", err
	}
	if env.Code != 0 {
		if env.Code == 4016 { // 会话被其他手机顶替
			return "", errors.New("账号已在其他手机登录，请重新登录")
		}
		return "", errors.New("注册设备失败：" + env.Msg)
	}
	var devData struct {
		DeviceUID   string `json:"device_uid"`
		DeviceToken string `json:"device_token"`
	}
	if err := json.Unmarshal(env.Data, &devData); err != nil {
		return "", fmt.Errorf("设备注册响应解析失败：%w", err)
	}
	mu.Lock()
	deviceUID = devData.DeviceUID
	mu.Unlock()

	out, _ := json.Marshal(map[string]string{
		"platformUrl": base,
		"platformWs":  wsURL(base),
		"username":    loginData.User.Username,
		"tenant":      loginData.User.Tenant,
		"deviceUid":   devData.DeviceUID,
		"deviceName":  devName,
		"deviceToken": devData.DeviceToken,
	})
	return string(out), nil
}

// StartClient 启动客户端：本地监听映射入口 → 平台信令 → 按需打洞/中继建立隧道。
// mapsJSON 格式：[{"host":"192.168.5.20","remote":80,"local":"18080"}]（host 选填）。
func StartClient(deviceToken, platformWS, mapsJSON string) error {
	mu.Lock()
	if cancel != nil {
		mu.Unlock()
		return errors.New("客户端已在运行，请先停止")
	}
	var maps []config.PortMap
	if err := json.Unmarshal([]byte(mapsJSON), &maps); err != nil {
		mu.Unlock()
		return fmt.Errorf("映射配置解析失败：%w", err)
	}
	if len(maps) == 0 {
		mu.Unlock()
		return errors.New("至少需要一条端口映射")
	}
	gen++
	runGen := gen
	mu.Unlock()

	cfg := &config.Config{
		Role:        "client",
		Platform:    platformWS,
		Name:        deviceUID,
		DeviceToken: deviceToken,
		Maps:        maps,
		Punch:       true,
	}
	logx.SetLabel("Agent·客户端")
	logx.Infof("LiYou P2P Agent 启动：%s", cfg)

	ctx, c := context.WithCancel(context.Background())
	w := ws.New(cfg.Platform, deviceToken)
	c2 := client.New(cfg, w)
	c2.Bind()

	mu.Lock()
	cancel = c
	wc = w
	cl = c2
	mu.Unlock()

	go func() {
		go w.Run(ctx)
		if err := c2.Run(ctx); err != nil && ctx.Err() == nil {
			logx.Errorf("客户端入口异常：%v", err)
		}
		mu.Lock()
		if runGen == gen {
			cancel = nil
			cl = nil
			wc = nil
		}
		mu.Unlock()
		logx.Infof("Agent 已停止")
	}()
	return nil
}

// AddMapping 运行中热增一条映射（本地监听即时生效，隧道不受影响）。
func AddMapping(host string, remote int, local string) error {
	mu.Lock()
	c := cl
	mu.Unlock()
	if c == nil {
		return errors.New("客户端未运行")
	}
	_, err := c.AddMap(host, remote, local)
	return err
}

// RemoveMapping 运行中热删一条映射（按 host+remote+local 匹配，停止对应本地监听）。
func RemoveMapping(host string, remote int, local string) error {
	mu.Lock()
	c := cl
	mu.Unlock()
	if c == nil {
		return errors.New("客户端未运行")
	}
	host = strings.TrimSpace(host)
	for _, m := range c.ListMaps() {
		if m.Host == host && m.Remote == remote && normalizeLocal(m.Local) == normalizeLocal(local) {
			return c.RemoveMap(m.ID)
		}
	}
	return fmt.Errorf("映射 %s:%d:%s 不存在", host, remote, local)
}

// Stop 停止客户端：断开信令、关闭全部映射监听与隧道。
func Stop() {
	mu.Lock()
	c := cancel
	mu.Unlock()
	if c != nil {
		c()
	}
}

// GetStatus 当前状态 JSON：{running, wsConnected, maps:[{host,remote,local,addr,listening}]}。
func GetStatus() string {
	mu.Lock()
	c := cl
	w := wc
	mu.Unlock()
	st := map[string]any{"running": c != nil, "wsConnected": false, "maps": []any{}}
	if c != nil && w != nil {
		st["wsConnected"] = w.Connected()
		ms := make([]map[string]any, 0)
		for _, m := range c.ListMaps() {
			ms = append(ms, map[string]any{
				"host": m.Host, "remote": m.Remote, "local": m.Local,
				"addr": m.Addr, "listening": m.Listening,
			})
		}
		st["maps"] = ms
	}
	out, _ := json.Marshal(st)
	return string(out)
}

// ---------- 工具 ----------

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func normalizeBaseURL(u string) (string, error) {
	u = strings.TrimSpace(u)
	if u == "" {
		return "", errors.New("服务器地址不能为空")
	}
	if strings.HasPrefix(u, "ws://") || strings.HasPrefix(u, "wss://") {
		return strings.TrimRight(u, "/"), nil
	}
	if !strings.Contains(u, "://") {
		u = "http://" + u
	}
	u = strings.TrimRight(u, "/")
	return u, nil
}

func wsURL(base string) string {
	if strings.HasPrefix(base, "wss://") || strings.HasPrefix(base, "ws://") {
		if strings.HasSuffix(base, "/ws") {
			return base
		}
		return base + "/ws"
	}
	ws := strings.Replace(base, "https://", "wss://", 1)
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
	cli := &http.Client{Timeout: 10 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return env, fmt.Errorf("无法连接平台 %s：%w", url, err)
	}
	defer resp.Body.Close()
	if err := json.NewDecoder(resp.Body).Decode(&env); err != nil {
		return env, fmt.Errorf("平台响应解析失败（HTTP %d）：%w", resp.StatusCode, err)
	}
	return env, nil
}

func normalizeLocal(local string) string {
	local = strings.TrimSpace(local)
	if strings.Contains(local, ":") {
		return local
	}
	return "127.0.0.1:" + local
}

// 保持 io 引用（scanner 使用），避免部分 linter 误报未使用。
var _ io.Reader = (*bytes.Reader)(nil)
