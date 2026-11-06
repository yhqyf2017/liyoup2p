// Package config 平台配置：环境变量 + 命令行 flag，flag 优先。
package config

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
)

// Config 平台运行配置。
type Config struct {
	DSN             string // PostgreSQL 连接串
	HTTPAddr        string // REST API + WebSocket 监听地址
	RelayAddr       string // TCP 中继监听地址（局域网兼容通道）
	RelayPublicAddr string // 中继对外地址（下发给 Agent），支持 ws://host/relay 或 host:port，为空则按各设备信令入口同源推导
	JWTSecret       string // JWT 签名密钥
	AdminPassword   string // 种子账号 admin 的密码
	SuperPassword   string // 种子账号 superadmin 的密码
}

// Load 读取配置：flag > 环境变量。敏感项（数据库 DSN、JWT 密钥、种子账号密码）
// 不设默认值——缺失时拒绝启动并给出中文指引，避免凭据落入源码或被默认值接管。
func Load() (*Config, error) {
	c := &Config{}
	flag.StringVar(&c.DSN, "dsn", os.Getenv("LIYOU_DSN"), "PostgreSQL DSN（环境变量 LIYOU_DSN）")
	flag.StringVar(&c.HTTPAddr, "http-addr", envOr("LIYOU_HTTP_ADDR", ":8900"), "HTTP/WebSocket 监听地址")
	flag.StringVar(&c.RelayAddr, "relay-addr", envOr("LIYOU_RELAY_ADDR", ":8901"), "TCP 中继监听地址")
	flag.StringVar(&c.RelayPublicAddr, "relay-public", envOr("LIYOU_RELAY_PUBLIC_ADDR", ""), "中继对外地址(ws://host/relay 或 host:port)，默认按各设备信令入口同源推导")
	flag.StringVar(&c.JWTSecret, "jwt-secret", os.Getenv("LIYOU_JWT_SECRET"), "JWT 签名密钥（环境变量 LIYOU_JWT_SECRET）")
	flag.StringVar(&c.AdminPassword, "admin-password", os.Getenv("LIYOU_ADMIN_PASSWORD"), "种子账号 admin 的密码（环境变量 LIYOU_ADMIN_PASSWORD）")
	flag.StringVar(&c.SuperPassword, "superadmin-password", os.Getenv("LIYOU_SUPERADMIN_PASSWORD"), "种子账号 superadmin 的密码（环境变量 LIYOU_SUPERADMIN_PASSWORD）")
	flag.Parse()

	var missing []string
	if strings.TrimSpace(c.DSN) == "" {
		missing = append(missing, "LIYOU_DSN")
	}
	if strings.TrimSpace(c.JWTSecret) == "" {
		missing = append(missing, "LIYOU_JWT_SECRET")
	}
	if strings.TrimSpace(c.AdminPassword) == "" {
		missing = append(missing, "LIYOU_ADMIN_PASSWORD")
	}
	if strings.TrimSpace(c.SuperPassword) == "" {
		missing = append(missing, "LIYOU_SUPERADMIN_PASSWORD")
	}
	if len(missing) > 0 {
		return nil, errors.New("缺少必需配置：" + strings.Join(missing, "、") +
			"——请在启动前设置这些环境变量（推荐：把凭据写入 platform-go\\.env，用 start-platform.ps1 启动）")
	}
	return c, nil
}

func envOr(key, def string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return def
}

// String 便于启动日志输出（隐藏密码）。
func (c *Config) String() string {
	return fmt.Sprintf("HTTP=%s Relay=%s JWT=%s", c.HTTPAddr, c.RelayAddr, maskSecret(c.JWTSecret))
}

func maskSecret(s string) string {
	if len(s) > 8 {
		return s[:4] + "****"
	}
	return "****"
}
