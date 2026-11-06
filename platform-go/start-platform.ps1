# 励联 P2P 平台启动脚本：读取 platform-go\.env 注入环境变量后启动平台。
# 用法：powershell -ExecutionPolicy Bypass -File start-platform.ps1
$ErrorActionPreference = 'Stop'

$envFile = Join-Path $PSScriptRoot '.env'
if (-not (Test-Path $envFile)) {
    Write-Error "未找到凭据文件 $envFile——请先创建 .env（LIYOU_DSN / LIYOU_JWT_SECRET / LIYOU_ADMIN_PASSWORD / LIYOU_SUPERADMIN_PASSWORD）"
}
Get-Content $envFile -Encoding UTF8 | ForEach-Object {
    $line = $_.Trim()
    if ($line -and -not $line.StartsWith('#')) {
        $i = $line.IndexOf('=')
        if ($i -gt 0) {
            [Environment]::SetEnvironmentVariable($line.Substring(0, $i).Trim(), $line.Substring($i + 1).Trim(), 'Process')
        }
    }
}

$exe = Join-Path (Split-Path $PSScriptRoot -Parent) 'liyou-platform.exe'
if (-not (Test-Path $exe)) { $exe = Join-Path $PSScriptRoot 'liyou-platform.exe' }
Write-Host "启动平台：$exe"
Start-Process -FilePath $exe -WorkingDirectory (Split-Path $PSScriptRoot -Parent) -WindowStyle Minimized
Write-Host '平台已启动（凭据经环境变量注入，未写入命令行）'
