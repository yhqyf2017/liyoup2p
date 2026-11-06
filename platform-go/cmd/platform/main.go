// LiYou P2P 平台入口：REST API + WebSocket 信令 + TCP 中继。
package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"os/signal"
	"syscall"
	"time"

	"liyou-platform/internal/api"
	"liyou-platform/internal/auth"
	"liyou-platform/internal/config"
	"liyou-platform/internal/hub"
	"liyou-platform/internal/relay"
	"liyou-platform/internal/store"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("[平台] %v", err)
	}
	log.Printf("[平台] LiYou P2P 平台启动：%s", cfg)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	st, err := store.Open(cfg.DSN)
	if err != nil {
		log.Fatalf("[平台] %v", err)
	}
	defer st.DB.Close()
	log.Printf("[平台] 数据库连接成功")

	if err := st.Migrate(ctx); err != nil {
		log.Fatalf("[平台] 建表失败：%v", err)
	}
	log.Printf("[平台] 数据表就绪")

	adminHash, err := auth.HashPassword(cfg.AdminPassword)
	if err != nil {
		log.Fatalf("[平台] 生成种子密码失败：%v", err)
	}
	superHash, err := auth.HashPassword(cfg.SuperPassword)
	if err != nil {
		log.Fatalf("[平台] 生成种子密码失败：%v", err)
	}
	if err := st.Seed(ctx, adminHash, superHash); err != nil {
		log.Fatalf("[平台] 种子数据写入失败：%v", err)
	}
	log.Printf("[平台] 种子数据就绪（admin / superadmin）")
	if n, err := st.ReplaceDefaultPasswords(ctx, adminHash, superHash); err != nil {
		log.Printf("[平台] 检查种子账号默认密码失败：%v", err)
	} else if n > 0 {
		log.Printf("[平台] 已将 %d 个仍使用默认密码的种子账号重置为环境变量指定的新密码", n)
	}

	jm := auth.NewManager(cfg.JWTSecret)
	grants := relay.NewGrantTable()

	h := hub.New(st, jm, grants, hub.Options{RelayPublicAddr: cfg.RelayPublicAddr})

	// 中继：TCP :8901（局域网兼容）+ WebSocket /relay（与信令同入口，跨网络可达）
	relaySrv := relay.NewServer(st, grants)
	go func() {
		if err := relaySrv.Run(ctx, cfg.RelayAddr); err != nil {
			log.Fatalf("[平台] 中继服务异常：%v", err)
		}
	}()

	// HTTP API + WebSocket
	// 升级包目录：与控制台目录同样的三级解析（exe 工作目录 → 上级 → 项目根）
	updateDir := "update"
	for _, cand := range []string{"update", filepath.Join("..", "update"), `D:\dev\liyou\liyoup2p\update`} {
		if st2, err := os.Stat(filepath.Join(cand, "version.json")); err == nil && !st2.IsDir() {
			updateDir = cand
			break
		}
	}
	mux := api.NewRouter(st, jm, h, updateDir)
	mux.HandleFunc("GET /ws", h.ServeWS)
	mux.HandleFunc("GET /relay", relaySrv.ServeWS)

	// 升级包静态托管（http.FileServer 自带路径穿越防护；/update/ 精确前缀优先于 GET /）
	mux.Handle("GET /update/", http.StripPrefix("/update/", http.FileServer(http.Dir(updateDir))))
	log.Printf("[平台] 升级包目录：%s（GET /api/update/latest + GET /update/<安装包>）", updateDir)

	// 控制台静态托管：优先 exe 同目录的 console/，其次工作目录
	consoleDir := "console"
	for _, cand := range []string{"console", filepath.Join("..", "console"), `D:\dev\liyou\liyoup2p\console`} {
		if st2, err := os.Stat(filepath.Join(cand, "index.html")); err == nil && !st2.IsDir() {
			consoleDir = cand
			break
		}
	}
	mux.Handle("GET /", http.FileServer(http.Dir(consoleDir)))
	log.Printf("[平台] 控制台目录：%s（浏览器打开 http://<平台IP>%s/）", consoleDir, cfg.HTTPAddr)

	srv := &http.Server{Addr: cfg.HTTPAddr, Handler: mux}
	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()
	log.Printf("[平台] HTTP API 监听 %s（信令 /ws，中继 /relay + TCP %s）", cfg.HTTPAddr, cfg.RelayAddr)
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Fatalf("[平台] HTTP 服务异常：%v", err)
	}
	log.Printf("[平台] 已退出")
}
