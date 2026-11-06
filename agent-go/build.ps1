# LiYou P2P Agent 构建脚本
$env:PATH = "D:\soft\go\bin;$env:PATH"
$env:GOPROXY = "https://goproxy.cn,direct"
Set-Location $PSScriptRoot
Write-Host "[构建] liyou-agent ..."
go build -trimpath -ldflags "-s -w" -o ..\liyou-agent.exe ./cmd/agent
if ($LASTEXITCODE -eq 0) { Write-Host "[构建] 完成 -> ..\liyou-agent.exe" } else { Write-Host "[构建] 失败"; exit 1 }
