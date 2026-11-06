package api

import (
	"context"
	"database/sql"
	"fmt"
	"net/http"
	"strings"

	"liyou-platform/internal/auth"
	"liyou-platform/internal/store"
)

// SessionValidator 会话版本校验所需存储能力（*store.Store 实现；测试可注入假实现）。
type SessionValidator interface {
	GetSessionSeq(ctx context.Context, userID int64, clientType string) (int64, error)
	InsertAudit(ctx context.Context, e store.AuditEntry) error
}

// withAuth JWT 认证中间件：校验 access 令牌与同端会话版本并注入用户上下文；审计员强制只读。
// sv 为 nil 时跳过会话版本校验（仅单元测试使用）。
func withAuth(jm *auth.Manager, sv SessionValidator, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if token == "" {
			Fail(w, http.StatusUnauthorized, 4010, "缺少访问令牌")
			return
		}
		claims, err := jm.Parse(token)
		if err != nil {
			Fail(w, http.StatusUnauthorized, 4011, "令牌无效或已过期")
			return
		}
		if auth.ClaimType(claims) != auth.TypeAccess {
			Fail(w, http.StatusUnauthorized, 4012, "令牌类型错误")
			return
		}
		u := &UserCtx{
			UserID:   auth.ClaimInt64(claims, "uid"),
			Username: auth.ClaimStr(claims, "username"),
			Role:     auth.ClaimStr(claims, "role"),
			TenantID: auth.ClaimInt64(claims, "tid"),
		}
		if u.UserID == 0 {
			Fail(w, http.StatusUnauthorized, 4013, "令牌缺少用户信息")
			return
		}
		// 会话版本校验：令牌端类型/seq 与库中当前值不一致 = 该端已被新登录顶替
		if sv != nil {
			clientType := auth.NormalizeClientType(auth.ClaimStr(claims, "ct"))
			seq := auth.ClaimInt64(claims, "seq")
			cur, err := sv.GetSessionSeq(r.Context(), u.UserID, clientType)
			if err != nil {
				Fail(w, http.StatusUnauthorized, 4013, "登录状态已失效，请重新登录")
				return
			}
			if cur != seq {
				if seq < cur {
					_ = sv.InsertAudit(r.Context(), store.AuditEntry{
						TenantID: auditTenantID(u.TenantID),
						Actor:    u.Username, Action: "login",
						Detail:   fmt.Sprintf("会话被顶替：%s 端新登录生效（seq %d→%d）", clientType, seq, cur),
						ClientIP: ClientIP(r),
					})
				}
				Fail(w, http.StatusUnauthorized, SessionKickedCode, ErrSessionKicked.Error())
				return
			}
		}
		if u.Role == "auditor" && r.Method != http.MethodGet && r.Method != http.MethodHead {
			Fail(w, http.StatusForbidden, 4030, "审计员账号为只读权限")
			return
		}
		next(w, r.WithContext(WithUser(r.Context(), u)))
	}
}

// auditTenantID 租户 ID 转 AuditEntry 存储（0 = 平台级）。
func auditTenantID(tid int64) (n sql.NullInt64) {
	n.Int64, n.Valid = tid, tid > 0
	return
}

// requireRoles 角色校验中间件。
func requireRoles(roles ...string) func(http.HandlerFunc) http.HandlerFunc {
	return func(next http.HandlerFunc) http.HandlerFunc {
		return func(w http.ResponseWriter, r *http.Request) {
			u := CurrentUser(r)
			if u == nil {
				Fail(w, http.StatusUnauthorized, 4010, "未认证")
				return
			}
			for _, role := range roles {
				if u.Role == role {
					next(w, r)
					return
				}
			}
			Fail(w, http.StatusForbidden, 4031, "当前角色无权执行该操作")
		}
	}
}

// chain 组合中间件（从左到右执行）。
func chain(h http.HandlerFunc, mw ...func(http.HandlerFunc) http.HandlerFunc) http.HandlerFunc {
	for i := len(mw) - 1; i >= 0; i-- {
		h = mw[i](h)
	}
	return h
}

// tenantScope 返回当前用户的租户过滤范围：super_admin 可通过 ?tenant_id= 指定，其余强制本租户。
func tenantScope(r *http.Request, u *UserCtx) int64 {
	if u.IsSuper() {
		return int64(QueryInt(r, "tenant_id", 0))
	}
	return u.TenantID
}

// TenantNullID 供写入场景使用：super_admin 指定租户，否则取本租户。
func TenantNullID(r *http.Request, u *UserCtx, specified int64) (int64, bool) {
	if u.IsSuper() {
		return specified, true
	}
	return u.TenantID, u.TenantID > 0
}

// storeIface 业务处理器依赖的存储类型别名。
type storeIface = *store.Store
