// Package hub 设备 WebSocket 信令网关：上线/心跳/端口申报/会话协调/打洞透传。
package hub

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"liyou-platform/internal/auth"
	"liyou-platform/internal/relay"
	"liyou-platform/internal/store"
)

const (
	readWait    = 35 * time.Second // 心跳 10s，超时 30s+
	writeWait   = 10 * time.Second
	maxMsgSize  = 1 << 20
	grantExpiry = 10 * time.Minute
)

// Options hub 配置。
type Options struct {
	RelayPublicAddr string // 中继对外地址（覆盖按信令入口的自动推导），支持 ws://host/relay 或 host:port
}

// Hub 信令网关。
type Hub struct {
	st     *store.Store
	jm     *auth.Manager
	grants *relay.GrantTable
	opts   Options

	mu       sync.Mutex
	conns    map[int64]*Conn           // device_id -> conn
	pending  map[string]*pendingReq    // request_id -> 请求
	sessions map[string]*sessionState  // session_id -> 会话
}

// Conn 单个设备连接。
type Conn struct {
	hub    *Hub
	ws     *websocket.Conn
	wmu    sync.Mutex
	device store.Device
	host   string // WS 请求 Host（信令入口 host[:port]，用于同源推导中继地址）
	scheme string // 信令入口对外协议：ws / wss（TLS 直连或反代 X-Forwarded-Proto 均识别）
	ip     string
	closed bool
}

// pendingReq 待确认的会话请求。
type pendingReq struct {
	ID        string
	TenantID  int64
	ClientID  int64
	ClientUID string
	ServerID  int64
	Port      int
}

// sessionState 活跃会话。
type sessionState struct {
	ID        string
	TenantID  int64
	ClientID  int64
	ClientUID string
	ServerID  int64
	ServerUID string
	Port      int
	Mode      string
	Mux       bool // 已进入持久多路复用模式（后续子流无需平台参与）
}

type frame struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

// New 创建 hub。
func New(st *store.Store, jm *auth.Manager, grants *relay.GrantTable, opts Options) *Hub {
	return &Hub{
		st: st, jm: jm, grants: grants, opts: opts,
		conns:    make(map[int64]*Conn),
		pending:  make(map[string]*pendingReq),
		sessions: make(map[string]*sessionState),
	}
}

// ServeWS 处理 /ws。设备令牌经 Authorization: Bearer 头携带，
// 不再放在 URL query，避免令牌落入访问日志/代理日志。
func (h *Hub) ServeWS(w http.ResponseWriter, r *http.Request) {
	token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
	if token == "" {
		http.Error(w, "缺少设备令牌（须以 Authorization: Bearer 头携带）", http.StatusUnauthorized)
		return
	}
	claims, err := h.jm.Parse(token)
	if err != nil || auth.ClaimType(claims) != auth.TypeDevice {
		http.Error(w, "设备令牌无效", http.StatusUnauthorized)
		return
	}
	deviceID := auth.ClaimInt64(claims, "did")
	deviceUID := auth.ClaimStr(claims, "duid")
	device, err := h.st.FindDeviceByID(r.Context(), deviceID)
	if err != nil || device == nil {
		http.Error(w, "设备不存在", http.StatusUnauthorized)
		return
	}
	if device.Status == "revoked" || device.DeviceUID != deviceUID {
		http.Error(w, "设备已被吊销", http.StatusForbidden)
		return
	}
	sum := sha256.Sum256([]byte(token))
	if device.TokenHash != hex.EncodeToString(sum[:]) {
		http.Error(w, "设备令牌已失效，请重新注册", http.StatusUnauthorized)
		return
	}

	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ws, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	c := &Conn{hub: h, ws: ws, device: *device, host: r.Host, scheme: relayScheme(r), ip: clientIP(r.RemoteAddr)}
	h.register(c)
	defer h.unregister(c)

	if err := h.st.SetDeviceOnline(context.Background(), device.ID); err != nil {
		log.Printf("[信令] 更新设备上线状态失败: %v", err)
	}
	h.broadcastTo(c.device.TenantID, frame{Type: "device_online", Data: map[string]any{
		"device_uid": c.device.DeviceUID, "name": c.device.Name, "role": c.device.Role,
	}}, c.device.ID)
	c.send(frame{Type: "hello", Data: map[string]any{
		"device_id": c.device.ID, "device_uid": c.device.DeviceUID, "role": c.device.Role, "tenant_id": c.device.TenantID,
	}})
	log.Printf("[信令] 设备上线 %s(%s) 角色=%s 租户=%d IP=%s", c.device.Name, c.device.DeviceUID, c.device.Role, c.device.TenantID, c.ip)

	for {
		ws.SetReadLimit(maxMsgSize)
		ws.SetReadDeadline(time.Now().Add(readWait))
		_, raw, err := ws.ReadMessage()
		if err != nil {
			break
		}
		var f frame
		if err := json.Unmarshal(raw, &f); err != nil || f.Type == "" {
			continue
		}
		h.dispatch(c, f)
	}
}

// Kick 强制断开设备连接（吊销时调用）。
func (h *Hub) Kick(deviceID int64) {
	h.mu.Lock()
	c := h.conns[deviceID]
	h.mu.Unlock()
	if c != nil {
		c.send(frame{Type: "kick", Data: map[string]any{"reason": "设备已被吊销"}})
		c.close()
	}
}

func (h *Hub) register(c *Conn) {
	h.mu.Lock()
	if old := h.conns[c.device.ID]; old != nil {
		old.closed = true
		old.ws.Close()
	}
	c.hub.conns[c.device.ID] = c
	// 旧连接被顶替（如客户端异常退出后重新登录）：旧连接名下的待处理请求与
	// 会话在此一并作废。必须锁内同步清理——旧连接读循环随后触发的 unregister
	// 会因「非当前连接」直接返回，若在此漏清，新连接刚建立的会话会被误删，
	// 导致 punch_offer 转发失败（打洞必败）与中继凭证无法下发（兜底也超时）。
	var affected []*sessionState
	for id, pr := range h.pending {
		if pr.ClientID == c.device.ID || pr.ServerID == c.device.ID {
			delete(h.pending, id)
		}
	}
	for id, s := range h.sessions {
		if s.ClientID == c.device.ID || s.ServerID == c.device.ID {
			affected = append(affected, s)
			delete(h.sessions, id)
		}
	}
	h.mu.Unlock()
	for _, s := range affected {
		h.notifyPeerClosed(s, c.device.ID, "对端重新上线，旧会话已作废")
		h.endSession(s, c.device.DeviceUID, "设备重新上线，旧会话作废")
	}
}

func (h *Hub) unregister(c *Conn) {
	h.mu.Lock()
	// 仅当前连接有权清理设备状态：被新连接顶替的旧连接（异常退出后重登录）
	// 直接返回，否则会误删新连接正在建立的会话，造成打洞失败回落中继且
	// 中继凭证无法下发的连锁故障。
	if cur := h.conns[c.device.ID]; cur != c {
		h.mu.Unlock()
		return
	}
	delete(h.conns, c.device.ID)
	// 清理该设备相关的待处理请求
	for id, pr := range h.pending {
		if pr.ClientID == c.device.ID || pr.ServerID == c.device.ID {
			delete(h.pending, id)
		}
	}
	// 结束该设备参与且未结束的会话
	affected := make([]*sessionState, 0, 2)
	for id, s := range h.sessions {
		if s.ClientID == c.device.ID || s.ServerID == c.device.ID {
			affected = append(affected, s)
			delete(h.sessions, id)
		}
	}
	h.mu.Unlock()

	_ = h.st.SetDeviceOffline(context.Background(), c.device.ID)
	h.broadcastTo(c.device.TenantID, frame{Type: "device_offline", Data: map[string]any{
		"device_uid": c.device.DeviceUID, "name": c.device.Name, "role": c.device.Role,
	}}, c.device.ID)
	log.Printf("[信令] 设备下线 %s(%s)", c.device.Name, c.device.DeviceUID)
	for _, s := range affected {
		h.notifyPeerClosed(s, c.device.ID, "对端已断线，会话关闭")
		h.endSession(s, c.device.DeviceUID, "设备断开连接")
	}
	c.close()
}

func (h *Hub) dispatch(c *Conn, f frame) {
	switch f.Type {
	case "heartbeat":
		c.send(frame{Type: "pong", Data: map[string]any{"t": time.Now().Unix()}})
	case "declare_ports":
		h.onDeclarePorts(c, f.Data)
	case "session_request":
		h.onSessionRequest(c, f.Data)
	case "session_confirm":
		h.onSessionConfirm(c, f.Data)
	case "punch_offer", "punch_answer", "punch_result":
		h.forwardSession(c, f)
	case "session_fallback":
		h.onSessionFallback(c, f.Data)
	case "mux_ready":
		// 持久多路复用就绪通知：识别并记录，不阻断（后续子流不再经过平台核准）
		h.onMuxReady(c, f.Data)
	case "session_close":
		h.onSessionClose(c, f.Data)
	}
}

// ---------- 端口申报 ----------

func (h *Hub) onDeclarePorts(c *Conn, data map[string]any) {
	if c.device.Role != "server" {
		return
	}
	raw, _ := data["ports"].([]any)
	ports := make([]store.PortInput, 0, len(raw))
	list := make([]map[string]any, 0, len(raw))
	for _, item := range raw {
		m, ok := item.(map[string]any)
		if !ok {
			continue
		}
		pf, _ := m["port"].(float64)
		port := int(pf)
		if port <= 0 || port > 65535 {
			continue
		}
		label, _ := m["label"].(string)
		declared, _ := m["declared"].(bool)
		ports = append(ports, store.PortInput{Port: port, Label: label, Declared: declared})
		list = append(list, map[string]any{"port": port, "label": label, "declared": declared})
	}
	osInfo, _ := data["os"].(string)
	if err := h.st.ReplaceDevicePorts(context.Background(), c.device.ID, ports, osInfo); err != nil {
		log.Printf("[信令] 保存设备端口失败: %v", err)
		return
	}
	log.Printf("[信令] 设备 %s 申报 %d 个端口", c.device.DeviceUID, len(ports))
	h.broadcastTo(c.device.TenantID, frame{Type: "ports_updated", Data: map[string]any{
		"device_uid": c.device.DeviceUID, "device_name": c.device.Name, "ports": list,
	}}, 0)
}

// ---------- 会话协调 ----------

func (h *Hub) onSessionRequest(c *Conn, data map[string]any) {
	port := int(num(data["port"]))
	serverUID, _ := data["server_device_uid"].(string)
	// 沿用客户端生成的 request_id，确保 session_start 能回配到等待中的本地连接
	reqID, _ := data["request_id"].(string)
	if reqID == "" {
		reqID = randomHex(8)
	}
	ctx := context.Background()

	block := func(reason string) {
		log.Printf("[信令] 拒绝会话请求 %s：客户端=%s 目标端口=%d 原因=%s", reqID, c.device.DeviceUID, port, reason)
		_ = h.st.InsertAudit(ctx, store.AuditEntry{
			TenantID: sqlTenant(c.device.TenantID), Actor: c.device.DeviceUID, Action: "blocked",
			Detail: fmt.Sprintf("请求目标端口 %d 被拒绝：%s", port, reason), ClientIP: c.ip,
		})
		_ = h.st.InsertAlert(ctx, sqlTenant(c.device.TenantID), "warn", "连接被拒绝",
			c.device.DeviceUID+" 请求目标端口 "+itoa(port)+"："+reason)
		c.send(frame{Type: "error", Data: map[string]any{"msg": reason, "request_id": reqID}})
	}

	var server *store.Device
	if serverUID != "" {
		d, err := h.st.FindDeviceByUID(ctx, serverUID)
		if err != nil {
			block("查询服务端设备失败")
			return
		}
		server = d
	} else {
		// 未指定服务端：自动选择同租户在线服务端，优先有实时信令连接的设备，
		// 避免 DB 中陈旧的 online 状态（旧设备未正常下线）选中无连接的僵尸记录。
		servers, err := h.st.OnlineServers(ctx, c.device.TenantID)
		if err != nil || len(servers) == 0 {
			block("租户内没有在线的服务端设备")
			return
		}
		for i := range servers {
			if h.connOf(servers[i].ID) != nil {
				server = &servers[i]
				break
			}
		}
		if server == nil {
			server = &servers[0]
		}
	}
	if server == nil {
		block("服务端设备不存在")
		return
	}
	if server.TenantID != c.device.TenantID {
		block("服务端设备不属于当前租户")
		return
	}
	if server.Status != "online" && h.connOf(server.ID) == nil {
		block("服务端设备不在线")
		return
	}
	if server.Role != "server" {
		block("目标设备不是服务端角色")
		return
	}
	if port <= 0 || port > 65535 {
		block("目标端口号非法")
		return
	}

	h.mu.Lock()
	h.pending[reqID] = &pendingReq{
		ID: reqID, TenantID: c.device.TenantID, ClientID: c.device.ID, ClientUID: c.device.DeviceUID,
		ServerID: server.ID, Port: port,
	}
	sc := h.conns[server.ID]
	h.mu.Unlock()
	if sc == nil {
		h.mu.Lock()
		delete(h.pending, reqID)
		h.mu.Unlock()
		block("服务端设备不在线")
		return
	}
	log.Printf("[信令] 会话请求 %s：客户端 %s -> 服务端 %s 端口 %d", reqID, c.device.DeviceUID, server.DeviceUID, port)
	sc.send(frame{Type: "session_request", Data: map[string]any{
		"request_id": reqID, "port": port,
		"client_device_uid": c.device.DeviceUID, "client_name": c.device.Name,
	}})
}

func (h *Hub) onSessionConfirm(sc *Conn, data map[string]any) {
	reqID, _ := data["request_id"].(string)
	mode, _ := data["mode"].(string)
	if mode != "p2p" && mode != "relay" {
		mode = "p2p"
	}
	h.mu.Lock()
	pr := h.pending[reqID]
	if pr == nil || pr.ServerID != sc.device.ID {
		h.mu.Unlock()
		return
	}
	delete(h.pending, reqID)
	cc := h.conns[pr.ClientID]
	h.mu.Unlock()
	if cc == nil {
		return
	}

	sessionID := randomHex(16)
	key := make([]byte, 32)
	_, _ = rand.Read(key)
	ctx := context.Background()
	if err := h.st.CreateSession(ctx, sessionID, pr.TenantID, pr.ClientID, pr.ServerID, pr.Port, mode); err != nil {
		log.Printf("[信令] 写入会话记录失败: %v", err)
		return
	}
	sess := &sessionState{
		ID: sessionID, TenantID: pr.TenantID, ClientID: pr.ClientID, ClientUID: pr.ClientUID,
		ServerID: pr.ServerID, ServerUID: sc.device.DeviceUID, Port: pr.Port, Mode: mode,
	}
	h.mu.Lock()
	h.sessions[sessionID] = sess
	h.mu.Unlock()

	keyHex := hex.EncodeToString(key)
	sc.send(frame{Type: "session_start", Data: map[string]any{
		"session_id": sessionID, "key": keyHex, "mode": mode, "port": pr.Port, "role": "server",
		"request_id": reqID, "client_ip": cc.ip,
	}})
	cc.send(frame{Type: "session_start", Data: map[string]any{
		"session_id": sessionID, "key": keyHex, "mode": mode, "port": pr.Port, "role": "client",
		"request_id": reqID, "server_ip": sc.ip,
	}})

	_ = h.st.InsertAudit(ctx, store.AuditEntry{
		TenantID: sqlTenant(sess.TenantID), Actor: sess.ClientUID, Action: "connect",
		Detail: fmt.Sprintf("会话 %s：客户端 %s → 服务端 %s 端口 %d（%s）", sessionID, sess.ClientUID, sess.ServerUID, sess.Port, modeLabel(mode)),
		ClientIP: cc.ip,
	})
	log.Printf("[信令] 会话建立 %s：客户端 %s → 服务端 %s 端口 %d 模式=%s", sessionID, sess.ClientUID, sess.ServerUID, sess.Port, mode)

	if mode == "relay" {
		h.grantRelay(sess, sc, cc)
	}
}

// onSessionFallback 客户端打洞失败，回落中继。
func (h *Hub) onSessionFallback(c *Conn, data map[string]any) {
	sessionID, _ := data["session_id"].(string)
	h.mu.Lock()
	sess := h.sessions[sessionID]
	var sc, cc *Conn
	if sess != nil && sess.ClientID == c.device.ID {
		sess.Mode = "relay"
		sc = h.conns[sess.ServerID]
		cc = c
	}
	h.mu.Unlock()
	if sess == nil {
		return
	}
	_ = h.st.SetSessionMode(context.Background(), sessionID, "relay")
	log.Printf("[信令] 会话 %s 打洞失败，回落中继", sessionID)
	h.grantRelay(sess, sc, cc)
}

// grantRelay 生成一次性中继凭证并下发给双端，各自携带按其信令入口推导的中继地址。
func (h *Hub) grantRelay(sess *sessionState, sc, cc *Conn) {
	token := h.grants.New(sess.ID)
	if sc != nil {
		sc.send(frame{Type: "relay_grant", Data: map[string]any{
			"session_id": sess.ID, "relay_addr": h.relayAddrFor(sc), "grant_token": token,
		}})
	}
	if cc != nil {
		cc.send(frame{Type: "relay_grant", Data: map[string]any{
			"session_id": sess.ID, "relay_addr": h.relayAddrFor(cc), "grant_token": token,
		}})
	}
}

// relayAddrFor 按该设备自己的信令入口推导中继地址：信令经哪个 host 进来，
// 中继就走同一入口的 ws(s)://<host>/relay（同源同通道，外网无需为 8901 另开映射）。
// RelayPublicAddr 显式配置时优先（兼容 ws://host/relay 与 host:port 两种格式）。
func (h *Hub) relayAddrFor(c *Conn) string {
	if h.opts.RelayPublicAddr != "" {
		return h.opts.RelayPublicAddr
	}
	if c == nil || c.host == "" {
		return ""
	}
	return c.scheme + "://" + c.host + "/relay"
}

// forwardSession 打洞相关帧在会话双端间透传。
func (h *Hub) forwardSession(c *Conn, f frame) {
	sessionID, _ := f.Data["session_id"].(string)
	if sessionID == "" {
		sessionID, _ = f.Data["request_id"].(string)
	}
	h.mu.Lock()
	sess := h.sessions[sessionID]
	var peer *Conn
	if sess != nil {
		if c.device.ID == sess.ClientID {
			peer = h.conns[sess.ServerID]
		} else if c.device.ID == sess.ServerID {
			peer = h.conns[sess.ClientID]
		}
	}
	h.mu.Unlock()
	if peer != nil {
		peer.send(f)
	}
}

// onMuxReady 持久多路复用就绪通知：校验会话归属后标记复用模式，仅识别不阻断。
// 之后的子流开合在两端 Agent 之间直接进行，不再经过平台核准。
func (h *Hub) onMuxReady(c *Conn, data map[string]any) {
	sessionID, _ := data["session_id"].(string)
	h.mu.Lock()
	sess := h.sessions[sessionID]
	ok := sess != nil && (c.device.ID == sess.ClientID || c.device.ID == sess.ServerID)
	if ok {
		sess.Mux = true
	}
	h.mu.Unlock()
	if ok {
		log.Printf("[信令] 会话 %s 进入多路复用模式（后续子流无需平台参与）", sessionID)
	}
}

// onSessionClose 结束会话并记录审计。
func (h *Hub) onSessionClose(c *Conn, data map[string]any) {
	sessionID, _ := data["session_id"].(string)
	reason, _ := data["reason"].(string)
	if reason == "" {
		reason = "对端主动关闭"
	}
	h.mu.Lock()
	sess := h.sessions[sessionID]
	var peer *Conn
	if sess != nil {
		if c.device.ID == sess.ClientID || c.device.ID == sess.ServerID {
			if c.device.ID == sess.ClientID {
				peer = h.conns[sess.ServerID]
			} else {
				peer = h.conns[sess.ClientID]
			}
			delete(h.sessions, sessionID)
		} else {
			sess = nil
		}
	}
	h.mu.Unlock()
	if sess == nil {
		return
	}
	if peer != nil {
		peer.send(frame{Type: "session_close", Data: map[string]any{"session_id": sessionID, "reason": reason}})
	}
	h.endSession(sess, c.device.DeviceUID, reason)
}

// endSession 落库 + 审计。
func (h *Hub) endSession(sess *sessionState, actorUID, reason string) {
	ctx := context.Background()
	_ = h.st.EndSession(ctx, sess.ID)
	_ = h.st.InsertAudit(ctx, store.AuditEntry{
		TenantID: sqlTenant(sess.TenantID), Actor: actorUID, Action: "close",
		Detail: fmt.Sprintf("会话 %s 关闭（端口 %d，%s）：%s", sess.ID, sess.Port, modeLabel(sess.Mode), reason),
	})
	log.Printf("[信令] 会话结束 %s：%s", sess.ID, reason)
}

// notifyPeerClosed 平台强制结束会话时立刻通知存活的对端拆除旧会话。
// 否则对端可靠通道会空转重传 30 秒：既浪费流量，其持续外发流量还会
// 把对端 NAT 中本会话的陈旧映射条目钉住，干扰下一轮会话的首轮打洞。
func (h *Hub) notifyPeerClosed(s *sessionState, closedDeviceID int64, reason string) {
	peerID := s.ClientID
	if closedDeviceID == s.ClientID {
		peerID = s.ServerID
	}
	if peer := h.connOf(peerID); peer != nil {
		peer.send(frame{Type: "session_close", Data: map[string]any{"session_id": s.ID, "reason": reason}})
	}
}

// ---------- 工具 ----------

// broadcastTo 向同租户在线设备广播。
func (h *Hub) broadcastTo(tenantID int64, f frame, excludeDeviceID int64) {
	h.mu.Lock()
	var targets []*Conn
	for id, c := range h.conns {
		if c.device.TenantID == tenantID && id != excludeDeviceID {
			targets = append(targets, c)
		}
	}
	h.mu.Unlock()
	for _, c := range targets {
		c.send(f)
	}
}

// connOf 取设备当前信令连接（加锁）。
func (h *Hub) connOf(deviceID int64) *Conn {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.conns[deviceID]
}

func (c *Conn) send(f frame) {
	raw, err := json.Marshal(f)
	if err != nil {
		return
	}
	c.wmu.Lock()
	defer c.wmu.Unlock()
	if c.closed {
		return
	}
	c.ws.SetWriteDeadline(time.Now().Add(writeWait))
	if err := c.ws.WriteMessage(websocket.TextMessage, raw); err != nil {
		c.closed = true
	}
}

func (c *Conn) close() {
	c.wmu.Lock()
	c.closed = true
	c.wmu.Unlock()
	c.ws.Close()
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func sqlTenant(id int64) sql.NullInt64 {
	return sql.NullInt64{Int64: id, Valid: id > 0}
}

func itoa(n int) string {
	return fmt.Sprintf("%d", n)
}

func modeLabel(mode string) string {
	if mode == "relay" {
		return "中继"
	}
	return "P2P直连"
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func clientIP(remote string) string {
	host, _, err := net.SplitHostPort(remote)
	if err != nil {
		return strings.TrimSpace(remote)
	}
	return host
}

// relayScheme 判定信令入口对外协议：TLS 直连或经反代（X-Forwarded-Proto: https）均视为 wss。
func relayScheme(r *http.Request) string {
	if r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https") {
		return "wss"
	}
	return "ws"
}
