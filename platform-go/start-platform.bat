@echo off
chcp 65001 >nul
title LiLink Platform
cd /d "%~dp0"

if not exist ".env" (
  echo [X] .env not found.
  echo     Copy .env.example to .env and fill in the 4 required values first.
  pause
  exit /b 1
)

where pwsh >nul 2>nul
if %errorlevel%==0 (
  pwsh -NoProfile -ExecutionPolicy Bypass -File "%~dp0start-platform.ps1"
) else (
  powershell -NoProfile -ExecutionPolicy Bypass -File "%~dp0start-platform.ps1"
)

echo.
echo Platform started. Close this window is OK, logs are in logs\platform.err.log
pause
