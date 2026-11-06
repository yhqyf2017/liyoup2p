// Package store 数据访问层：pgx(stdlib) 连接、幂等建表、种子数据与业务查询。
package store

import (
	"database/sql"
	"time"
)

// Tenant 租户。
type Tenant struct {
	ID        int64
	Name      string
	Code      string
	PlanID    sql.NullInt64
	Status    string
	CreatedAt time.Time
}

// Plan 套餐。
type Plan struct {
	ID              int64
	Name            string
	MaxDevices      int
	MaxPorts        int
	MonthlyTrafficGB int
	PriceCents      int
}

// User 平台用户。
type User struct {
	ID           int64
	TenantID     sql.NullInt64
	Username     string
	PasswordHash string
	Role         string // super_admin | tenant_admin | operator | auditor
	Status       string
	CreatedAt    time.Time
	LastLoginAt  sql.NullTime
}

// UserView 用户列表视图。
type UserView struct {
	ID           int64
	TenantID     sql.NullInt64
	TenantName   string
	Username     string
	Role         string
	Status       string
	CreatedAt    time.Time
	LastLoginAt  sql.NullTime
}

// Device 设备。
type Device struct {
	ID           int64
	TenantID     int64
	DeviceUID    string
	Name         string
	Role         string // server | client
	TokenHash    string
	Status       string // online | offline | revoked
	OSInfo       string
	LastOnlineAt sql.NullTime
	CreatedAt    time.Time
	PortCount    int64
}

// DeviceView 设备列表视图（含租户名）。
type DeviceView struct {
	Device
	TenantName string
}

// PortInput 服务端口上报。
type PortInput struct {
	Port     int
	Label    string
	Declared bool
}

// PortView 端口聚合视图。
type PortView struct {
	ID         int64
	DeviceUID  string
	DeviceName string
	Role       string
	Status     string
	Port       int
	Label      string
	Declared   bool
}

// TunnelSession 隧道会话。
type TunnelSession struct {
	ID             int64
	SessionID      string
	TenantID       int64
	ClientDeviceID sql.NullInt64
	ServerDeviceID sql.NullInt64
	ClientUID      string
	ServerUID      string
	Port           int
	Mode           string // p2p | relay
	Status         string // active | ended
	RelayBytes     int64
	StartedAt      time.Time
	EndedAt        sql.NullTime
}

// AuditEntry 审计写入。
type AuditEntry struct {
	TenantID sql.NullInt64
	Actor    string
	Action   string // connect | close | blocked | login
	Detail   string
	ClientIP string
}

// AuditView 审计查询行。
type AuditView struct {
	ID        int64
	TenantID  sql.NullInt64
	Actor     string
	Action    string
	Detail    string
	ClientIP  sql.NullString
	CreatedAt time.Time
}

// Alert 告警。
type Alert struct {
	ID        int64
	TenantID  sql.NullInt64
	Level     string
	Title     string
	Detail    string
	IsRead    bool
	CreatedAt time.Time
}

// Stats 平台统计。
type Stats struct {
	OnlineDevices int64
	TotalDevices  int64
	TodayRequests int64
	RelayBytes    int64
	TodaySessions int64
	P2PSessions   int64
}
