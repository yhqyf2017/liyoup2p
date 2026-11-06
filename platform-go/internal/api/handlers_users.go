package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"

	"liyou-platform/internal/auth"
	"liyou-platform/internal/store"
)

// UsersAPI 用户管理处理器（直接依赖 *store.Store）。
type UsersAPI struct {
	St storeIface
}

var validRoles = map[string]bool{"super_admin": true, "tenant_admin": true, "operator": true, "auditor": true}

// ListUsers 用户列表。
func (a *UsersAPI) List(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r)
	users, err := a.St.ListUsers(r.Context(), tenantScope(r, u))
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "查询用户失败: "+err.Error())
		return
	}
	OK(w, map[string]any{"items": users, "total": len(users)})
}

type createUserRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
	Role     string `json:"role"`
	TenantID int64  `json:"tenant_id"`
}

// Create 新建用户：super_admin 可任意指定租户与角色；tenant_admin 仅限本租户。
func (a *UsersAPI) Create(w http.ResponseWriter, r *http.Request) {
	var req createUserRequest
	if !ReadJSON(w, r, &req) {
		return
	}
	actor := CurrentUser(r)
	req.Username = strings.TrimSpace(req.Username)
	if req.Username == "" || len(req.Password) < 6 {
		Fail(w, http.StatusBadRequest, 1003, "用户名不能为空，密码至少 6 位")
		return
	}
	if !validRoles[req.Role] {
		Fail(w, http.StatusBadRequest, 1004, "角色必须是 super_admin/tenant_admin/operator/auditor")
		return
	}
	if !actor.IsSuper() {
		if req.Role == "super_admin" {
			Fail(w, http.StatusForbidden, 4033, "租户管理员不能创建超级管理员")
			return
		}
		req.TenantID = actor.TenantID
	}
	if req.Role == "super_admin" {
		req.TenantID = 0
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "密码散列失败")
		return
	}
	tid := sqlNullTenant(req.TenantID)
	id, err := a.St.CreateUser(r.Context(), tid, req.Username, hash, req.Role)
	if err != nil {
		Fail(w, http.StatusConflict, 4090, "创建用户失败（用户名可能已存在）: "+err.Error())
		return
	}
	a.St.InsertAudit(r.Context(), store.AuditEntry{
		TenantID: tid, Actor: actor.Username, Action: "login",
		Detail: "创建用户 " + req.Username + " (" + req.Role + ")",
	})
	OK(w, map[string]any{"id": id})
}

func sqlNullTenant(id int64) (v sql.NullInt64) {
	if id > 0 {
		v.Int64 = id
		v.Valid = true
	}
	return
}

type updateUserRequest struct {
	Role   *string `json:"role"`
	Status *string `json:"status"`
}

// Update 更新用户角色/状态。
func (a *UsersAPI) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req updateUserRequest
	if !ReadJSON(w, r, &req) {
		return
	}
	actor := CurrentUser(r)
	target, err := a.St.GetUserByID(r.Context(), id)
	if err != nil || target == nil {
		Fail(w, http.StatusNotFound, 4040, "用户不存在")
		return
	}
	if !actor.IsSuper() && (!target.TenantID.Valid || target.TenantID.Int64 != actor.TenantID) {
		Fail(w, http.StatusForbidden, 4033, "无权操作其他租户的用户")
		return
	}
	if req.Role != nil && !validRoles[*req.Role] {
		Fail(w, http.StatusBadRequest, 1004, "角色非法")
		return
	}
	if req.Status != nil && *req.Status != "active" && *req.Status != "disabled" {
		Fail(w, http.StatusBadRequest, 1005, "状态只能是 active/disabled")
		return
	}
	if err := a.St.UpdateUser(r.Context(), id, req.Role, req.Status); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "更新用户失败: "+err.Error())
		return
	}
	OK(w, nil)
}

// Delete 删除用户。
func (a *UsersAPI) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	actor := CurrentUser(r)
	if id == actor.UserID {
		Fail(w, http.StatusBadRequest, 1006, "不能删除自己")
		return
	}
	target, err := a.St.GetUserByID(r.Context(), id)
	if err != nil || target == nil {
		Fail(w, http.StatusNotFound, 4040, "用户不存在")
		return
	}
	if !actor.IsSuper() && (!target.TenantID.Valid || target.TenantID.Int64 != actor.TenantID) {
		Fail(w, http.StatusForbidden, 4033, "无权操作其他租户的用户")
		return
	}
	if err := a.St.DeleteUser(r.Context(), id); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "删除用户失败: "+err.Error())
		return
	}
	OK(w, nil)
}

type resetPasswordRequest struct {
	Password string `json:"password"`
}

// ResetPassword 重置密码。
func (a *UsersAPI) ResetPassword(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req resetPasswordRequest
	if !ReadJSON(w, r, &req) {
		return
	}
	if len(req.Password) < 6 {
		Fail(w, http.StatusBadRequest, 1003, "密码至少 6 位")
		return
	}
	actor := CurrentUser(r)
	target, err := a.St.GetUserByID(r.Context(), id)
	if err != nil || target == nil {
		Fail(w, http.StatusNotFound, 4040, "用户不存在")
		return
	}
	if !actor.IsSuper() && (!target.TenantID.Valid || target.TenantID.Int64 != actor.TenantID) {
		Fail(w, http.StatusForbidden, 4033, "无权操作其他租户的用户")
		return
	}
	hash, err := auth.HashPassword(req.Password)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "密码散列失败")
		return
	}
	if err := a.St.ResetPassword(r.Context(), id, hash); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "重置密码失败: "+err.Error())
		return
	}
	OK(w, nil)
}
