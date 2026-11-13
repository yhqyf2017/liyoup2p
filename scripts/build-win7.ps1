# 励联 LiLink Win7 专用版一键构建（Go 1.20.14：Go 1.21+ 产物不支持 Win7）
# 产出：
#   dist\win7\LiYouP2P-<arch>.exe / liyou-agent-<arch>.exe（amd64 + 386）
#   dist\LiLink-pc-win7-<ver>-x64.zip / -x86.zip
#   dist\LiLink-Setup-Win7-<ver>.exe（amd64 安装器，需 Inno Setup）
# 前置：
#   Go 1.20.14 解压到 D:\soft\go1.20.14（https://go.dev/dl/go1.20.14.windows-amd64.zip）
#   go-winres 已生成 gui-walk\*.syso（改动 winres.json/图标后重跑：
#     go-winres make --in winres.json --out rsrc --arch=amd64,386）
# 注意：agent 构建复制到临时目录并把 go 指令改为 1.20，不动 agent-go 仓库文件。
$ErrorActionPreference = 'Stop'

$Go120 = 'D:\soft\go1.20.14\bin\go.exe'
$Ver   = '1.1.9'
$Root  = Split-Path -Parent $PSScriptRoot
$Out   = Join-Path $Root 'dist\win7'
$ISCC  = 'D:\soft\innosetup\ISCC.exe'
$env:GOPROXY = 'https://goproxy.cn,direct'

if (-not (Test-Path $Go120)) {
    throw "未找到 Go 1.20.14：$Go120（从 https://go.dev/dl/go1.20.14.windows-amd64.zip 解压到 D:\soft\go1.20.14）"
}

Remove-Item $Out -Recurse -Force -ErrorAction SilentlyContinue
New-Item $Out -ItemType Directory -Force | Out-Null

# ---------- 1. Agent（go1.20 编译，临时目录改写 go.mod） ----------
$TmpRoot = Join-Path $env:TEMP 'liyou-win7-build'
$Tmp = Join-Path $TmpRoot 'agent-go'
Remove-Item $TmpRoot -Recurse -Force -ErrorAction SilentlyContinue
Copy-Item (Join-Path $Root 'agent-go') $Tmp -Recurse -Force
$gm = Join-Path $Tmp 'go.mod'
(Get-Content $gm) |
    Where-Object { $_ -notmatch '^\s*toolchain\s' } |
    ForEach-Object { $_ -replace '^go\s+.*', 'go 1.20' } |
    Set-Content $gm -Encoding ascii
# Go 1.20 兼容补丁：原版 internal/admin 用了 Go 1.22 的方法路由/PathValue，
# 用行为一致的兼容版覆盖临时目录中的文件（仓库文件不动）。
Copy-Item (Join-Path $Root 'scripts\win7-agent-patch\admin.go') (Join-Path $Tmp 'internal\admin\admin.go') -Force

foreach ($arch in 'amd64', '386') {
    $env:GOARCH = $arch
    Push-Location $Tmp
    & $Go120 build -trimpath -ldflags '-s -w' -o (Join-Path $Out "liyou-agent-$arch.exe") ./cmd/agent
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw "agent $arch 构建失败" }
    Pop-Location
    Write-Host "[OK] liyou-agent-$arch.exe"
}

# ---------- 2. GUI（gui-walk，go1.20 + walk） ----------
foreach ($arch in 'amd64', '386') {
    $env:GOARCH = $arch
    Push-Location (Join-Path $Root 'gui-walk')
    & $Go120 build -trimpath -ldflags "-s -w -H windowsgui -X main.isRelease=true -X main.appVersion=$Ver" -o (Join-Path $Out "LiYouP2P-$arch.exe") .
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw "gui $arch 构建失败" }
    Pop-Location
    Write-Host "[OK] LiYouP2P-$arch.exe"
}

# ---------- 3. zip 组包 ----------
foreach ($arch in 'amd64', '386') {
    $stage = Join-Path $env:TEMP "liyou-win7-zip-$arch"
    Remove-Item $stage -Recurse -Force -ErrorAction SilentlyContinue
    New-Item $stage -ItemType Directory -Force | Out-Null
    Copy-Item (Join-Path $Out "LiYouP2P-$arch.exe") (Join-Path $stage 'LiYouP2P.exe')
    Copy-Item (Join-Path $Out "liyou-agent-$arch.exe") (Join-Path $stage 'liyou-agent.exe')
    $suffix = if ($arch -eq 'amd64') { 'x64' } else { 'x86' }
    $zip = Join-Path $Root "dist\LiLink-pc-win7-$Ver-$suffix.zip"
    Compress-Archive -Path (Join-Path $stage '*') -DestinationPath $zip -Force
    Write-Host "[OK] $(Split-Path -Leaf $zip)"
}

# ---------- 4. 安装器（amd64；32 位 Win7 用 x86 zip） ----------
if (Test-Path $ISCC) {
    Push-Location (Join-Path $Root 'installer')
    & $ISCC 'LiLink-win7.iss'
    if ($LASTEXITCODE -ne 0) { Pop-Location; throw 'ISCC 打包失败' }
    Pop-Location
    Write-Host "[OK] LiLink-Setup-Win7-$Ver.exe"
} else {
    Write-Warning "未找到 ISCC.exe（$ISCC），跳过安装器打包"
}

# ---------- 5. 产物 SHA256 汇总 ----------
Write-Host "`n===== 产物清单（SHA256） ====="
Get-ChildItem (Join-Path $Root 'dist') -Filter 'LiLink-*win7*' | ForEach-Object {
    '{0}  {1}' -f (Get-FileHash $_.FullName -Algorithm SHA256).Hash, $_.Name
}
