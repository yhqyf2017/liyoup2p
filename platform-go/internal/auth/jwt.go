package auth

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Token 类型常量。
const (
	TypeAccess  = "access"
	TypeRefresh = "refresh"
	TypeDevice  = "device"
)

const (
	AccessTTL  = 2 * time.Hour
	RefreshTTL = 30 * 24 * time.Hour
	DeviceTTL  = 30 * 24 * time.Hour
)

// Manager JWT 签发与校验（HS256）。
type Manager struct {
	secret []byte
}

// NewManager 创建 JWT 管理器。
func NewManager(secret string) *Manager {
	return &Manager{secret: []byte(secret)}
}

// ClientTypePC / ClientTypeMobile 用户会话端类型（同端单会话互踢，两端互不影响）。
const (
	ClientTypePC     = "pc"
	ClientTypeMobile = "mobile"
)

// NormalizeClientType 归一化端类型：mobile 之外（含空值/旧客户端）一律按 pc。
func NormalizeClientType(ct string) string {
	if ct == ClientTypeMobile {
		return ClientTypeMobile
	}
	return ClientTypePC
}

// AccessToken 用户访问令牌（2 小时）。clientType+seq 为会话版本：
// 同端类型新登录 seq 自增，旧令牌校验失败即被踢下线。
func (m *Manager) AccessToken(uid int64, username, role string, tenantID int64, clientType string, seq int64) (string, error) {
	claims := jwt.MapClaims{
		"typ": TypeAccess, "uid": uid, "username": username, "role": role,
		"ct": clientType, "seq": seq,
		"iat": time.Now().Unix(), "exp": time.Now().Add(AccessTTL).Unix(),
	}
	if tenantID > 0 {
		claims["tid"] = tenantID
	}
	return m.sign(claims)
}

// RefreshToken 用户刷新令牌（30 天）。携带与 access 相同的 ct/seq，刷新时不自增 seq。
func (m *Manager) RefreshToken(uid int64, username, role string, tenantID int64, clientType string, seq int64) (string, error) {
	claims := jwt.MapClaims{
		"typ": TypeRefresh, "uid": uid, "username": username, "role": role,
		"ct": clientType, "seq": seq,
		"iat": time.Now().Unix(), "exp": time.Now().Add(RefreshTTL).Unix(),
	}
	if tenantID > 0 {
		claims["tid"] = tenantID
	}
	return m.sign(claims)
}

// DeviceToken 设备令牌（30 天，含设备与租户信息）。
func (m *Manager) DeviceToken(deviceID int64, deviceUID string, tenantID int64, name string) (string, error) {
	claims := jwt.MapClaims{
		"typ": TypeDevice, "did": deviceID, "duid": deviceUID, "name": name,
		"iat": time.Now().Unix(), "exp": time.Now().Add(DeviceTTL).Unix(),
	}
	if tenantID > 0 {
		claims["tid"] = tenantID
	}
	return m.sign(claims)
}

func (m *Manager) sign(claims jwt.MapClaims) (string, error) {
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
}

// Parse 校验并解析令牌，返回 claims。
func (m *Manager) Parse(tokenStr string) (jwt.MapClaims, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (any, error) {
		return m.secret, nil
	}, jwt.WithValidMethods([]string{"HS256"}))
	if err != nil {
		return nil, err
	}
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok || !token.Valid {
		return nil, errors.New("令牌无效")
	}
	return claims, nil
}

// ClaimType 取令牌类型。
func ClaimType(c jwt.MapClaims) string { return str(c, "typ") }

// ClaimInt64 取整型声明。
func ClaimInt64(c jwt.MapClaims, key string) int64 {
	if v, ok := c[key].(float64); ok {
		return int64(v)
	}
	return 0
}

// ClaimStr 取字符串声明。
func ClaimStr(c jwt.MapClaims, key string) string { return str(c, key) }

func str(c jwt.MapClaims, key string) string {
	if v, ok := c[key].(string); ok {
		return v
	}
	return ""
}
