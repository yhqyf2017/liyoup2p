// Package server 服务端角色：接受平台核准的会话，建立 P2P 直连或中继通路后，
// 优先以持久多路复用会话承载业务：每条子流首帧携带目标端口，服务端按需连接
// 本机服务并双向转发；旧版客户端自动降级为「每连接一会话」的单连接桥接。
package server

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"sync"
	"time"

	"os"

	"github.com/xtaci/smux/v2"

	"liyou-agent/internal/config"
	"liyou-agent/internal/logx"
	"liyou-agent/internal/muxlink"
	"liyou-agent/internal/punch"
	"liyou-agent/internal/reliable"
	"liyou-agent/internal/relaydial"
	"liyou-agent/internal/tunnel"
	"liyou-agent/internal/ws"
)

const handshakeWait = 10 * time.Second // 等待复用协商魔数/旧版首帧的时长

// Server 服务端角色。
type Server struct {
	cfg   *config.Config
	ws    *ws.Client
	hosts *HostWhitelist // 可穿透目标主机白名单（--hosts 种子 + admin API 热管理）

	mu       sync.Mutex
	pending  map[string]*session // request_id -> 等待 session_start
	sessions map[string]*session // session_id -> 活跃会话
}

type session struct {
	requestID string
	id        string
	port      int    // 服务端本机目标端口
	peer      string // 来源设备（客户端名称/UID，来自平台转发的会话请求）
	key       [32]byte
	mode      string // p2p | relay（由平台核准结果决定）

	offerCh  chan []string
	grantCh  chan grantInfo
	resultCh chan bool
	startCh  chan struct{}

	udp     *net.UDPConn
	udpPeer *net.UDPAddr
	relay   net.Conn
	target  net.Conn
	mux     *smux.Session // 复用模式下的会话

	// 旧版降级：UDP 协商期间收到的对端首帧
	pendType    byte
	pendPayload []byte
	pendOK      bool

	done chan struct{}
	once sync.Once
}

func (s *session) closeAll() {
	s.once.Do(func() { close(s.done) })
	if s.mux != nil {
		s.mux.Close()
	}
	if s.udp != nil {
		s.udp.Close()
	}
	if s.relay != nil {
		s.relay.Close()
	}
	if s.target != nil {
		s.target.Close()
	}
}

type grantInfo struct {
	Addr  string
	Token string
}

// New 创建服务端角色。
func New(cfg *config.Config, w *ws.Client) *Server {
	seed := cfg.Hosts
	if len(seed) == 0 {
		seed = []string{"127.0.0.1"} // 未指定白名单时保持旧行为：仅可穿透本机
	}
	return &Server{
		cfg:      cfg,
		ws:       w,
		hosts:    NewHostWhitelist(seed),
		pending:  make(map[string]*session),
		sessions: make(map[string]*session),
	}
}

// ListHosts 白名单列表（管理通道查询用）。
func (s *Server) ListHosts() []string { return s.hosts.List() }

// AddHost 白名单热加主机（管理通道调用）。
func (s *Server) AddHost(host string) error { return s.hosts.Add(host) }

// RemoveHost 白名单热删主机（管理通道调用）。
func (s *Server) RemoveHost(host string) error { return s.hosts.Remove(host) }

// Bind 绑定信令回调。
func (s *Server) Bind() {
	s.ws.OnFrame = s.onFrame
	s.ws.OnConnected = s.onConnected
}

func (s *Server) onConnected() {
	logx.Infof("信令通道就绪，服务端口=%d（打洞=%v），等待客户端穿透接入", s.cfg.Port, s.cfg.Punch)
}

func (s *Server) onFrame(f ws.Frame) {
	switch f.Type {
	case "session_request":
		s.onSessionRequest(f.Data)
	case "session_start":
		s.onSessionStart(f.Data)
	case "session_close":
		sid, _ := f.Data["session_id"].(string)
		if sess := s.getByID(sid); sess != nil {
			logx.Infof("会话 %s 客户端关闭：%s", sid, f.Data["reason"])
			s.remove(sid)
			sess.closeAll()
		}
	case "relay_grant":
		sid, _ := f.Data["session_id"].(string)
		if sess := s.getByID(sid); sess != nil {
			addr, _ := f.Data["relay_addr"].(string)
			token, _ := f.Data["grant_token"].(string)
			select {
			case sess.grantCh <- grantInfo{Addr: addr, Token: token}:
			default:
			}
		}
	case "punch_offer":
		sid, _ := f.Data["session_id"].(string)
		if sess := s.getByID(sid); sess != nil {
			select {
			case sess.offerCh <- punch.ParseCandidates(f.Data):
			default:
			}
		}
	case "punch_result":
		sid, _ := f.Data["session_id"].(string)
		if sess := s.getByID(sid); sess != nil {
			ok, _ := f.Data["success"].(bool)
			select {
			case sess.resultCh <- ok:
			default:
			}
		}
	case "error":
		msg, _ := f.Data["msg"].(string)
		logx.Warnf("平台提示：%s", msg)
	case "hello":
		uid, _ := f.Data["device_uid"].(string)
		logx.Infof("平台已确认本机身份 %s", uid)
	case "device_online":
		uid, _ := f.Data["device_uid"].(string)
		logx.Infof("设备上线：%s", uid)
	case "device_offline":
		uid, _ := f.Data["device_uid"].(string)
		logx.Infof("设备下线：%s", uid)
	case "kick":
		logx.Errorf("设备令牌已被吊销，请重新注册：%v", f.Data["reason"])
		os.Exit(1)
	}
}

func (s *Server) onSessionRequest(data map[string]any) {
	reqID, _ := data["request_id"].(string)
	port := int(num(data["port"]))
	if port <= 0 || port > 65535 {
		logx.Warnf("会话请求 %s 被拒：目标端口号非法", reqID)
		s.ws.Send("error", map[string]any{"request_id": reqID, "msg": "目标端口号非法"})
		return
	}
	mode := "p2p"
	if !s.cfg.Punch {
		mode = "relay"
	}
	peer, _ := data["client_name"].(string)
	if uid, _ := data["client_device_uid"].(string); uid != "" {
		if peer != "" {
			peer += "(" + uid + ")"
		} else {
			peer = uid
		}
	}
	sess := &session{
		requestID: reqID,
		port:      port,
		peer:      peer,
		mode:      mode, // 本端偏好；session_start 以平台核准为准
		offerCh:   make(chan []string, 4),
		grantCh:   make(chan grantInfo, 4),
		resultCh:  make(chan bool, 4),
		startCh:   make(chan struct{}),
		done:      make(chan struct{}),
	}
	s.mu.Lock()
	s.pending[reqID] = sess
	s.mu.Unlock()
	s.ws.Send("session_confirm", map[string]any{
		"request_id": reqID,
		"mode":       mode,
	})
	go func() {
		select {
		case <-sess.startCh:
		case <-sess.done:
			return
		case <-time.After(10 * time.Second):
			logx.Warnf("会话请求 %s 未获得平台核准（session_start 超时）", reqID)
			s.mu.Lock()
			delete(s.pending, reqID)
			s.mu.Unlock()
			sess.closeAll()
			return
		}
		s.runSession(sess)
	}()
}

// onSessionStart 平台核准结果：以平台生成的会话 ID 与密钥为准，启动会话主体。
func (s *Server) onSessionStart(data map[string]any) {
	reqID, _ := data["request_id"].(string)
	sess := s.takePending(reqID)
	if sess == nil {
		return
	}
	sid, _ := data["session_id"].(string)
	mode, _ := data["mode"].(string)
	keyHex, _ := data["key"].(string)
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 || sid == "" {
		logx.Errorf("会话核准数据非法")
		sess.closeAll()
		return
	}
	copy(sess.key[:], key)
	sess.id = sid
	sess.mode = mode
	s.mu.Lock()
	s.sessions[sid] = sess
	s.mu.Unlock()
	logx.Infof("会话 %s 获平台核准：目标端口 %d，模式=%s", sid, sess.port, modeLabel(mode))
	close(sess.startCh)
}

// runSession 会话主体：建立传输 → 复用协商 → 按模式承载业务。
func (s *Server) runSession(sess *session) {
	defer s.remove(sess.id)
	defer sess.closeAll()

	transportOK := false
	if sess.mode == "p2p" && s.setupP2P(sess) {
		transportOK = true
	}
	if !transportOK {
		if sess.mode == "p2p" {
			logx.Warnf("会话 %s：打洞失败，请求中继兜底", sess.id)
			s.ws.Send("session_fallback", map[string]any{"session_id": sess.id})
		}
		if !s.setupRelay(sess) {
			s.ws.Send("session_close", map[string]any{"session_id": sess.id, "reason": "隧道建立失败"})
			return
		}
	}

	var (
		muxSess *smux.Session
		streamR io.Reader // 旧版流式降级（中继路径的推回流）
		path    string
	)
	switch {
	case sess.udp != nil:
		p, isMux, err := s.udpHandshake(sess)
		path = p
		if err != nil {
			logx.Errorf("会话 %s：传输异常（路径=%s）：%v", sess.id, path, err)
			s.ws.Send("session_close", map[string]any{"session_id": sess.id, "reason": "传输异常"})
			return
		}
		if isMux {
			muxSess, err = smux.Server(reliable.New(sess.udp, sess.udpPeer, sess.key), muxlink.Config())
			if err != nil {
				logx.Errorf("会话 %s：复用会话建立失败：%v", sess.id, err)
				s.ws.Send("session_close", map[string]any{"session_id": sess.id, "reason": "复用会话建立失败"})
				return
			}
			sess.udp = nil // 所有权移交可靠层
		}
	case sess.relay != nil:
		path = "中继"
		ms, raw, isMux, err := muxlink.AcceptServer(sess.relay, handshakeWait)
		if err != nil {
			logx.Errorf("会话 %s：中继传输异常：%v", sess.id, err)
			s.ws.Send("session_close", map[string]any{"session_id": sess.id, "reason": "中继传输异常"})
			return
		}
		if isMux {
			muxSess = ms
			sess.relay = nil // 所有权移交复用会话
		} else {
			streamR = raw
		}
	default:
		s.ws.Send("session_close", map[string]any{"session_id": sess.id, "reason": "无可用传输"})
		return
	}

	if muxSess != nil {
		s.runMux(sess, muxSess, path)
		return
	}
	s.runLegacy(sess, streamR, path)
}

// udpHandshake UDP 路径复用协商：等待客户端魔数（回显后进入复用模式）或旧版隧道帧
// （暂存首帧后进入旧版单连接模式）；打洞残留探测报文一律忽略。
func (s *Server) udpHandshake(sess *session) (path string, mux bool, err error) {
	_ = sess.udp.SetReadDeadline(time.Now().Add(handshakeWait))
	defer sess.udp.SetReadDeadline(time.Time{})
	buf := make([]byte, 4096)
	for {
		n, from, rerr := sess.udp.ReadFromUDP(buf)
		if rerr != nil {
			// 超时且无任何业务报文：视为旧版静默客户端，进入旧版模式（目标横幅可正常下发）
			if errors.Is(rerr, net.ErrClosed) {
				return "", false, errors.New("套接字已关闭")
			}
			return punch.PathLabel(sess.udpPeer), false, nil
		}
		if from != nil {
			sess.udpPeer = from
		}
		if plain, derr := tunnel.Open(sess.key, buf[:n]); derr == nil {
			if muxlink.IsMagic(plain) {
				_, _ = sess.udp.WriteToUDP(muxlink.SealMagic(sess.key), from)
				return punch.PathLabel(from), true, nil
			}
			continue
		}
		if t, payload, ok := muxlink.ParseLegacyUDP(sess.key, buf[:n]); ok {
			sess.pendType, sess.pendPayload, sess.pendOK = t, payload, true
			return punch.PathLabel(from), false, nil
		}
	}
}

// runMux 复用模式：接受子流，按子流首帧的目标端口连接本地服务并双向转发。
func (s *Server) runMux(sess *session, muxSess *smux.Session, path string) {
	sess.mux = muxSess
	logx.Infof("持久隧道已建立：会话 %s 路径=%s 多路复用就绪（子流按需开合，无需平台逐条核准）", sess.id, path)
	s.ws.Send("mux_ready", map[string]any{"session_id": sess.id, "mode": "mux"})
	for {
		stream, err := muxSess.AcceptStream()
		if err != nil {
			logx.Infof("会话 %s：持久隧道断开（路径=%s）：%v", sess.id, path, err)
			break
		}
		go s.serveStream(sess, stream)
	}
	s.ws.Send("session_close", map[string]any{"session_id": sess.id, "reason": "持久隧道断开"})
}

// maxStreamHostLen 子流首帧目标主机最大长度。
const maxStreamHostLen = 255

// parseStreamHead 兼容解析子流首帧，返回目标主机与端口。首两字节为目标端口（旧版
// v1 格式，端口恒 ≥1）；为 0 时判定为新版 v2 扩展帧：[0x0000][主机长度 u16][目标主机]
// [目标端口 u16]（仅新客户端携带非空目标主机时使用，旧客户端与服务端均可正常互通）。
func parseStreamHead(r io.Reader) (host string, port int, err error) {
	var head [2]byte
	if _, err = io.ReadFull(r, head[:]); err != nil {
		return "", 0, err
	}
	port = int(binary.BigEndian.Uint16(head[:]))
	if port != 0 { // v1 旧版：仅目标端口
		return "", port, nil
	}
	var l [2]byte
	if _, err = io.ReadFull(r, l[:]); err != nil {
		return "", 0, err
	}
	hl := int(binary.BigEndian.Uint16(l[:]))
	if hl > maxStreamHostLen {
		return "", 0, fmt.Errorf("目标主机长度 %d 超限（≤%d）", hl, maxStreamHostLen)
	}
	if hl > 0 {
		hb := make([]byte, hl)
		if _, err = io.ReadFull(r, hb); err != nil {
			return "", 0, err
		}
		host = string(hb)
	}
	var p [2]byte
	if _, err = io.ReadFull(r, p[:]); err != nil {
		return "", 0, err
	}
	return host, int(binary.BigEndian.Uint16(p[:])), nil
}

// serveStream 处理一条子流：解析首帧目标主机/端口，校验白名单后连接目标主机并双向转发。
func (s *Server) serveStream(sess *session, stream *smux.Stream) {
	defer stream.Close()
	host, port, err := parseStreamHead(stream)
	if err != nil {
		return
	}
	if port <= 0 {
		return
	}
	if host != "" {
		if verr := config.ValidateHost(host); verr != nil {
			logx.Warnf("会话 %s 子流：目标主机 %q 格式非法（%v），已拒绝", sess.id, host, verr)
			s.writeDeny(stream, fmt.Sprintf("目标主机 %q 格式非法", host))
			return
		}
	}
	dialIP, ok := s.hosts.Check(host)
	if !ok {
		target := host
		if target == "" {
			target = "127.0.0.1"
		}
		// 拒绝访问一行日志：来源设备 + 目标主机
		logx.Warnf("会话 %s：来源设备 %s 请求目标主机 %q 未被服务器允许（不在白名单），子流已拒绝", sess.id, sess.peer, target)
		s.writeDeny(stream, fmt.Sprintf("目标主机 %s 未被服务器允许（不在服务端白名单）", target))
		return
	}
	target, err := net.DialTimeout("tcp", net.JoinHostPort(dialIP, strconv.Itoa(port)), 3*time.Second)
	if err != nil {
		logx.Warnf("会话 %s 子流 %d：%s:%d 连接失败：%v", sess.id, stream.ID(), dialIP, port, err)
		return
	}
	defer target.Close()
	if host != "" {
		// 非默认目标主机（用户显式指定的穿透目标），逐条记录实际拨号地址便于审计
		logx.Infof("会话 %s：来源设备 %s → 目标 %s:%d 转发中", sess.id, sess.peer, dialIP, port)
	}
	logx.CountActivity() // 子流开闭属高频事件，聚合输出避免日志风暴
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(stream, target); done <- struct{}{} }()
	go func() { _, _ = io.Copy(target, stream); done <- struct{}{} }()
	<-done
}

// writeDeny 回写拒绝错误帧：魔数 + 原因文本，随后由 defer 关闭子流。
func (s *Server) writeDeny(stream *smux.Stream, reason string) {
	buf := make([]byte, 0, len(muxlink.DenyMagic)+len(reason))
	buf = append(buf, muxlink.DenyMagic[:]...)
	buf = append(buf, reason...)
	_, _ = stream.Write(buf)
}

// runLegacy 旧版单连接模式：按会话端口连接本地服务，桥接隧道帧。
func (s *Server) runLegacy(sess *session, streamR io.Reader, path string) {
	target, err := net.DialTimeout("tcp", "127.0.0.1:"+strconv.Itoa(sess.port), 3*time.Second)
	if err != nil {
		reason := fmt.Sprintf("服务器上无服务监听端口 %d", sess.port)
		logx.Errorf("会话 %s：%s（127.0.0.1:%d 连接失败：%v）", sess.id, reason, sess.port, err)
		s.ws.Send("session_close", map[string]any{"session_id": sess.id, "reason": reason})
		return
	}
	sess.target = target
	logx.Infof("会话 %s：已连接本地服务 127.0.0.1:%d，单连接转发中（路径=%s）", sess.id, sess.port, path)
	go s.pumpTunnelToTarget(sess, streamR)
	s.pumpTargetToTunnel(sess)
}

// setupP2P 建立 UDP 直连；失败时释放 UDP 套接字，确保后续走中继。
func (s *Server) setupP2P(sess *session) (ok bool) {
	defer func() {
		if !ok && sess.udp != nil {
			sess.udp.Close()
			sess.udp = nil
		}
	}()
	udp, err := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4zero})
	if err != nil {
		logx.Errorf("会话 %s：创建 UDP 套接字失败：%v", sess.id, err)
		return false
	}
	tunnel.SetUDPBuffer(udp, logx.Infof)
	sess.udp = udp
	cands := punch.BuildCandidates(udp, logx.Infof)
	logx.Infof("会话 %s：本端候选 %v，等待对端候选", sess.id, cands)
	s.ws.Send("punch_offer", map[string]any{"session_id": sess.id, "candidates": cands})

	var peerCands []string
	select {
	case peerCands = <-sess.offerCh:
	case <-time.After(5 * time.Second):
		logx.Warnf("会话 %s：等待对端候选超时", sess.id)
		return false
	}
	logx.Infof("会话 %s：打洞目标 %v", sess.id, peerCands)
	targets := punch.ResolveTargets(peerCands, logx.Infof)
	if len(targets) == 0 {
		logx.Warnf("会话 %s：对端候选均不可用", sess.id)
		return false
	}
	peer, err := punch.Punch(udp, targets, 3*time.Second, sess.key, logx.Infof)
	mineOK := err == nil
	s.ws.Send("punch_result", map[string]any{"session_id": sess.id, "success": mineOK})
	if !mineOK {
		logx.Warnf("会话 %s：%v", sess.id, err)
		return false
	}
	select {
	case peerOK := <-sess.resultCh:
		if peerOK {
			sess.udpPeer = peer
			logx.Infof("会话 %s：UDP 直连已建立（%s 对端 %s）", sess.id, punch.PathLabel(peer), peer)
			return true
		}
	case <-time.After(5 * time.Second):
	}
	logx.Warnf("会话 %s：对端打洞未成功，准备回落中继", sess.id)
	return false
}

// setupRelay 接入中继。
func (s *Server) setupRelay(sess *session) bool {
	select {
	case g := <-sess.grantCh:
		conn, addr, err := relaydial.Dial(g.Addr, s.cfg.Platform, "server", sess.id, g.Token)
		if err != nil {
			logx.Errorf("会话 %s：连接中继失败：%v", sess.id, err)
			return false
		}
		sess.relay = conn
		logx.Infof("会话 %s：已接入中继 %s（平台仅转发，业务数据直达对端）", sess.id, addr)
		return true
	case <-time.After(20 * time.Second):
		logx.Errorf("会话 %s：等待中继凭证超时", sess.id)
		return false
	case <-sess.done:
		return false
	}
}

// pumpTunnelToTarget 隧道 → 本地服务（旧版单连接模式）。
func (s *Server) pumpTunnelToTarget(sess *session, streamR io.Reader) {
	defer sess.closeAll()
	deliver := func(t byte, payload []byte) bool {
		switch t {
		case tunnel.FrameData:
			if _, err := sess.target.Write(payload); err != nil {
				logx.Infof("会话 %s：向本地服务写入失败：%v", sess.id, err)
				return false
			}
		case tunnel.FrameClose:
			logx.Infof("会话 %s：对端请求关闭", sess.id)
			return false
		}
		return true
	}
	if sess.pendOK {
		sess.pendOK = false
		if !deliver(sess.pendType, sess.pendPayload) {
			return
		}
	}
	for {
		var t byte
		var payload []byte
		var err error
		switch {
		case sess.udp != nil:
			t, payload, _, err = tunnel.ReadUDPFrame(sess.udp, sess.key)
			if errors.Is(err, tunnel.ErrDecrypt) || errors.Is(err, tunnel.ErrNotFrame) {
				// 打洞残留探测包等非法报文，跳过
				continue
			}
		case streamR != nil:
			t, payload, err = tunnel.ReadFrame(streamR, sess.key)
		default:
			return
		}
		if err != nil {
			logx.Infof("会话 %s：隧道读取结束：%v", sess.id, err)
			return
		}
		if !deliver(t, payload) {
			return
		}
	}
}

// pumpTargetToTunnel 本地服务 → 隧道（旧版单连接模式）。
func (s *Server) pumpTargetToTunnel(sess *session) {
	defer sess.closeAll()
	buf := make([]byte, tunnel.UDPChunk)
	for {
		n, err := sess.target.Read(buf)
		if n > 0 {
			if werr := s.writeData(sess, tunnel.FrameData, buf[:n]); werr != nil {
				logx.Infof("会话 %s：向隧道写入失败：%v", sess.id, werr)
				return
			}
		}
		if err != nil {
			logx.Infof("会话 %s：本地服务读取结束：%v", sess.id, err)
			return
		}
	}
}

func (s *Server) writeData(sess *session, t byte, payload []byte) error {
	if sess.udp != nil && sess.udpPeer != nil {
		return tunnel.WriteUDPFrame(sess.udp, sess.udpPeer, t, payload, sess.key)
	}
	if sess.relay != nil {
		return tunnel.WriteFrame(sess.relay, t, payload, sess.key)
	}
	return fmt.Errorf("隧道未就绪")
}

func (s *Server) getByID(id string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.sessions[id]
}

func (s *Server) takePending(reqID string) *session {
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.pending[reqID]
	delete(s.pending, reqID)
	return sess
}

func (s *Server) remove(id string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.sessions, id)
}

func num(v any) float64 {
	f, _ := v.(float64)
	return f
}

func modeLabel(mode string) string {
	if mode == "relay" {
		return "中继"
	}
	return "P2P直连"
}
