// Win7 构建专用补丁：internal/admin 的 Go 1.20 兼容版。
// 原版（agent-go/internal/admin/admin.go）使用了 Go 1.22 的方法路由
// （"GET /maps"）与 r.PathValue，Go 1.20 编译不了；本文件在
// scripts/build-win7.ps1 构建临时目录中原地覆盖原文件，仓库文件不动。
// 对外 API（MapStatus/Service/HostsService/Start）与行为与原版完全一致。
package admin

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
)

// MapStatus 一条映射的当前状态。
type MapStatus struct {
	ID        uint64 `json:"id"`
	Host      string `json:"host,omitempty"` // 远端目标主机（空 = 服务端本机 127.0.0.1）
	Remote    int    `json:"remote"`
	Local     string `json:"local"`
	Addr      string `json:"addr"` // 归一化后的本地监听地址
	Listening bool   `json:"listening"`
}

// Service 映射管理能力（由客户端角色实现）。
type Service interface {
	ListMaps() []MapStatus
	AddMap(host string, remote int, local string) (uint64, error)
	RemoveMap(id uint64) error
}

// HostsService 主机白名单管理能力（由服务端角色实现）。
type HostsService interface {
	ListHosts() []string
	AddHost(host string) error
	RemoveHost(host string) error
}

// Start 启动本地管理 HTTP 服务（仅监听回环地址，所有请求须携带启动参数约定的
// 随机令牌 Authorization: Bearer）。svc 为映射管理（客户端角色），hosts 为白名单
// 管理（服务端角色），二者可任一为 nil（对应路由不注册）。
// addr 形如 127.0.0.1:0（随机端口）；非回环地址一律强制改为 127.0.0.1；
// token 为空时自动生成随机令牌。返回实际监听地址与生效令牌，ctx 结束时关闭。
func Start(ctx context.Context, addr, token string, svc Service, hosts HostsService) (string, string, error) {
	host, port, err := net.SplitHostPort(strings.TrimSpace(addr))
	if err != nil {
		return "", "", fmt.Errorf("管理地址非法（应为 127.0.0.1:端口）：%w", err)
	}
	if ip := net.ParseIP(host); ip == nil || !ip.IsLoopback() {
		host = "127.0.0.1" // 管理通道仅限本机访问
	}
	if token == "" {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		token = hex.EncodeToString(b)
	}
	ln, err := net.Listen("tcp", net.JoinHostPort(host, port))
	if err != nil {
		return "", "", fmt.Errorf("管理端口监听失败：%w", err)
	}

	mux := http.NewServeMux()
	if svc != nil {
		mux.HandleFunc("/maps", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				writeData(w, http.StatusOK, svc.ListMaps())
			case http.MethodPost:
				var body struct {
					Host   string `json:"host"`
					Remote int    `json:"remote"`
					Local  string `json:"local"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					writeErr(w, http.StatusBadRequest, `请求体应为 JSON：{"remote":9123,"local":"19123"}（host 可选，指定服务端内网目标主机）`)
					return
				}
				id, err := svc.AddMap(body.Host, body.Remote, body.Local)
				if err != nil {
					writeErr(w, http.StatusBadRequest, err.Error())
					return
				}
				writeData(w, http.StatusOK, map[string]any{"id": id})
			default:
				writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
			}
		})
		mux.HandleFunc("/maps/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
				return
			}
			id, err := strconv.ParseUint(strings.TrimPrefix(r.URL.Path, "/maps/"), 10, 64)
			if err != nil {
				writeErr(w, http.StatusBadRequest, "映射 ID 非法")
				return
			}
			if err := svc.RemoveMap(id); err != nil {
				writeErr(w, http.StatusNotFound, err.Error())
				return
			}
			writeData(w, http.StatusOK, map[string]any{"ok": true})
		})
	}
	if hosts != nil {
		mux.HandleFunc("/hosts", func(w http.ResponseWriter, r *http.Request) {
			switch r.Method {
			case http.MethodGet:
				writeData(w, http.StatusOK, hosts.ListHosts())
			case http.MethodPost:
				var body struct {
					Host string `json:"host"`
				}
				if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
					writeErr(w, http.StatusBadRequest, `请求体应为 JSON：{"host":"192.168.5.20"}（精确 IPv4/IPv6/主机名）`)
					return
				}
				if err := hosts.AddHost(body.Host); err != nil {
					writeErr(w, http.StatusBadRequest, err.Error())
					return
				}
				writeData(w, http.StatusOK, map[string]any{"ok": true})
			default:
				writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
			}
		})
		mux.HandleFunc("/hosts/", func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodDelete {
				writeErr(w, http.StatusMethodNotAllowed, "方法不支持")
				return
			}
			host := strings.TrimPrefix(r.URL.Path, "/hosts/")
			// GUI 侧 url.PathEscape 编码，此处还原（与原版 PathValue 解码行为一致）
			if unescaped, err := url.PathUnescape(host); err == nil && unescaped != "" {
				host = unescaped
			}
			if host == "" {
				writeErr(w, http.StatusBadRequest, "主机不能为空")
				return
			}
			if err := hosts.RemoveHost(host); err != nil {
				writeErr(w, http.StatusNotFound, err.Error())
				return
			}
			writeData(w, http.StatusOK, map[string]any{"ok": true})
		})
	}

	srv := &http.Server{Handler: bearerAuth(token, mux)}
	go func() { <-ctx.Done(); _ = srv.Close() }()
	go func() { _ = srv.Serve(ln) }()
	return ln.Addr().String(), token, nil
}

// bearerAuth 令牌鉴权：请求须携带 Authorization: Bearer <token>。
func bearerAuth(token string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer "+token {
			writeErr(w, http.StatusUnauthorized, "管理令牌无效")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func writeData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 0, "msg": "", "data": data})
}

func writeErr(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": 1, "msg": msg})
}
