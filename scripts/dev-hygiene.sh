#!/bin/sh
# dev-hygiene.sh — 每日开发环境防膨胀治理。
#
# 四个动作：
#   1. 清理超过 3 天的调试/IDE 日志（mylogs、$TMPDIR/deveco、Logs/Huawei、hdc*.log）
#   2. 清理 IDE 安装后遗留的插件 zip（~/Library/Caches/Huawei/**/plugins/*.zip）
#   3. deveco CLI 替代式安装：versions/ 下仅保留 symlink 指向的当前版本
#   4. Chrome 未运行时清理超过 7 天的 code_sign_clone 陈旧克隆
#
# 用法：dev-hygiene.sh [--dry-run|--apply]
#   --dry-run（默认）只输出将执行的动作，不删除任何文件
#   --apply 实际执行，运行摘要写入 ~/Library/Logs/dev-hygiene.log（超 1M 自截断）
#
# 由 ~/Library/LaunchAgents/com.nocaptain.dev-hygiene.plist 每日调用。
# 测试环境可用 DEV_HYGIENE_HOME / DEV_HYGIENE_TMP 覆盖家目录与临时目录。

set -eu

MODE=${1:---dry-run}
case "$MODE" in
  --dry-run|--apply) ;;
  *)
    printf 'usage: %s [--dry-run|--apply]\n' "$0" >&2
    exit 2
    ;;
esac

HOME_DIR=${DEV_HYGIENE_HOME:-$HOME}
if [ -n "${DEV_HYGIENE_TMP:-}" ]; then
  TMP_ROOT=$DEV_HYGIENE_TMP
else
  # launchd 环境没有 TMPDIR，必须用 getconf 解析用户临时目录
  TMP_ROOT=$(getconf DARWIN_USER_TEMP_DIR 2>/dev/null || printf '%s' "${TMPDIR:-/tmp}")
fi
# Chrome 的 X 目录与 T 平级（var/folders 基目录下），不在 T 内
if [ -n "${DEV_HYGIENE_VAR_BASE:-}" ]; then
  VAR_BASE=$DEV_HYGIENE_VAR_BASE
else
  VAR_BASE=$(dirname "$TMP_ROOT")
fi

SELF_LOG="$HOME_DIR/Library/Logs/dev-hygiene.log"
LOG_DAYS=3
ZIP_DAYS=1
CLONE_DAYS=7

DRY=1
[ "$MODE" = "--apply" ] && DRY=0

say() { printf '[dev-hygiene] %s\n' "$*"; }

delete_paths() {
  while IFS= read -r p; do
    [ -n "$p" ] || continue
    if [ "$DRY" = 1 ]; then
      say "dry-run: would remove $p"
    else
      rm -rf "$p"
      say "removed: $p"
    fi
  done
}

action_logs() {
  say "== 1/4 清理 ${LOG_DAYS} 天前日志 =="
  {
    if [ -d "$HOME_DIR/mylogs" ]; then
      find "$HOME_DIR/mylogs" -type f -name "*.log" -mtime +"$LOG_DAYS"
    fi
    if [ -d "$HOME_DIR/Library/Logs/Huawei" ]; then
      find "$HOME_DIR/Library/Logs/Huawei" -type f -mtime +"$LOG_DAYS"
    fi
    if [ -d "$TMP_ROOT/deveco" ]; then
      find "$TMP_ROOT/deveco" -maxdepth 1 -type f -name "*.log" -mtime +"$LOG_DAYS"
    fi
    if [ -d "$TMP_ROOT" ]; then
      find "$TMP_ROOT" -maxdepth 1 -type f -name "hdc*.log" -mtime +"$LOG_DAYS"
    fi
  } | delete_paths
}

action_plugin_zips() {
  say "== 2/4 清理 IDE 已安装插件的遗留 zip（>${ZIP_DAYS} 天）=="
  if [ -d "$HOME_DIR/Library/Caches/Huawei" ]; then
    find "$HOME_DIR/Library/Caches/Huawei" -type f -path "*/plugins/*.zip" -mtime +"$ZIP_DAYS" | delete_paths
  else
    say "skip: ~/Library/Caches/Huawei 不存在"
  fi
}

action_deveco_versions() {
  say "== 3/4 deveco 替代式安装：清理被取代版本 =="
  link="$HOME_DIR/.local/bin/deveco"
  versions_dir="$HOME_DIR/.local/share/deveco/versions"
  if [ ! -L "$link" ] || [ ! -d "$versions_dir" ]; then
    say "skip: deveco symlink 或 versions 目录缺失"
    return
  fi
  current=$(readlink "$link")
  current_dir=$(dirname "$(dirname "$current")")
  for d in "$versions_dir"/*/; do
    [ -d "$d" ] || continue
    if [ "${d%/}" != "$current_dir" ]; then
      printf '%s\n' "$d" | delete_paths
    fi
  done
}

chrome_running() {
  # DEV_HYGIENE_CHROME_RUNNING=0/1 供测试隔离宿主机 Chrome 状态
  if [ -n "${DEV_HYGIENE_CHROME_RUNNING:-}" ]; then
    [ "$DEV_HYGIENE_CHROME_RUNNING" = 0 ] && return 1
    return 0
  fi
  pgrep -x "Google Chrome" >/dev/null 2>&1
}

action_chrome_clones() {
  say "== 4/4 Chrome 陈旧 code_sign_clone（>${CLONE_DAYS} 天）=="
  if chrome_running; then
    say "skip: Chrome 正在运行，下次再清"
    return
  fi
  clone_root="$VAR_BASE/X/com.google.Chrome.code_sign_clone"
  if [ -d "$clone_root" ]; then
    find "$clone_root" -maxdepth 1 -type d -name "code_sign_clone.*" -mtime +"$CLONE_DAYS" | delete_paths
  else
    say "skip: 克隆目录不存在"
  fi
}

run_actions() {
  say "mode=$MODE home=$HOME_DIR tmp=$TMP_ROOT"
  action_logs
  action_plugin_zips
  action_deveco_versions
  action_chrome_clones
  say "done"
}

if [ "$DRY" = 0 ]; then
  mkdir -p "$(dirname "$SELF_LOG")"
  # 自轮转：超 1M 截断
  if [ -f "$SELF_LOG" ] && [ "$(wc -c < "$SELF_LOG")" -gt 1048576 ]; then
    : > "$SELF_LOG"
  fi
  {
    printf '=== %s ===\n' "$(date '+%Y-%m-%d %H:%M:%S')"
    run_actions
  } >> "$SELF_LOG" 2>&1
else
  run_actions
fi
