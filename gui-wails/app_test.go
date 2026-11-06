package main

import (
	"strings"
	"testing"
)

func TestParseAdminReady(t *testing.T) {
	line := "[19:36:44] [Agent·客户端] 管理通道已就绪：http://127.0.0.1:2892（token=0123abcd5678ef90）"
	addr, tok := parseAdminReady(line)
	if addr != "127.0.0.1:2892" || tok != "0123abcd5678ef90" {
		t.Fatalf("解析失败：addr=%q token=%q", addr, tok)
	}
	if a, k := parseAdminReady("[19:36:44] [Agent·客户端] 映射 8080:18080 已监听"); a != "" || k != "" {
		t.Fatalf("普通日志行不应被解析：addr=%q token=%q", a, k)
	}
}

func TestNormalizeRule(t *testing.T) {
	if r, err := normalizeRule("", 9123, "19123"); err != nil || r.Remote != 9123 || r.Local != "19123" || r.Host != "" {
		t.Fatalf("合法规则被拒绝：%v", err)
	}
	if r, err := normalizeRule("192.168.5.20", 9123, "19123"); err != nil || r.Host != "192.168.5.20" {
		t.Fatalf("带主机规则被拒绝：%v", err)
	}
	for _, c := range []struct {
		host   string
		remote int
		local  string
	}{{"", 0, "80"}, {"", 70000, "80"}, {"", 80, ""}, {"", 80, "abc"}, {"", 80, "0"}, {"", 80, "70000"},
		{"0.0.0.0", 80, "18080"}, {"192.168.5.*", 80, "18080"}, {"192.168.5.0/24", 80, "18080"}, {"bad host!", 80, "18080"}} {
		if _, err := normalizeRule(c.host, c.remote, c.local); err == nil {
			t.Fatalf("非法规则未被拒绝：%+v", c)
		}
	}
}

func TestNormalizeLocal(t *testing.T) {
	if got := normalizeLocal("19123"); got != "127.0.0.1:19123" {
		t.Fatalf("纯端口归一化失败：%q", got)
	}
	if got := normalizeLocal("127.0.0.1:19123"); got != "127.0.0.1:19123" {
		t.Fatalf("含地址归一化失败：%q", got)
	}
	if !strings.Contains(normalizeLocal(" 19123 "), "19123") {
		t.Fatal("空格未裁剪")
	}
}

func TestSanitizeServerText(t *testing.T) {
	isRelease = "true"
	defer func() { isRelease = "false" }()
	for _, in := range []string{
		"正在登录平台 http://jyly8888.gnway.vip:58894（账号 admin）…",
		"已连接信令 ws://jyly8888.gnway.vip:58894/ws",
		"平台 jyly8888.gnway.vip:58894 就绪",
		"请求 https://jyly8888.gnway.vip:58894/api/auth/login 失败",
		"wss://jyly8888.gnway.vip:58894/ws 已断开",
	} {
		got := sanitizeServerText(in)
		if strings.Contains(got, "jyly8888") || !strings.Contains(got, "服务器") {
			t.Fatalf("正式包地址未脱敏：%q -> %q", in, got)
		}
	}
	if got := sanitizeServerText("平台 http://jyly8888.gnway.vip:58894 正常"); strings.Contains(got, "http://") {
		t.Fatalf("前缀形式应整体替换：%q", got)
	}
}

func TestSanitizeServerTextDev(t *testing.T) {
	in := "平台 http://jyly8888.gnway.vip:58894 就绪"
	if got := sanitizeServerText(in); got != in {
		t.Fatalf("开发模式不应脱敏：%q -> %q", in, got)
	}
}
