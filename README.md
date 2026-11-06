# 励联 LiLink（LiYou P2P）

**零公网端口安全穿透平台** —— 面向企业防勒索场景的跨网络远程访问解决方案。

服务端机器**不暴露任何公网端口**，业务流量经 UDP 打洞在客户端与服务端之间直连传输（打洞失败时回落中继），平台仅负责信令撮合，不经手业务数据。

> 济源市励友软件有限公司

---

## 一、产品定位

| 传统内网穿透 | 励联 LiLink |
| --- | --- |
| 服务端映射端口暴露公网，勒索病毒/扫描器可直接触达 | 服务端零端口暴露，攻击面为零 |
| 业务流量经第三方服务器中转，带宽受限且隐私存疑 | P2P 直连为主（约 90% 场景），平台不经手流量 |
| 平台可解密全部流量 | 会话密钥端到端加密，AES-256-GCM |

典型场景：跨网络访问公司内网服务（Web、数据库、NAS、远程桌面），无需公网 IP、无需开放防火墙入站端口。

## 二、系统架构

```
┌──────────────┐         ┌──────────────────────────────┐
│  控制台       │  HTTP   │  协调平台 liyou-platform       │
│ console-web  ├────────►│  REST API / JWT / 租户隔离     │
│ (Vue 3)      │         │  信令 hub (WS)  :8900          │
└──────────────┘         │  TCP 中继       :8901          │
                         │  更新通道（PC/移动双通道）        │
┌──────────────┐         └───────┬──────────────────────┘
│  Windows GUI │    WS 信令/     │            PostgreSQL
│  LiYouP2P    ├──────登录注册───┤           （仅平台直连）
│  (Wails)     │                 │
└──────┬───────┘         ┌───────┴──────────┐
       │ 拉起/管理        │  Agent（同一 exe） │
       ▼                 │  --role client    │
┌──────────────┐         │  --role server    │
│ 移动端 Flutter├────────►└───────┬──────────┘
│ (gomobile 嵌入)│               │
└──────────────┘        ═══════╧═════════
                        P2P 数据面（UDP 直连优先，AES-256-GCM；
                        失败回落 TCP 中继，密文盲转发）
```

- **控制面**：客户端/服务端 Agent 经 WebSocket 与平台交换信令（注册、撮合、候选地址交换），设备令牌鉴权。
- **数据面**：平台核准会话并下发一次性会话密钥后，两端经 STUN + UDP 打洞直连；打洞失败回落中继（密文透传，平台无法解密）。
- **数据库**：PostgreSQL 仅平台直连；Agent 不访问数据库。

## 三、目录结构

```
liyoup2p/
├── platform-go/     Go 平台：REST API、JWT/RBAC、租户隔离、信令 hub、TCP 中继、更新通道
├── agent-go/        Go Agent：客户端/服务端双角色单 exe（--role server|client）
│                    STUN、UDP 打洞、reliable 可靠传输层、AES-256-GCM 隧道、本地管理 API
├── gui-wails/       Windows 图形客户端（Wails + Vue）：登录、映射管理、自动更新
├── console-web/     管理控制台前端（Vue 3 + Element Plus），Nginx 部署
├── mobile/          Flutter 移动端（gomobile 嵌入 agent-go 网络栈）
├── installer/       Inno Setup 全中文安装包（双角色单包，首启选择）
├── update/          平台更新通道文件（运行数据，不入库）
└── docs/            设计文档
```

## 四、快速开始

### 1. 构建与启动平台

```powershell
# 构建（Go 1.22+）
cd platform-go
.\build.ps1                 # 产出 ..\liyou-platform.exe

# 配置凭据（首次）：编辑 platform-go\.env
# LIYOU_DSN=host=<DB地址> port=5432 user=<用户> password=<密码> dbname=liyoup2p sslmode=disable
# LIYOU_JWT_SECRET=<64字节十六进制随机值，可用 openssl rand -hex 64 生成>
# LIYOU_ADMIN_PASSWORD=<种子账号 admin 密码>
# LIYOU_SUPERADMIN_PASSWORD=<种子账号 superadmin 密码>

# 启动（自动读取 .env 注入环境变量；缺失必需凭据时拒绝启动）
.\start-platform.ps1        # 默认 HTTP/WS :8900，TCP 中继 :8901
```

> 敏感凭据一律经环境变量注入，源码中无任何默认密码/密钥；`.env` 已列入 `.gitignore`。

### 2. 构建 Agent

```powershell
cd agent-go
.\build.ps1                 # 产出 ..\liyou-agent.exe
```

运行方式：

```powershell
# 服务端角色（被访问方；--hosts 限制可穿透的内网目标白名单）
liyou-agent.exe --role server --platform wss://<平台地址>/ws --device-token <设备令牌> --hosts 127.0.0.1

# 客户端角色（访问方；--map [目标主机:]远端端口:本地端口，支持多条）
liyou-agent.exe --role client --platform wss://<平台地址>/ws --device-token <设备令牌> --map 8080:18080 --admin 127.0.0.1:0
```

设备令牌在控制台「设备管理」注册获得；`--admin` 开启本地管理通道（仅监听 127.0.0.1，随机 token 鉴权），支持映射/白名单热增删。

### 3. 构建 Windows GUI（Wails）

```powershell
cd gui-wails
wails build                 # 产出 build/bin/LiYouP2P.exe
```

### 4. 打包安装包（Inno Setup）

```
installer/LiLink.iss  →  ISCC 编译产出 installer/dist/LiLink-Setup-<版本>.exe
```

安装包全中文向导、含品牌视觉，自动创建桌面快捷方式/开始菜单/卸载条目；服务端与客户端角色在应用首次启动时选择。

### 5. 控制台部署（Nginx）

`console-web` 构建产物由 Nginx 托管于 80 端口，`/api` 反向代理平台 8900 端口。

## 五、部署形态

| 层级 | 形态 |
| --- | --- |
| 内网 | 平台部署于管理网段（PostgreSQL 同网段），控制台 Nginx 80 端口 |
| 外网入口 | 金万维隧道（wss://jyly8888.gnway.vip:58894/ws）转发至平台信令端口，外网 Agent 经此接入 |
| 业务流量 | 不经过平台与外网入口：跨网场景下两端通过 STUN/打洞在公网侧直连（UDP 加密流） |
| 防火墙 | 仅需放行各端出站；Agent 启动时自动添加本机 UDP 入站放行规则（打洞回包） |

## 六、安全设计

- **密码存储**：argon2id（内存困难型哈希），无明文、无弱哈希。
- **凭据环境化**：JWT 签名密钥、数据库 DSN、种子账号密码全部经环境变量注入，源码零默认凭据；缺失即拒绝启动。
- **JWT**：HS256 强制（拒收其他算法）、`exp` 过期校验；设备令牌与服务端令牌分离。
- **登录防爆破**：同一来源 IP 60 秒内 10 次失败锁定 15 分钟，锁定期间正确密码同样拒绝。
- **端到端加密**：每会话一次性 32 字节 CSPRNG 会话密钥，AES-256-GCM（96bit 随机 nonce）加密全部业务流量；密钥仅下发到会话两端，中继只见密文。
- **信令安全**：设备令牌经 `Authorization: Bearer` 头携带（不进 URL，不落访问日志）；打洞探测报文携带会话密钥 HMAC 校验，伪造探测不影响路径选择。
- **租户隔离**：JWT 携带租户 ID，所有数据查询强制租户过滤；设备撮合、会话密钥签发均限同租户；仅超级管理员跨租户可见。
- **设备治理**：吊销设备 UID 不可重注册；同机重复注册被拒绝；撮合优先最近活跃实例。
- **本地管理通道**：仅监听 127.0.0.1，随机 token 鉴权。
- **本地凭据**：设备令牌文件权限 0600，令牌不出现在命令行参数中。

## 七、更新分发机制

平台内置双通道更新服务（超管经控制台上传发布）：

- `pc` 通道：Windows 安装包（Inno Setup）
- `mobile` 通道：Android APK

客户端经 `/api/update/latest` 检测版本，下载后强校验 SHA-256（防 MITM 替换）再进入 `.old` 备份替换流程；移动端检测到更新自动触发安装。

## 八、License 与公司信息

© 济源市励友软件有限公司。本项目及其配套安装包为商业软件，未经授权不得复制、分发或反编译。

- 品牌：励联 LiLink
- 官网/支持：请联系公司官方渠道
