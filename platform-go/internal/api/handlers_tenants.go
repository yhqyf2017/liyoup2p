package api

import (
	"database/sql"
	"net/http"
	"strconv"
	"strings"
)

// TenantsAPI 租户管理（仅 super_admin，由路由中间件保证）。
type TenantsAPI struct {
	St storeIface
}

// List 租户列表（分页，keyword 模糊匹配名称/编码）。
func (a *TenantsAPI) List(w http.ResponseWriter, r *http.Request) {
	page, size := PageArgs(r)
	keyword := strings.TrimSpace(r.URL.Query().Get("keyword"))
	tenants, total, err := a.St.ListTenants(r.Context(), keyword, page, size)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "查询租户失败: "+err.Error())
		return
	}
	OK(w, map[string]any{"items": tenants, "total": total})
}

type createTenantRequest struct {
	Name   string `json:"name"`
	Code   string `json:"code"`
	PlanID int64  `json:"plan_id"`
}

// Create 新建租户。
func (a *TenantsAPI) Create(w http.ResponseWriter, r *http.Request) {
	var req createTenantRequest
	if !ReadJSON(w, r, &req) {
		return
	}
	if req.Name == "" || req.Code == "" {
		Fail(w, http.StatusBadRequest, 1007, "租户名称与编码不能为空")
		return
	}
	var planID sql.NullInt64
	if req.PlanID > 0 {
		planID.Int64 = req.PlanID
		planID.Valid = true
	}
	id, err := a.St.CreateTenant(r.Context(), req.Name, req.Code, planID)
	if err != nil {
		Fail(w, http.StatusConflict, 4091, "创建租户失败（编码可能已存在）: "+err.Error())
		return
	}
	OK(w, map[string]any{"id": id})
}

type updateTenantRequest struct {
	Name   *string `json:"name"`
	Status *string `json:"status"`
	PlanID *int64  `json:"plan_id"`
}

// Update 更新租户。
func (a *TenantsAPI) Update(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	var req updateTenantRequest
	if !ReadJSON(w, r, &req) {
		return
	}
	if req.Status != nil && *req.Status != "active" && *req.Status != "disabled" {
		Fail(w, http.StatusBadRequest, 1005, "状态只能是 active/disabled")
		return
	}
	if err := a.St.UpdateTenant(r.Context(), id, req.Name, req.Status, req.PlanID); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "更新租户失败: "+err.Error())
		return
	}
	OK(w, nil)
}

// Delete 删除租户。
func (a *TenantsAPI) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err := a.St.DeleteTenant(r.Context(), id); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "删除租户失败: "+err.Error())
		return
	}
	OK(w, nil)
}
