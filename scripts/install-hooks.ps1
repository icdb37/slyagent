# =============================================================================
#  安装 Git Hook (Windows PowerShell)
# =============================================================================
#  将项目的 .githooks/ 目录注册为 Git hooks 路径,
#  使 hooks 与代码一同被版本管理。
#
#  使用方法 (在项目根目录下执行):
#    powershell -ExecutionPolicy Bypass -File .\scripts\install-hooks.ps1
# =============================================================================

$ErrorActionPreference = "Stop"

# 定位仓库根目录
$RepoRoot = git rev-parse --show-toplevel 2>$null
if (-not $RepoRoot) {
    Write-Host "[install-hooks] 错误: 当前目录不是 Git 仓库" -ForegroundColor Red
    exit 1
}

$HooksDir = Join-Path $RepoRoot ".githooks"

if (-not (Test-Path -Path $HooksDir -PathType Container)) {
    Write-Host "[install-hooks] 错误: 未找到 $HooksDir" -ForegroundColor Red
    exit 1
}

Write-Host "[install-hooks] 设置 core.hooksPath = $HooksDir"
git config core.hooksPath $HooksDir

# 列出已注册的 hooks
Get-ChildItem -Path $HooksDir -File | ForEach-Object {
    Write-Host "[install-hooks] 已注册 hook: $($_.Name)"
}

Write-Host ""
Write-Host "[install-hooks] 安装完成 ✓" -ForegroundColor Green
Write-Host ""
Write-Host "提示:"
Write-Host "  - 钩子在 'git commit' 时自动执行"
Write-Host "  - 跳过检查:   git commit --no-verify"
Write-Host "  - 查看配置:   git config --get core.hooksPath"