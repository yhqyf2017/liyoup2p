package api

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/http"
	"time"

	"liyou-platform/internal/auth"
	"liyou-platform/internal/store"
)

// ErrSessionKicked 会话被同端新登录顶替（HTTP 401 + 业务码 4016）。
var ErrSessionKicked = errors.New("账号已在其他设备登录")

// SessionKickedCode 业务码：供客户端区分「被踢下线」与普通登录过期。
const SessionKickedCode = 4016

// AuthStore 认证相关存储接口（便于单元测试替换实现）。
type AuthStore interface {
	FindUserByUsername(ctx context.Context, username string) (*store.User, error)
	FindTenantByCode(ctx context.Context, code string) (*store.Tenant, error)
	FindTenantByID(ctx context.Context, id int64) (*store.Tenant, error)
	TouchLogin(ctx context.Context, userID int64) error
	BumpSessionSeq(ctx context.Context, userID int64, clientType string) (int64, error)
	GetSessionSeq(ctx context.Context, userID int64, clientType string) (int64, error)
	InsertAudit(ctx context.Context, e store.AuditEntry) error
}

// AuthAPI 认证处理器。
type AuthAPI struct {
	St  AuthStore
	JWT *auth.Manager
}

type loginRequest struct {
	Username   string `json:"username"`
	Password   string `json:"password"`
	Tenant     string `json:"tenant"`
	ClientType string `json:"client_type"` // pc | mobile，缺省 pc
}

type userPayload struct {
	ID       int64  `json:"id"`
	Username string `json:"username"`
	Role     string `json:"role"`
	TenantID int64  `json:"tenant_id"`
	Tenant   string `json:"tenant"`
}

// Login 用户登录，签发 access + refresh 令牌。
// 同一 IP 60 秒内 10 次密码错误锁定 15 分钟；锁定期间凭据正确也一律拒绝。
func (a *AuthAPI) Login(w http.ResponseWriter, r *http.Request) {
	ip := ClientIP(r)
	if locked, remain := loginGuard.check(ip); locked {
		Fail(w, http.StatusTooManyRequests, 4290,
			fmt.Sprintf("登录尝试过于频繁，该地址已被锁定，请约 %d 分钟后重试", int(remain.Minutes())+1))
		return
	}
	var req loginRequest
	if !ReadJSON(w, r, &req) {
		return
	}
	if req.Username == "" || req.Password == "" {
		Fail(w, http.StatusBadRequest, 1002, "用户名和密码不能为空")
		return
	}
	ctx := r.Context()
	u, err := a.St.FindUserByUsername(ctx, req.Username)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "查询用户失败: "+err.Error())
		return
	}
	if u == nil || u.Status != "active" || auth.VerifyPassword(u.PasswordHash, req.Password) != nil {
		loginGuard.recordFail(ip)
		a.audit(ctx, u, "login", "登录失败：用户名或密码错误", ip)
		Fail(w, http.StatusUnauthorized, 4014, "用户名或密码错误")
		return
	}
	loginGuard.reset(ip)
	tid := int64(0)
	tenantCode := "platform"
	if u.TenantID.Valid {
		tid = u.TenantID.Int64
		if req.Tenant != "" && req.Tenant != "platform" {
			t, err := a.St.FindTenantByCode(ctx, req.Tenant)
			if err != nil {
				Fail(w, http.StatusInternalServerError, 5000, "查询租户失败: "+err.Error())
				return
			}
			if t == nil || t.ID != tid {
				a.audit(ctx, u, "login", "登录失败：租户不匹配", ClientIP(r))
				Fail(w, http.StatusForbidden, 4032, "账号不属于该租户")
				return
			}
		}
		if t, _ := a.St.FindTenantByID(ctx, tid); t != nil {
			tenantCode = t.Code
		}
	} else if req.Tenant != "" && req.Tenant != "platform" {
		a.audit(ctx, u, "login", "登录失败：平台账号需以 platform 租户登录", ClientIP(r))
		Fail(w, http.StatusForbidden, 4032, "平台账号请使用 tenant=platform 登录")
		return
	}
	clientType := auth.NormalizeClientType(req.ClientType)
	// 会话版本自增：同端类型的新登录使旧令牌全部失效（另一端类型不受影响）
	seq, err := a.St.BumpSessionSeq(ctx, u.ID, clientType)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "会话版本更新失败: "+err.Error())
		return
	}
	access, err := a.JWT.AccessToken(u.ID, u.Username, u.Role, tid, clientType, seq)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "签发令牌失败: "+err.Error())
		return
	}
	refresh, err := a.JWT.RefreshToken(u.ID, u.Username, u.Role, tid, clientType, seq)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "签发令牌失败: "+err.Error())
		return
	}
	_ = a.St.TouchLogin(ctx, u.ID)
	detail := "登录成功（" + clientType + " 端）"
	if seq > 1 {
		detail += "，已顶替该端原有登录会话"
	}
	a.audit(ctx, u, "login", detail, ClientIP(r))
	OK(w, map[string]any{
		"access_token":  access,
		"refresh_token": refresh,
		"token_type":    "Bearer",
		"expires_in":    int(auth.AccessTTL / time.Second),
		"user": userPayload{
			ID: u.ID, Username: u.Username, Role: u.Role, TenantID: tid, Tenant: tenantCode,
		},
	})
}

func (a *AuthAPI) audit(ctx context.Context, u *store.User, action, detail, ip string) {
	e := store.AuditEntry{Actor: "user", Action: action, Detail: detail, ClientIP: ip}
	if u != nil {
		e.Actor = u.Username
		e.TenantID = u.TenantID
	}
	_ = a.St.InsertAudit(ctx, e)
}

type refreshRequest struct {
	RefreshToken string `json:"refresh_token"`
}

// Refresh 刷新令牌：校验刷新令牌的会话版本（被同端新登录顶替后刷新同样失败）。
func (a *AuthAPI) Refresh(w http.ResponseWriter, r *http.Request) {
	var req refreshRequest
	if !ReadJSON(w, r, &req) {
		return
	}
	claims, err := a.JWT.Parse(req.RefreshToken)
	if err != nil || auth.ClaimType(claims) != auth.TypeRefresh {
		Fail(w, http.StatusUnauthorized, 4015, "刷新令牌无效或已过期")
		return
	}
	uid := auth.ClaimInt64(claims, "uid")
	username := auth.ClaimStr(claims, "username")
	role := auth.ClaimStr(claims, "role")
	tid := auth.ClaimInt64(claims, "tid")
	clientType := auth.NormalizeClientType(auth.ClaimStr(claims, "ct"))
	seq := auth.ClaimInt64(claims, "seq")
	// 旧客户端升级前签发的刷新令牌无 ct/seq 声明（视为 pc/0），仍可换取新令牌
	if a.St != nil {
		cur, err := a.St.GetSessionSeq(r.Context(), uid, clientType)
		if err != nil {
			if errors.Is(err, sql.ErrNoRows) {
				Fail(w, http.StatusUnauthorized, 4015, "用户不存在或已删除，请重新登录")
			} else {
				Fail(w, http.StatusInternalServerError, 5000, "会话校验失败")
			}
			return
		}
		if cur != seq {
			Fail(w, http.StatusUnauthorized, SessionKickedCode, ErrSessionKicked.Error())
			return
		}
	}
	access, err := a.JWT.AccessToken(uid, username, role, tid, clientType, seq)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "签发令牌失败")
		return
	}
	refresh, err := a.JWT.RefreshToken(uid, username, role, tid, clientType, seq)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "签发令牌失败")
		return
	}
	OK(w, map[string]any{"access_token": access, "refresh_token": refresh, "expires_in": int(auth.AccessTTL / time.Second)})
}

// Me 当前用户信息。
func (a *AuthAPI) Me(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r)
	tenantCode := "platform"
	tenantName := "平台管理"
	if u.TenantID > 0 {
		if t, _ := a.St.FindTenantByID(r.Context(), u.TenantID); t != nil {
			tenantCode = t.Code
			tenantName = t.Name
		}
	}
	OK(w, map[string]any{
		"user": userPayload{ID: u.UserID, Username: u.Username, Role: u.Role, TenantID: u.TenantID, Tenant: tenantCode},
		"tenant_name": tenantName,
	})
}
