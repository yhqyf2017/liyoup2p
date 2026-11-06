// Package relay 中继：打洞失败时兜底转发两端密文流量（平台不解密）。
// 接入通道两种：TCP :8901（局域网直连兼容）与 WebSocket /relay（与信令同入口，跨网络可达），
// 两者共用同一套一次性凭证与会话帧格式。
package relay

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net"
	"net/http"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"

	"liyou-platform/internal/store"
)

const pairTimeout = 60 * time.Second

// maxWSMessage WS 中继单条消息上限（隧道帧 ≤1MB + 密文与头部长度余量）。
const maxWSMessage = 2 << 20

// grant 一次性中继凭证：同一会话的 client / server 各可用一次。
type grant struct {
	sessionID string
	clientOK  bool
	serverOK  bool
	expireAt  time.Time
}

// GrantTable 中继凭证表（内存）。
type GrantTable struct {
	mu sync.Mutex
	m  map[string]*grant // token -> grant
}

// NewGrantTable 创建凭证表。
func NewGrantTable() *GrantTable {
	return &GrantTable{m: make(map[string]*grant)}
}

// New 为会话生成一次性凭证。
func (g *GrantTable) New(sessionID string) string {
	b := make([]byte, 24)
	_, _ = rand.Read(b)
	token := hex.EncodeToString(b)
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanup()
	g.m[token] = &grant{sessionID: sessionID, expireAt: time.Now().Add(10 * time.Minute)}
	return token
}

// Consume 校验并消耗凭证（每个方向一次）。
func (g *GrantTable) Consume(token, sessionID, role string) bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.cleanup()
	gr := g.m[token]
	if gr == nil || gr.sessionID != sessionID || time.Now().After(gr.expireAt) {
		return false
	}
	switch role {
	case "client":
		if gr.clientOK {
			return false
		}
		gr.clientOK = true
	case "server":
		if gr.serverOK {
			return false
		}
		gr.serverOK = true
	default:
		return false
	}
	if gr.clientOK && gr.serverOK {
		delete(g.m, token)
	}
	return true
}

func (g *GrantTable) cleanup() {
	now := time.Now()
	for k, gr := range g.m {
		if now.After(gr.expireAt) {
			delete(g.m, k)
		}
	}
}

type handshake struct {
	Role      string `json:"role"`
	SessionID string `json:"session_id"`
	Token     string `json:"token"`
}

type pair struct {
	client net.Conn
	server net.Conn
	timer  *time.Timer
}

// Server 中继服务器（TCP 监听 + HTTP WS 入口）。
type Server struct {
	st     *store.Store
	grants *GrantTable
	ln     net.Listener

	mu    sync.Mutex
	pairs map[string]*pair // session_id -> pair
}

// NewServer 创建中继服务器（不监听）。
func NewServer(st *store.Store, grants *GrantTable) *Server {
	return &Server{st: st, grants: grants, pairs: make(map[string]*pair)}
}

// Run 启动 TCP 监听循环，直到 ctx 取消或出错。
func (s *Server) Run(ctx context.Context, addr string) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("中继监听失败: %w", err)
	}
	s.ln = ln
	log.Printf("[中继] TCP 中继已启动 %s", addr)
	go func() {
		<-ctx.Done()
		ln.Close()
	}()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handle(conn)
	}
}

func (s *Server) handle(conn net.Conn) {
	// 逐字节读首行 JSON 握手，避免 bufio 预读吞掉后续密文。
	// 注意：握手失败时才就地关闭；成功存入配对表的连接由 pump / dropPair 负责关闭，
	// 不能 defer conn.Close()，否则先接入的一端会在等待配对期间被误关。
	line, err := readLine(conn)
	if err != nil {
		conn.Close()
		return
	}
	var hs handshake
	if err := json.Unmarshal(line, &hs); err != nil {
		conn.Close()
		return
	}
	s.admit(hs, conn)
}

// ServeWS WebSocket 中继入口（GET /relay）：Upgrade 后首条消息为 JSON 握手（与 TCP 同一凭证体系），
// 其后每条二进制消息即隧道字节流的一段，边读边转发不落盘，会话帧格式与 TCP 中继完全一致。
func (s *Server) ServeWS(w http.ResponseWriter, r *http.Request) {
	up := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	ws, err := up.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	ws.SetReadLimit(maxWSMessage)
	_, raw, err := ws.ReadMessage()
	if err != nil {
		ws.Close()
		return
	}
	var hs handshake
	if err := json.Unmarshal(bytes.TrimSpace(raw), &hs); err != nil {
		ws.Close()
		return
	}
	s.admit(hs, &wsNetConn{ws: ws})
}

// admit 校验凭证并把连接存入会话配对表，双端齐后开始转发（TCP 与 WS 接入共用）。
func (s *Server) admit(hs handshake, conn net.Conn) {
	if hs.Role != "client" && hs.Role != "server" {
		conn.Close()
		return
	}
	if !s.grants.Consume(hs.Token, hs.SessionID, hs.Role) {
		log.Printf("[中继] 无效中继凭证：会话=%s 角色=%s", hs.SessionID, hs.Role)
		conn.Close()
		return
	}
	log.Printf("[中继] 接入 会话=%s 角色=%s 远端=%s", hs.SessionID, hs.Role, conn.RemoteAddr())

	s.mu.Lock()
	p := s.pairs[hs.SessionID]
	if p == nil {
		p = &pair{timer: time.AfterFunc(pairTimeout, func() { s.dropPair(hs.SessionID) })}
		s.pairs[hs.SessionID] = p
	}
	if hs.Role == "client" {
		p.client = conn
	} else {
		p.server = conn
	}
	both := p.client != nil && p.server != nil
	if both {
		p.timer.Stop()
		delete(s.pairs, hs.SessionID)
	}
	s.mu.Unlock()
	if !both {
		return
	}
	s.pump(hs.SessionID, p.client, p.server)
}

func (s *Server) dropPair(sessionID string) {
	s.mu.Lock()
	p := s.pairs[sessionID]
	delete(s.pairs, sessionID)
	s.mu.Unlock()
	if p != nil {
		if p.client != nil {
			p.client.Close()
		}
		if p.server != nil {
			p.server.Close()
		}
		log.Printf("[中继] 会话 %s 配对超时", sessionID)
	}
}

// pump 双向转发并累计中继流量。
func (s *Server) pump(sessionID string, a, b net.Conn) {
	var total atomic.Int64
	done := make(chan struct{}, 2)
	copyHalf := func(dst, src net.Conn) {
		n, _ := io.Copy(dst, src)
		total.Add(n)
		done <- struct{}{}
	}
	go copyHalf(b, a)
	go copyHalf(a, b)
	<-done
	// 一端结束后关闭双端，促使另一方向尽快结束
	a.Close()
	b.Close()
	<-done
	log.Printf("[中继] 会话 %s 转发结束，流量 %d 字节", sessionID, total.Load())
	if s.st != nil {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := s.st.AddRelayBytes(ctx, sessionID, total.Load()); err != nil {
			log.Printf("[中继] 回写中继流量失败: %v", err)
		}
	}
}

func readLine(conn net.Conn) ([]byte, error) {
	var buf []byte
	one := make([]byte, 1)
	for len(buf) < 4096 {
		if _, err := io.ReadFull(conn, one); err != nil {
			if len(buf) > 0 {
				return buf, nil
			}
			return nil, err
		}
		if one[0] == '\n' {
			return buf, nil
		}
		buf = append(buf, one[0])
	}
	return buf, nil
}

// wsNetConn 把 WebSocket 连接适配为流式 net.Conn：读侧把消息拼接成字节流，写侧每次 Write 发一条二进制消息。
type wsNetConn struct {
	ws  *websocket.Conn
	buf []byte
	wmu sync.Mutex
}

func (c *wsNetConn) Read(p []byte) (int, error) {
	for {
		if len(c.buf) > 0 {
			n := copy(p, c.buf)
			c.buf = c.buf[n:]
			return n, nil
		}
		_, data, err := c.ws.ReadMessage()
		if err != nil {
			return 0, err
		}
		c.buf = data
	}
}

func (c *wsNetConn) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wsNetConn) Close() error         { return c.ws.Close() }
func (c *wsNetConn) LocalAddr() net.Addr  { return c.ws.LocalAddr() }
func (c *wsNetConn) RemoteAddr() net.Addr { return c.ws.RemoteAddr() }
func (c *wsNetConn) SetDeadline(t time.Time) error {
	if err := c.ws.SetReadDeadline(t); err != nil {
		return err
	}
	return c.ws.SetWriteDeadline(t)
}
func (c *wsNetConn) SetReadDeadline(t time.Time) error {
	return c.ws.SetReadDeadline(t)
}
func (c *wsNetConn) SetWriteDeadline(t time.Time) error {
	return c.ws.SetWriteDeadline(t)
}
