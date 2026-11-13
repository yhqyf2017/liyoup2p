// Package api REST API 处理器、中间件与路由。
package api

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"strconv"
)

// Envelope 统一响应包装 {code, msg, data}。
type Envelope struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data any    `json:"data,omitempty"`
}

// OK 输出成功响应。
func OK(w http.ResponseWriter, data any) {
	writeJSON(w, http.StatusOK, Envelope{Code: 0, Msg: "ok", Data: data})
}

// Fail 输出失败响应。
func Fail(w http.ResponseWriter, status, code int, msg string) {
	writeJSON(w, status, Envelope{Code: code, Msg: msg})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// ReadJSON 解析请求体。
func ReadJSON(w http.ResponseWriter, r *http.Request, dst any) bool {
	if err := json.NewDecoder(r.Body).Decode(dst); err != nil {
		Fail(w, http.StatusBadRequest, 1001, "请求体 JSON 解析失败: "+err.Error())
		return false
	}
	return true
}

// QueryInt 读取整型查询参数，默认 def。
func QueryInt(r *http.Request, key string, def int) int {
	v := r.URL.Query().Get(key)
	if v == "" {
		return def
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return def
	}
	return n
}

// PageArgs 解析分页查询参数：page 默认 1；page_size 默认 20、上限 100。
func PageArgs(r *http.Request) (page, size int) {
	page = QueryInt(r, "page", 1)
	size = QueryInt(r, "page_size", 20)
	if page < 1 {
		page = 1
	}
	if size < 1 {
		size = 20
	}
	if size > 100 {
		size = 100
	}
	return page, size
}

// ClientIP 取客户端 IP。
func ClientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// ---------- 请求上下文（当前用户） ----------

type ctxKey int

const userCtxKey ctxKey = 1

// UserCtx 当前登录用户上下文。
type UserCtx struct {
	UserID   int64
	Username string
	Role     string // super_admin | tenant_admin | operator | auditor
	TenantID int64  // 0 表示平台级（super_admin）
}

// IsSuper 是否超级管理员。
func (u *UserCtx) IsSuper() bool { return u.Role == "super_admin" }

// WithUser 写入用户上下文。
func WithUser(ctx context.Context, u *UserCtx) context.Context {
	return context.WithValue(ctx, userCtxKey, u)
}

// CurrentUser 取当前用户，未认证返回 nil。
func CurrentUser(r *http.Request) *UserCtx {
	u, _ := r.Context().Value(userCtxKey).(*UserCtx)
	return u
}
