// Package muxlink 持久隧道多路复用协商：传输通路建立后，用固定魔数区分
// 「多路复用模式」（smux，keepalive 10s/30s）与「旧版单连接模式」。
// 协商只需一次，之后的子流开合不再经过平台信令。
package muxlink

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"

	"github.com/xtaci/smux/v2"

	"liyou-agent/internal/tunnel"
)

// Magic 复用模式协商魔数（8 字节）。旧版 Agent 会把它当作未知类型帧丢弃（UDP）
// 或因帧长超限断开（中继流式），客户端均可据此识别对端为旧版。
var Magic = [8]byte{'L', 'Y', 'M', 'U', 'X', 'v', '1', 0x01}

// ErrMuxUnsupported 对端不支持多路复用（旧版 Agent 或协商无响应）。
var ErrMuxUnsupported = errors.New("对端不支持多路复用")

// DenyMagic 子流拒绝错误帧魔数：服务端白名单拒绝时作为子流首批字节写出，
// 紧随其后为 UTF-8 原因文本，然后关闭子流；客户端据此识别并输出明确提示。
var DenyMagic = [8]byte{'L', 'Y', 'D', 'E', 'N', 'Y', '1', '!'}

// IsDeny 判断子流首批字节是否为拒绝错误帧。
func IsDeny(b []byte) bool {
	return len(b) == len(DenyMagic) && bytes.Equal(b, DenyMagic[:])
}

// Config smux 会话参数：10s 心跳、30s 无响应判死（3 次）、单帧 8KB。
func Config() *smux.Config {
	cfg := smux.DefaultConfig()
	cfg.KeepAliveInterval = 10 * time.Second
	cfg.KeepAliveTimeout = 30 * time.Second
	cfg.MaxFrameSize = 8192
	cfg.MaxReceiveBuffer = 1 << 20
	cfg.MaxStreamBuffer = 64 << 10
	return cfg
}

// SealMagic 生成加密的魔数探测报文（UDP 路径按单报文交换）。
func SealMagic(key [32]byte) []byte {
	return tunnel.Seal(key, Magic[:])
}

// IsMagic 判断解密后的报文是否为魔数。
func IsMagic(plain []byte) bool {
	return len(plain) == len(Magic) && bytes.Equal(plain, Magic[:])
}

// ParseLegacyUDP 校验原始 UDP 报文是否旧版隧道帧（[1B 类型][4B 长度][密文]）。
// 合法时返回帧类型与明文载荷，用于识别旧版对端并暂存首帧。
func ParseLegacyUDP(key [32]byte, datagram []byte) (byte, []byte, bool) {
	// 最小帧：5B 头 + 12B nonce + 16B tag
	if len(datagram) < 5+28 {
		return 0, nil, false
	}
	t := datagram[0]
	if t != tunnel.FrameData && t != tunnel.FrameClose {
		return 0, nil, false
	}
	n := binary.BigEndian.Uint32(datagram[1:5])
	if int(n) != len(datagram)-5 || n > tunnel.MaxPayload {
		return 0, nil, false
	}
	payload, err := tunnel.Open(key, datagram[5:])
	if err != nil {
		return 0, nil, false
	}
	return t, payload, true
}

// ExchangeClient 流式传输（中继/可靠流）上的客户端协商：发送魔数并等待回显。
// 超时或回显不符一律按 ErrMuxUnsupported 处理（调用方降级或重建）。
func ExchangeClient(rwc io.ReadWriteCloser, wait time.Duration) error {
	if _, err := rwc.Write(Magic[:]); err != nil {
		return err
	}
	if nc, ok := rwc.(net.Conn); ok {
		_ = nc.SetReadDeadline(time.Now().Add(wait))
		defer nc.SetReadDeadline(time.Time{})
	}
	echo := make([]byte, len(Magic))
	if _, err := io.ReadFull(rwc, echo); err != nil {
		return ErrMuxUnsupported
	}
	if !bytes.Equal(echo, Magic[:]) {
		return ErrMuxUnsupported
	}
	return nil
}

// DialClient 流式传输上的客户端协商并建立 smux 会话。
func DialClient(rwc io.ReadWriteCloser, wait time.Duration) (*smux.Session, error) {
	if err := ExchangeClient(rwc, wait); err != nil {
		return nil, err
	}
	return smux.Client(rwc, Config())
}

// AcceptServer 流式传输上的服务端协商：读首 8 字节，命中魔数则回显并建立
// smux 服务端会话；否则判定旧版单连接，返回带推回缓冲的原始流供旧协议桥接。
// 读失败（含等待超时，对应不发魔数的旧版静默客户端）也按旧版处理。
func AcceptServer(rwc io.ReadWriteCloser, wait time.Duration) (*smux.Session, io.Reader, bool, error) {
	if nc, ok := rwc.(net.Conn); ok {
		_ = nc.SetReadDeadline(time.Now().Add(wait))
		defer nc.SetReadDeadline(time.Time{})
	}
	head := make([]byte, len(Magic))
	n, err := io.ReadFull(rwc, head)
	if err == nil && bytes.Equal(head, Magic[:]) {
		if _, err := rwc.Write(Magic[:]); err != nil {
			return nil, nil, false, err
		}
		sess, err := smux.Server(rwc, Config())
		if err != nil {
			return nil, nil, false, err
		}
		return sess, nil, true, nil
	}
	// 旧版单连接：已读字节推回，交由旧协议桥接
	var r io.Reader = rwc
	if n > 0 {
		r = io.MultiReader(bytes.NewReader(head[:n]), rwc)
	}
	return nil, r, false, nil
}
