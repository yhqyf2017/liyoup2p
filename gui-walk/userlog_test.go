package main

import (
	"errors"
	"strings"
	"testing"
)

// 用户可见日志：内部行要被丢弃，技术细节不能出现在提示里。
func TestUserizeAgentLine(t *testing.T) {
	dropped := []string{
		"[19:36:44] [Agent·客户端] 管理通道已就绪：http://127.0.0.1:2892（token=0123abcd5678ef90）",
		"[19:36:44] [Agent] 打洞目标已就绪（共 4 个，内网优先）：1.2.3.4:9",
		"[19:36:44] [Agent] STUN 公网探测完成，已取得公网映射候选",
	}
	for _, in := range dropped {
		if got, ok := userizeAgentLine(in); ok {
			t.Fatalf("内部日志应被丢弃，实际输出 %q", got)
		}
	}

	kept := []struct{ in, want string }{
		{"[19:36:44] [Agent·客户端] 已连接平台信令通道 ws://jyly8888.gnway.vip:58894/ws", "已连接到服务器"},
		{"[19:36:44] [Agent] 信令连接断开，5 秒后重连", "与服务器的连接中断，正在自动重连…"},
		{"[19:36:44] [Agent·客户端] 映射 8080:18080 已监听 127.0.0.1:18080：本机访问 http://127.0.0.1:18080 即达服务端端口 8080", "已开通：本机端口 8080:18080 ↔ 服务器 8080 端口"},
		{"[19:36:44] [Agent] 映射 8080:18080 已停止监听（入口 127.0.0.1:18080 已关闭）", "已断开：本机端口 8080:18080"},
	}
	for _, c := range kept {
		got, ok := userizeAgentLine(c.in)
		if !ok {
			t.Fatalf("用户可见日志被误丢弃：%q", c.in)
		}
		if got != c.want {
			t.Fatalf("翻译结果不符\n输入：%q\n期望：%q\n实际：%q", c.in, c.want, got)
		}
	}

	// 兜底脱敏：任何残留网址都不能出现在用户日志里
	got, ok := userizeAgentLine("[错误] 请求 https://example.com/api/x 失败")
	if !ok || strings.Contains(got, "http") {
		t.Fatalf("网址未被脱敏：ok=%v got=%q", ok, got)
	}
}

// 面向用户的错误：中文业务提示保留，底层英文错误转白话。
func TestUserErr(t *testing.T) {
	if got := userErr(errors.New("账号或密码错误")); got != "账号或密码错误" {
		t.Fatalf("中文提示应原样保留，实际 %q", got)
	}
	cases := map[string]string{
		"dial tcp 1.2.3.4:58894: connectex: No connection could be made because the target machine actively refused it": "与服务器的连接已中断，请稍后重试",
		"lookup jyly8888.gnway.vip: no such host":                "无法连接服务器，请检查网络后重试",
		"context deadline exceeded":                              "连接超时，请检查网络后重试",
		"open C:\\ProgramData\\LiYouP2P\\gui.json: access is denied": "权限不足，请以管理员身份运行后重试",
	}
	for in, want := range cases {
		if got := userErr(errors.New(in)); got != want {
			t.Fatalf("错误翻译不符\n输入：%q\n期望：%q\n实际：%q", in, want, got)
		}
	}
}
