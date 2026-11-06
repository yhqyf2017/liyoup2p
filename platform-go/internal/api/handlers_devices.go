package api

import (
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"net/http"
	"strconv"

	"liyou-platform/internal/auth"
	"liyou-platform/internal/store"
)

// DeviceKicker 设备连接管理（由 hub 实现，用于吊销后强制下线）。
type DeviceKicker interface {
	Kick(deviceID int64)
}

// DevicesAPI 设备管理处理器。
type DevicesAPI struct {
	St     storeIface
	JWT    *auth.Manager
	Kicker DeviceKicker // 可为 nil
}

// List 设备列表。
func (a *DevicesAPI) List(w http.ResponseWriter, r *http.Request) {
	u := CurrentUser(r)
	devices, err := a.St.ListDevices(r.Context(), tenantScope(r, u), r.URL.Query().Get("role"), r.URL.Query().Get("status"))
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "查询设备失败: "+err.Error())
		return
	}
	items := make([]map[string]any, 0, len(devices))
	for _, d := range devices {
		items = append(items, map[string]any{
			"id": d.ID, "tenant_id": d.TenantID, "tenant_name": d.TenantName,
			"device_uid": d.DeviceUID, "name": d.Name, "role": d.Role,
			"status": d.Status, "os_info": d.OSInfo, "port_count": d.PortCount,
			"last_online_at": d.LastOnlineAt, "created_at": d.CreatedAt,
		})
	}
	OK(w, map[string]any{"items": items, "total": len(items)})
}

type registerDeviceRequest struct {
	DeviceUID string `json:"device_uid"`
	Name      string `json:"name"`
	Role      string `json:"role"`
	TenantID  int64  `json:"tenant_id"`
}

// Register 注册设备并签发设备令牌；同租户同 UID 的设备将重新签发令牌。
func (a *DevicesAPI) Register(w http.ResponseWriter, r *http.Request) {
	var req registerDeviceRequest
	if !ReadJSON(w, r, &req) {
		return
	}
	actor := CurrentUser(r)
	if req.Role != "server" && req.Role != "client" {
		Fail(w, http.StatusBadRequest, 1008, "设备角色必须是 server 或 client")
		return
	}
	tid, ok := TenantNullID(r, actor, req.TenantID)
	if !ok {
		Fail(w, http.StatusBadRequest, 1009, "缺少租户信息")
		return
	}
	ctx := r.Context()
	var device *store.Device
	if req.DeviceUID != "" {
		if d, err := a.St.FindDeviceByUID(ctx, req.DeviceUID); err != nil {
			Fail(w, http.StatusInternalServerError, 5000, "查询设备失败: "+err.Error())
			return
		} else if d != nil {
			if !actor.IsSuper() && d.TenantID != actor.TenantID {
				Fail(w, http.StatusForbidden, 4033, "设备 UID 已被其他租户占用")
				return
			}
			if d.Status == "revoked" {
				// 已吊销设备不允许复用 UID 重签令牌，防止注册接口复活吊销设备
				Fail(w, http.StatusForbidden, 4034, "设备已被吊销，无法重新注册")
				return
			}
			device = d
		}
	}
	if device == nil {
		uid := req.DeviceUID
		if uid == "" {
			uid = "LY" + req.Role[:1] + "-" + randomHex(6)
		}
		id, err := a.St.CreateDevice(ctx, tid, uid, req.Name, req.Role, "")
		if err != nil {
			Fail(w, http.StatusInternalServerError, 5000, "注册设备失败: "+err.Error())
			return
		}
		if d, err := a.St.FindDeviceByID(ctx, id); err == nil {
			device = d
		}
	}
	if device == nil {
		Fail(w, http.StatusInternalServerError, 5000, "注册设备失败")
		return
	}
	token, err := a.JWT.DeviceToken(device.ID, device.DeviceUID, device.TenantID, req.Name)
	if err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "签发设备令牌失败")
		return
	}
	sum := sha256.Sum256([]byte(token))
	if err := a.St.SetDeviceToken(ctx, device.ID, hex.EncodeToString(sum[:])); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "保存设备令牌失败: "+err.Error())
		return
	}
	a.St.InsertAudit(ctx, store.AuditEntry{
		TenantID: sql.NullInt64{Int64: device.TenantID, Valid: true}, Actor: actor.Username,
		Action: "login", Detail: "注册设备 " + device.DeviceUID + " (" + device.Role + ")",
	})
	OK(w, map[string]any{
		"device_id": device.ID, "device_uid": device.DeviceUID, "role": device.Role,
		"device_token": token, "expires_in": "30d",
	})
}

// Delete 吊销设备并强制下线。
func (a *DevicesAPI) Delete(w http.ResponseWriter, r *http.Request) {
	id, _ := strconv.ParseInt(r.PathValue("id"), 10, 64)
	actor := CurrentUser(r)
	ctx := r.Context()
	device, err := a.St.FindDeviceByID(ctx, id)
	if err != nil || device == nil {
		Fail(w, http.StatusNotFound, 4041, "设备不存在")
		return
	}
	if !actor.IsSuper() && device.TenantID != actor.TenantID {
		Fail(w, http.StatusForbidden, 4033, "无权操作其他租户的设备")
		return
	}
	if err := a.St.RevokeDevice(ctx, id); err != nil {
		Fail(w, http.StatusInternalServerError, 5000, "吊销设备失败: "+err.Error())
		return
	}
	if a.Kicker != nil {
		a.Kicker.Kick(id)
	}
	a.St.InsertAudit(ctx, store.AuditEntry{
		TenantID: sql.NullInt64{Int64: device.TenantID, Valid: true}, Actor: actor.Username,
		Action: "blocked", Detail: "吊销设备 " + device.DeviceUID,
	})
	OK(w, nil)
}

func randomHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
