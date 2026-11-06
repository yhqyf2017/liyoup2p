package api

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"liyou-platform/internal/auth"
	"liyou-platform/internal/store"
)

// fakeAuthStore 内存实现 AuthStore 接口，用于最小单测。
type fakeAuthStore struct {
	users   map[string]*store.User
	tenants map[string]*store.Tenant
	audits  []store.AuditEntry
	touched []int64
	seqs    map[string]int64 // "uid:clientType" → 会话版本
}

func newFakeStore(t *testing.T, pass string) *fakeAuthStore {
	t.Helper()
	hash, err := auth.HashPassword(pass)
	if err != nil {
		t.Fatalf("散列失败: %v", err)
	}
	return &fakeAuthStore{
		users: map[string]*store.User{
			"admin": {ID: 2, TenantID: storeInt64(1), Username: "admin", PasswordHash: hash, Role: "tenant_admin", Status: "active"},
		},
		tenants: map[string]*store.Tenant{
			"liyou-demo": {ID: 1, Name: "武汉理游商贸", Code: "liyou-demo", Status: "active"},
		},
		seqs: map[string]int64{},
	}
}

func storeInt64(v int64) (n sql.NullInt64) { n.Int64 = v; n.Valid = true; return }

func (f *fakeAuthStore) FindUserByUsername(_ context.Context, username string) (*store.User, error) {
	return f.users[username], nil
}
func (f *fakeAuthStore) FindTenantByCode(_ context.Context, code string) (*store.Tenant, error) {
	return f.tenants[code], nil
}
func (f *fakeAuthStore) FindTenantByID(_ context.Context, id int64) (*store.Tenant, error) {
	for _, t := range f.tenants {
		if t.ID == id {
			return t, nil
		}
	}
	return nil, nil
}
func (f *fakeAuthStore) TouchLogin(_ context.Context, userID int64) error {
	f.touched = append(f.touched, userID)
	return nil
}
func (f *fakeAuthStore) BumpSessionSeq(_ context.Context, userID int64, clientType string) (int64, error) {
	key := fmt.Sprintf("%d:%s", userID, clientType)
	f.seqs[key]++
	return f.seqs[key], nil
}
func (f *fakeAuthStore) GetSessionSeq(_ context.Context, userID int64, clientType string) (int64, error) {
	return f.seqs[fmt.Sprintf("%d:%s", userID, clientType)], nil
}
func (f *fakeAuthStore) InsertAudit(_ context.Context, e store.AuditEntry) error {
	f.audits = append(f.audits, e)
	return nil
}

type envelope struct {
	Code int             `json:"code"`
	Msg  string          `json:"msg"`
	Data json.RawMessage `json:"data"`
}

func doLogin(t *testing.T, apiH *AuthAPI, body string) (*http.Response, envelope) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(apiH.Login))
	defer srv.Close()
	resp, err := http.Post(srv.URL, "application/json", strings.NewReader(body))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	var env envelope
	_ = json.NewDecoder(resp.Body).Decode(&env)
	return resp, env
}

// TestLoginSuccess 正确密码登录返回令牌且可解析。
func TestLoginSuccess(t *testing.T) {
	st := newFakeStore(t, "admin123")
	apiH := &AuthAPI{St: st, JWT: auth.NewManager("test-secret")}
	resp, env := doLogin(t, apiH, `{"username":"admin","password":"admin123"}`)
	if resp.StatusCode != http.StatusOK || env.Code != 0 {
		t.Fatalf("登录应成功，got %d %+v", resp.StatusCode, env)
	}
	var data struct {
		AccessToken  string `json:"access_token"`
		RefreshToken string `json:"refresh_token"`
		User         struct {
			Username string `json:"username"`
			Role     string `json:"role"`
			Tenant   string `json:"tenant"`
		} `json:"user"`
	}
	if err := json.Unmarshal(env.Data, &data); err != nil {
		t.Fatalf("解析 data 失败: %v", err)
	}
	if data.User.Username != "admin" || data.User.Role != "tenant_admin" || data.User.Tenant != "liyou-demo" {
		t.Fatalf("用户信息不符: %+v", data.User)
	}
	claims, err := apiH.JWT.Parse(data.AccessToken)
	if err != nil || auth.ClaimType(claims) != auth.TypeAccess {
		t.Fatalf("access 令牌应有效: %v", err)
	}
	if auth.ClaimInt64(claims, "tid") != 1 {
		t.Fatalf("应包含租户 ID")
	}
	// 刷新令牌
	claims2, err := apiH.JWT.Parse(data.RefreshToken)
	if err != nil || auth.ClaimType(claims2) != auth.TypeRefresh {
		t.Fatalf("refresh 令牌应有效: %v", err)
	}
	if len(st.audits) == 0 {
		t.Fatalf("登录应写审计日志")
	}
}

// TestLoginWrongPassword 密码错误返回 401。
func TestLoginWrongPassword(t *testing.T) {
	apiH := &AuthAPI{St: newFakeStore(t, "admin123"), JWT: auth.NewManager("test-secret")}
	resp, env := doLogin(t, apiH, `{"username":"admin","password":"bad"}`)
	if resp.StatusCode != http.StatusUnauthorized || env.Code == 0 {
		t.Fatalf("错误密码应 401，got %d %+v", resp.StatusCode, env)
	}
}

// TestLoginTenantMismatch 租户不匹配被拒绝。
func TestLoginTenantMismatch(t *testing.T) {
	apiH := &AuthAPI{St: newFakeStore(t, "admin123"), JWT: auth.NewManager("test-secret")}
	resp, _ := doLogin(t, apiH, `{"username":"admin","password":"admin123","tenant":"other-tenant"}`)
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("租户不匹配应 403，got %d", resp.StatusCode)
	}
}

// TestRefresh 换发新令牌对。
func TestRefresh(t *testing.T) {
	jm := auth.NewManager("test-secret")
	refresh, err := jm.RefreshToken(2, "admin", "tenant_admin", 1, auth.ClientTypePC, 1)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	st := newFakeStore(t, "admin123")
	st.seqs["2:pc"] = 1
	apiH := &AuthAPI{St: st, JWT: jm}
	srv := httptest.NewServer(http.HandlerFunc(apiH.Refresh))
	defer srv.Close()
	resp, err := http.Post(srv.URL, "application/json",
		strings.NewReader(`{"refresh_token":"`+refresh+`"}`))
	if err != nil {
		t.Fatalf("请求失败: %v", err)
	}
	defer resp.Body.Close()
	var env envelope
	_ = json.NewDecoder(resp.Body).Decode(&env)
	if resp.StatusCode != http.StatusOK || env.Code != 0 {
		t.Fatalf("刷新应成功，got %d %+v", resp.StatusCode, env)
	}
	var data struct {
		AccessToken string `json:"access_token"`
	}
	_ = json.Unmarshal(env.Data, &data)
	claims, err := jm.Parse(data.AccessToken)
	if err != nil || auth.ClaimType(claims) != auth.TypeAccess {
		t.Fatalf("新 access 令牌应有效: %v", err)
	}
}

// TestAuditorReadOnly 审计员角色在中间件下禁止写操作。
func TestAuditorReadOnly(t *testing.T) {
	jm := auth.NewManager("test-secret")
	token, err := jm.AccessToken(9, "aud1", "auditor", 1, auth.ClientTypePC, 0)
	if err != nil {
		t.Fatalf("签发失败: %v", err)
	}
	called := false
	h := withAuth(jm, nil, func(w http.ResponseWriter, r *http.Request) { called = true; OK(w, nil) })
	req := httptest.NewRequest(http.MethodPost, "/api/x", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	rec := httptest.NewRecorder()
	h(rec, req)
	if rec.Code != http.StatusForbidden || called {
		t.Fatalf("审计员写操作应被拒绝，got %d", rec.Code)
	}
	req2 := httptest.NewRequest(http.MethodGet, "/api/x", nil)
	req2.Header.Set("Authorization", "Bearer "+token)
	rec2 := httptest.NewRecorder()
	h(rec2, req2)
	if rec2.Code != http.StatusOK || !called {
		t.Fatalf("审计员读操作应放行，got %d", rec2.Code)
	}
}

// TestSessionKick 会话版本机制：同端新登录踢掉旧会话，另一端类型不受影响。
func TestSessionKick(t *testing.T) {
	st := newFakeStore(t, "admin123")
	jm := auth.NewManager("test-secret")
	apiH := &AuthAPI{St: st, JWT: jm}

	type tokenPair struct{ Access, Refresh string }
	parse := func(env envelope) tokenPair {
		var d struct {
			AccessToken  string `json:"access_token"`
			RefreshToken string `json:"refresh_token"`
		}
		_ = json.Unmarshal(env.Data, &d)
		return tokenPair{d.AccessToken, d.RefreshToken}
	}

	// 第一次 PC 登录（旧客户端不传 client_type，按 pc 处理）
	_, env1 := doLogin(t, apiH, `{"username":"admin","password":"admin123"}`)
	pc1 := parse(env1)
	// 移动端登录（不影响 PC 会话）
	_, envM := doLogin(t, apiH, `{"username":"admin","password":"admin123","client_type":"mobile"}`)
	mob := parse(envM)
	// 第二次 PC 登录（顶替第一次）
	_, env2 := doLogin(t, apiH, `{"username":"admin","password":"admin123"}`)
	pc2 := parse(env2)

	h := withAuth(jm, st, func(w http.ResponseWriter, r *http.Request) { OK(w, nil) })
	call := func(tok string) (int, envelope) {
		req := httptest.NewRequest(http.MethodGet, "/api/me", nil)
		req.Header.Set("Authorization", "Bearer "+tok)
		rec := httptest.NewRecorder()
		h(rec, req)
		var env envelope
		_ = json.Unmarshal(rec.Body.Bytes(), &env)
		return rec.Code, env
	}

	// 场景①：旧 PC 令牌被新 PC 登录顶替 → 401 业务码 4016
	code, env := call(pc1.Access)
	if code != http.StatusUnauthorized || env.Code != SessionKickedCode {
		t.Fatalf("旧 PC 会话应 401/%d，got %d/%+v", SessionKickedCode, code, env)
	}
	// 场景②：新 PC 令牌与移动端令牌均有效
	if code, _ := call(pc2.Access); code != http.StatusOK {
		t.Fatalf("新 PC 会话应 200，got %d", code)
	}
	if code, _ := call(mob.Access); code != http.StatusOK {
		t.Fatalf("移动端会话应 200，got %d", code)
	}
	// 场景③：被顶替端的刷新令牌同样失效
	srv := httptest.NewServer(http.HandlerFunc(apiH.Refresh))
	defer srv.Close()
	resp, err := http.Post(srv.URL, "application/json",
		strings.NewReader(`{"refresh_token":"`+pc1.Refresh+`"}`))
	if err != nil {
		t.Fatalf("刷新请求失败: %v", err)
	}
	defer resp.Body.Close()
	var envR envelope
	_ = json.NewDecoder(resp.Body).Decode(&envR)
	if resp.StatusCode != http.StatusUnauthorized || envR.Code != SessionKickedCode {
		t.Fatalf("被顶替端刷新应 401/%d，got %d/%+v", SessionKickedCode, resp.StatusCode, envR)
	}
	// 被踢事件应写审计日志
	found := false
	for _, a := range st.audits {
		if strings.Contains(a.Detail, "会话被顶替") {
			found = true
		}
	}
	if !found {
		t.Fatalf("被踢应写审计日志，got %+v", st.audits)
	}
}
