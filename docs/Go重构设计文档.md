# LiYou P2P 安全穿透云 · Go 企业级重构设计文档

> 版本 v1.1 · 2026-11-05
> 结论先行：平台 + 服务端 Agent + 客户端 Agent 全部使用 **Go**，数据库使用 **PostgreSQL**，GUI 一期保留 C# WPF。
> 视觉规范：全产品线基于「励友软件」品牌色调（见第 11 章）。

---

## 1. 背景与目标

当前系统为演示版：Node.js 平台 + Node.js Agent + C# WPF GUI + 静态 Web 控制台，无账号体系、无持久化、无租户隔离。

本次重构目标：

1. **企业级平台**：登录认证、RBAC 权限、多租户隔离、审计落库、套餐计费
2. **Agent 双端合一**：服务端（装在业务服务器）与客户端（装在员工电脑）合并为同一个 Go 二进制，按 `--role` 区分
3. **交互流程**：GUI 先选角色（业务服务器 / 员工电脑）→ 登录（租户 + 账号 + 密码）→ 平台下发设备令牌 → 进入工作状态
4. **零依赖分发**：平台、Agent 都是单个 exe，员工电脑无需安装任何运行时

## 2. 总体架构

```
┌──────────────────┐        ┌────────────────────────────────┐
│  员工电脑（客户端） │        │        Go 平台 (liyou-platform) │
│ ┌──────────────┐ │  wss   │ ┌────────────────────────────┐ │       ┌────────────┐
│ │ liyou-agent  │←────────→│ │ 信令网关 (WebSocket Hub)    │ │       │ PostgreSQL │
│ │ --role client│ │  信令   │ │ HTTP API（登录/管理/审计）   │←──────→│ 租户/账号   │
│ └──────┬───────┘ │        │ │ 打洞协调 (ICE-like)         │ │       │ 设备/会话   │
│        │ 浏览器   │        │ │ 中继兜底 (relay, 按需)       │ │       │ 审计/计费   │
│        ▼         │        │ └────────────────────────────┘ │       └────────────┘
│  127.0.0.1:18080 │        └───────────────▲────────────────┘
└──────────────────┘                        │ wss 信令
                                   ┌────────┴─────────┐
                                   │ 业务服务器（服务端）│
                                   │  liyou-agent      │
                                   │  --role server    │
                                   │  自动发现本机服务   │
                                   │  127.0.0.1:8080   │
                                   └──────────────────┘
         P2P 数据隧道：客户端 ↔ 服务端 点对点直达（UDP 打洞），不经过平台
```

核心原则：**平台只牵线不过数据**。信令（注册、地址交换、打洞协调）走 wss；业务流量走两端之间的直连 UDP 隧道；仅对称 NAT 打洞失败时走平台中继兜底。

## 3. 技术栈

| 层 | 选型 | 说明 |
|---|---|---|
| 平台语言 | Go 1.22+ | HTTP 框架 gin / chi，WebSocket gorilla/websocket 或 nhooyr |
| Agent 语言 | Go 1.22+ | 与平台同语言，双角色单二进制 |
| 数据库 | PostgreSQL 15+ | 驱动 pgx/v5，迁移工具 golang-migrate |
| 缓存/在线状态 | Redis（可选） | 设备在线表、限流；规模小时先内存实现 |
| 认证 | JWT (RS256) | 平台签发；Agent 设备令牌为长效 JWT |
| 密码哈希 | argon2id | golang.org/x/crypto/argon2 |
| GUI | C# WPF（保留） | 登录 + 选角色 + 启动/停止 Agent 进程 |
| 控制台前端 | 现有 console 静态页 | 对接 Go HTTP API |
| 部署 | Windows 服务 / Docker | 平台支持双形态；Agent 注册 Windows 服务 |

## 4. 平台设计（liyou-platform）

### 4.1 模块划分

```
liyou-platform/
├── cmd/server/main.go        # 入口：加载配置、连接 DB、启动 HTTP+WS
├── internal/
│   ├── api/                  # REST API（gin）：auth、tenants、devices、ports、audit、plans
│   ├── hub/                  # WebSocket 信令网关：设备注册、消息路由、在线表
│   ├── punch/                # 打洞协调：交换双方候选地址、协调时序
│   ├── relay/                # 中继兜底：打洞失败时 TCP/UDP 转发（计流量）
│   ├── auth/                 # JWT 签发/校验、RBAC 中间件、argon2 密码
│   ├── tenant/               # 租户隔离：所有查询强制 tenant_id 过滤
│   ├── audit/                # 审计写入（异步批量落库）
│   └── store/                # PostgreSQL 数据访问层（pgx）
├── migrations/               # golang-migrate SQL
└── web/console/              # 静态控制台（沿用现有前端）
```

### 4.2 认证与权限

**登录流程（GUI 与控制台一致）：**

```
POST /api/auth/login  { tenant, username, password }
→ 校验 argon2id 哈希 → 签发 access_token (2h) + refresh_token (30d)
```

**Agent 登录（设备级）：**

```
GUI 登录成功 → POST /api/devices/register  { name, role, platform_version }
→ 平台建 device 记录（绑定 tenant）→ 返回 device_id + device_token（长效 JWT，可吊销）
→ Agent 后续所有 wss 连接带 device_token；密码不再出现在 Agent 参数中
```

**RBAC 角色：**

| 角色 | 能力 |
|---|---|
| super_admin | 平台方：管理所有租户、套餐、全局配置 |
| tenant_admin | 租户管理员：本租户设备/端口/成员/审计全部权限 |
| operator | 运维：设备与端口管理，不可管理成员与计费 |
| auditor | 只读：审计日志、监控告警查看 |

中间件按 `resource → action` 校验，且**所有查询强制注入 tenant_id**（super_admin 除外）。

### 4.3 信令协议（WebSocket，JSON）

沿用现有协议并扩展鉴权字段：

| 消息类型 | 方向 | 说明 |
|---|---|---|
| `auth` | A→P | 首条消息，携带 device_token，失败即断开 |
| `heartbeat` | A→P | 10s 心跳，超时 30s 判离线 |
| `declare_ports` | S→P | 服务端声明端口列表（自动发现结果） |
| `ports_updated` | P→C | 服务端端口变化推送给同租户客户端 |
| `session_request` | C→P | 客户端请求连接某服务端端口 |
| `punch_offer` / `punch_answer` | 双向 | 交换 UDP 候选地址（ip:port 列表） |
| `punch_result` | C→P | 打洞结果上报；失败则平台分配中继 |
| `relay_grant` | P→C/S | 下发中继通道凭据（限时一次性） |
| `session_close` | 双向 | 会话结束，审计落库 |

### 4.4 打洞与隧道（Go 实现）

- 采用 ICE 思路简化版：双方向平台汇报自己的公网映射地址（通过平台 UDP 探测获得）+ 本机局域网地址，平台撮合后两端同时向对方发包打洞
- 服务端 Agent 在收到 `session_request` 后才接受指定客户端的入站 UDP（最小暴露：无会话授权时端口对外不响应）
- 打洞失败（双对称 NAT）→ 平台中继兜底：`relay/` 模块建立 UDP 转发，计量流量写入 `tunnel_sessions.relay_bytes`
- 数据面隧道加密：隧道外层用平台下发的**会话对称密钥**（AES-GCM）封装，密钥每次会话随机生成、经信令通道（TLS 保护）分发

## 5. Agent 设计（liyou-agent，单 exe 双角色）

```
liyou-agent/
├── cmd/agent/main.go
├── internal/
│   ├── login/       # 设备注册/令牌管理（令牌存 %ProgramData%\LiYouP2P\token.json）
│   ├── discovery/   # 服务端：端口自动探测（8080 用友T+ / 80 / 443 / 8000 金蝶 / 8081 OA；
│   │                #   数据库 1433/3306/3389 只发现不开放，按需最小暴露）
│   ├── forward/     # UDP 隧道收发、AES-GCM 加解密、断线重连
│   ├── local/       # 客户端：本机 TCP 监听（18080）→ 隧道 → 服务端目标端口
│   ├── service/     # Windows 服务安装/卸载（golang.org/x/sys/windows/svc）
│   └── ui/          # 无 GUI，输出结构化日志（供 WPF GUI 读取）
└── flags            # --role server|client  --platform wss://...  --name ... --port ... --local ...
```

**运行形态：**
- 服务端：`liyou-agent.exe --role server --name 财务部-T+服务器`（无需端口参数，自动发现）→ 可注册为 Windows 服务
- 客户端：`liyou-agent.exe --role client --port 8080 --local 18080` → 本机监听，浏览器访问 `http://127.0.0.1:18080`
- 令牌优先：有 token.json 则静默重连，无需重复登录（员工电脑"记住登录"体验）

## 6. PostgreSQL 数据库设计

```sql
-- 租户
CREATE TABLE tenants (
  id            BIGSERIAL PRIMARY KEY,
  name          VARCHAR(128) NOT NULL UNIQUE,      -- 租户标识（登录用）
  display_name  VARCHAR(128) NOT NULL,
  plan_id       BIGINT REFERENCES plans(id),
  status        VARCHAR(16) NOT NULL DEFAULT 'active',  -- active/suspended/expired
  expires_at    TIMESTAMPTZ,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- 套餐
CREATE TABLE plans (
  id            BIGSERIAL PRIMARY KEY,
  name          VARCHAR(64) NOT NULL,              -- 基础版/专业版/旗舰版
  max_devices   INT NOT NULL,
  max_ports     INT NOT NULL,
  relay_quota_gb INT NOT NULL DEFAULT 0,           -- 中继流量配额
  price_month   NUMERIC(10,2)
);

-- 用户（控制台与 GUI 登录共用）
CREATE TABLE users (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     BIGINT REFERENCES tenants(id),     -- super_admin 为 NULL
  username      VARCHAR(64) NOT NULL,
  password_hash TEXT NOT NULL,                     -- argon2id
  role          VARCHAR(16) NOT NULL,              -- super_admin/tenant_admin/operator/auditor
  status        VARCHAR(16) NOT NULL DEFAULT 'active',
  last_login_at TIMESTAMPTZ,
  UNIQUE (tenant_id, username)
);

-- 设备
CREATE TABLE devices (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     BIGINT NOT NULL REFERENCES tenants(id),
  device_uid    VARCHAR(16) NOT NULL UNIQUE,       -- DV-5EF640 展示用
  token_hash    TEXT NOT NULL,                     -- 设备令牌哈希（可吊销）
  name          VARCHAR(128) NOT NULL,
  role          VARCHAR(8)  NOT NULL,              -- server/client
  version       VARCHAR(16),
  os_info       VARCHAR(64),
  last_online_at TIMESTAMPTZ,
  status        VARCHAR(16) NOT NULL DEFAULT 'offline',
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON devices (tenant_id, status);

-- 端口声明（服务端）
CREATE TABLE device_ports (
  id            BIGSERIAL PRIMARY KEY,
  device_id     BIGINT NOT NULL REFERENCES devices(id) ON DELETE CASCADE,
  tenant_id     BIGINT NOT NULL REFERENCES tenants(id),
  port          INT NOT NULL,
  service_name  VARCHAR(64) NOT NULL,              -- 用友 T+（ERP）
  declared      BOOLEAN NOT NULL DEFAULT TRUE,     -- false=仅发现未开放
  updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
  UNIQUE (device_id, port)
);

-- 穿透会话（审计与计费数据源）
CREATE TABLE tunnel_sessions (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     BIGINT NOT NULL REFERENCES tenants(id),
  client_device_id BIGINT REFERENCES devices(id),
  server_device_id BIGINT REFERENCES devices(id),
  target_port   INT NOT NULL,
  mode          VARCHAR(8) NOT NULL,               -- p2p/relay
  relay_bytes   BIGINT NOT NULL DEFAULT 0,
  started_at    TIMESTAMPTZ NOT NULL,
  ended_at      TIMESTAMPTZ
);
CREATE INDEX ON tunnel_sessions (tenant_id, started_at);
-- 量大后按月分区：PARTITION BY RANGE (started_at)

-- 访问审计（客户端每次访问一行，异步批量写入）
CREATE TABLE audit_logs (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     BIGINT NOT NULL REFERENCES tenants(id),
  session_id    BIGINT REFERENCES tunnel_sessions(id),
  client_device_id BIGINT,
  server_device_id BIGINT,
  target_port   INT,
  action        VARCHAR(16) NOT NULL,              -- connect/close/blocked
  client_ip     INET,
  occurred_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);
CREATE INDEX ON audit_logs (tenant_id, occurred_at DESC);

-- 告警
CREATE TABLE alerts (
  id            BIGSERIAL PRIMARY KEY,
  tenant_id     BIGINT NOT NULL REFERENCES tenants(id),
  level         VARCHAR(8) NOT NULL,               -- info/warn/critical
  type          VARCHAR(32) NOT NULL,              -- device_offline/relay_over_quota/...
  message       TEXT NOT NULL,
  is_read       BOOLEAN NOT NULL DEFAULT FALSE,
  created_at    TIMESTAMPTZ NOT NULL DEFAULT now()
);
```

要点：除平台级表（plans）外全部携带 `tenant_id`，数据访问层统一注入；审计与会话表按月分区，心跳等高频状态只存内存/Redis，不进库。

## 7. GUI（C# WPF）改造点

现有浅色界面结构保留（第 1 步选角色 → 第 2 步连接设置），改造为三步流程：

```
第 1 步 选角色（现有两张卡片，保留）
第 2 步 登录（新增）：租户 + 账号 + 密码；平台地址由管理员预置在配置文件
        → 调 POST /api/auth/login → POST /api/devices/register → 保存 device_token
第 3 步 工作区：
        服务端 → 只显示"自动发现结果"列表（端口/服务名/开关），无端口输入
        客户端 → 目标端口 + 本机端口 + "打开服务器页面"
```

- GUI 不再传 node 命令，改为拉起 `liyou-agent.exe`（携带 token 与角色参数）
- 日志区读取 agent 的结构化输出（stdout JSON 行），渲染更友好
- 界面配色按第 11 章品牌规范执行；exe 内嵌 favicon.ico 图标（/win32icon）

## 8. 部署形态

| 组件 | 形态 |
|---|---|
| 平台 | `liyou-platform.exe` 注册为 Windows 服务；或 Docker（含 PostgreSQL） |
| 数据库 | PostgreSQL 15：Windows 安装包或容器；每日备份 |
| 反向代理 | 可选 Nginx/Caddy 终结 TLS（wss/https），正式环境必须 TLS |
| Agent | 单 exe 分发；服务端机器装为系统服务；员工电脑绿色运行 |
| 控制台 | 平台内置静态托管 `https://平台域名/` 直接访问 |

## 9. 安全设计清单

1. 传输：控制台 https；Agent 信令 wss；隧道数据 AES-GCM 会话密钥
2. 最小暴露：服务端端口仅对**已授权会话**响应，无请求不回包（对抗端口扫描）
3. 令牌：设备令牌可吊销、可轮换；refresh_token 检测异地异常
4. 租户隔离：数据层强制 tenant_id + 控制台按租户过滤；跨租户设备无法 session_request
5. 审计：所有访问行为落库不可删（auditor 可查不可改）
6. 中继配额：套餐 relay_quota_gb 超限告警/限速
7. 密码：argon2id；登录失败限流（Redis 计数）
8. 杀软友好：Go exe 带版本信息与代码签名（正式版建议购买证书签名）

## 10. 实施路线

**一期（业务闭环，Go 重写全部三端）**
1. 数据库建表 + Go 平台骨架（登录/JWT/RBAC/设备注册）
2. 信令网关 + 打洞协调 + P2P 隧道（Go Agent 双角色）
3. 中继兜底 + 审计落库
4. WPF GUI 三步流程改造（登录 + 工作区）
5. 控制台前端对接真实 API（设备/端口/审计页先接通）

**二期（运营增强）**
- 套餐计费、告警中心、监控图表真实数据
- 会话月度分区、Redis 在线表、集群化（多平台节点）
- Wails 单 exe 合并 GUI、macOS/Linux Agent

## 11. 品牌 UI 规范（取自励友软件 logo）

### 11.1 品牌色板

| Token | 值 | 来源与用途 |
|---|---|---|
| `--primary` | `#1667C8` | 品牌深蓝（logo 文字/盾形主色）：主按钮、链接、图表主色、选中态 |
| `--primary-deep` | `#0F51A8` | 深蓝加深：按钮渐变底、徽章文字 |
| `--sky` | `#2AA4E9` | 天空蓝（logo 翅膀高光）：渐变高光、激活指示条、进度条 |
| `--gold` | `#F5A623` | 星光金（logo 四角星）：Logo 星星、强调点缀、评分/星标 |
| `--bg` | `#F3F5F9` | 页面浅灰底 |
| `--surface` | `#FFFFFF` | 卡片白底 |
| `--line` | `#E3E8F0` | 分隔线 |
| `--t1 / --t2 / --t3` | `#16233C / #57677F / #8C99AE` | 主文字 / 次级 / 提示（正文对比度 ≥ 12:1） |

### 11.2 应用规则

- **渐变**：主按钮与 Logo 底用 `135deg，--sky → --primary-deep`，呼应 logo 蓝色渐变
- **点缀**：金色只做小面积点缀（Logo 星星、星标），不做大面积背景
- **深色场景**：登录页左右品牌区可用深蓝渐变（`#0A1A33 → #0C2144`）衬托金星
- **图标**：全产品统一"蓝渐变盾形 + 金色四角星" SVG；WPF 窗口 Logo 为蓝底金星 ✦；exe 与控制台 favicon 使用品牌 favicon.ico
- **字体**：优先 HarmonyOS Sans SC → Microsoft YaHei UI 回退；正文 13-14px，数字等宽
- **图表**：折线/环图主色 `--primary`，中继相关用紫 `#7052D6`，成功绿 `#0E9F6E`、警告橙 `#DC8A0E`、危险红 `#DE4657`

---

*本文档为 Go 重构的总体设计，后续按模块出详细 API 契约与信令协议文档。*
