// Package config Agent 命令行配置。
package config

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
)

// Config Agent 运行配置。
type Config struct {
	Role        string    // server | client
	Platform    string    // 平台信令地址 ws://host:port/ws
	Name        string    // 设备名称
	Port        int       // 服务端：额外申报的业务端口；客户端：兼容旧参数的远端端口
	Local       string    // 客户端：兼容旧参数的本地监听地址
	Maps        []PortMap // 客户端端口映射列表（远端端口 → 本地入口）
	TokenFile   string    // 设备令牌文件
	DeviceToken string    // 直接传入设备令牌
	DemoHTTP    bool      // 在 :8080 起演示页
	ServerUID   string    // 客户端目标服务端设备 UID（留空由平台自动选择）
	Punch       bool      // 服务端是否启用 UDP 打洞（关闭则始终走中继）
	Admin       string    // 管理通道监听地址（如 127.0.0.1:0）；客户端热增删映射，服务端热管理主机白名单
	AdminToken  string    // 管理通道鉴权令牌（GUI 随机生成；为空则 Agent 自行生成）
	Hosts       []string  // 服务端：可穿透目标主机白名单（精确 IPv4/IPv6/主机名；空则默认 127.0.0.1）
}

// PortMap 一条端口映射：远端目标端口 → 本地监听地址（可选指定目标主机）。
type PortMap struct {
	Host   string // 远端目标主机；空 = 127.0.0.1（服务端本机）
	Remote int    // 远端（服务器上）目标端口
	Local  string // 本地监听地址；纯端口默认绑定 127.0.0.1
}

func (m PortMap) String() string {
	if m.Host != "" {
		return fmt.Sprintf("%s:%d:%s", m.Host, m.Remote, m.Local)
	}
	return fmt.Sprintf("%d:%s", m.Remote, m.Local)
}

// LocalAddr 归一化本地监听地址（纯端口补 127.0.0.1）。
func (m PortMap) LocalAddr() string {
	if strings.Contains(m.Local, ":") {
		return m.Local
	}
	return "127.0.0.1:" + m.Local
}

// mapFlag 支持 --map 远端:本地（旧两段式）与 主机:远端:本地（新三段式）可重复传入。
// 区分规则：三段且中段为合法端口 → 新三段式；否则按旧式处理（本地地址可自带冒号）。
type mapFlag struct{ c *Config }

func (f mapFlag) String() string {
	ms := make([]string, len(f.c.Maps))
	for i, m := range f.c.Maps {
		ms[i] = m.String()
	}
	return strings.Join(ms, " ")
}

func isPort(s string) bool {
	p, err := strconv.Atoi(strings.TrimSpace(s))
	return err == nil && p >= 1 && p <= 65535
}

func (f mapFlag) Set(v string) error {
	v = strings.TrimSpace(v)
	parts := strings.Split(v, ":")
	var host, remoteStr, local string
	switch {
	case len(parts) == 2:
		// 旧两段式：远端端口:本地地址
		remoteStr, local = parts[0], parts[1]
	case len(parts) == 3 && isPort(parts[1]):
		// 新三段式：目标主机:远端端口:本地端口
		host, remoteStr, local = parts[0], parts[1], parts[2]
		if err := ValidateHost(host); err != nil {
			return fmt.Errorf("--map 目标主机 %q 非法：%v", host, err)
		}
	default:
		// 其余（含旧式 本地地址自带冒号 的三段以上写法）：远端端口:本地地址
		idx := strings.Index(v, ":")
		remoteStr, local = v[:idx], v[idx+1:]
	}
	if strings.TrimSpace(remoteStr) == "" || strings.TrimSpace(local) == "" {
		return fmt.Errorf("--map 格式应为 远端端口:本地地址 或 目标主机:远端端口:本地端口（如 --map 80:18080 / --map 192.168.5.20:80:18080）")
	}
	remote, err := strconv.Atoi(strings.TrimSpace(remoteStr))
	if err != nil || remote < 1 || remote > 65535 {
		return fmt.Errorf("--map 远端端口 %q 非法（1-65535）", remoteStr)
	}
	local = strings.TrimSpace(local)
	if !strings.Contains(local, ":") {
		p, err := strconv.Atoi(local)
		if err != nil || p < 1 || p > 65535 {
			return fmt.Errorf("--map 本地端口 %q 非法（1-65535）", local)
		}
	}
	f.c.Maps = append(f.c.Maps, PortMap{Host: host, Remote: remote, Local: local})
	return nil
}

// hostsFlag 支持 --hosts 精确主机 可重复传入（服务端白名单）。
type hostsFlag struct{ c *Config }

func (f hostsFlag) String() string { return strings.Join(f.c.Hosts, " ") }

func (f hostsFlag) Set(v string) error {
	v = strings.TrimSpace(v)
	if err := ValidateHost(v); err != nil {
		return fmt.Errorf("--hosts %q 非法：%v", v, err)
	}
	f.c.Hosts = append(f.c.Hosts, v)
	return nil
}

// ValidateHost 校验白名单目标主机：仅允许精确 IPv4/IPv6 或主机名（RFC1123 风格标签，
// 兼容含下划线的内网机器名），禁止 0.0.0.0/:: 等未指定地址与通配、网段写法。
func ValidateHost(host string) error {
	h := strings.TrimSpace(host)
	h = strings.TrimSuffix(h, ".")
	if h == "" {
		return errors.New("主机不能为空")
	}
	if len(h) > 253 {
		return errors.New("主机名过长（超过 253 字符）")
	}
	if ip := net.ParseIP(h); ip != nil {
		if ip.IsUnspecified() {
			return errors.New("不允许 0.0.0.0 / :: 等未指定地址")
		}
		return nil
	}
	if strings.Contains(h, ":") {
		return errors.New("含冒号但不是合法 IPv6 地址")
	}
	if strings.ContainsAny(h, "*/\\?%!$@#^&|+=[](){}'\" \t") {
		return errors.New("不允许通配符、网段或特殊字符（仅精确主机）")
	}
	for _, label := range strings.Split(h, ".") {
		if label == "" || len(label) > 63 {
			return fmt.Errorf("主机名标签 %q 非法", label)
		}
		if !isHostAlnum(label[0]) || !isHostAlnum(label[len(label)-1]) {
			return fmt.Errorf("主机名标签 %q 须以字母数字开头与结尾", label)
		}
		for i := 0; i < len(label); i++ {
			if !isHostAlnum(label[i]) && label[i] != '-' && label[i] != '_' {
				return fmt.Errorf("主机名含非法字符 %q", string(label[i]))
			}
		}
	}
	return nil
}

func isHostAlnum(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9'
}

// Load 解析命令行参数（环境变量作为兜底）。
func Load() *Config {
	c := &Config{}
	host, _ := os.Hostname()
	if host == "" {
		host = "未命名设备"
	}
	defTokenFile := strings.TrimSuffix(os.Getenv("ProgramData"), `\`) + `\LiYouP2P\token.json`

	flag.StringVar(&c.Role, "role", "client", "角色：server（业务服务器）| client（员工电脑）")
	flag.StringVar(&c.Platform, "platform", envOr("LIYOU_PLATFORM", "ws://jyly8888.gnway.vip:58894/ws"), "平台信令 WebSocket 地址（外网；内网可覆盖为 ws://192.168.5.66:8900/ws）")
	flag.StringVar(&c.Name, "name", host, "设备名称")
	flag.Var(mapFlag{c}, "map", "客户端端口映射 远端:本地（可重复，如 --map 80:18080 --map 3306:13306）")
	flag.IntVar(&c.Port, "port", 8080, "服务端：额外申报端口；客户端：旧版单端口参数（与 --local 搭配，等价一条映射）")
	flag.StringVar(&c.Local, "local", "127.0.0.1:18080", "客户端本地监听地址（旧版单端口参数，与 --port 搭配）")
	flag.StringVar(&c.TokenFile, "token-file", envOr("LIYOU_TOKEN_FILE", defTokenFile), "设备令牌文件路径")
	flag.StringVar(&c.DeviceToken, "device-token", envOr("LIYOU_DEVICE_TOKEN", ""), "直接指定设备令牌")
	flag.BoolVar(&c.DemoHTTP, "demo-http", false, "在本机 :8080 启动演示页（供隧道验证）")
	flag.StringVar(&c.ServerUID, "server", envOr("LIYOU_SERVER_UID", ""), "目标服务端设备 UID（留空由平台自动选择在线服务端）")
	flag.BoolVar(&c.Punch, "punch", true, "服务端启用 UDP 打洞（false 则强制中继模式）")
	flag.StringVar(&c.Admin, "admin", "", "管理通道监听地址（如 127.0.0.1:0；客户端热增删映射，服务端热管理主机白名单）")
	flag.StringVar(&c.AdminToken, "admin-token", "", "管理通道鉴权令牌（为空则自动生成）")
	flag.Var(hostsFlag{c}, "hosts", "服务端可穿透目标主机白名单，精确 IPv4/IPv6/主机名（可重复，如 --hosts 127.0.0.1 --hosts 192.168.5.20）")
	flag.Parse()
	// 未传 --map 时用旧参数 --port/--local 转换成一条映射，保持向后兼容
	if len(c.Maps) == 0 {
		c.Maps = []PortMap{{Remote: c.Port, Local: c.Local}}
	}
	return c
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// String 配置摘要（隐藏令牌）。
func (c *Config) String() string {
	ms := make([]string, len(c.Maps))
	for i, m := range c.Maps {
		ms[i] = m.String()
	}
	hosts := ""
	if len(c.Hosts) > 0 {
		hosts = fmt.Sprintf(" 白名单=[%s]", strings.Join(c.Hosts, " "))
	}
	return fmt.Sprintf("角色=%s 平台=%s 设备=%s 映射=[%s]%s 打洞=%v",
		c.Role, c.Platform, c.Name, strings.Join(ms, " "), hosts, c.Punch)
}
