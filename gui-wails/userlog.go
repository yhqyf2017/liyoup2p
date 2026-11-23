package main

// 用户可见日志层：底层连接程序的输出是给技术人员看的（含地址、令牌、套接字、
// 打洞轮次等），普通用户看不懂也不该看到。这里统一"翻译"成面向用户的说法：
//   - 纯内部过程行直接丢弃；
//   - 技术行按规则改写为业务语言（连接、端口、服务器）；
//   - 残余的网址/令牌等一律脱敏。
// 规则同时用于本程序自身产生的提示（friendlyErr 把底层错误转成人话）。

import (
	"regexp"
	"strings"
)

// logNoise 纯内部日志：对用户没有意义，直接丢弃。
var logNoise = []string{
	"管理通道已就绪",
	"管理通道启动失败",
	"演示页已启动",
	"STUN 公网探测",
	"打洞目标已就绪",
	"更换套接字",
	"等待对端候选",
}

// agentPrefixRe 底层程序自带的 "[19:36:44] [Agent·客户端]" 前缀（面板本身已有时间）。
var agentPrefixRe = regexp.MustCompile(`^\[\d{2}:\d{2}:\d{2}\]\s*(\[[^\]]*\]\s*)?`)

// logRules 技术日志 → 用户语言（按顺序首次匹配即替换）。
var logRules = []struct {
	re   *regexp.Regexp
	repl string
}{
	{regexp.MustCompile(`已连接(平台)?信令通道.*`), "已连接到服务器"},
	{regexp.MustCompile(`信令通道就绪.*`), "已连接到服务器，服务已就绪"},
	{regexp.MustCompile(`信令连接断开.*`), "与服务器的连接中断，正在自动重连…"},
	{regexp.MustCompile(`防火墙自愈：.*`), "网络连接放行未完成：跨网直连可能不畅，请以管理员身份运行一次本程序"},
	{regexp.MustCompile(`连接平台失败.*`), "暂时无法连接服务器，正在重试…"},
	{regexp.MustCompile(`映射 (\S+) 已监听 \S+：本机访问 \S+ 即达服务端内网主机 (\S+) 的端口 (\d+)`), "已开通：本机端口 $1 ↔ 服务器内网主机 $2 的 $3 端口"},
	{regexp.MustCompile(`映射 (\S+) 已监听 \S+：本机访问 \S+ 即达服务端端口 (\d+)`), "已开通：本机端口 $1 ↔ 服务器 $2 端口"},
	{regexp.MustCompile(`映射 (\S+) 已监听.*`), "已开通：本机端口 $1"},
	{regexp.MustCompile(`映射 (\S+) 已停止监听.*`), "已断开：本机端口 $1"},
	{regexp.MustCompile(`映射 (\S+) 入口 \S+ 监听异常.*`), "本机端口 $1 无法继续使用（可能已被其他程序占用），可删除后重新添加"},
	{regexp.MustCompile(`本地入口监听失败（映射 (\S+)）：.*`), "本机端口 $1 无法使用（可能已被其他程序占用），请更换端口"},
	{regexp.MustCompile(`本地入口 \S+ 已被映射 (\S+) 占用`), "该本机端口已被其他映射占用"},
	{regexp.MustCompile(`对端不支持多路复用.*`), "对方版本较旧，已自动使用兼容方式连接"},
	{regexp.MustCompile(`直连升级第 \d+ 次尝试未获.*`), "正在优化连接线路，稍后自动重试"},
	{regexp.MustCompile(`请求未得到响应.*`), "请求超时，请确认服务器与对端设备在线"},
	{regexp.MustCompile(`目标主机未被服务器允许.*`), "服务器未允许访问该内网主机，请联系管理员"},
	{regexp.MustCompile(`连接中继失败.*`), "线路切换中，正在尝试其他路径…"},
	{regexp.MustCompile(`已连接本地服务 \S+，单连接转发中.*`), "已接通服务器上的目标服务"},
	{regexp.MustCompile(`浏览器连接结束.*`), "本次访问已结束"},
	{regexp.MustCompile(`^会话 \S+[：:]\s*`), ""}, // 会话编号对用户无意义
}

var (
	urlRe   = regexp.MustCompile(`\b(?:https?|wss?)://\S+`)
	tokenRe = regexp.MustCompile(`[（(]?\btoken=\S*?[）)]?`)
	spaceRe = regexp.MustCompile(`[ \t]{2,}`)
)

// userizeAgentLine 把底层程序的一行日志转成面向用户的文字。
// 返回 ok=false 表示该行属于内部细节，应丢弃。
func userizeAgentLine(line string) (string, bool) {
	s := strings.TrimSpace(line)
	if s == "" {
		return "", false
	}
	for _, noise := range logNoise {
		if strings.Contains(s, noise) {
			return "", false
		}
	}
	s = agentPrefixRe.ReplaceAllString(s, "")
	for _, r := range logRules {
		if r.re.MatchString(s) {
			s = r.re.ReplaceAllString(s, r.repl)
			break
		}
	}
	s = urlRe.ReplaceAllString(s, "服务器")
	s = tokenRe.ReplaceAllString(s, "")
	s = strings.ReplaceAll(s, "Agent", "连接程序")
	s = strings.ReplaceAll(s, "WebSocket", "网络连接")
	s = strings.ReplaceAll(s, "websocket", "网络连接")
	s = strings.ReplaceAll(s, "sha256", "完整性校验")
	s = spaceRe.ReplaceAllString(s, " ")
	s = strings.Trim(strings.TrimSpace(s), "：:，,")
	if s == "" {
		return "", false
	}
	return s, true
}

// userErr 面向用户的错误文案：已经是我们写给用户看的中文提示就原样保留，
// 底层返回的英文错误（网络、文件、系统调用）统一转成白话。
func userErr(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.TrimSpace(err.Error())
	if hasHan(msg) {
		return msg
	}
	return friendlyErr(msg)
}

// hasHan 是否含中文（Go 1.20 无 strings.ContainsFunc，手写判断）。
func hasHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// friendlyErr 把底层错误转为面向用户的一句话，避免把技术细节（地址、超时参数、
// 文件路径）直接抛给用户。
func friendlyErr(errMsg string) string {
	m := strings.ToLower(errMsg)
	switch {
	case m == "":
		return "操作未完成，请稍后重试"
	case strings.Contains(m, "no such host"), strings.Contains(m, "lookup "),
		strings.Contains(m, "network is unreachable"), strings.Contains(m, "no route to host"):
		return "无法连接服务器，请检查网络后重试"
	case strings.Contains(m, "timeout"), strings.Contains(m, "deadline exceeded"):
		return "连接超时，请检查网络后重试"
	case strings.Contains(m, "refused"), strings.Contains(m, "connection reset"),
		strings.Contains(m, "broken pipe"), strings.Contains(m, "eof"):
		return "与服务器的连接已中断，请稍后重试"
	case strings.Contains(m, "permission denied"), strings.Contains(m, "access is denied"):
		return "权限不足，请以管理员身份运行后重试"
	case strings.Contains(m, "no space left"), strings.Contains(m, "disk full"):
		return "磁盘空间不足，请清理后重试"
	case strings.Contains(m, "not found"), strings.Contains(m, "cannot find"):
		return "程序文件不完整，请重新安装"
	}
	return "操作未完成，请稍后重试"
}
