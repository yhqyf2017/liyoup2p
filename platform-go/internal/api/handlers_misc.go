package api

import (
	"net/http"
	"strconv"
)

// MiscAPI 审计 / 会话 / 告警 / 统计处理器。
type MiscAPI struct {
	St storeIface
}

// Audit 审计日志分页查询。
func (a *MiscAPI) Audit(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r)
	page := QueryInt(r, "page", 1)
	size := QueryInt(r, "page_size", 20)
	items, total, err := a.St.ListAudit(r.Context(), tenantScope(r, u), r.URL.Query().Get("action"), page, size)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "查询审计失败: "+err.Error())
		return
	}
	type auditItem struct {
		ID         int64  `json:"id"`
		TenantID   int64  `json:"tenant_id"`
		TenantName string `json:"tenant_name"`
		Actor      string `json:"actor"`
		Action     string `json:"action"`
		Detail     string `json:"detail"`
		ClientIP   string `json:"client_ip"`
		CreatedAt  string `json:"created_at"`
	}
	list := make([]auditItem, 0, len(items))
	for _, it := range items {
		list = append(list, auditItem{
			ID: it.ID, TenantID: it.TenantID.Int64, TenantName: it.TenantName, Actor: it.Actor, Action: it.Action,
			Detail: it.Detail, ClientIP: it.ClientIP.String, CreatedAt: it.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	OK(w, map[string]any{"items": list, "total": total, "page": page, "page_size": size})
}

// Sessions 隧道会话列表（含租户名/两端设备名/归属账号）。
func (a *MiscAPI) Sessions(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r)
	sessions, err := a.St.ListSessions(r.Context(), tenantScope(r, u), r.URL.Query().Get("status"))
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "查询会话失败: "+err.Error())
		return
	}
	items := make([]map[string]any, 0, len(sessions))
	for _, s := range sessions {
		var ended any
		if s.EndedAt.Valid {
			ended = s.EndedAt.Time.Format("2006-01-02 15:04:05")
		}
		items = append(items, map[string]any{
			"session_id": s.SessionID, "tenant_id": s.TenantID, "tenant_name": s.TenantName,
			"client_device_uid": s.ClientUID, "server_device_uid": s.ServerUID,
			"client_name": s.ClientName, "server_name": s.ServerName,
			"client_owner": s.ClientOwner, "server_owner": s.ServerOwner,
			"port": s.Port, "mode": s.Mode, "status": s.Status,
			"relay_bytes": s.RelayBytes, "started_at": s.StartedAt.Format("2006-01-02 15:04:05"),
			"ended_at": ended,
		})
	}
	OK(w, map[string]any{"items": items, "total": len(items)})
}

// Alerts 告警列表（分页）。
func (a *MiscAPI) Alerts(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r)
	unread := r.URL.Query().Get("unread") == "1"
	page, size := PageArgs(r)
	alerts, total, err := a.St.ListAlerts(r.Context(), tenantScope(r, u), unread, page, size)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "查询告警失败: "+err.Error())
		return
	}
	items := make([]map[string]any, 0, len(alerts))
	for _, al := range alerts {
		items = append(items, map[string]any{
			"id": al.ID, "tenant_id": al.TenantID.Int64, "tenant_name": al.TenantName, "level": al.Level,
			"title": al.Title, "detail": al.Detail, "is_read": al.IsRead,
			"created_at": al.CreatedAt.Format("2006-01-02 15:04:05"),
		})
	}
	OK(w, map[string]any{"items": items, "total": total})
}

// AlertRead 标记单条告警已读。
func (a *MiscAPI) AlertRead(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	u := CurrentUser(r)
	if err := a.St.MarkAlertRead(r.Context(), id, u.TenantID); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "标记已读失败: "+err.Error())
		return
	}
	OK(w, nil)
}

// AlertsReadAll 全部已读。
func (a *MiscAPI) AlertsReadAll(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r)
	if err := a.St.MarkAllAlertsRead(r.Context(), u.TenantID); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "标记已读失败: "+err.Error())
		return
	}
	OK(w, nil)
}

// Stats 平台统计。
func (a *MiscAPI) Stats(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r)
	st, err := a.St.Stats(r.Context(), tenantScope(r, u))
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "查询统计失败: "+err.Error())
		return
	}
	rate := 0.0
	if st.TodaySessions > 0 {
		rate = float64(st.P2PSessions) / float64(st.TodaySessions) * 100
	}
	OK(w, map[string]any{
		"online_devices": st.OnlineDevices,
		"total_devices":  st.TotalDevices,
		"today_requests": st.TodayRequests,
		"relay_bytes":    st.RelayBytes,
		"today_sessions": st.TodaySessions,
		"p2p_rate":       float64(int(rate*10+0.5)) / 10,
	})
}
