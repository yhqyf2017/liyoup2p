// Package relaydial 中继通道拨号：支持 WebSocket 多路复用（ws://、wss://）与原生 TCP（host:port）
// 两种接入方式，握手协议一致——首条消息为 JSON 凭证，之后为端到端加密的隧道帧字节流。
package relaydial

import (
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

// legacyRelayPort 旧平台派生地址兜底的 TCP 中继端口。
const legacyRelayPort = "8901"

type handshake struct {
	Role      string `json:"role"`
	SessionID string `json:"session_id"`
	Token     string `json:"token"`
}

// Dial 建立中继连接并完成角色握手，返回隧道字节流。addr 语义：
//   - ws://host[:port]/relay 或 wss://...：WebSocket 多路复用（平台下发的同源中继地址，优先）
//   - host:port（无 scheme）：TCP 直连（旧平台派生地址或局域网兼容）
//
// addr 为空（旧平台不下发地址）时从信令地址自行推导：先试 ws://<signaling host>/relay，
// 再回退 TCP <signaling host>:8901。
func Dial(addr, signalingURL, role, sessionID, token string) (net.Conn, string, error) {
	hs := handshake{Role: role, SessionID: sessionID, Token: token}
	if addr != "" {
		useWS := strings.HasPrefix(addr, "ws://") || strings.HasPrefix(addr, "wss://")
		c, err := dial(addr, useWS, hs)
		return c, addr, err
	}
	// 旧平台兼容：按信令入口同源推导
	u, err := url.Parse(strings.TrimSpace(signalingURL))
	if err != nil || u.Host == "" {
		return nil, "", fmt.Errorf("中继地址未下发且无法从信令地址 %q 推导", signalingURL)
	}
	scheme := "ws"
	if u.Scheme == "wss" {
		scheme = "wss"
	}
	var errs []error
	wsTarget := scheme + "://" + u.Host + "/relay"
	c, err := dial(wsTarget, true, hs)
	if err == nil {
		return c, wsTarget, nil
	}
	errs = append(errs, fmt.Errorf("%s: %w", wsTarget, err))
	tcpTarget := u.Host + ":" + legacyRelayPort
	c, err = dial(tcpTarget, false, hs)
	if err == nil {
		return c, tcpTarget, nil
	}
	errs = append(errs, fmt.Errorf("%s: %w", tcpTarget, err))
	return nil, "", errors.Join(errs...)
}

// dial 按目标类型建立连接并发送握手（WS 走首条文本消息，TCP 走一行 JSON + 换行）。
func dial(target string, useWS bool, hs handshake) (net.Conn, error) {
	raw, err := json.Marshal(hs)
	if err != nil {
		return nil, err
	}
	if useWS {
		d := websocket.Dialer{
			HandshakeTimeout: 5 * time.Second,
			TLSClientConfig:  &tls.Config{InsecureSkipVerify: true}, // 平台多为自签证书
		}
		ws, _, err := d.Dial(target, nil)
		if err != nil {
			return nil, err
		}
		_ = ws.SetWriteDeadline(time.Now().Add(10 * time.Second))
		if err := ws.WriteMessage(websocket.TextMessage, raw); err != nil {
			ws.Close()
			return nil, err
		}
		return &wsStream{ws: ws}, nil
	}
	conn, err := net.DialTimeout("tcp", target, 5*time.Second)
	if err != nil {
		return nil, err
	}
	if _, err := conn.Write(append(raw, '\n')); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

// wsStream 把 WebSocket 连接适配为流式 net.Conn：读侧把消息拼接成字节流，写侧每次 Write 发一条二进制消息。
type wsStream struct {
	ws  *websocket.Conn
	buf []byte
	wmu sync.Mutex
}

func (c *wsStream) Read(p []byte) (int, error) {
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

func (c *wsStream) Write(p []byte) (int, error) {
	c.wmu.Lock()
	defer c.wmu.Unlock()
	_ = c.ws.SetWriteDeadline(time.Now().Add(30 * time.Second))
	if err := c.ws.WriteMessage(websocket.BinaryMessage, p); err != nil {
		return 0, err
	}
	return len(p), nil
}

func (c *wsStream) Close() error         { return c.ws.Close() }
func (c *wsStream) LocalAddr() net.Addr  { return c.ws.LocalAddr() }
func (c *wsStream) RemoteAddr() net.Addr { return c.ws.RemoteAddr() }
func (c *wsStream) SetDeadline(t time.Time) error {
	if err := c.ws.SetReadDeadline(t); err != nil {
		return err
	}
	return c.ws.SetWriteDeadline(t)
}
func (c *wsStream) SetReadDeadline(t time.Time) error {
	return c.ws.SetReadDeadline(t)
}
func (c *wsStream) SetWriteDeadline(t time.Time) error {
	return c.ws.SetWriteDeadline(t)
}
