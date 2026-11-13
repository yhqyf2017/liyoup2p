package store

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"

	"liyou-platform/internal/auth"
)

// Store 数据库访问封装。
type Store struct {
	DB *sql.DB
}

// Open 打开 PostgreSQL 连接并 Ping 验证。
func Open(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("打开数据库失败: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(5)
	if err := db.Ping(); err != nil {
		db.Close()
		return nil, fmt.Errorf("数据库连接失败: %w", err)
	}
	return &Store{DB: db}, nil
}

var schema = []string{
	`CREATE TABLE IF NOT EXISTS plans (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL UNIQUE,
		max_devices INT NOT NULL DEFAULT 5,
		max_ports INT NOT NULL DEFAULT 3,
		monthly_traffic_gb INT NOT NULL DEFAULT 10,
		price_cents INT NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE TABLE IF NOT EXISTS tenants (
		id BIGSERIAL PRIMARY KEY,
		name TEXT NOT NULL,
		code TEXT NOT NULL UNIQUE,
		plan_id BIGINT REFERENCES plans(id),
		status TEXT NOT NULL DEFAULT 'active',
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE TABLE IF NOT EXISTS users (
		id BIGSERIAL PRIMARY KEY,
		tenant_id BIGINT REFERENCES tenants(id) ON DELETE CASCADE,
		username TEXT NOT NULL UNIQUE,
		password_hash TEXT NOT NULL,
		role TEXT NOT NULL DEFAULT 'operator',
		status TEXT NOT NULL DEFAULT 'active',
		token_seq_pc BIGINT NOT NULL DEFAULT 0,
		token_seq_mobile BIGINT NOT NULL DEFAULT 0,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		last_login_at TIMESTAMPTZ
	)`,
	// 旧库兼容：为已存在的 users 表补会话版本列（幂等）
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS token_seq_pc BIGINT NOT NULL DEFAULT 0`,
	`ALTER TABLE users ADD COLUMN IF NOT EXISTS token_seq_mobile BIGINT NOT NULL DEFAULT 0`,
	`CREATE TABLE IF NOT EXISTS devices (
		id BIGSERIAL PRIMARY KEY,
		tenant_id BIGINT NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
		device_uid TEXT NOT NULL UNIQUE,
		name TEXT NOT NULL DEFAULT '',
		role TEXT NOT NULL DEFAULT 'client',
		token_hash TEXT NOT NULL DEFAULT '',
		status TEXT NOT NULL DEFAULT 'offline',
		os_info TEXT NOT NULL DEFAULT '',
		last_online_at TIMESTAMPTZ,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	// 旧库兼容：为已存在的 devices 表补归属账号列（幂等）
	`ALTER TABLE devices ADD COLUMN IF NOT EXISTS owner TEXT NOT NULL DEFAULT ''`,
	`CREATE TABLE IF NOT EXISTS device_ports (
		id BIGSERIAL PRIMARY KEY,
		device_id BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
		port INT NOT NULL,
		label TEXT NOT NULL DEFAULT '',
		declared BOOLEAN NOT NULL DEFAULT TRUE,
		UNIQUE(device_id, port)
	)`,
	`CREATE TABLE IF NOT EXISTS tunnel_sessions (
		id BIGSERIAL PRIMARY KEY,
		session_id TEXT NOT NULL UNIQUE,
		tenant_id BIGINT NOT NULL,
		client_device_id BIGINT,
		server_device_id BIGINT,
		port INT NOT NULL DEFAULT 0,
		mode TEXT NOT NULL DEFAULT 'p2p',
		status TEXT NOT NULL DEFAULT 'active',
		relay_bytes BIGINT NOT NULL DEFAULT 0,
		started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
		ended_at TIMESTAMPTZ
	)`,
	`CREATE TABLE IF NOT EXISTS audit_logs (
		id BIGSERIAL PRIMARY KEY,
		tenant_id BIGINT,
		actor TEXT NOT NULL DEFAULT '',
		action TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT '',
		client_ip INET,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE TABLE IF NOT EXISTS alerts (
		id BIGSERIAL PRIMARY KEY,
		tenant_id BIGINT,
		level TEXT NOT NULL DEFAULT 'info',
		title TEXT NOT NULL,
		detail TEXT NOT NULL DEFAULT '',
		is_read BOOLEAN NOT NULL DEFAULT FALSE,
		created_at TIMESTAMPTZ NOT NULL DEFAULT now()
	)`,
	`CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_logs(created_at)`,
	`CREATE INDEX IF NOT EXISTS idx_sessions_started ON tunnel_sessions(started_at)`,
	`CREATE INDEX IF NOT EXISTS idx_devices_tenant ON devices(tenant_id)`,
}

// Migrate 幂等建表。
func (s *Store) Migrate(ctx context.Context) error {
	for _, stmt := range schema {
		if _, err := s.DB.ExecContext(ctx, stmt); err != nil {
			return fmt.Errorf("迁移失败: %w (SQL: %.80s)", err, stmt)
		}
	}
	return nil
}

// Seed 幂等写入套餐 / 演示租户 / 种子账号。
func (s *Store) Seed(ctx context.Context, adminHash, superHash string) error {
	seeds := []struct {
		name                     string
		maxDevices, maxPorts, gb int
	}{
		{"基础版", 5, 3, 10},
		{"专业版", 20, 10, 50},
		{"旗舰版", 100, 50, 500},
	}
	for _, p := range seeds {
		if _, err := s.DB.ExecContext(ctx, `INSERT INTO plans (name, max_devices, max_ports, monthly_traffic_gb)
			VALUES ($1,$2,$3,$4) ON CONFLICT (name) DO NOTHING`, p.name, p.maxDevices, p.maxPorts, p.gb); err != nil {
			return fmt.Errorf("种子套餐写入失败: %w", err)
		}
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO tenants (name, code, plan_id)
		SELECT '武汉理游商贸', 'liyou', id FROM plans WHERE name='专业版'
		ON CONFLICT (code) DO NOTHING`); err != nil {
		return fmt.Errorf("种子租户写入失败: %w", err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO users (tenant_id, username, password_hash, role)
		SELECT t.id, 'admin', $1, 'tenant_admin' FROM tenants t WHERE t.code='liyou'
		ON CONFLICT (username) DO NOTHING`, adminHash); err != nil {
		return fmt.Errorf("种子用户 admin 写入失败: %w", err)
	}
	if _, err := s.DB.ExecContext(ctx, `INSERT INTO users (tenant_id, username, password_hash, role)
		VALUES (NULL, 'superadmin', $1, 'super_admin')
		ON CONFLICT (username) DO NOTHING`, superHash); err != nil {
		return fmt.Errorf("种子用户 superadmin 写入失败: %w", err)
	}
	return nil
}

// ReplaceDefaultPasswords 将仍使用历史默认密码（admin123）的种子账号重置为环境变量
// 指定的新密码；已是自定义密码的账号不动（重启不覆盖运营期修改）。返回重置的账号数。
func (s *Store) ReplaceDefaultPasswords(ctx context.Context, adminHash, superHash string) (int, error) {
	seeds := []struct {
		username string
		newHash  string
	}{
		{"admin", adminHash},
		{"superadmin", superHash},
	}
	reset := 0
	for _, a := range seeds {
		var cur string
		err := s.DB.QueryRowContext(ctx, `SELECT password_hash FROM users WHERE username=$1`, a.username).Scan(&cur)
		if err != nil {
			if noRows(err) {
				continue
			}
			return reset, err
		}
		if auth.VerifyPassword(cur, "admin123") != nil {
			continue // 已不是默认密码，不覆盖运营期修改
		}
		if _, err := s.DB.ExecContext(ctx, `UPDATE users SET password_hash=$1 WHERE username=$2`, a.newHash, a.username); err != nil {
			return reset, err
		}
		reset++
	}
	return reset, nil
}

func tenantArg(tenantID int64) any {
	if tenantID > 0 {
		return tenantID
	}
	return nil
}

func noRows(err error) bool { return errors.Is(err, sql.ErrNoRows) }

// ---------- 用户 ----------

// FindUserByUsername 按用户名查用户，未找到返回 (nil, nil)。
func (s *Store) FindUserByUsername(ctx context.Context, username string) (*User, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT id, tenant_id, username, password_hash, role, status, created_at, last_login_at
		FROM users WHERE username=$1`, username)
	var u User
	if err := row.Scan(&u.ID, &u.TenantID, &u.Username, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt, &u.LastLoginAt); err != nil {
		if noRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// FindTenantByCode 按租户编码查询。
func (s *Store) FindTenantByCode(ctx context.Context, code string) (*Tenant, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT id, name, code, plan_id, status, created_at FROM tenants WHERE code=$1`, code)
	var t Tenant
	if err := row.Scan(&t.ID, &t.Name, &t.Code, &t.PlanID, &t.Status, &t.CreatedAt); err != nil {
		if noRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// FindTenantByID 按 ID 查询租户。
func (s *Store) FindTenantByID(ctx context.Context, id int64) (*Tenant, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT id, name, code, plan_id, status, created_at FROM tenants WHERE id=$1`, id)
	var t Tenant
	if err := row.Scan(&t.ID, &t.Name, &t.Code, &t.PlanID, &t.Status, &t.CreatedAt); err != nil {
		if noRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &t, nil
}

// TouchLogin 更新最后登录时间。
func (s *Store) TouchLogin(ctx context.Context, userID int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET last_login_at=now() WHERE id=$1`, userID)
	return err
}

// seqColumn 端类型 → 会话版本列名（白名单映射，避免拼接注入）。
func seqColumn(clientType string) string {
	if clientType == "mobile" {
		return "token_seq_mobile"
	}
	return "token_seq_pc"
}

// BumpSessionSeq 自增指定端类型的会话版本号并返回新值（新登录使该端旧令牌全部失效）。
func (s *Store) BumpSessionSeq(ctx context.Context, userID int64, clientType string) (int64, error) {
	col := seqColumn(clientType)
	var seq int64
	err := s.DB.QueryRowContext(ctx,
		fmt.Sprintf(`UPDATE users SET %s=%s+1 WHERE id=$1 RETURNING %s`, col, col, col),
		userID).Scan(&seq)
	return seq, err
}

// GetSessionSeq 查询指定端类型当前会话版本号；用户不存在返回 sql.ErrNoRows。
func (s *Store) GetSessionSeq(ctx context.Context, userID int64, clientType string) (int64, error) {
	var seq int64
	err := s.DB.QueryRowContext(ctx,
		fmt.Sprintf(`SELECT %s FROM users WHERE id=$1`, seqColumn(clientType)),
		userID).Scan(&seq)
	return seq, err
}

// ListUsers 用户列表，tenantID<=0 表示全部（超管）。
func (s *Store) ListUsers(ctx context.Context, tenantID int64) ([]UserView, error) {
	q := `SELECT u.id, u.tenant_id, COALESCE(t.name,''), u.username, u.role, u.status, u.created_at, u.last_login_at
		FROM users u LEFT JOIN tenants t ON t.id=u.tenant_id
		WHERE ($1::bigint IS NULL OR u.tenant_id=$1) ORDER BY u.id`
	rows, err := s.DB.QueryContext(ctx, q, tenantArg(tenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserView
	for rows.Next() {
		var v UserView
		if err := rows.Scan(&v.ID, &v.TenantID, &v.TenantName, &v.Username, &v.Role, &v.Status, &v.CreatedAt, &v.LastLoginAt); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CreateUser 新建用户。
func (s *Store) CreateUser(ctx context.Context, tenantID sql.NullInt64, username, passwordHash, role string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO users (tenant_id, username, password_hash, role)
		VALUES ($1,$2,$3,$4) RETURNING id`, tenantID, username, passwordHash, role).Scan(&id)
	return id, err
}

// UpdateUser 更新角色/状态（指针为 nil 表示不更新）。
func (s *Store) UpdateUser(ctx context.Context, id int64, role, status *string) error {
	if role != nil {
		if _, err := s.DB.ExecContext(ctx, `UPDATE users SET role=$2 WHERE id=$1`, id, *role); err != nil {
			return err
		}
	}
	if status != nil {
		if _, err := s.DB.ExecContext(ctx, `UPDATE users SET status=$2 WHERE id=$1`, id, *status); err != nil {
			return err
		}
	}
	return nil
}

// DeleteUser 删除用户。
func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM users WHERE id=$1`, id)
	return err
}

// ResetPassword 重置密码。
func (s *Store) ResetPassword(ctx context.Context, id int64, passwordHash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE users SET password_hash=$2 WHERE id=$1`, id, passwordHash)
	return err
}

// GetUserByID 按 ID 查用户。
func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT id, tenant_id, username, password_hash, role, status, created_at, last_login_at
		FROM users WHERE id=$1`, id)
	var u User
	if err := row.Scan(&u.ID, &u.TenantID, &u.Username, &u.PasswordHash, &u.Role, &u.Status, &u.CreatedAt, &u.LastLoginAt); err != nil {
		if noRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &u, nil
}

// ---------- 租户 ----------

// ListTenants 租户列表（仅超管）。
func (s *Store) ListTenants(ctx context.Context) ([]map[string]any, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT t.id, t.name, t.code, t.plan_id, COALESCE(p.name,''), t.status, t.created_at,
		(SELECT count(*) FROM devices d WHERE d.tenant_id=t.id),
		(SELECT count(*) FROM users u WHERE u.tenant_id=t.id)
		FROM tenants t LEFT JOIN plans p ON p.id=t.plan_id ORDER BY t.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var id, planID sql.NullInt64
		var name, code, planName, status string
		var createdAt time.Time
		var devCount, userCount int64
		if err := rows.Scan(&id, &name, &code, &planID, &planName, &status, &createdAt, &devCount, &userCount); err != nil {
			return nil, err
		}
		out = append(out, map[string]any{
			"id": id.Int64, "name": name, "code": code, "device_count": devCount, "user_count": userCount,
			"plan_id": planID.Int64, "plan_name": planName, "status": status, "created_at": createdAt,
		})
	}
	return out, rows.Err()
}

// CreateTenant 新建租户。
func (s *Store) CreateTenant(ctx context.Context, name, code string, planID sql.NullInt64) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO tenants (name, code, plan_id) VALUES ($1,$2,$3) RETURNING id`,
		name, code, planID).Scan(&id)
	return id, err
}

// UpdateTenant 更新租户。
func (s *Store) UpdateTenant(ctx context.Context, id int64, name, status *string, planID *int64) error {
	if name != nil {
		if _, err := s.DB.ExecContext(ctx, `UPDATE tenants SET name=$2 WHERE id=$1`, id, *name); err != nil {
			return err
		}
	}
	if status != nil {
		if _, err := s.DB.ExecContext(ctx, `UPDATE tenants SET status=$2 WHERE id=$1`, id, *status); err != nil {
			return err
		}
	}
	if planID != nil {
		if _, err := s.DB.ExecContext(ctx, `UPDATE tenants SET plan_id=$2 WHERE id=$1`, id, *planID); err != nil {
			return err
		}
	}
	return nil
}

// DeleteTenant 删除租户（级联删除其用户/设备）。
func (s *Store) DeleteTenant(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `DELETE FROM tenants WHERE id=$1`, id)
	return err
}

// ---------- 设备 ----------

// FindDeviceByUID 按设备 UID 查询。
func (s *Store) FindDeviceByUID(ctx context.Context, uid string) (*Device, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT id, tenant_id, device_uid, name, role, token_hash, status, owner, os_info, last_online_at, created_at
		FROM devices WHERE device_uid=$1`, uid)
	return scanDevice(row)
}

// FindDeviceByID 按 ID 查询设备。
func (s *Store) FindDeviceByID(ctx context.Context, id int64) (*Device, error) {
	row := s.DB.QueryRowContext(ctx, `SELECT id, tenant_id, device_uid, name, role, token_hash, status, owner, os_info, last_online_at, created_at
		FROM devices WHERE id=$1`, id)
	return scanDevice(row)
}

func scanDevice(row *sql.Row) (*Device, error) {
	var d Device
	if err := row.Scan(&d.ID, &d.TenantID, &d.DeviceUID, &d.Name, &d.Role, &d.TokenHash, &d.Status, &d.Owner, &d.OSInfo, &d.LastOnlineAt, &d.CreatedAt); err != nil {
		if noRows(err) {
			return nil, nil
		}
		return nil, err
	}
	return &d, nil
}

// ListDevices 设备列表（含端口数），tenantID<=0 表示全部。
func (s *Store) ListDevices(ctx context.Context, tenantID int64, role, status string) ([]DeviceView, error) {
	q := `SELECT d.id, d.tenant_id, d.device_uid, d.name, d.role, d.status, d.owner, d.os_info, d.last_online_at, d.created_at,
		(SELECT count(*) FROM device_ports p WHERE p.device_id=d.id AND p.declared), COALESCE(t.name,'')
		FROM devices d LEFT JOIN tenants t ON t.id=d.tenant_id WHERE ($1::bigint IS NULL OR d.tenant_id=$1)`
	args := []any{tenantArg(tenantID)}
	if role != "" {
		args = append(args, role)
		q += fmt.Sprintf(` AND d.role=$%d`, len(args))
	}
	if status != "" {
		args = append(args, status)
		q += fmt.Sprintf(` AND d.status=$%d`, len(args))
	}
	q += ` ORDER BY d.id DESC`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []DeviceView
	for rows.Next() {
		var v DeviceView
		if err := rows.Scan(&v.ID, &v.TenantID, &v.DeviceUID, &v.Name, &v.Role, &v.Status, &v.Owner, &v.OSInfo,
			&v.LastOnlineAt, &v.CreatedAt, &v.PortCount, &v.TenantName); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// CreateDevice 新建设备（owner 为归属账号）。
func (s *Store) CreateDevice(ctx context.Context, tenantID int64, uid, name, role, tokenHash, owner string) (int64, error) {
	var id int64
	err := s.DB.QueryRowContext(ctx, `INSERT INTO devices (tenant_id, device_uid, name, role, token_hash, owner)
		VALUES ($1,$2,$3,$4,$5,$6) RETURNING id`, tenantID, uid, name, role, tokenHash, owner).Scan(&id)
	return id, err
}

// SetDeviceOSInfo 更新设备操作系统信息（agent 连接时上报）。
func (s *Store) SetDeviceOSInfo(ctx context.Context, id int64, osInfo string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE devices SET os_info=$2 WHERE id=$1`, id, osInfo)
	return err
}

// SetDeviceOwner 更新设备归属账号（同 UID 被其他账号重新注册时跟随最新账号）。
func (s *Store) SetDeviceOwner(ctx context.Context, id int64, owner string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE devices SET owner=$2 WHERE id=$1`, id, owner)
	return err
}

// SetDeviceToken 更新设备令牌哈希。
func (s *Store) SetDeviceToken(ctx context.Context, id int64, tokenHash string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE devices SET token_hash=$2 WHERE id=$1`, id, tokenHash)
	return err
}

// RevokeDevice 吊销设备。
func (s *Store) RevokeDevice(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE devices SET status='revoked' WHERE id=$1`, id)
	return err
}

// SetDeviceOnline 设备上线。
func (s *Store) SetDeviceOnline(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE devices SET status='online', last_online_at=now() WHERE id=$1`, id)
	return err
}

// SetDeviceOffline 设备下线。
func (s *Store) SetDeviceOffline(ctx context.Context, id int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE devices SET status='offline' WHERE id=$1`, id)
	return err
}

// OnlineServers 租户内在线的服务端设备（最近活跃优先：客户端未指定目标时，
// 自动路由应选中用户正在 GUI 管理的那台实例，而非最早注册的历史设备记录）。
func (s *Store) OnlineServers(ctx context.Context, tenantID int64) ([]Device, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT id, tenant_id, device_uid, name, role, token_hash, status, owner, os_info, last_online_at, created_at
		FROM devices WHERE tenant_id=$1 AND role='server' AND status='online' ORDER BY last_online_at DESC NULLS LAST, id DESC LIMIT 50`, tenantID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Device
	for rows.Next() {
		var d Device
		if err := rows.Scan(&d.ID, &d.TenantID, &d.DeviceUID, &d.Name, &d.Role, &d.TokenHash, &d.Status, &d.Owner, &d.OSInfo, &d.LastOnlineAt, &d.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// IsPortDeclared 校验端口是否已在设备上申报。
func (s *Store) IsPortDeclared(ctx context.Context, deviceID int64, port int) (bool, error) {
	var ok bool
	err := s.DB.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM device_ports WHERE device_id=$1 AND port=$2 AND declared)`,
		deviceID, port).Scan(&ok)
	return ok, err
}

// ReplaceDevicePorts 全量替换设备端口申报，并更新系统信息。
func (s *Store) ReplaceDevicePorts(ctx context.Context, deviceID int64, ports []PortInput, osInfo string) error {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `DELETE FROM device_ports WHERE device_id=$1`, deviceID); err != nil {
		return err
	}
	for _, p := range ports {
		if _, err := tx.ExecContext(ctx, `INSERT INTO device_ports (device_id, port, label, declared) VALUES ($1,$2,$3,$4)`,
			deviceID, p.Port, p.Label, p.Declared); err != nil {
			return err
		}
	}
	if _, err := tx.ExecContext(ctx, `UPDATE devices SET os_info=$2, last_online_at=now() WHERE id=$1`, deviceID, osInfo); err != nil {
		return err
	}
	return tx.Commit()
}

// ---------- 会话 ----------

// CreateSession 创建隧道会话记录。
func (s *Store) CreateSession(ctx context.Context, sessionID string, tenantID int64, clientID, serverID int64, port int, mode string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO tunnel_sessions (session_id, tenant_id, client_device_id, server_device_id, port, mode)
		VALUES ($1,$2,$3,$4,$5,$6)`, sessionID, tenantID, clientID, serverID, port, mode)
	return err
}

// SetSessionMode 更新会话模式（p2p 失败回落中继时调用）。
func (s *Store) SetSessionMode(ctx context.Context, sessionID, mode string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE tunnel_sessions SET mode=$2 WHERE session_id=$1`, sessionID, mode)
	return err
}

// EndSession 结束会话。
func (s *Store) EndSession(ctx context.Context, sessionID string) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE tunnel_sessions SET status='ended', ended_at=now()
		WHERE session_id=$1 AND status='active'`, sessionID)
	return err
}

// AddRelayBytes 累加中继流量。
func (s *Store) AddRelayBytes(ctx context.Context, sessionID string, n int64) error {
	_, err := s.DB.ExecContext(ctx, `UPDATE tunnel_sessions SET relay_bytes = relay_bytes + $2 WHERE session_id=$1`, sessionID, n)
	return err
}

// ListSessions 会话列表（含租户名与两端设备名/归属账号）。
func (s *Store) ListSessions(ctx context.Context, tenantID int64, status string) ([]SessionView, error) {
	q := `SELECT s.id, s.session_id, s.tenant_id, s.client_device_id, s.server_device_id,
		COALESCE(cd.device_uid,''), COALESCE(sd.device_uid,''), s.port, s.mode, s.status, s.relay_bytes, s.started_at, s.ended_at,
		COALESCE(t.name,''), COALESCE(cd.name,''), COALESCE(sd.name,''), COALESCE(cd.owner,''), COALESCE(sd.owner,'')
		FROM tunnel_sessions s
		LEFT JOIN devices cd ON cd.id=s.client_device_id
		LEFT JOIN devices sd ON sd.id=s.server_device_id
		LEFT JOIN tenants t ON t.id=s.tenant_id
		WHERE ($1::bigint IS NULL OR s.tenant_id=$1)`
	args := []any{tenantArg(tenantID)}
	if status != "" {
		args = append(args, status)
		q += fmt.Sprintf(` AND s.status=$%d`, len(args))
	}
	q += ` ORDER BY s.started_at DESC LIMIT 200`
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []SessionView
	for rows.Next() {
		var v SessionView
		if err := rows.Scan(&v.ID, &v.SessionID, &v.TenantID, &v.ClientDeviceID, &v.ServerDeviceID,
			&v.ClientUID, &v.ServerUID, &v.Port, &v.Mode, &v.Status, &v.RelayBytes, &v.StartedAt, &v.EndedAt,
			&v.TenantName, &v.ClientName, &v.ServerName, &v.ClientOwner, &v.ServerOwner); err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, rows.Err()
}

// ---------- 审计 / 告警 / 统计 ----------

// InsertAudit 写审计日志。
func (s *Store) InsertAudit(ctx context.Context, e AuditEntry) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO audit_logs (tenant_id, actor, action, detail, client_ip)
		VALUES ($1,$2,$3,$4, NULLIF($5,'')::inet)`, e.TenantID, e.Actor, e.Action, e.Detail, e.ClientIP)
	return err
}

// ListAudit 分页查询审计日志。
func (s *Store) ListAudit(ctx context.Context, tenantID int64, action string, page, size int) ([]AuditView, int64, error) {
	where := ` WHERE ($1::bigint IS NULL OR tenant_id=$1)`
	args := []any{tenantArg(tenantID)}
	if action != "" {
		args = append(args, action)
		where += fmt.Sprintf(` AND action=$%d`, len(args))
	}
	var total int64
	if err := s.DB.QueryRowContext(ctx, `SELECT count(*) FROM audit_logs`+where, args...).Scan(&total); err != nil {
		return nil, 0, err
	}
	if page < 1 {
		page = 1
	}
	if size < 1 || size > 200 {
		size = 20
	}
	q := `SELECT a.id, a.tenant_id, COALESCE(t.name,''), a.actor, a.action, a.detail, a.client_ip, a.created_at
		FROM audit_logs a LEFT JOIN tenants t ON t.id=a.tenant_id` +
		where + fmt.Sprintf(` ORDER BY a.id DESC LIMIT %d OFFSET %d`, size, (page-1)*size)
	rows, err := s.DB.QueryContext(ctx, q, args...)
	if err != nil {
		return nil, 0, err
	}
	defer rows.Close()
	var out []AuditView
	for rows.Next() {
		var v AuditView
		if err := rows.Scan(&v.ID, &v.TenantID, &v.TenantName, &v.Actor, &v.Action, &v.Detail, &v.ClientIP, &v.CreatedAt); err != nil {
			return nil, 0, err
		}
		out = append(out, v)
	}
	return out, total, rows.Err()
}

// InsertAlert 写告警。
func (s *Store) InsertAlert(ctx context.Context, tenantID sql.NullInt64, level, title, detail string) error {
	_, err := s.DB.ExecContext(ctx, `INSERT INTO alerts (tenant_id, level, title, detail) VALUES ($1,$2,$3,$4)`,
		tenantID, level, title, detail)
	return err
}

// ListAlerts 告警列表。
func (s *Store) ListAlerts(ctx context.Context, tenantID int64, unreadOnly bool) ([]Alert, error) {
	q := `SELECT al.id, al.tenant_id, COALESCE(t.name,''), al.level, al.title, al.detail, al.is_read, al.created_at
		FROM alerts al LEFT JOIN tenants t ON t.id=al.tenant_id WHERE ($1::bigint IS NULL OR al.tenant_id=$1)`
	if unreadOnly {
		q += ` AND is_read=FALSE`
	}
	q += ` ORDER BY id DESC LIMIT 100`
	rows, err := s.DB.QueryContext(ctx, q, tenantArg(tenantID))
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Alert
	for rows.Next() {
		var a Alert
		if err := rows.Scan(&a.ID, &a.TenantID, &a.TenantName, &a.Level, &a.Title, &a.Detail, &a.IsRead, &a.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, a)
	}
	return out, rows.Err()
}

// MarkAlertRead 标记告警已读。
func (s *Store) MarkAlertRead(ctx context.Context, id int64, tenantID int64) error {
	q := `UPDATE alerts SET is_read=TRUE WHERE id=$1`
	args := []any{id}
	if tenantID > 0 {
		q += ` AND tenant_id=$2`
		args = append(args, tenantID)
	}
	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}

// MarkAllAlertsRead 全部已读。
func (s *Store) MarkAllAlertsRead(ctx context.Context, tenantID int64) error {
	q := `UPDATE alerts SET is_read=TRUE WHERE is_read=FALSE`
	var args []any
	if tenantID > 0 {
		q += ` AND tenant_id=$1`
		args = append(args, tenantID)
	}
	_, err := s.DB.ExecContext(ctx, q, args...)
	return err
}

// Stats 平台统计。
func (s *Store) Stats(ctx context.Context, tenantID int64) (Stats, error) {
	var st Stats
	q := `SELECT
		(SELECT count(*) FROM devices WHERE status='online' AND ($1::bigint IS NULL OR tenant_id=$1)),
		(SELECT count(*) FROM devices WHERE status<>'revoked' AND ($1::bigint IS NULL OR tenant_id=$1)),
		(SELECT count(*) FROM audit_logs WHERE action='connect' AND created_at>=date_trunc('day', now()) AND ($1::bigint IS NULL OR tenant_id=$1)),
		(SELECT COALESCE(SUM(relay_bytes),0) FROM tunnel_sessions WHERE started_at>=date_trunc('day', now()) AND ($1::bigint IS NULL OR tenant_id=$1)),
		(SELECT count(*) FROM tunnel_sessions WHERE started_at>=date_trunc('day', now()) AND ($1::bigint IS NULL OR tenant_id=$1)),
		(SELECT count(*) FROM tunnel_sessions WHERE started_at>=date_trunc('day', now()) AND mode='p2p' AND ($1::bigint IS NULL OR tenant_id=$1))`
	err := s.DB.QueryRowContext(ctx, q, tenantArg(tenantID)).Scan(
		&st.OnlineDevices, &st.TotalDevices, &st.TodayRequests, &st.RelayBytes, &st.TodaySessions, &st.P2PSessions)
	return st, err
}
