package server

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"
)

// TestHostWhitelistIP 以 IP 字面量为主的白名单增删与校验（不依赖 DNS）。
func TestHostWhitelistIP(t *testing.T) {
	w := NewHostWhitelist([]string{"127.0.0.1", "192.168.5.20"})
	if got := w.List(); len(got) != 2 {
		t.Fatalf("初始白名单应为 2 条: %v", got)
	}
	if ip, ok := w.Check(""); !ok || ip != "127.0.0.1" {
		t.Fatalf("空主机应视为 127.0.0.1: ip=%q ok=%v", ip, ok)
	}
	if ip, ok := w.Check("127.0.0.1"); !ok || ip != "127.0.0.1" {
		t.Fatalf("127.0.0.1 应放行: ip=%q ok=%v", ip, ok)
	}
	if ip, ok := w.Check("192.168.5.20"); !ok || ip != "192.168.5.20" {
		t.Fatalf("192.168.5.20 应放行: ip=%q ok=%v", ip, ok)
	}
	if _, ok := w.Check("192.168.5.21"); ok {
		t.Fatalf("未加白 IP 应拒绝")
	}
	if err := w.Add("0.0.0.0"); err == nil {
		t.Fatalf("0.0.0.0 应被拒绝")
	}
	if err := w.Add("192.168.5.0/24"); err == nil {
		t.Fatalf("网段写法应被拒绝")
	}
	if err := w.Add("192.168.5.20"); err == nil {
		t.Fatalf("重复添加应报错")
	}
	if err := w.Remove("192.168.5.20"); err != nil {
		t.Fatalf("移除失败: %v", err)
	}
	if _, ok := w.Check("192.168.5.20"); ok {
		t.Fatalf("移除后应拒绝")
	}
	if err := w.Remove("192.168.5.20"); err == nil {
		t.Fatalf("移除不存在的主机应报错")
	}
	if err := w.Add("192.168.5.20"); err != nil {
		t.Fatalf("移除后重新添加失败: %v", err)
	}
	if _, ok := w.Check("192.168.5.20"); !ok {
		t.Fatalf("重新添加后应放行")
	}
}

// TestParseStreamHead 子流首帧 v1/v2 兼容解析。
func TestParseStreamHead(t *testing.T) {
	// v1 旧版：仅端口，无主机字段
	var v1 [2]byte
	binary.BigEndian.PutUint16(v1[:], 8080)
	host, port, err := parseStreamHead(bytes.NewReader(v1[:]))
	if err != nil || host != "" || port != 8080 {
		t.Fatalf("v1 解析错误: host=%q port=%d err=%v", host, port, err)
	}

	// v2 扩展：0x0000 标记 + 主机 + 端口
	hb := []byte("192.168.5.20")
	v2 := make([]byte, 0, 2+2+len(hb)+2)
	var l, p [2]byte
	binary.BigEndian.PutUint16(l[:], uint16(len(hb)))
	binary.BigEndian.PutUint16(p[:], 9123)
	v2 = append(v2, 0, 0)
	v2 = append(v2, l[:]...)
	v2 = append(v2, hb...)
	v2 = append(v2, p[:]...)
	host, port, err = parseStreamHead(bytes.NewReader(v2))
	if err != nil || host != "192.168.5.20" || port != 9123 {
		t.Fatalf("v2 解析错误: host=%q port=%d err=%v", host, port, err)
	}

	// v2 空主机：主机长度为 0
	v2e := append([]byte{0, 0, 0, 0}, p[:]...)
	host, port, err = parseStreamHead(bytes.NewReader(v2e))
	if err != nil || host != "" || port != 9123 {
		t.Fatalf("v2 空主机解析错误: host=%q port=%d err=%v", host, port, err)
	}

	// 主机长度超限
	bad := append([]byte{0, 0, 0x01, 0x00}, make([]byte, 8)...)
	if _, _, err := parseStreamHead(bytes.NewReader(bad)); err == nil || !strings.Contains(err.Error(), "超限") {
		t.Fatalf("主机长度超限应报错: %v", err)
	}
}
