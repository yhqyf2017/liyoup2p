// Package client 客户端角色：本机监听入口端口，浏览器连接优先复用持久多路复用隧道
// （打洞/中继成功后仅核准一次，之后按需开子流）；隧道不可用时现场重建，复用不可行时
// 降级为旧版「每连接一会话」路径。
package client

import (
	"context"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/xtaci/smux/v2"

	"liyou-agent/internal/admin"
	"liyou-agent/internal/config"
	"liyou-agent/internal/logx"
	"liyou-agent/internal/muxlink"
	"liyou-agent/internal/punch"
	"liyou-agent/internal/reliable"
	"liyou-agent/internal/relaydial"
	"liyou-agent/internal/tunnel"
	"liyou-agent/internal/ws"
)

const (
	muxWait        = 8 * time.Second  // 复用协商等待回显时长
	muxUnsupported = 10 * time.Minute // 判定对端不支持复用后的降级缓存时长
)

// Client 客户端角色。
type Client struct {
	cfg *config.Config
	ws  *ws.Client

	mu       sync.Mutex
	pending  map[string]*session // request_id -> 等待 session_start
	sessions map[string]*session // session_id -> 活跃会话

	lnMu    sync.Mutex
	entries map[uint64]*mapEntry // 映射 ID → 本地监听（运行中可动态增删）
	nextID  uint64

	muxMu               sync.Mutex
	mux                 *muxTunnel  // 当前持久复用隧道（nil 表示无）
	est                 *muxEst     // 进行中的隧道建立（单飞，并发连接共享）
	fallbackSess        *session    // 复用协商失败但通路尚存的降级会话
	muxUnsupportedUntil time.Time   // 对端旧版不支持复用的降级缓存
}

type grantInfo struct {
	Addr  string
	Token string
}

type session struct {
	requestID string
	id        string
	browser   net.Conn
	key       [32]byte
	mode      string

	remotePort int    // 本会话对应映射的远端目标端口
	mapDesc    string // 映射描述（如 80:18080），用于日志定位是哪条映射

	startCh  chan struct{}
	offerCh  chan []string
	grantCh  chan grantInfo
	resultCh chan bool

	udp     *net.UDPConn
	udpPeer *net.UDPAddr
	relay   net.Conn

	// 旧版降级：协商期间收到的对端首帧（旧版对端在复用协商窗口内发出的数据）
	pendType    byte
	pendPayload []byte
	pendOK      bool

	muxTs *muxTunnel // 复用模式下关联的持久隧道（信令关闭时联动清理）

	done chan struct{}
	once sync.Once
}

func (s *session) closeAll() {
	s.once.Do(func() { close(s.done) })
	if s.udp != nil {
		s.udp.Close()
	}
	if s.relay != nil {
		s.relay.Close()
	}
	if s.browser != nil {
		s.browser.Close()
	}
}

// muxTunnel 持久复用隧道：一条 smux 会话 + 底层承载（可靠 UDP 流或中继连接）。
type muxTunnel struct {
	id      string
	sess    *smux.Session
	carrier io.Closer
	owner   *session // 关联的平台会话（断开时联动清理）
}

func (t *muxTunnel) close() {
	t.sess.Close()
	if t.carrier != nil {
		t.carrier.Close()
	}
}

// muxEst 一次隧道建立（单飞结果共享）。
type muxEst struct {
	done chan struct{}
	ts   *muxTunnel
	err  error
}

// New 创建客户端角色。
func New(cfg *config.Config, w *ws.Client) *Client {
	return &Client{cfg: cfg, ws: w, pending: make(map[string]*session), sessions: make(map[string]*session), entries: make(map[uint64]*mapEntry)}
}

// Bind 绑定信令回调。
func (c *Client) Bind() {
	c.ws.OnFrame = c.onFrame
	c.ws.OnConnected = c.onConnected
}

func (c *Client) onConnected() {
	var b strings.Builder
	for _, s := range c.ListMaps() {
		if s.Host != "" {
			fmt.Fprintf(&b, " %s:%d→%s", s.Host, s.Remote, s.Addr)
		} else {
			fmt.Fprintf(&b, " %d→%s", s.Remote, s.Addr)
		}
	}
	logx.Infof("信令通道就绪，端口映射:%s（本机访问本地端口即达服务端内网对应服务）", b.String())
}

// Run 启动本地入口监听并常驻：初始映射逐条注册，ctx 结束时全部关闭。
// 运行中可经管理通道（AddMap/RemoveMap）热增删映射，隧道不受影响。
func (c *Client) Run(ctx context.Context) error {
	for _, m := range c.cfg.Maps {
		if _, err := c.AddMap(m.Host, m.Remote, m.Local); err != nil {
			c.closeAllEntries()
			return fmt.Errorf("本地入口监听失败（映射 %s）：%w", m, err)
		}
	}
	<-ctx.Done()
	c.closeAllEntries()
	return nil
}

// mapEntry 一条已注册的本地映射监听（支持运行中动态增删）。
type mapEntry struct {
	id     uint64
	m      config.PortMap
	ln     net.Listener
	closed chan struct{} // 主动删除时关闭，accept 循环据此静默退出
	once   sync.Once
	dead   atomic.Bool // 非主动原因失效（监听异常）
}

func (e *mapEntry) stop() { e.once.Do(func() { close(e.closed); _ = e.ln.Close() }) }

// AddMap 注册并启动一条本地映射监听：仅新增本地监听，持久隧道与多路复用会话完全不动。
// host 为空表示服务端本机（127.0.0.1），否则为服务端内网目标主机（须在服务端白名单内）。
// 返回映射 ID（供删除）。
func (c *Client) AddMap(host string, remote int, local string) (uint64, error) {
	host = strings.TrimSpace(host)
	if host != "" {
		if err := config.ValidateHost(host); err != nil {
			return 0, fmt.Errorf("目标主机 %q 非法：%w", host, err)
		}
	}
	if remote < 1 || remote > 65535 {
		return 0, fmt.Errorf("远端端口 %d 非法（1-65535）", remote)
	}
	local = strings.TrimSpace(local)
	if local == "" {
		return 0, errors.New("本地监听地址不能为空")
	}
	if !strings.Contains(local, ":") {
		p, err := strconv.Atoi(local)
		if err != nil || p < 1 || p > 65535 {
			return 0, fmt.Errorf("本地端口 %q 非法（1-65535）", local)
		}
	}
	m := config.PortMap{Host: host, Remote: remote, Local: local}
	addr := m.LocalAddr()

	c.lnMu.Lock()
	defer c.lnMu.Unlock()
	for _, e := range c.entries {
		if e.m.Host == m.Host && e.m.Remote == remote && e.m.LocalAddr() == addr {
			return 0, fmt.Errorf("映射 %s 已存在", m)
		}
		if e.m.LocalAddr() == addr {
			return 0, fmt.Errorf("本地入口 %s 已被映射 %s 占用", addr, e.m)
		}
	}
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return 0, fmt.Errorf("本地监听 %s 启动失败（端口可能已被其他程序占用）：%w", addr, err)
	}
	c.nextID++
	e := &mapEntry{id: c.nextID, m: m, ln: ln, closed: make(chan struct{})}
	c.entries[e.id] = e
	go c.acceptLoop(e)
	if host != "" {
		logx.Infof("映射 %s 已监听 %s：本机访问 http://%s 即达服务端内网主机 %s 的端口 %d", m, addr, addr, host, remote)
	} else {
		logx.Infof("映射 %s 已监听 %s：本机访问 http://%s 即达服务端端口 %d", m, addr, addr, remote)
	}
	return e.id, nil
}

// RemoveMap 停止并移除指定映射的本地监听；已建立的连接不中断。
func (c *Client) RemoveMap(id uint64) error {
	c.lnMu.Lock()
	e, ok := c.entries[id]
	if ok {
		delete(c.entries, id)
	}
	c.lnMu.Unlock()
	if !ok {
		return fmt.Errorf("映射 #%d 不存在", id)
	}
	e.stop()
	logx.Infof("映射 %s 已停止监听（入口 %s 已关闭）", e.m, e.m.LocalAddr())
	return nil
}

// ListMaps 当前映射列表及各监听状态（管理通道查询用）。
func (c *Client) ListMaps() []admin.MapStatus {
	c.lnMu.Lock()
	defer c.lnMu.Unlock()
	ids := make([]uint64, 0, len(c.entries))
	for id := range c.entries {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	out := make([]admin.MapStatus, 0, len(ids))
	for _, id := range ids {
		e := c.entries[id]
		out = append(out, admin.MapStatus{
			ID: e.id, Host: e.m.Host, Remote: e.m.Remote, Local: e.m.Local,
			Addr: e.m.LocalAddr(), Listening: !e.dead.Load(),
		})
	}
	return out
}

// acceptLoop 单条映射的本地接入循环；监听被删除时静默退出，异常时标记失效。
func (c *Client) acceptLoop(e *mapEntry) {
	for {
		conn, err := e.ln.Accept()
		if err != nil {
			select {
			case <-e.closed: // 主动删除，静默退出
			default:
				e.dead.Store(true)
				logx.Errorf("映射 %s 入口 %s 监听异常：%v（该映射已停止服务，可删除后重新添加）", e.m, e.m.LocalAddr(), err)
			}
			return
		}
		m := e.m
		go c.handleConn(conn, m)
	}
}

func (c *Client) closeAllEntries() {
	c.lnMu.Lock()
	old := c.entries
	c.entries = make(map[uint64]*mapEntry)
	c.lnMu.Unlock()
	for _, e := range old {
		e.stop()
	}
}

func (c *Client) onFrame(f ws.Frame) {
	switch f.Type {
	case "session_start":
		c.onSessionStart(f.Data)
	case "relay_grant":
		sid, _ := f.Data["session_id"].(string)
		if sess := c.getByID(sid); sess != nil {
			addr, _ := f.Data["relay_addr"].(string)
			token, _ := f.Data["grant_token"].(string)
			select {
			case sess.grantCh <- grantInfo{Addr: addr, Token: token}:
			default:
			}
		}
	case "punch_offer":
		sid, _ := f.Data["session_id"].(string)
		if sess := c.getByID(sid); sess != nil {
			select {
			case sess.offerCh <- punch.ParseCandidates(f.Data):
			default:
			}
		}
	case "punch_result":
		sid, _ := f.Data["session_id"].(string)
		if sess := c.getByID(sid); sess != nil {
			ok, _ := f.Data["success"].(bool)
			select {
			case sess.resultCh <- ok:
			default:
			}
		}
	case "session_close":
		sid, _ := f.Data["session_id"].(string)
		reason, _ := f.Data["reason"].(string)
		if sess := c.getByID(sid); sess != nil {
			logx.Infof("会话 %s 对端关闭：%s", sid, reason)
			if sess.muxTs != nil {
				c.dropMux(sess.muxTs)
			}
			sess.closeAll()
		}
	case "error":
		msg, _ := f.Data["msg"].(string)
		if reqID, _ := f.Data["request_id"].(string); reqID != "" {
			if sess := c.takePending(reqID); sess != nil {
				logx.Warnf("会话请求被平台拒绝（映射 %s）：%s", sess.mapDesc, msg)
				close(sess.startCh)
				return
			}
		}
		logx.Warnf("平台提示：%s", msg)
	case "ports_updated":
		uid, _ := f.Data["device_uid"].(string)
		raw, _ := f.Data["ports"].([]any)
		var b strings.Builder
		for _, item := range raw {
			if m, ok := item.(map[string]any); ok {
				port := int(num(m["port"]))
				label, _ := m["label"].(string)
				if declared, _ := m["declared"].(bool); declared {
					fmt.Fprintf(&b, " %d(%s)", port, label)
				}
			}
		}
		logx.Infof("服务端口更新 [%s] 可穿透端口:%s", uid, b.String())
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

func (c *Client) onSessionStart(data map[string]any) {
	reqID, _ := data["request_id"].(string)
	sess := c.takePending(reqID)
	if sess == nil {
		return
	}
	sid, _ := data["session_id"].(string)
	mode, _ := data["mode"].(string)
	keyHex, _ := data["key"].(string)
	key, err := hex.DecodeString(keyHex)
	if err != nil || len(key) != 32 {
		logx.Errorf("会话密钥非法")
		close(sess.startCh)
		return
	}
	copy(sess.key[:], key)
	sess.id = sid
	sess.mode = mode
	c.mu.Lock()
	c.sessions[sid] = sess
	c.mu.Unlock()
	logx.Infof("会话 %s 获平台核准：映射 %s（远端端口 %d），模式=%s", sid, sess.mapDesc, sess.remotePort, modeLabel(mode))
	close(sess.startCh)
}

// handleConn 处理一个本地接入连接：优先在持久隧道上开子流；
// 隧道不存在/已死则单飞重建；复用不可行时降级为旧版单连接会话。
func (c *Client) handleConn(browser net.Conn, m config.PortMap) {
	if ts := c.currentMux(); ts != nil {
		if c.serveViaMux(ts, browser, m) {
			return
		}
	}
	if ts, err := c.ensureMux(m.Remote, m.String()); err == nil {
		if c.serveViaMux(ts, browser, m) {
			return
		}
	}
	if sess := c.takeFallback(); sess != nil {
		// 复用协商失败但传输通路仍可用（旧版对端）：直接用该会话做单连接桥接
		sess.browser = browser
		c.remove(sess.id, sess.requestID)
		c.bridge(sess)
		return
	}
	c.legacyConn(browser, m)
}

// serveViaMux 在持久隧道上开子流：首帧携带目标端口（u16；指定目标主机时另携主机字段），
// 随后双向转发。
func (c *Client) serveViaMux(ts *muxTunnel, browser net.Conn, m config.PortMap) bool {
	stream, err := ts.sess.OpenStream()
	if err != nil {
		logx.Warnf("持久隧道 %s 开子流失败：%v（将现场重建隧道）", ts.id, err)
		c.dropMux(ts)
		return false
	}
	if _, err := stream.Write(streamHead(m)); err != nil {
		stream.Close()
		return false
	}
	logx.CountActivity() // 子流开闭属高频事件，聚合输出避免日志风暴
	if m.Host != "" {
		go pipeMux(browser, stream, m) // 指定目标主机：先探测服务端拒绝帧再转入常规转发
	} else {
		go pipeConn(browser, stream)
	}
	return true
}

// streamHead 构造子流首帧。Host 为空时与旧版完全一致：[目标端口 u16]；否则为扩展帧
// [0x0000][主机长度 u16][目标主机][目标端口 u16]——旧版端口恒 ≥1，服务端可安全区分。
func streamHead(m config.PortMap) []byte {
	if m.Host == "" {
		var prefix [2]byte
		binary.BigEndian.PutUint16(prefix[:], uint16(m.Remote))
		return prefix[:]
	}
	hb := []byte(m.Host)
	buf := make([]byte, 2+2+len(hb)+2)
	binary.BigEndian.PutUint16(buf[2:4], uint16(len(hb)))
	copy(buf[4:], hb)
	binary.BigEndian.PutUint16(buf[4+len(hb):], uint16(m.Remote))
	return buf
}

// pipeMux 双向转发；先限时探测子流首批字节，命中服务端拒绝错误帧时输出明确提示并断开。
func pipeMux(browser net.Conn, stream *smux.Stream, m config.PortMap) {
	defer browser.Close()
	defer stream.Close()
	head := make([]byte, len(muxlink.DenyMagic))
	_ = stream.SetReadDeadline(time.Now().Add(3 * time.Second))
	n, err := io.ReadFull(stream, head)
	_ = stream.SetReadDeadline(time.Time{})
	if err == nil && muxlink.IsDeny(head) {
		reason, _ := io.ReadAll(io.LimitReader(stream, 512))
		logx.Warnf("映射 %s：目标主机未被服务器允许：%s", m, strings.TrimSpace(string(reason)))
		return
	}
	if n > 0 {
		if _, werr := browser.Write(head[:n]); werr != nil {
			return
		}
	}
	pipeConn(browser, stream)
}

// pipeConn 双向转发直至任一侧结束。
func pipeConn(a net.Conn, b io.ReadWriteCloser) {
	defer a.Close()
	defer b.Close()
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(b, a); done <- struct{}{} }()
	go func() { _, _ = io.Copy(a, b); done <- struct{}{} }()
	<-done
}

// currentMux 取当前可用的持久隧道。
func (c *Client) currentMux() *muxTunnel {
	c.muxMu.Lock()
	defer c.muxMu.Unlock()
	if c.mux != nil && c.mux.sess.IsClosed() {
		c.mux = nil
		return nil
	}
	return c.mux
}

// ensureMux 取用或单飞重建持久隧道；并发到达的连接共享同一次建立结果。
func (c *Client) ensureMux(remotePort int, mapDesc string) (*muxTunnel, error) {
	c.muxMu.Lock()
	if ts := c.mux; ts != nil && !ts.sess.IsClosed() {
		c.muxMu.Unlock()
		return ts, nil
	}
	if time.Now().Before(c.muxUnsupportedUntil) {
		c.muxMu.Unlock()
		return nil, muxlink.ErrMuxUnsupported
	}
	if c.est != nil {
		est := c.est
		c.muxMu.Unlock()
		<-est.done
		return est.ts, est.err
	}
	est := &muxEst{done: make(chan struct{})}
	c.est = est
	c.muxMu.Unlock()

	ts, err := c.establishMux(remotePort, mapDesc)
	est.ts, est.err = ts, err
	close(est.done)

	c.muxMu.Lock()
	c.est = nil
	c.muxMu.Unlock()
	return ts, err
}

// establishMux 现场建立持久隧道：平台核准（一次性）→ 打洞/中继 → 复用协商。
func (c *Client) establishMux(remotePort int, mapDesc string) (*muxTunnel, error) {
	sess, err := c.negotiate(remotePort, mapDesc)
	if err != nil {
		return nil, err
	}
	ts, err := c.handshakeMux(sess)
	if err != nil {
		if sess.usable() {
			// 传输通路尚存（旧版对端已在发数据或 UDP 判定静默）：保留给本次/后续连接降级桥接
			c.stashFallback(sess)
		} else {
			c.ws.Send("session_close", map[string]any{"session_id": sess.id, "reason": "复用协商失败"})
		}
		if errors.Is(err, muxlink.ErrMuxUnsupported) {
			c.markMuxUnsupported()
		}
		return nil, err
	}
	return ts, nil
}

// negotiate 走平台核准并建立传输通路（打洞优先、中继兜底），不桥接业务数据。
func (c *Client) negotiate(remotePort int, mapDesc string) (sess *session, err error) {
	reqID := randHex(8)
	sess = &session{
		requestID:  reqID,
		remotePort: remotePort,
		mapDesc:    mapDesc,
		startCh:    make(chan struct{}),
		offerCh:    make(chan []string, 4),
		grantCh:    make(chan grantInfo, 4),
		resultCh:   make(chan bool, 4),
		done:       make(chan struct{}),
	}
	c.mu.Lock()
	c.pending[reqID] = sess
	c.mu.Unlock()
	// 具名返回值 sess 在 return nil 路径会被置空，defer 须用注册时的引用做清理，
	// 否则平台拒绝/超时等失败路径会空指针崩溃。
	reg := sess
	defer func() {
		if err != nil {
			reg.closeAll()
			c.remove(reg.id, reg.requestID)
		}
	}()

	c.ws.Send("session_request", map[string]any{
		"request_id":        reqID,
		"server_device_uid": c.cfg.ServerUID,
		"port":              remotePort,
	})
	select {
	case <-sess.startCh:
	case <-sess.done:
		return nil, errors.New("会话请求中止")
	case <-time.After(10 * time.Second):
		// 平台正常应立即放行或即时拒绝（error 帧会触发上面的快速失败），
		// 走到这里说明信令链路异常，作为兜底直接放弃本次请求。
		logx.Warnf("会话请求未得到响应（映射 %s），请确认平台与服务端在线", mapDesc)
		return nil, errors.New("会话请求超时")
	}
	if sess.id == "" {
		return nil, errors.New("会话核准异常")
	}
	if !c.setupTransport(sess) {
		return nil, errors.New("传输通路建立失败")
	}
	return sess, nil
}

func (c *Client) setupTransport(sess *session) bool {
	transportOK := false
	if sess.mode == "p2p" && c.setupP2P(sess) {
		transportOK = true
	}
	if !transportOK {
		// 打洞失败回落中继：请求中继凭证
		if sess.mode == "p2p" {
			logx.Warnf("会话 %s：打洞失败，请求中继兜底", sess.id)
			c.ws.Send("session_fallback", map[string]any{"session_id": sess.id})
		}
		if !c.setupRelay(sess) {
			return false
		}
	}
	return true
}

// usable 传输通路是否仍可用于降级桥接。
func (s *session) usable() bool {
	if s.relay != nil {
		return true
	}
	return s.udp != nil && s.udpPeer != nil
}

// handshakeMux 在已建立的传输上协商持久复用会话；成功后传输连接所有权移交隧道。
func (c *Client) handshakeMux(sess *session) (ts *muxTunnel, err error) {
	var (
		raw  io.ReadWriteCloser
		path string
	)
	switch {
	case sess.udp != nil && sess.udpPeer != nil:
		if err := c.udpMagic(sess); err != nil {
			return nil, err
		}
		raw = reliable.New(sess.udp, sess.udpPeer, sess.key)
		sess.udp = nil // 所有权移交可靠层
		path = punch.PathLabel(sess.udpPeer)
	case sess.relay != nil:
		// 仅做魔数交换，会话统一由下方 smux.Client 建立；
		// 若在此处调用 DialClient 会产生第二个 smux 会话，两个 recvLoop
		// 竞争同一条中继流导致帧被撕碎、响应被幽灵会话吞掉。
		if err := muxlink.ExchangeClient(sess.relay, muxWait); err != nil {
			return nil, err
		}
		raw = sess.relay
		sess.relay = nil // 所有权移交复用会话
		path = "中继"
	default:
		return nil, errors.New("无可用传输")
	}
	muxSess, err := smux.Client(raw, muxlink.Config())
	if err != nil {
		raw.Close()
		return nil, err
	}
	ts = &muxTunnel{id: sess.id, sess: muxSess, carrier: raw, owner: sess}
	sess.muxTs = ts
	c.setMux(ts)
	go c.watchMux(ts)
	logx.Infof("持久隧道已建立：会话 %s 路径=%s（复用就绪，后续子流无需平台核准）", sess.id, path)
	return ts, nil
}

// udpMagic UDP 路径复用协商：以原始加密报文交换魔数；期间若收到旧版隧道帧，
// 判定对端为旧版并暂存首帧供降级桥接。
func (c *Client) udpMagic(sess *session) error {
	_, err := sess.udp.WriteToUDP(muxlink.SealMagic(sess.key), sess.udpPeer)
	if err != nil {
		return err
	}
	_ = sess.udp.SetReadDeadline(time.Now().Add(muxWait))
	defer sess.udp.SetReadDeadline(time.Time{})
	buf := make([]byte, 2048)
	for {
		n, _, rerr := sess.udp.ReadFromUDP(buf)
		if rerr != nil {
			return muxlink.ErrMuxUnsupported
		}
		plain, derr := tunnel.Open(sess.key, buf[:n])
		if derr == nil {
			if muxlink.IsMagic(plain) {
				return nil
			}
			continue
		}
		if t, payload, ok := muxlink.ParseLegacyUDP(sess.key, buf[:n]); ok {
			// 旧版对端已在发数据：缓存判定并保留通路
			c.markMuxUnsupported()
			sess.pendType, sess.pendPayload, sess.pendOK = t, payload, true
			return muxlink.ErrMuxUnsupported
		}
		// 打洞残留探测等非法报文，忽略
	}
}

// watchMux 监视持久隧道：断开后清理并通知平台（后续连接将现场重建隧道）。
func (c *Client) watchMux(ts *muxTunnel) {
	for {
		if _, err := ts.sess.AcceptStream(); err != nil {
			break
		}
	}
	c.dropMux(ts)
	if ts.owner != nil {
		ts.owner.closeAll()
		c.remove(ts.owner.id, ts.owner.requestID)
	}
	c.ws.Send("session_close", map[string]any{"session_id": ts.id, "reason": "持久隧道断开"})
}

func (c *Client) setMux(ts *muxTunnel) {
	c.muxMu.Lock()
	old := c.mux
	c.mux = ts
	c.muxMu.Unlock()
	if old != nil && old != ts {
		old.close()
	}
}

func (c *Client) dropMux(ts *muxTunnel) {
	c.muxMu.Lock()
	if c.mux == ts {
		c.mux = nil
	}
	c.muxMu.Unlock()
	ts.close()
}

func (c *Client) markMuxUnsupported() {
	c.muxMu.Lock()
	already := time.Now().Before(c.muxUnsupportedUntil)
	c.muxUnsupportedUntil = time.Now().Add(muxUnsupported)
	c.muxMu.Unlock()
	if !already {
		logx.Warnf("对端不支持多路复用（旧版 Agent？），新连接将在 %v 内直接使用单连接会话", muxUnsupported)
	}
}

// stashFallback 暂存复用协商失败但通路尚存的会话，供一条连接直接降级桥接；
// 30 秒内无人认领则结束该会话，避免资源滞留。
func (c *Client) stashFallback(sess *session) {
	c.muxMu.Lock()
	old := c.fallbackSess
	c.fallbackSess = sess
	c.muxMu.Unlock()
	if old != nil && old != sess {
		c.discardFallback(old)
	}
	go func() {
		time.Sleep(30 * time.Second)
		c.muxMu.Lock()
		mine := c.fallbackSess == sess
		if mine {
			c.fallbackSess = nil
		}
		c.muxMu.Unlock()
		if mine {
			c.discardFallback(sess)
		}
	}()
}

func (c *Client) discardFallback(sess *session) {
	c.ws.Send("session_close", map[string]any{"session_id": sess.id, "reason": "复用协商失败"})
	sess.closeAll()
	c.remove(sess.id, sess.requestID)
}

// takeFallback 取走降级会话（仅一条连接可复用）。
func (c *Client) takeFallback() *session {
	c.muxMu.Lock()
	defer c.muxMu.Unlock()
	sess := c.fallbackSess
	c.fallbackSess = nil
	return sess
}

// legacyConn 旧版单连接降级路径：每条接入连接单独核准会话并桥接。
func (c *Client) legacyConn(browser net.Conn, m config.PortMap) {
	sess, err := c.negotiate(m.Remote, m.String())
	if err != nil {
		browser.Close()
		return
	}
	defer func() {
		sess.closeAll()
		c.remove(sess.id, sess.requestID)
	}()
	sess.browser = browser
	c.bridge(sess)
}

// setupP2P 建立 UDP 直连；失败时释放 UDP 套接字，确保后续走中继。
func (c *Client) setupP2P(sess *session) (ok bool) {
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
	c.ws.Send("punch_offer", map[string]any{"session_id": sess.id, "candidates": cands})

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
	c.ws.Send("punch_result", map[string]any{"session_id": sess.id, "success": mineOK})
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

// setupRelay 接入中继：优先用平台下发的 relay_addr（ws:// 走 WS 多路复用，host:port 走 TCP），
// 未下发时从信令地址自行推导（旧平台兼容，先 WS 后 TCP 8901）。
func (c *Client) setupRelay(sess *session) bool {
	select {
	case g := <-sess.grantCh:
		conn, addr, err := relaydial.Dial(g.Addr, c.cfg.Platform, "client", sess.id, g.Token)
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

// bridge 双向桥接浏览器 ↔ 隧道（旧版单连接模式）。
func (c *Client) bridge(sess *session) {
	// 隧道 → 浏览器
	go func() {
		defer sess.closeAll()
		if sess.pendOK {
			sess.pendOK = false
			if sess.pendType == tunnel.FrameClose {
				return
			}
			if _, err := sess.browser.Write(sess.pendPayload); err != nil {
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
			case sess.relay != nil:
				t, payload, err = tunnel.ReadFrame(sess.relay, sess.key)
			default:
				return
			}
			if err != nil {
				return
			}
			switch t {
			case tunnel.FrameData:
				if _, err := sess.browser.Write(payload); err != nil {
					return
				}
			case tunnel.FrameClose:
				return
			}
		}
	}()

	// 浏览器 → 隧道
	defer func() {
		c.writeClose(sess)
		c.ws.Send("session_close", map[string]any{"session_id": sess.id, "reason": "客户端关闭"})
		sess.closeAll()
	}()
	buf := make([]byte, tunnel.UDPChunk)
	for {
		n, err := sess.browser.Read(buf)
		if n > 0 {
			if werr := c.writeData(sess, tunnel.FrameData, buf[:n]); werr != nil {
				logx.Warnf("会话 %s：向隧道写入失败：%v", sess.id, werr)
				return
			}
		}
		if err != nil {
			logx.Infof("会话 %s：浏览器连接结束：%v", sess.id, err)
			return
		}
	}
}

func (c *Client) writeData(sess *session, t byte, payload []byte) error {
	if sess.udp != nil && sess.udpPeer != nil {
		return tunnel.WriteUDPFrame(sess.udp, sess.udpPeer, t, payload, sess.key)
	}
	if sess.relay != nil {
		return tunnel.WriteFrame(sess.relay, t, payload, sess.key)
	}
	return fmt.Errorf("隧道未就绪")
}

func (c *Client) writeClose(sess *session) {
	_ = c.writeData(sess, tunnel.FrameClose, nil)
}

func (c *Client) takePending(reqID string) *session {
	c.mu.Lock()
	defer c.mu.Unlock()
	sess := c.pending[reqID]
	delete(c.pending, reqID)
	return sess
}

func (c *Client) getByID(id string) *session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.sessions[id]
}

func (c *Client) remove(id, reqID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.pending, reqID)
	if id != "" {
		delete(c.sessions, id)
	}
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

func randHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
