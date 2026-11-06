# LiYou P2P 平台构建脚本
$env:PATH = "D:\soft\go\bin;$env:PATH"
$env:GOPROXY = "https://goproxy.cn,direct"
Set-Location $PSScriptRoot
Write-Host "[构建] liyou-platform ..."
go build -trimpath -ldflags "-s -w" -o ..\liyou-platform.exe ./cmd/platform
if ($LASTEXITCODE -eq 0) { Write-Host "[构建] 完成 -> ..\liyou-platform.exe" } else { Write-Host "[构建] 失败"; exit 1 }
