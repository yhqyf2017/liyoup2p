# 技能：构建 Agent（agent-go）

适用：桌面 Agent（client/server 双角色单 exe）与 gomobile AAR 的编译。

## 1. 桌面 exe

```powershell
cd D:\dev\liyou\liyoup2p\agent-go
$env:Path = 'D:\soft\go\bin;' + $env:Path
$env:GOPROXY = 'https://goproxy.cn,direct'
.\build.ps1        # 产出 ..\liyou-agent.exe
```

手动等价：`go build -trimpath -ldflags "-s -w" -o ..\liyou-agent.exe ./cmd/agent`

回归门槛：`go build ./...`（internal/ws、internal/client 是桌面与移动共用网络栈，改动必须双端回归）

## 2. 运行参数

```powershell
# 服务端角色（被访问方；--hosts 为可穿透目标白名单）
liyou-agent.exe --role server --platform ws://<平台地址>/ws --device-token <令牌> --hosts 127.0.0.1

# 客户端角色（访问方；--map 远端端口:本地端口，可多条）
liyou-agent.exe --role client --platform ws://<平台地址>/ws --device-token <令牌> --map 8080:18081
```

- 设备令牌：控制台「设备管理」注册获得，或 API `POST /api/devices/register`（响应 `data.device_token`）
- 信令鉴权：**Authorization: Bearer 头**携带（`internal/ws/ws.go`），禁止 URL query（平台 401）
- 平台地址协议：明文 `ws://`——金万维隧道为纯 TCP 转发不终结 TLS，`wss://` 会报 `tls: first record does not look like a TLS handshake`
- `-token-file <路径>`：落盘令牌（0600），重启用

## 3. 常驻多实例模板

同机跑多角色时每台一个 UID + 独立 token 文件：

```powershell
Start-Process liyou-agent.exe -ArgumentList @('--role','client','--platform','ws://127.0.0.1:8900/ws',
  '--device-token',$tok,'-token-file',"$env:ProgramData\LiYouP2P\tokens\<uid>.json",
  '--name','<主机名>','--map','8080:18081') -WindowStyle Hidden
```

验证：`Get-Process liyou-agent` 计数、`Get-NetTCPConnection -LocalPort 18081..18083 -State Listen`、平台设备列表 status=online。

## 4. gomobile AAR（移动端嵌入栈）

```powershell
cd D:\dev\liyou\liyoup2p\agent-go
$env:PATH = 'D:\soft\go\bin;<JDK21>\bin;' + $env:PATH
$env:ANDROID_HOME = 'D:\Android\sdk'
$env:JAVA_HOME = 'D:\Program Files\Java\graalvm-jdk-21.0.7+8.1'
$env:GOTOOLCHAIN = 'local'
go run golang.org/x/mobile/cmd/gomobile bind -target=android -androidapi=24 -ldflags='-s -w' -o liyou-stripped.aar ./mobile
Copy-Item liyou-stripped.aar ..\mobile\android\app\libs\liyou.aar -Force
```

- `-androidapi=24` 须 ≥ 移动端 minSdk（24）
- 首次若报 go.sum 缺条目：`go get golang.org/x/mobile/cmd/gomobile@<go.mod 中版本>`
- 验证：解包 AAR 内 `arm64-v8a/libgojni.so`，ASCII 搜索应含 `Authorization`/`Bearer ` 且无 `?token=`
- 源码改动后**必须重编 AAR**——APK 里的网络栈是编译期固化，改 Go 代码不重编等于没改
