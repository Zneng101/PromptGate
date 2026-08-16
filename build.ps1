# PromptGate Windows 构建脚本
# 用法：powershell -ExecutionPolicy Bypass -File .\build.ps1
param([switch]$Run)

$ErrorActionPreference = "Stop"
Write-Host "==> 1/3 安装前端依赖..." -ForegroundColor Cyan
Push-Location webui
npm install
Write-Host "==> 2/3 构建前端..." -ForegroundColor Cyan
npm run build
Pop-Location

Write-Host "==> 3/3 构建后端二进制..." -ForegroundColor Cyan
go build -o promptgate.exe ./cmd/promptgate
Write-Host "完成：promptgate.exe" -ForegroundColor Green

if ($Run) {
    Write-Host "==> 启动..." -ForegroundColor Cyan
    .\promptgate.exe
}
