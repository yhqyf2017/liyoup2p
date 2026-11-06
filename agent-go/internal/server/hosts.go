package server

import (
	"fmt"
	"net"
	"sort"
	"strings"
	"sync"

	"liyou-agent/internal/config"
	"liyou-agent/internal/logx"
)

// HostWhitelist 服务端可穿透目标主机白名单：仅精确主机（IPv4/IPv6/主机名），
// 禁止 0.0.0.0 与网段通配。主机名条目在加入时解析并保存 IP 集合，同时保留原名（双份）；
// 校验时先把请求的主机解析成 IP 再比对，dial 的也是校验通过的 IP，防止 DNS 绕过。
type HostWhitelist struct {
	mu      sync.RWMutex
	entries map[string][]string // 小写主机名/IP → 加入时解析出的 IP 串列表（IP 字面量条目即自身）
	ipRefs  map[string]int      // 归一化 IP 串 → 被多少条白名单条目引用
}

// NewHostWhitelist 以种子主机（--hosts；空则默认 127.0.0.1）创建白名单。
func NewHostWhitelist(seed []string) *HostWhitelist {
	w := &HostWhitelist{entries: make(map[string][]string), ipRefs: make(map[string]int)}
	for _, h := range seed {
		if err := w.Add(h); err != nil {
			logx.Errorf("白名单初始主机 %q 非法：%v", h, err)
		}
	}
	return w
}

func normalizeHost(host string) string {
	h := strings.ToLower(strings.TrimSpace(host))
	return strings.TrimSuffix(h, ".")
}

// Add 加入白名单：校验 → 解析（IP 字面量即自身）→ 存原名 + IP 集合，输出一行变更日志。
func (w *HostWhitelist) Add(host string) error {
	host = strings.TrimSpace(host)
	if err := config.ValidateHost(host); err != nil {
		return err
	}
	key := normalizeHost(host)
	var ips []string
	if ip := net.ParseIP(key); ip != nil {
		ips = []string{ip.String()}
	} else {
		resolved, err := net.LookupIP(host)
		if err != nil {
			logx.Warnf("白名单主机 %q 解析失败：%v（仍按原名加入）", host, err)
		}
		for _, ip := range resolved {
			s := ip.String()
			if !containsStr(ips, s) {
				ips = append(ips, s)
			}
		}
	}

	w.mu.Lock()
	if _, ok := w.entries[key]; ok {
		w.mu.Unlock()
		return fmt.Errorf("主机 %s 已在白名单", host)
	}
	w.entries[key] = ips
	for _, s := range ips {
		w.ipRefs[s]++
	}
	list := w.listLocked()
	w.mu.Unlock()

	// 白名单变更一行日志
	logx.Infof("白名单已添加主机 %s（解析 IP：%s），当前白名单：[%s]", host, joinOr(ips, "未解析"), strings.Join(list, " "))
	return nil
}

// Remove 移出白名单（含其引用的解析 IP），输出一行变更日志。
func (w *HostWhitelist) Remove(host string) error {
	key := normalizeHost(host)
	w.mu.Lock()
	ips, ok := w.entries[key]
	if ok {
		delete(w.entries, key)
		for _, s := range ips {
			w.ipRefs[s]--
			if w.ipRefs[s] <= 0 {
				delete(w.ipRefs, s)
			}
		}
		list := w.listLocked()
		w.mu.Unlock()
		logx.Infof("白名单已移除主机 %s，当前白名单：[%s]", host, strings.Join(list, " "))
		return nil
	}
	w.mu.Unlock()
	return fmt.Errorf("主机 %s 不在白名单", host)
}

// Check 校验目标主机是否允许穿透，返回应拨号的目标 IP（主机名已解析成 IP，防 DNS 绕过）。
// host 为空视为 127.0.0.1。匹配规则：IP 字面量命中 IP 集合；主机名解析后命中 IP 集合，
// 或原名精确在白名单（DNS 变更兜底，拨当前解析 IP）。
func (w *HostWhitelist) Check(host string) (dialIP string, ok bool) {
	h := normalizeHost(host)
	if h == "" {
		h = "127.0.0.1"
	}
	if ip := net.ParseIP(h); ip != nil {
		s := ip.String()
		w.mu.RLock()
		ok = w.ipRefs[s] > 0
		w.mu.RUnlock()
		return s, ok
	}
	resolved, err := net.LookupIP(h)
	if err == nil {
		for _, ip := range resolved {
			s := ip.String()
			w.mu.RLock()
			hit := w.ipRefs[s] > 0
			w.mu.RUnlock()
			if hit {
				return s, true
			}
		}
	}
	w.mu.RLock()
	_, named := w.entries[h]
	w.mu.RUnlock()
	if named && len(resolved) > 0 {
		return pickIPv4(resolved), true
	}
	return "", false
}

// List 当前白名单主机名列表（管理通道查询用）。
func (w *HostWhitelist) List() []string {
	w.mu.RLock()
	defer w.mu.RUnlock()
	return w.listLocked()
}

func (w *HostWhitelist) listLocked() []string {
	out := make([]string, 0, len(w.entries))
	for k := range w.entries {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func pickIPv4(ips []net.IP) string {
	for _, ip := range ips {
		if ip.To4() != nil {
			return ip.String()
		}
	}
	return ips[0].String()
}

func containsStr(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

func joinOr(list []string, def string) string {
	if len(list) == 0 {
		return def
	}
	return strings.Join(list, " ")
}
