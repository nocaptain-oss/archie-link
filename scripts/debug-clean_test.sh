#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

fake_repo=$out/repo

mkdir -p \
  "$fake_repo/dist/android" \
  "$fake_repo/dist" \
  "$fake_repo/artifacts/harmony-companion/ui-review" \
  "$fake_repo/mobile/harmony-companion/build" \
  "$fake_repo/mobile/harmony-companion/.hvigor" \
  "$fake_repo/mobile/harmony-companion/entry/build" \
  "$fake_repo/mobile/harmony-companion/entry/.cxx" \
  "$fake_repo/mobile/harmony-companion/entry/src" \
  "$fake_repo/cmd"

# 调试痕迹
touch "$fake_repo/dist/android/archielink-companion-v0.4.0-debug.apk"
touch "$fake_repo/dist/android/archielink-companion-v0.4.0-debug.apk.idsig"
touch "$fake_repo/artifacts/harmony-companion/splash-h1.jpeg"
touch "$fake_repo/artifacts/harmony-companion/ui-review/index.html"
touch "$fake_repo/mobile/harmony-companion/build/marker"
touch "$fake_repo/mobile/harmony-companion/.hvigor/marker"
touch "$fake_repo/mobile/harmony-companion/entry/build/marker"
touch "$fake_repo/mobile/harmony-companion/entry/.cxx/marker"
touch "$fake_repo/cmd/.DS_Store"

# 保留项：发布产物与源码
touch "$fake_repo/dist/archie-link-v0.3-darwin-arm64"
touch "$fake_repo/mobile/harmony-companion/entry/src/main.ets"

run() {
  DEBUG_CLEAN_ROOT=$fake_repo sh "$root/scripts/debug-clean.sh" "$@"
}

# dry-run：列出但不删除
dry_out=$(run --dry-run 2>&1)
echo "$dry_out" | grep -q 'v0.4.0-debug.apk'
echo "$dry_out" | grep -q 'splash-h1.jpeg'
echo "$dry_out" | grep -q 'ui-review'
echo "$dry_out" | grep -q '\.hvigor'
echo "$dry_out" | grep -q '\.cxx'
echo "$dry_out" | grep -q '\.DS_Store'
[ -f "$fake_repo/dist/android/archielink-companion-v0.4.0-debug.apk" ]
[ -d "$fake_repo/mobile/harmony-companion/entry/.cxx" ]

# apply：痕迹清零
apply_out=$(run --apply 2>&1)
echo "$apply_out" | grep -q '共 '
[ ! -f "$fake_repo/dist/android/archielink-companion-v0.4.0-debug.apk" ]
[ ! -f "$fake_repo/dist/android/archielink-companion-v0.4.0-debug.apk.idsig" ]
[ ! -f "$fake_repo/artifacts/harmony-companion/splash-h1.jpeg" ]
[ ! -d "$fake_repo/artifacts/harmony-companion/ui-review" ]
[ ! -d "$fake_repo/mobile/harmony-companion/build" ]
[ ! -d "$fake_repo/mobile/harmony-companion/.hvigor" ]
[ ! -d "$fake_repo/mobile/harmony-companion/entry/build" ]
[ ! -d "$fake_repo/mobile/harmony-companion/entry/.cxx" ]
[ ! -f "$fake_repo/cmd/.DS_Store" ]

# apply：保留项完好
[ -f "$fake_repo/dist/archie-link-v0.3-darwin-arm64" ]
[ -f "$fake_repo/mobile/harmony-companion/entry/src/main.ets" ]

# 再次 apply：已干净
clean_out=$(run --apply 2>&1)
echo "$clean_out" | grep -q 'repo 已干净'

# 非法参数报错
if run --bogus >/dev/null 2>&1; then
  printf 'bogus arg should fail\n' >&2
  exit 1
fi

printf 'debug-clean: PASS\n'
