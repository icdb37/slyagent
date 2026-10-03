#!/usr/bin/env bash
# =============================================================================
#  安装 Git Hook (Unix/Linux/macOS/Git Bash)
# =============================================================================
#  将项目的 .githooks/ 目录注册为 Git hooks 路径,
#  使 hooks 与代码一同被版本管理。
# =============================================================================

set -euo pipefail

REPO_ROOT="$(git rev-parse --show-toplevel 2>/dev/null || pwd)"
HOOKS_DIR="$REPO_ROOT/.githooks"

if [ ! -d "$HOOKS_DIR" ]; then
    echo "[install-hooks] 错误: 未找到 $HOOKS_DIR" >&2
    exit 1
fi

echo "[install-hooks] 设置 core.hooksPath = $HOOKS_DIR"
git config core.hooksPath "$HOOKS_DIR"

# 给所有 hook 文件添加可执行权限 (Unix 环境需要)
for hook in "$HOOKS_DIR"/*; do
    [ -f "$hook" ] || continue
    chmod +x "$hook"
    echo "[install-hooks] 已设置可执行: $hook"
done

echo ""
echo "[install-hooks] 安装完成 ✓"
echo ""
echo "提示:"
echo "  - 钩子在 'git commit' 时自动执行"
echo "  - 跳过检查:   git commit --no-verify"
echo "  - 查看配置:   git config --get core.hooksPath"