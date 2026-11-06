# 励友 P2P 移动端（Flutter）技术调研报告

| 文档属性 | 内容 |
| --- | --- |
| 文档编号 | LY-PLAN-MOBILE-001 |
| 版本 | v1.0（调研稿，仅调研不实施） |
| 编制日期 | 2026-11-05 |
| 适用范围 | 移动端 Android / iOS / HarmonyOS NEXT 客户端选型与里程碑规划 |
| 关联资产 | `agent-go`（Go 网络栈）、`liyou-platform`（信令/中继）、`gui-wails`（Windows 桌面端）、`console-web`（Web 控制台） |

---

## 1. 背景与调研目标

励友 P2P 当前已交付 Windows 桌面端（Wails v2 + Go，`LiYouP2P.exe`）、Web 控制台（console-web）与 Go 平台（liyou-platform）。核心网络能力全部沉淀在 **agent-go**（Go 语言实现）：UDP 打洞（STUN + 候选交换）、smux v2 多路复用、AES-256-GCM 加密可靠层、WebSocket 信令、平台中继兜底、本地 HTTP 管理通道。

本报告回答一个问题：**如何在移动端复用上述 Go 网络栈，以可控成本交付 Android / iOS / 鸿蒙三端客户端**，并给出平台差异、UI 方案与里程碑拆分。

---

## 2. 现有技术资产盘点（移动端可复用性评估）

### 2.1 agent-go 结构与规模

| 模块 | 职责 | 行数 |
| --- | --- | --- |
| internal/client | 客户端会话、端口映射（本地 TCP 监听 → 隧道转发） | 923 |
| internal/server | 服务端：端口开放、内网主机（hosts）白名单 | 867 |
| internal/reliable | UDP 之上的 **AES-256-GCM** 可靠有序字节流 | 761 |
| internal/punch | UDP 打洞：候选收集/交换/排序/双向打洞 | 324 |
| internal/muxlink | smux v2 多路复用（keepalive 10s/30s，单帧 8KB） | 267 |
| internal/config | 命令行/参数配置 | 258 |
| internal/admin | 本地 HTTP 管理通道（桌面 GUI 即通过它取状态/管理映射） | 149 |
| internal/relaydial | 平台中继兜底（打洞失败走 relay） | 135 |
| internal/stun | 公网 STUN 探测 | 139 |
| internal/ws | WebSocket 信令（gorilla/websocket） | 121 |
| internal/tunnel / device / logx / demo / fwutil | 隧道帧、设备标识、日志、演示页、防火墙 | ~486 |
| **合计** | | **约 4400 行** |

### 2.2 关键结论

1. **依赖极简且纯 Go**：`go.mod` 仅 `gorilla/websocket` + `xtaci/smux v2`，**零 cgo**。这意味着 gomobile / Go 交叉编译到移动平台（Android/iOS）不存在原生依赖障碍，这是本次选型最重要的有利条件。
2. **网络栈与 UI 天然解耦**：agent 以独立进程 + 管理通道（HTTP）与 GUI 通信，核心状态机（连接、映射增删、日志）均已接口化，移动端可整体内嵌。
3. **客户端已具备「本地端口映射」模式**：client 侧对每条映射执行 `net.Listen("tcp", 127.0.0.1:port)`，App 内 WebView / 本机应用访问本地端口即达远端服务——移动端第一版可直接复用该交互模型。

---

## 3. 技术可行性核心问题：Go 网络栈如何进入 Flutter App

### 3.1 三条候选路线对比

| 维度 | 路线 A：gomobile bind 内嵌 | 路线 B：Dart 重写 | 路线 C：VPNService/TUN 全局代理 |
| --- | --- | --- | --- |
| 思路 | `gomobile bind` 将 agent 核心编译为 Android `.aar` / iOS `.xcframework`，Flutter 通过 Platform Channel 调用 | 用 Dart 重写打洞/加密/复用/信令全部网络栈 | Go 栈不变，外层套 VPNService（Android）/ NEPacketTunnelProvider（iOS），TUN 流量经本地 socks 转入 Go 栈 |
| 网络栈复用度 | 100%（约 4400 行全保留） | 0%（全部重写） | 100% |
| 改造成本 | 低：新增一个 `mobile/` bind 包（DTO + 回调封装），预计 300–500 行适配层 | 极高：打洞、加密、拥塞/重传、协议兼容性全部重造，且需与现有 PC 端保持协议级一致 | 中高：bind 层之外需实现 TUN 读取 + socks5 服务 + VPN 配置，两端各一套 |
| 协议一致性风险 | 无（同一份代码） | 高（重写引入的字节级偏差难以穷举测试） | 无 |
| 双端后台能力 | 受平台限制（见 §4） | 同 A | **最强**：VPN 通道由系统进程承载，iOS 上是唯一可靠的后台穿透路径 |
| 审核风险 | 无 | 无 | iOS Network Extension entitlement 需向 Apple 申请，Android API 30+ 全部通过系统 VPN 确认框 |
| 工作量（相对） | ★☆☆ | ★★★★ | ★★★ |

**结论：以路线 A 为主体、路线 C 为增强的混合方案。** 路线 B 明确不推荐。

### 3.2 推荐混合方案（分层架构）

```
┌─────────────────────────────────────────────────┐
│ Flutter UI（登录 / 设备 / 映射 / 日志 / 设置）        │
├─────────────────────────────────────────────────┤
│ Platform Channel（MethodChannel + EventChannel）   │
├─────────────────────────────────────────────────┤
│ mobile bind 适配层（Go，新增 ~400 行）               │
│   · Login/Start/Stop/AddMap/RemoveMap/Events       │
│   · 复用 internal/*（打洞、smux、AES-GCM、WS 信令）  │
├─────────────────────────────────────────────────┤
│ 流量接入层（按平台分阶段）                            │
│   M1: 本地端口映射（App 内 WebView/浏览器访问        │
│        127.0.0.1:<port>，复用现有 client 监听逻辑）  │
│   M2: Android VPNService + tun2socks（全局代理）    │
│   M2+: iOS NEPacketTunnelProvider（系统级隧道）     │
└─────────────────────────────────────────────────┘
```

**gomobile 适配要点（工程实践约束，须在排期中预留）：**

- bind 包只暴露 **DTO（结构体）+ 方法 + 回调**，避免 map、泛型、interface{} 跨界（gomobile 不支持）；现有代码中 `map[string]any` 类信令帧需在适配层转为强类型 DTO。
- 回调（日志、状态推送）必须在 Go 侧持有引用对象（`goRef`），经 EventChannel 转成 Dart Stream。
- Android：`gomobile bind -target android -androidapi 23`，产物 `.aar` 随主工程构建；需 NDK（r25+）与 Go 1.22 交叉编译链。
- iOS：`gomobile bind -target ios`，产物 `.xcframework`（真机 arm64 + 模拟器 x86_64/arm64 两个 slice）；CI 需 macOS 环境。
- 现有 `internal/admin` 的本地 HTTP 管理通道在移动端可保留（Flutter 侧直接 HTTP 调用），减少一层绑定面。

### 3.3 明确不推荐 Dart 重写的原因

重写范围不只是 4400 行代码，而是**隐含协议**：WS 帧格式、UDP 魔法包、候选排序策略（内网同网段优先等）、AES-GCM 包格式、smux 参数（10s/30s keepalive、8KB 帧）、中继兜底次序。任何偏差都会造成移动端与存量 PC 端互通回归问题，且打洞成功率对时序极其敏感，重写后需要重建整套真网测试矩阵。性价比显著低于内嵌方案。

---

## 4. 平台差异分析

### 4.1 Android（M1 目标平台，条件最成熟）

| 事项 | 说明与要求 |
| --- | --- |
| 网络 | `INTERNET` 权限即可，UDP 打洞/STUN 在前台完全可用；蜂窝网络下 NAT 类型更严（CGNAT），打洞失败率高于 Wi-Fi，需确保中继兜底链路稳定 |
| 后台保活 | **必须使用前台服务**（Foreground Service）：`FOREGROUND_SERVICE` + Android 14 起需声明具体类型（建议 `connectedDevice` 或 `dataSync`）+ 常驻通知（即「托盘式常驻通知」，展示连接状态、一键断开）；Doze 模式会限制网络与定时器，需引导用户关闭电池优化（`REQUEST_IGNORE_BATTERY_OPTIMIZATIONS`） |
| 厂商保活 | 华为/小米/OPPO/vivo 需逐厂商引导「自启动 + 后台运行白名单」，作为设置页引导项而非代码强求；这是 Android 端「连接常驻」的最大现实变数 |
| VPNService（M2） | `android.net.VpnService` 系统确认对话框；Android 13+ 需 `POST_NOTIFICATIONS` 运行时权限；tun2socks 建议用 `hev-socks5-tunnel`（成熟、许可证友好） |
| 打包 | minSdk 23+，gomobile 产物约增加 10–15MB（含 Go runtime），总体积可控 |

### 4.2 iOS（M2 平台，存在硬限制，须如实评估）

| 事项 | 说明与硬限制 |
| --- | --- |
| 前台使用 | 无障碍：App 在前台时，打洞/穿透全功能可用 |
| **后台穿透硬限制** | iOS 无「后台网络常驻」机制：App 退后台后 socket 数十秒内被系统挂起，UDP/WS 长连接即断；Background Modes（audio/location/voip）均不能合法维持穿透链路，BGTaskScheduler 执行时长不可控。**结论：常规 App 形态下，iOS 只能承诺「前台/亮屏期间穿透可用」，回前台后由信令自动重连**（agent 已具备断线重连，改造量小） |
| 唯一后台路径 | **NEPacketTunnelProvider**（Network Extension）：隧道由系统独立进程承载，不随 App 挂起，是 iOS 上唯一可靠的「后台穿透」方案。代价：① `com.apple.developer.networking.networkextension` entitlement 需向 Apple 提交申请（周期数周，不保证批准）；② 用户侧需经过 VPN 配置确认；③ 需实现 TUN 层与 Go 栈对接。建议作为 M2 的可选增强、独立排期 |
| 审核风险 | VPN/网络扩展类 App 审核：需提交用途说明；若申请 Network Extension 权限被拒，产品形态退回「前台可用」 |
| 打包 | gomobile 生成 xcframework；App Store 上架需 macOS CI；IPv6-only 蜂窝网络兼容性需回归（STUN/打洞地址族处理已有 v4 优先逻辑，需专项验证） |

### 4.3 HarmonyOS NEXT（M3 平台，生态成熟度风险最高）

| 事项 | 现状（截至调研日） |
| --- | --- |
| Flutter 支持 | Flutter 官方主仓库**不包含** ohos 目标；华为维护 openharmony-sig/flutter_flutter 分支（持续跟进 Flutter 版本，但滞后于官方稳定版）。商业项目采用需锁定分支版本并承担升级断档风险；常用三方插件（推送、WebView、权限等）需逐一验证/适配 ohos 实现 |
| Go 网络栈复用 | gomobile 产物（.aar/.xcframework）**不适用**鸿蒙。可行路径：鸿蒙 NDK（LLVM 工具链）`GOOS=android` 不可用，需以 `c-shared` 交叉编译出 .so + ArkTS NAPI 封装层（工作量约等于再做一次 bind 适配层）；官方对鸿蒙原生 NAPI + Go 的组合无成熟样板，属探索性工作 |
| 上架要求 | 华为应用市场（AppGallery Connect）：软件著作权、隐私政策与个人信息保护合规检测、必要时软件检测报告；纯净模式仅允许应用市场分发——侧载不可行，合规成本需计入 |
| 生态 | ArkTS/ArkUI 为主流原生栈；若走「ArkTS 原生 + Go .so」而非 Flutter ohos，可规避 Flutter 分支风险，但 UI 需重写（与 Flutter 共享诉求冲突） |
| 结论 | **M3 观望项**：建议以 Android/iOS 上线后的真实需求量决定是否启动；若启动，优先验证「Flutter ohos 分支 + Go c-shared/NAPI」的可行性 PoC（5–8 人日）后再排全量 |

---

## 5. UI 方案（与现有产品视觉统一）

1. **框架**：Flutter（官方稳定通道）+ Material 3，不引入已停止维护的第三方仿生组件库（如 flutter Darling 已无活跃维护）；以**设计令牌（Design Tokens）**方式落地品牌规范。
2. **品牌主题**：主色沿用现有产品品牌蓝 **#1667C8**（gui-wails 前端已在用），派生 Material 3 ColorScheme（seed color 方式生成明暗两套）；页面结构对齐 LiYouP2P.exe：登录页 → 工作页（连接状态卡 / 服务端已开放列表 / 允许访问主机 / 端口映射 / 日志）。
3. **移动端信息架构**（M1）：登录（平台地址/租户/账号）→ 设备列表与注册 → 映射管理（增删改 + 生效开关，复用桌面端交互）→ 常驻通知（状态 + 快捷启停）→ 日志面板（EventChannel 实时流）→ 设置（电池优化引导、厂商自启动引导、关于）。
4. **状态管理**：Riverpod；与 Go 侧的实时数据（日志、连接状态、映射状态）统一经 EventChannel 流式接入，禁止轮询式界面刷新。
5. **复用资产**：icon 沿用 `favicon.ico` 品牌图标（导出适配 Android 自适应图标与 iOS AppIcon 各尺寸）；文案与交互话术与桌面端保持一致。

---

## 6. 里程碑拆分与工作量评估

> 评估基准：2 名开发（1 Flutter + 1 Go 兼职适配层）、现有 agent-go/平台代码可直接复用、真机调试环境（含外网打洞验证机）已具备。人日为净开发+联调工作量，不含 PM/测试专项。

### M1：Android MVP（建议 6–8 周，约 30–40 人日）

| 范围 | 内容 | 人日 |
| --- | --- | --- |
| bind 适配层 | mobile 包（DTO/回调/生命周期）+ gomobile 构建脚本 | 4 |
| Flutter 壳 | 工程搭建、主题令牌、路由、 Riverpod 骨架 | 3 |
| 登录与设备 | 平台登录、设备注册/令牌管理（对接平台 API） | 4 |
| 连接管理 | 启动/停止/状态机/自动重连/日志流 | 5 |
| 映射管理 | 增删改查 + 生效状态（复用 client AddMap/RemoveMap） | 4 |
| 穿透验证 | App 内 WebView/浏览器访问本地映射端口；打洞/中继真网联调 | 4 |
| 常驻能力 | 前台服务 + 常驻通知（托盘式）+ 电池优化/厂商自启动引导页 | 5 |
| 质量收尾 | 兼容性（Android 8–15）、崩溃/内存、打包签名 | 3 |

**主要风险**：厂商后台查杀（缓解：白名单引导 + 重连兜底）；CGNAT 打洞成功率（缓解：中继兜底必须全量回归）；gomobile 回调内存管理（缓解：适配层单测 + 压测）。

### M2：iOS（M1 后启动，约 20–30 人日 + 审批等待）

| 范围 | 人日 |
| --- | --- |
| xcframework 集成 + macOS CI | 4 |
| 登录/设备/映射/日志 UI 适配（与 M1 共享 Dart 层，仅平台通道差异） | 6 |
| iOS 后台挂起适配：回前台自动重连、状态提示 | 4 |
| IPv6-only 网络与蜂窝真网回归 | 4 |
| 上架物料（隐私清单、用途说明、审核沟通） | 2 |
| （可选增强）NEPacketTunnelProvider VPN 模式： entitlement 申请 + TUN 对接 + 联调 | +15–20（独立排期，建议单列项目） |

**主要风险**：Network Extension entitlement 审批不可控；后台限制属产品级约束，需提前与业务方对齐预期（iOS「后台保持穿透」仅在 VPN 模式下成立）。

### M3：鸿蒙 HarmonyOS NEXT（建议观望，启动前置 PoC）

| 范围 | 人日 |
| --- | --- |
| PoC：Flutter ohos 分支可行性 + Go c-shared → NAPI 封装打通 | 5–8 |
| 全量（UI 三端复用 + ohos 适配层 + 上架合规） | 30–40 |
| **主要风险** | Flutter ohos 分支成熟度、三方插件适配、Go/NDK 组合无成熟样板、上架合规周期；建议以 PoC 结果决策是否立项 |

---

## 7. 结论与建议

1. **可行性结论：技术可行，推荐「gomobile 内嵌 + 本地端口映射起步、VPNService/NEPacketTunnel 增强」的混合路线。** agent-go 纯 Go、零 cgo、依赖极简，是三条路线中最关键的先决条件，已满足。
2. **推荐技术栈**：Flutter（Material 3 + 品牌蓝 #1667C8 设计令牌）+ Riverpod；Go 侧新增 mobile bind 适配层；流量接入按「App 内本地端口（M1）→ Android VPNService（M2）→ iOS Network Extension（M2+ 可选）」渐进。
3. **平台预期管理**：Android 可实现与桌面端一致的「后台常驻」体验（前台服务 + 厂商白名单引导）；**iOS 在常规 App 形态下不存在后台穿透常驻能力**，只能前台可用 + 回前台自动重连，后台常驻唯一可行路径是 Network Extension（受 entitlement 审批约束）；**鸿蒙建议观望**，以 PoC 决策。
4. **风险清单（Top 5）**：
   - Android 厂商后台查杀导致连接中断（高概率/中影响）→ 白名单引导 + 前台服务 + 断线自动重连 + 常驻通知状态可见；
   - 蜂窝 CGNAT 打洞成功率下降（中概率/中影响）→ 中继兜底全量回归与监控指标化；
   - iOS Network Extension entitlement 审批（中概率/高影响）→ 申请前置到 M1 期间并行提交，被拒则产品形态按「前台可用」发布；
   - gomobile 跨语言边界的数据类型/内存约束（低概率/中影响）→ 适配层强类型 DTO + 单测覆盖；
   - 鸿蒙生态不确定性（高概率/低当前影响）→ M3 观望，PoC 后决策。
5. **下一步**：如立项，建议先执行「gomobile bind 打通 + Flutter 壳 + 本地端口映射端到端 Demo」为期两周的技术验证 Sprint，再进入 M1 正式排期。

---

*本报告基于 2026-11-05 的代码现状（agent-go @ liyoup2p 仓库）与公开平台政策整理；平台审核政策与鸿蒙生态变化较快，实施前应复核最新要求。*
