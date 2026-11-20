#!/bin/sh
# debug-clean.sh — 调试结束后的 repo 痕迹清零。
#
# 清理范围（均为可重建的过程文件，发布产物 dist/v0.* 不在范围内）：
#   - dist/android/ 下的 debug APK 与 .idsig
#   - artifacts/harmony-companion/ 下的 UI 审查过程文件
#   - mobile/harmony-companion/ 的构建产物（build/.hvigor/entry 的 build/.cxx）
#   - 全 repo .DS_Store
#
# 用法：debug-clean.sh [--dry-run|--apply]
#   --dry-run（默认）只列出将删除的路径
#   --apply 实际执行删除
#
# 测试环境可用 DEBUG_CLEAN_ROOT 覆盖目标 repo 根目录。

set -eu

MODE=${1:---dry-run}
case "$MODE" in
  --dry-run|--apply) ;;
  *)
    printf 'usage: %s [--dry-run|--apply]\n' "$0" >&2
    exit 2
    ;;
esac

ROOT=${DEBUG_CLEAN_ROOT:-$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)}

DRY=1
[ "$MODE" = "--apply" ] && DRY=0

say() { printf '[debug-clean] %s\n' "$*" >&2; }

# 从 stdin 读路径逐行删除；动作日志走 stderr，stdout 只输出计数。
delete_paths() {
  count=0
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    if [ "$DRY" = 1 ]; then
      say "dry-run: would remove $p"
    else
      rm -rf "$p"
      say "removed: $p"
    fi
    count=$((count + 1))
  done
  printf '%s\n' "$count"
}

say "mode=$MODE root=$ROOT"
say "== repo 调试痕迹清单 =="

total=$(
  {
    if [ -d "$ROOT/dist/android" ]; then
      find "$ROOT/dist/android" -maxdepth 1 -type f \( -name "*-debug.apk" -o -name "*-debug.apk.idsig" \)
    fi
    if [ -d "$ROOT/artifacts/harmony-companion" ]; then
      find "$ROOT/artifacts/harmony-companion" -maxdepth 1 \( -name "ui-review" -o -name "splash-h1.jpeg" \)
    fi
    if [ -d "$ROOT/mobile/harmony-companion" ]; then
      find "$ROOT/mobile/harmony-companion" -maxdepth 1 \( -name "build" -o -name ".hvigor" \)
    fi
    if [ -d "$ROOT/mobile/harmony-companion/entry" ]; then
      find "$ROOT/mobile/harmony-companion/entry" -maxdepth 1 \( -name "build" -o -name ".cxx" \)
    fi
    find "$ROOT" -name ".DS_Store" -not -path "*/.git/*"
  } | delete_paths
)

if [ "$total" = 0 ]; then
  say "repo 已干净，无需清理"
else
  say "共 $total 项"
  if [ "$DRY" = 1 ]; then
    say "确认无误后执行: $0 --apply"
  fi
fi
