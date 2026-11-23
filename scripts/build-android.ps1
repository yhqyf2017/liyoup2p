# Android 发布包构建脚本
# 用法: pwsh -File scripts\build-android.ps1 [-Ver 1.1.8]
# 作用:
#   1. 校验 pubspec.yaml 的 version 与 -Ver 一致（不一致即报错退出，防止版本漂移）
#   2. flutter build apk --release --dart-define=APP_VERSION=<ver> 注入应用版本
#   3. 产物拷贝到 dist\LiLink-android-<ver>.apk
# 注意: kAppVersion 的 defaultValue 也应与 pubspec 同步（双保险）
param(
    [string]$Ver = ''
)

$ErrorActionPreference = 'Stop'
$Root = Split-Path -Parent (Split-Path -Parent $PSCommandPath)
$Mobile = Join-Path $Root 'mobile'

# Gradle 需 JDK17：项目 AGP < 8.2.1 与 JDK21 不兼容（google issuetracker 294137077）。
# 用本机 JDK17 覆盖 JAVA_HOME（当前系统默认是 graalvm-jdk-21，会导致 assembleRelease 失败）。
$env:JAVA_HOME = 'D:\Program Files\Java\jdk17'
$env:Path = "$env:JAVA_HOME\bin;" + $env:Path

# 读取 pubspec version
$pubspec = Get-Content (Join-Path $Mobile 'pubspec.yaml') -Encoding UTF8
$pubVer = ''
foreach ($line in $pubspec) {
    if ($line -match '^\s*version:\s*([0-9][0-9.]+)\+\d+\s*$') { $pubVer = $Matches[1]; break }
}
if (-not $pubVer) { Write-Error '[NG] pubspec.yaml 缺少 version 行'; exit 1 }

if ($Ver -eq '') { $Ver = $pubVer }
if ($Ver -ne $pubVer) {
    Write-Error "[NG] 版本不一致: -Ver $Ver vs pubspec $pubVer —— 请先同步 pubspec.yaml"
    exit 1
}

Push-Location $Mobile
try {
    Write-Host "[1/3] flutter build apk --release (APP_VERSION=$Ver)"
    flutter build apk --release --dart-define=APP_VERSION=$Ver
    if ($LASTEXITCODE -ne 0) { Write-Error '[NG] flutter build 失败'; exit 1 }

    $src = Join-Path $Mobile 'build\app\outputs\flutter-apk\app-release.apk'
    if (-not (Test-Path $src)) { Write-Error "[NG] 找不到产物 $src"; exit 1 }

    $dist = Join-Path $Root 'dist'
    New-Item -ItemType Directory -Force -Path $dist | Out-Null
    $dst = Join-Path $dist "LiLink-android-$Ver.apk"
    Copy-Item $src $dst -Force
    $size = [math]::Round((Get-Item $dst).Length / 1MB, 1)
    Write-Host "[3/3] $dst ($size MB)"
    Write-Host "[OK] LiLink-android-$Ver.apk"
} finally {
    Pop-Location
}
