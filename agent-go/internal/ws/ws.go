// Package ws 平台信令 WebSocket 客户端：JSON 帧、10s 心跳、5s 自动重连。
package ws

import (
	"context"
	"encoding/json"
	"net/http"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"liyou-agent/internal/logx"
)

// Frame 信令帧。
type Frame struct {
	Type string         `json:"type"`
	Data map[string]any `json:"data,omitempty"`
}

// Client 信令客户端。
type Client struct {
	URL   string
	Token string

	OnFrame     func(Frame)          // 收到信令帧
	OnConnected func()               // 每次连接成功（重连后需重新申报端口）

	mu   sync.Mutex
	conn *websocket.Conn
}

// New 创建客户端。
func New(addr, token string) *Client {
	return &Client{URL: addr, Token: token}
}

// Send 发送信令帧（未连接时静默丢弃）。
func (c *Client) Send(t string, data map[string]any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.conn == nil {
		return
	}
	_ = c.conn.WriteJSON(Frame{Type: t, Data: data})
}

// Connected 是否已连接。
func (c *Client) Connected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.conn != nil
}

// Run 主循环：连接 → 读帧 + 心跳 → 断开 5 秒后重连。
func (c *Client) Run(ctx context.Context) {
	for {
		if ctx.Err() != nil {
			return
		}
		// 设备令牌经 Authorization: Bearer 头携带（与平台侧约定），不放入 URL，
		// 避免令牌出现在访问日志与代理日志中。
		header := http.Header{}
		header.Set("Authorization", "Bearer "+c.Token)
		conn, resp, err := websocket.DefaultDialer.Dial(c.URL, header)
		if err != nil {
			if resp != nil {
				logx.Warnf("连接平台失败（%v，HTTP %d），5 秒后重试", err, resp.StatusCode)
			} else {
				logx.Warnf("连接平台失败（%v），5 秒后重试", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(5 * time.Second):
			}
			continue
		}
		c.mu.Lock()
		c.conn = conn
		c.mu.Unlock()
		logx.Infof("已连接平台信令通道 %s", c.URL)

		done := make(chan struct{})
		go func() {
			defer close(done)
			c.readLoop(conn)
		}()
		if c.OnConnected != nil {
			c.OnConnected()
		}

		heartbeat := time.NewTicker(10 * time.Second)
	loop:
		for {
			select {
			case <-done:
				break loop
			case <-heartbeat.C:
				c.Send("heartbeat", nil)
			case <-ctx.Done():
				_ = conn.Close()
				heartbeat.Stop()
				return
			}
		}
		heartbeat.Stop()

		c.mu.Lock()
		c.conn = nil
		c.mu.Unlock()
		logx.Warnf("信令连接断开，5 秒后重连")
		select {
		case <-ctx.Done():
			return
		case <-time.After(5 * time.Second):
		}
	}
}

func (c *Client) readLoop(conn *websocket.Conn) {
	for {
		_, raw, err := conn.ReadMessage()
		if err != nil {
			return
		}
		var f Frame
		if json.Unmarshal(raw, &f) != nil || f.Type == "" {
			continue
		}
		if f.Type == "pong" {
			continue
		}
		if c.OnFrame != nil {
			c.OnFrame(f)
		}
	}
}
