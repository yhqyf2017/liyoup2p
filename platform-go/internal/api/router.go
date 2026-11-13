package api

import (
	"net/http"

	"liyou-platform/internal/auth"
	"liyou-platform/internal/store"
)

// NewRouter 构建全部 REST 路由。updateDir 为升级包目录（version.json 与安装包）。
func NewRouter(st *store.Store, jm *auth.Manager, kicker DeviceKicker, updateDir string) *http.ServeMux {
	mux := http.NewServeMux()

	authAPI := &AuthAPI{St: st, JWT: jm}
	usersAPI := &UsersAPI{St: st}
	tenantsAPI := &TenantsAPI{St: st}
	devicesAPI := &DevicesAPI{St: st, JWT: jm, Kicker: kicker}
	miscAPI := &MiscAPI{St: st}
	updateAPI := &UpdateAPI{Dir: updateDir}

	// 认证（匿名可访问）
	mux.HandleFunc("POST /api/auth/login", authAPI.Login)
	mux.HandleFunc("POST /api/auth/refresh", authAPI.Refresh)

	// 升级检查（匿名可访问：GUI 登录前后均可检查更新）
	mux.HandleFunc("GET /api/update/latest", updateAPI.Latest)

	au := func(h http.HandlerFunc) http.HandlerFunc { return withAuth(jm, st, h) }
	roles := func(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
		return func(h http.HandlerFunc) http.HandlerFunc { return withAuth(jm, st, requireRoles(roles...)(h)) }
	}

	// 升级发布/列表（仅 super_admin）：multipart 发布安装包、查看各通道版本
	mux.Handle("POST /api/platform/updates", roles("super_admin")(updateAPI.Publish))
	mux.Handle("GET /api/platform/updates", roles("super_admin")(updateAPI.List))

	// 我的信息
	mux.Handle("GET /api/me", au(authAPI.Me))

	// 用户：读=全部角色；写=super_admin/tenant_admin
	mux.Handle("GET /api/users", au(usersAPI.List))
	mux.Handle("POST /api/users", roles("super_admin", "tenant_admin")(usersAPI.Create))
	mux.Handle("PUT /api/users/{id}", roles("super_admin", "tenant_admin")(usersAPI.Update))
	mux.Handle("DELETE /api/users/{id}", roles("super_admin", "tenant_admin")(usersAPI.Delete))
	mux.Handle("POST /api/users/{id}/reset-password", roles("super_admin", "tenant_admin")(usersAPI.ResetPassword))

	// 租户：仅 super_admin
	mux.Handle("GET /api/tenants", roles("super_admin")(tenantsAPI.List))
	mux.Handle("POST /api/tenants", roles("super_admin")(tenantsAPI.Create))
	mux.Handle("PUT /api/tenants/{id}", roles("super_admin")(tenantsAPI.Update))
	mux.Handle("DELETE /api/tenants/{id}", roles("super_admin")(tenantsAPI.Delete))

	// 设备：读=全部角色；注册/吊销=super_admin/tenant_admin/operator
	mux.Handle("GET /api/devices", au(devicesAPI.List))
	mux.Handle("POST /api/devices/register", roles("super_admin", "tenant_admin", "operator")(devicesAPI.Register))
	mux.Handle("DELETE /api/devices/{id}", roles("super_admin", "tenant_admin", "operator")(devicesAPI.Delete))

	// 审计 / 会话 / 告警 / 统计
	mux.Handle("GET /api/audit", au(miscAPI.Audit))
	mux.Handle("GET /api/sessions", au(miscAPI.Sessions))
	mux.Handle("GET /api/alerts", au(miscAPI.Alerts))
	mux.Handle("POST /api/alerts/{id}/read", au(miscAPI.AlertRead))
	mux.Handle("POST /api/alerts/read-all", au(miscAPI.AlertsReadAll))
	mux.Handle("GET /api/stats", au(miscAPI.Stats))

	// 平台信息
	mux.HandleFunc("GET /api/health", func(w http.ResponseWriter, r *http.Request) {
		OK(w, map[string]any{"name": "LiYou P2P Platform", "version": "1.0.0"})
	})

	return mux
}
