// Package punch UDP 打洞：双端互换候选端点并互发探测报文，窗口内确认通路。
// 探测报文为 前缀 || 截断 HMAC-SHA256(会话密钥)，无密钥的对端无法伪造有效探测，
// 假探测报文一律忽略、不再影响路径选择。
package punch

import (
	"crypto/hmac"
	"crypto/sha256"
	"fmt"
	"net"
	"sort"
	"strings"
	"time"

	"liyou-agent/internal/stun"
)

// probePrefix 探测报文明文前缀，其后接 16 字节截断 HMAC-SHA256(会话密钥)。
const probePrefix = "LYP2P-PROBE"

// graceWindow 公网路径先确认后等待内网路径确认的宽限时长；
// 局域网内网直达确认通常毫秒级到达，跨网络场景仅多等这一小段。
const graceWindow = 400 * time.Millisecond

// StunServers 公网映射探测服务器（可用服务器会缓存，优先复用）。
var StunServers = stun.DefaultServers

// BuildCandidates 组装本端打洞候选：各网卡私网地址优先，其后 STUN 公网映射（若可得）。
// 映射按套接字生成，因此必须在传入的打洞套接字上探测。
func BuildCandidates(conn *net.UDPConn, logf func(string, ...any)) []string {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	port := conn.LocalAddr().(*net.UDPAddr).Port
	out := localCandidates(port)
	if pub, _, err := stun.Discover(conn, StunServers, 2*time.Second); err == nil {
		logf("STUN 公网探测完成，已取得公网映射候选")
		out = append(out, pub.String())
	} else {
		logf("STUN 公网探测未成功，仅用内网候选")
	}
	return out
}

// ParseCandidates 解析对端候选列表，兼容旧版单 endpoint 字段。
func ParseCandidates(data map[string]any) []string {
	if raw, ok := data["candidates"].([]any); ok {
		var out []string
		for _, v := range raw {
			if s, ok := v.(string); ok {
				if s = strings.TrimSpace(s); s != "" {
					out = append(out, s)
				}
			}
		}
		if len(out) > 0 {
			return out
		}
	}
	if ep, ok := data["endpoint"].(string); ok && ep != "" {
		return []string{ep}
	}
	return nil
}

// ResolveTargets 将候选字符串解析为 UDP 地址，跳过非法项。
func ResolveTargets(cands []string, logf func(string, ...any)) []*net.UDPAddr {
	var out []*net.UDPAddr
	for _, c := range cands {
		addr, err := net.ResolveUDPAddr("udp", c)
		if err != nil {
			if logf != nil {
				logf("部分候选地址解析失败，已跳过")
			}
			continue
		}
		out = append(out, addr)
	}
	return out
}

// SortTargets 打洞目标按路径优先级重排：与本机任一私网 IP 同 /24 的对端私网地址
// 排最前（同子网内网直达），其余私网地址次之，公网映射（srflx）最后；
// 同优先级保持原有顺序（稳定排序）。
func SortTargets(targets []*net.UDPAddr) []*net.UDPAddr {
	return sortTargets(targets, localPrivateIPs())
}

func sortTargets(targets []*net.UDPAddr, locals []net.IP) []*net.UDPAddr {
	priority := func(t *net.UDPAddr) int {
		if t == nil || !isPrivate(t.IP) {
			return 2
		}
		for _, l := range locals {
			if sameV4Subnet(l, t.IP, 24) {
				return 0
			}
		}
		return 1
	}
	out := make([]*net.UDPAddr, len(targets))
	copy(out, targets)
	sort.SliceStable(out, func(i, j int) bool { return priority(out[i]) < priority(out[j]) })
	return out
}

// PathLabel 按对端地址是否私网返回路径类型描述：内网直达 / 公网直连。
func PathLabel(addr *net.UDPAddr) string {
	if addr != nil && isPrivate(addr.IP) {
		return "内网直达"
	}
	return "公网直连"
}

// probePacket 组装探测报文：前缀 || 截断 HMAC-SHA256(key, 前缀)。
func probePacket(key [32]byte) []byte {
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(probePrefix))
	sum := mac.Sum(nil)
	pkt := make([]byte, 0, len(probePrefix)+16)
	pkt = append(pkt, probePrefix...)
	pkt = append(pkt, sum[:16]...)
	return pkt
}

// validProbe 校验报文是否为携带有效 HMAC 的探测包；
// 无会话密钥的对端无法伪造（前缀相同但 HMAC 不符即拒绝）。
func validProbe(key [32]byte, buf []byte) bool {
	if len(buf) != len(probePrefix)+16 || string(buf[:len(probePrefix)]) != probePrefix {
		return false
	}
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(probePrefix))
	return hmac.Equal(mac.Sum(nil)[:16], buf[len(probePrefix):])
}

// Punch 在 window 时间内向对端候选端点循环互发探测，确认双向通路后选定路径。
// 路径选择内网优先：收到对端私网地址探测立即选定；若公网路径先回包，
// 启动宽限窗等待内网路径确认，避免局域网场景误选公网绕行（hairpin）路径，
// 宽限窗超时（典型为跨网络场景无内网通路）才回落公网直连。
// 探测报文带会话密钥 HMAC 校验，伪造探测一律忽略。
func Punch(conn *net.UDPConn, targets []*net.UDPAddr, window time.Duration, key [32]byte, logf func(string, ...any)) (*net.UDPAddr, error) {
	if logf == nil {
		logf = func(string, ...any) {}
	}
	targets = SortTargets(targets)
	logf("打洞目标已就绪（共 %d 个，内网优先）", len(targets))
	deadline := time.Now().Add(window)
	buf := make([]byte, 128)
	probe := probePacket(key)
	var lastErr error
	var publicPath *net.UDPAddr // 公网路径先确认时暂记，宽限窗内等内网路径
	var graceEnd time.Time
	for time.Now().Before(deadline) {
		readEnd := deadline
		if !graceEnd.IsZero() && graceEnd.Before(readEnd) {
			readEnd = graceEnd
		}
		if d := time.Now().Add(150 * time.Millisecond); d.Before(readEnd) {
			readEnd = d
		}
		conn.SetReadDeadline(readEnd)
		if n, from, err := conn.ReadFromUDP(buf); err == nil && validProbe(key, buf[:n]) {
			// 无论本端是否最终选定该路径，先回探帮助对端确认双向通路
			_, _ = conn.WriteToUDP(probe, from)
			if isPrivate(from.IP) {
				// 高优先级路径（对端私网地址）确认，立即选定
				_ = conn.SetReadDeadline(time.Time{})
				logf("收到对端内网探测，UDP 通路确认（选定路径）")
				return from, nil
			}
			if publicPath == nil {
				publicPath = from
				graceEnd = time.Now().Add(graceWindow)
				logf("收到对端公网探测，宽限 %s 等待内网路径确认", graceWindow)
			}
			continue
		}
		if publicPath != nil && !time.Now().Before(graceEnd) {
			// 宽限窗内未等到内网确认（跨网络场景），回落公网直连
			_ = conn.SetReadDeadline(time.Time{})
			logf("宽限窗内未收到内网路径确认，选定公网直连")
			return publicPath, nil
		}
		for _, t := range targets {
			if _, err := conn.WriteToUDP(probe, t); err != nil {
				lastErr = err
			}
		}
	}
	if publicPath != nil {
		// 打洞窗口耗尽时已有公网路径确认，直接采用
		_ = conn.SetReadDeadline(time.Time{})
		logf("打洞窗口结束前仅有公网路径确认，选定公网直连")
		return publicPath, nil
	}
	if lastErr != nil {
		return nil, fmt.Errorf("UDP 打洞失败: %w", lastErr)
	}
	return nil, fmt.Errorf("UDP 打洞窗口超时，未收到对端探测")
}

// localCandidates 枚举本机各网卡的私网 IPv4 地址（排除回环与链路本地）。
func localCandidates(port int) []string {
	seen := map[string]bool{}
	var out []string
	for _, ip := range localPrivateIPs() {
		s := fmt.Sprintf("%s:%d", ip, port)
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	return out
}

// localPrivateIPs 枚举本机各网卡的私网 IPv4 地址（排除回环与链路本地）。
func localPrivateIPs() []net.IP {
	ifaces, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []net.IP
	for _, ifc := range ifaces {
		if ifc.Flags&net.FlagUp == 0 || ifc.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := ifc.Addrs()
		for _, a := range addrs {
			ipnet, ok := a.(*net.IPNet)
			if !ok {
				continue
			}
			ip4 := ipnet.IP.To4()
			if ip4 == nil || !isPrivate(ip4) {
				continue
			}
			out = append(out, ip4)
		}
	}
	return out
}

// sameV4Subnet 判断两个 IPv4 地址是否在同一前缀长度的子网内。
func sameV4Subnet(a, b net.IP, bits int) bool {
	a4, b4 := a.To4(), b.To4()
	if a4 == nil || b4 == nil {
		return false
	}
	m := net.CIDRMask(bits, 32)
	return a4.Mask(m).Equal(b4.Mask(m))
}

func isPrivate(ip net.IP) bool {
	if ip.IsLoopback() || ip.IsLinkLocalUnicast() {
		return false
	}
	return ip.IsPrivate()
}

