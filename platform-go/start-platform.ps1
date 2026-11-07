# 励联 P2P 平台启动脚本：读取 platform-go\.env 注入环境变量后构建并启动平台。
# 用法：pwsh -ExecutionPolicy Bypass -File start-platform.ps1
# 说明：
#   - 凭据（数据库 DSN / JWT 密钥 / 种子账号密码）只放 .env，不进源码、不进命令行；
#   - exe 缺失或源码比 exe 新时自动用 go 重新构建；
#   - 端口 8900 已被占用时视为平台已在运行，直接退出（不重复拉起）；
#   - 运行日志写入 platform-go\logs\platform.log，便于排障。
$ErrorActionPreference = 'Stop'

$root = Split-Path $PSScriptRoot -Parent

# ---------- 1. 读取凭据 ----------
$envFile = Join-Path $PSScriptRoot '.env'
if (-not (Test-Path $envFile)) {
    Write-Error "未找到凭据文件 $envFile——请先创建 .env（LIYOU_DSN / LIYOU_JWT_SECRET / LIYOU_ADMIN_PASSWORD / LIYOU_SUPERADMIN_PASSWORD）"
    exit 1
}
Get-Content $envFile -Encoding UTF8 | ForEach-Object {
    $line = $_.Trim()
    if ($line -and -not $line.StartsWith('#')) {
        $i = $line.IndexOf('=')
        if ($i -gt 0) {
            [Environment]::SetEnvironmentVariable($line.Substring(0, $i).Trim(), $line.Substring($i + 1).Trim(), 'Process')
        } else {
            Write-Warning ".env 中存在无法解析的行（应为 KEY=VALUE 格式）：$line"
        }
    }
}

# ---------- 2. 防重复启动 ----------
$occupied = $false
try {
    $c = New-Object Net.Sockets.TcpClient
    $c.Connect('127.0.0.1', 8900)
    $c.Close()
    $occupied = $true
} catch { $occupied = $false }
if ($occupied) {
    Write-Host '端口 8900 已被占用：平台疑似已在运行，本次不做任何操作。'
    Write-Host '如需重启，请先在任务管理器结束 liyou-platform.exe，或运行：'
    Write-Host '  taskkill /IM liyou-platform.exe /F'
    exit 0
}

# ---------- 3. 构建（缺失或源码较新时） ----------
$exe = Join-Path $PSScriptRoot 'liyou-platform.exe'
$srcMain = Join-Path $PSScriptRoot 'cmd\platform\main.go'
$needBuild = (-not (Test-Path $exe)) -or ((Get-Item $srcMain).LastWriteTime -gt (Get-Item $exe).LastWriteTime)
if ($needBuild) {
    Write-Host '正在构建平台…'
    # 本机装了两套 Go：D:\soft\go\bin（1.22，可用）与 D:\Program Files\Go（1.17，过旧）。
    # 优先用 1.22，避免 PATH 命中旧版导致 net/netip 等标准库缺失。
    $go = 'D:\soft\go\bin\go.exe'
    if (-not (Test-Path $go)) { $go = 'go' }
    Push-Location $PSScriptRoot
    & $go build -o liyou-platform.exe .\cmd\platform
    $ok = $LASTEXITCODE -eq 0
    Pop-Location
    if (-not $ok) { Write-Error '平台构建失败，请检查 go 环境与源码'; exit 1 }
    Write-Host '构建完成。'
}

# ---------- 4. 启动（日志落盘） ----------
$logDir = Join-Path $PSScriptRoot 'logs'
New-Item -ItemType Directory -Force -Path $logDir | Out-Null
$log = Join-Path $logDir 'platform.log'
$errLog = Join-Path $logDir 'platform.err.log'
# 平台日志走 stderr（Go log 默认），两路都落盘；-Redirect 要求文件不存在则自动创建
Remove-Item $log, $errLog -ErrorAction SilentlyContinue

Start-Process -FilePath $exe -WorkingDirectory $root -WindowStyle Hidden `
    -RedirectStandardOutput $log -RedirectStandardError $errLog
Write-Host "平台启动中（工作目录 $root，日志 $errLog）…"

# ---------- 5. 启动自检 ----------
$ready = $false
for ($i = 0; $i -lt 20; $i++) {
    Start-Sleep -Milliseconds 500
    try {
        $c = New-Object Net.Sockets.TcpClient
        $c.Connect('127.0.0.1', 8900)
        $c.Close()
        $ready = $true
        break
    } catch { }
}
if ($ready) {
    Write-Host '平台已就绪：'
    Write-Host '  控制台  http://127.0.0.1:8900/'
    Write-Host '  信令 WS http://127.0.0.1:8900/ws'
    Write-Host '  升级    http://127.0.0.1:8900/api/update/latest'
    Write-Host '  中继    TCP :8901 + WS /relay'
} else {
    Write-Error '平台启动超时：8900 未监听，请查看日志 platform-go\logs\platform.err.log'
    exit 1
}
