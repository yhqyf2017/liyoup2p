// LiYou P2P Agent 入口：服务端角色（申报端口/承接隧道）或客户端角色（本地入口/发起隧道）。
package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"liyou-agent/internal/admin"
	"liyou-agent/internal/client"
	"liyou-agent/internal/config"
	"liyou-agent/internal/demo"
	"liyou-agent/internal/device"
	"liyou-agent/internal/fwutil"
	"liyou-agent/internal/logx"
	"liyou-agent/internal/server"
	"liyou-agent/internal/ws"
)

func main() {
	cfg := config.Load()
	logx.SetLabel("Agent·初始化")
	logx.Infof("LiYou P2P Agent 启动：%s", cfg)

	token, err := device.Resolve(cfg.DeviceToken, cfg.TokenFile)
	if err != nil {
		logx.Errorf("%v", err)
		os.Exit(1)
	}
	if cfg.DeviceToken == "" {
		logx.Infof("已从令牌文件加载设备令牌：%s", cfg.TokenFile)
	}
	if added, err := fwutil.EnsureUDPRule(); err != nil {
		logx.Warnf("防火墙自愈：%v（跨网打洞需要放行入站 UDP）", err)
	} else if added {
		logx.Infof("已添加防火墙入站 UDP 放行规则：LiYou P2P Agent UDP-In")
	}

	switch cfg.Role {
	case "server":
		logx.SetLabel("Agent·服务端")
	case "client":
		logx.SetLabel("Agent·客户端")
	default:
		logx.Errorf("--role 必须是 server 或 client")
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	wc := ws.New(cfg.Platform, token)

	switch cfg.Role {
	case "server":
		if cfg.DemoHTTP {
			demo.Start(":8080")
		}
		srv := server.New(cfg, wc)
		srv.Bind()
		if cfg.Admin != "" {
			addr, tok, err := admin.Start(ctx, cfg.Admin, cfg.AdminToken, nil, srv)
			if err != nil {
				logx.Warnf("管理通道启动失败：%v（运行中将无法增删主机白名单）", err)
			} else {
				logx.Infof("管理通道已就绪：http://%s（token=%s，支持 /hosts 白名单热增删）", addr, tok)
			}
		}
	case "client":
		cl := client.New(cfg, wc)
		cl.Bind()
		if cfg.Admin != "" {
			addr, tok, err := admin.Start(ctx, cfg.Admin, cfg.AdminToken, cl, nil)
			if err != nil {
				logx.Warnf("管理通道启动失败：%v（运行中将无法增删映射）", err)
			} else {
				logx.Infof("管理通道已就绪：http://%s（token=%s）", addr, tok)
			}
		}
		go func() {
			if err := cl.Run(ctx); err != nil && ctx.Err() == nil {
				logx.Errorf("客户端入口异常：%v", err)
				os.Exit(1)
			}
		}()
	}

	go wc.Run(ctx)
	<-ctx.Done()
	logx.Infof("Agent 已退出")
}
