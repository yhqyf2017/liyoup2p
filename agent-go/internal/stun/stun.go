// Package stun 最小 STUN 客户端（RFC 5389 Binding）：获取 UDP 套接字的公网映射地址。
// 不引第三方库：手工编码 Binding Request（magic cookie + 随机 txid），
// 解析应答中的 XOR-MAPPED-ADDRESS（兼容 MAPPED-ADDRESS）。
package stun

import (
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"sync"
	"time"
)

// DefaultServers 公共 STUN 服务器。
var DefaultServers = []string{"stun.qq.com:3478", "stun.miwifi.com:3478", "stun.l.google.com:19302"}

const (
	cookie0 = 0x21
	cookie1 = 0x12
	cookie2 = 0xA4
	cookie3 = 0x42
)

var (
	mu         sync.Mutex
	lastGood   string    // 上次成功的服务器，优先复用
	lastFailAt time.Time // 最近整体失败时间（静默窗，防重试风暴）
	failQuiet  = 5 * time.Minute
)

// Discover 在 conn 上探测该套接字的公网映射，返回（映射地址, 服务器, 错误）。
// 映射按套接字生成，必须在打洞套接字上探测才有效；单轮超时 2s。
// 优先复用上次成功的服务器，失败后进入 5 分钟静默窗。
func Discover(conn *net.UDPConn, servers []string, timeout time.Duration) (*net.UDPAddr, string, error) {
	mu.Lock()
	if !lastFailAt.IsZero() && time.Since(lastFailAt) < failQuiet {
		mu.Unlock()
		return nil, "", errors.New("近期探测失败，本轮跳过")
	}
	lg := lastGood
	mu.Unlock()

	if lg != "" {
		if pub, _, err := queryAll(conn, []string{lg}, timeout); err == nil {
			return pub, lg, nil
		}
	}
	pub, name, err := queryAll(conn, servers, timeout)
	if err != nil {
		mu.Lock()
		lastFailAt = time.Now()
		mu.Unlock()
		return nil, "", err
	}
	mu.Lock()
	lastGood = name
	lastFailAt = time.Time{}
	mu.Unlock()
	return pub, name, nil
}

// queryAll 并行向所有服务器发 Binding Request，取首个与本轮 txid 匹配的有效应答。
func queryAll(conn *net.UDPConn, servers []string, timeout time.Duration) (*net.UDPAddr, string, error) {
	type pending struct {
		name string
		txid []byte
	}
	var reqs []pending
	for _, name := range servers {
		addr, err := net.ResolveUDPAddr("udp4", name)
		if err != nil {
			continue
		}
		txid := make([]byte, 12)
		_, _ = rand.Read(txid)
		req := make([]byte, 20)
		binary.BigEndian.PutUint16(req[0:2], 0x0001) // Binding Request
		req[4], req[5], req[6], req[7] = cookie0, cookie1, cookie2, cookie3
		copy(req[8:20], txid)
		if _, err := conn.WriteToUDP(req, addr); err != nil {
			continue
		}
		reqs = append(reqs, pending{name: name, txid: txid})
	}
	if len(reqs) == 0 {
		return nil, "", errors.New("无可用 STUN 服务器")
	}
	deadline := time.Now().Add(timeout)
	buf := make([]byte, 1024)
	for {
		_ = conn.SetReadDeadline(deadline)
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return nil, "", fmt.Errorf("STUN 应答超时: %w", err)
		}
		for _, r := range reqs {
			if pub, ok := parseBinding(buf[:n], r.txid); ok {
				return pub, r.name, nil
			}
		}
	}
}

// parseBinding 校验 Binding Success Response 并提取公网映射。
func parseBinding(pkt []byte, txid []byte) (*net.UDPAddr, bool) {
	if len(pkt) < 20 || binary.BigEndian.Uint16(pkt[0:2]) != 0x0101 {
		return nil, false
	}
	if pkt[4] != cookie0 || pkt[5] != cookie1 || pkt[6] != cookie2 || pkt[7] != cookie3 {
		return nil, false
	}
	for i := 8; i < 12; i++ {
		if pkt[i] != txid[i-8] {
			return nil, false
		}
	}
	end := 20 + int(binary.BigEndian.Uint16(pkt[2:4]))
	if end > len(pkt) {
		end = len(pkt)
	}
	i := 20
	for i+4 <= end {
		atype := binary.BigEndian.Uint16(pkt[i : i+2])
		alen := int(binary.BigEndian.Uint16(pkt[i+2 : i+4]))
		if alen < 8 || i+4+alen > end {
			break
		}
		v := pkt[i+4 : i+4+alen]
		switch atype {
		case 0x0020: // XOR-MAPPED-ADDRESS：family(1B) 后 xport/xip 与 magic cookie 异或
			port := int(binary.BigEndian.Uint16(v[2:4])) ^ 0x2112
			ip := net.IPv4(v[4]^cookie0, v[5]^cookie1, v[6]^cookie2, v[7]^cookie3)
			return &net.UDPAddr{IP: ip, Port: port}, true
		case 0x0001: // MAPPED-ADDRESS（明文兜底）
			if v[1] == 0x01 { // IPv4
				return &net.UDPAddr{
					IP:   net.IPv4(v[4], v[5], v[6], v[7]),
					Port: int(binary.BigEndian.Uint16(v[2:4])),
				}, true
			}
		}
		i += 4 + alen
	}
	return nil, false
}
