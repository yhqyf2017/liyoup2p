// Package demo 本机演示页：占用 :8080 时跳过，用于隧道连通性验证。
package demo

import (
	"fmt"
	"net/http"
	"runtime"
	"time"

	"liyou-agent/internal/logx"
)

const demoPage = `<!DOCTYPE html>
<html lang="zh-CN"><head><meta charset="utf-8"><title>LiYou P2P 隧道演示服务</title>
<style>body{font-family:"Microsoft YaHei",sans-serif;background:#0f172a;color:#e2e8f0;display:flex;
align-items:center;justify-content:center;height:100vh;margin:0}
.card{background:#1e293b;padding:40px 56px;border-radius:16px;box-shadow:0 8px 30px rgba(0,0,0,.4);text-align:center}
h1{margin:0 0 12px;font-size:26px;color:#38bdf8}p{margin:6px 0;color:#94a3b8}
b{color:#4ade80}code{background:#0f172a;padding:2px 8px;border-radius:6px}</style></head>
<body><div class="card"><h1>LiYou P2P 隧道演示服务</h1>
<p>如果你能看到本页，说明隧道链路已打通。</p>
<p>业务端口：<b>%s</b>　Agent 系统：<b>%s</b></p>
<p>响应时间：<b>%s</b></p>
<p style="margin-top:18px">浏览器 → <code>127.0.0.1:18080</code> → [P2P/中继加密隧道] → 服务端内网服务</p>
</div></body></html>`

// Start 在 addr（默认 :8080）启动演示页；端口被占用则跳过。
func Start(addr string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, demoPage, addr, runtime.GOOS+"/"+runtime.GOARCH, time.Now().Format("15:04:05"))
	})
	go func() {
		if err := http.ListenAndServe(addr, mux); err != nil {
			logx.Warnf("演示页监听 %s 失败（可能已被占用），已跳过", addr)
		}
	}()
	logx.Infof("演示页已启动 http://127.0.0.1%s", addr)
}
