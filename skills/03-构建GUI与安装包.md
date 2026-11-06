# 技能：构建 GUI 与安装包（gui-wails + installer）

适用：Windows 图形客户端（Wails）编译与 Inno Setup 全中文安装包打包。

## 1. 版本号位置（发版先改这里）

| 位置 | 内容 |
| --- | --- |
| `gui-wails/update.go:25` | `var appVersion = "1.0.0"`（默认值；顶栏经 `GetVersion()` 绑定自动显示） |
| `installer/LiLink.iss:4` | `#define MyAppVersion "1.0.1"`（决定安装包名 LiLink-Setup-<版本>.exe） |

ldflags 注入方式（可选）：`wails build -ldflags "-X main.appVersion=1.0.2"`；改默认常量更稳妥。

## 2. wails 构建

```powershell
cd D:\dev\liyou\liyoup2p\gui-wails
$env:Path = 'D:\soft\go\bin;' + $env:Path
wails build            # 产出 build\bin\LiYouP2P.exe
Copy-Item build\bin\LiYouP2P.exe ..\LiYouP2P.exe -Force   # iss 引用仓库根的 exe
```

要点：
- 品牌图标源：`build\appicon.png`（512px）→ wails 自动生成各尺寸 + `build\windows\icon.ico`
- 前端改动无需单独 npm build，`wails build` 自带前端构建（frontend/dist 为产物，gitignore）
- windowsgui 无控制台：升级关键节点写文件日志 `%ProgramData%\LiLink\update.log`，自动化验证看这里

## 3. Inno Setup 打包

```powershell
cd D:\dev\liyou\liyoup2p\installer
& 'D:\soft\innosetup\ISCC.exe' .\LiLink.iss   # 产出 dist\LiLink-Setup-<版本>.exe
```

LiLink.iss 关键配置：
- `[Languages]`：`Name: "chs"; MessagesFile: "compiler:Languages\ChineseSimplified.isl"`（全中文向导）
- `SetupIconFile=..\gui-wails\build\windows\icon.ico`
- `WizardImageFile=assets\LiLink-WizardImage.bmp` / `WizardSmallImageFile=assets\LiLink-WizardSmall.bmp`（向导品牌图，164x314 / 55x58 BMP）
- `[Files]` 打入三件：`..\LiYouP2P.exe`、`..\liyou-agent.exe`、icon.ico（双角色单包，GUI 首启自选）
- `OutputBaseFilename=LiLink-Setup-{#MyAppVersion}`，`Compression=lzma2`

## 4. 安装/验证

```powershell
# 静默安装（覆盖旧版本）
& 'dist\LiLink-Setup-1.0.2.exe' /VERYSILENT /SUPPRESSMSGBOXES /NORESTART
# 卸载：控制面板或 {autopf}\LiLink\unins000.exe /VERYSILENT
```

装后验证清单：
1. `Get-Process LiYouP2P` 启动成功
2. `%ProgramData%\LiLink\update.log` 有 `already latest: current=1.0.2 latest=1.0.2`（检查更新回归）
3. 桌面/开始菜单快捷方式指向 `{autopf}\LiLink\LiYouP2P.exe`

## 5. 注意

- 覆盖安装前若 GUI 在运行需先 `Stop-Process LiYouP2P`，否则 exe 被占用安装失败
- 安装包不嵌入平台/数据库，仅桌面双角色
- 静默安装后首启弹角色选择，脚本化验证可用 `/VERYSILENT` + 手动确认 UI
