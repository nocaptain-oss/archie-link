#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

fake_home=$out/home
fake_tmp=$out/tmp

mkdir -p \
  "$fake_home/mylogs/DevEcoStudio26.0.0.851" \
  "$fake_home/Library/Caches/Huawei/DevEcoStudio26.0/plugins" \
  "$fake_home/Library/Logs/Huawei" \
  "$fake_home/.local/share/deveco/versions/0.2.5/bin" \
  "$fake_home/.local/share/deveco/versions/0.9.9" \
  "$fake_home/.local/bin" \
  "$fake_tmp/deveco" \
  "$fake_tmp/X/com.google.Chrome.code_sign_clone/code_sign_clone.old" \
  "$fake_tmp/X/com.google.Chrome.code_sign_clone/code_sign_clone.fresh"

# 3 天前的过期项（2022-11-01）
touch -t 202211010000 "$fake_home/mylogs/DevEcoStudio26.0.0.851/old.log"
touch -t 202211010000 "$fake_home/Library/Caches/Huawei/DevEcoStudio26.0/plugins/codegenie.zip"
touch -t 202211010000 "$fake_home/Library/Logs/Huawei/idea.log"
touch -t 202211010000 "$fake_tmp/deveco/old-test.log"
touch -t 202211010000 "$fake_tmp/hdc-old.log"
touch -t 202211010000 "$fake_tmp/X/com.google.Chrome.code_sign_clone/code_sign_clone.old/chrome"
touch -t 202211010000 "$fake_tmp/X/com.google.Chrome.code_sign_clone/code_sign_clone.old"

# 保留项：今天写入的活跃文件与当前版本
touch "$fake_home/mylogs/DevEcoStudio26.0.0.851/fresh.log"
touch "$fake_tmp/deveco/fresh.log"
touch "$fake_tmp/hdc.log"
touch "$fake_tmp/X/com.google.Chrome.code_sign_clone/code_sign_clone.fresh/chrome"
ln -s "$fake_home/.local/share/deveco/versions/0.2.5/bin/deveco" "$fake_home/.local/bin/deveco"

run() {
  DEV_HYGIENE_HOME=$fake_home DEV_HYGIENE_TMP=$fake_tmp DEV_HYGIENE_VAR_BASE=$fake_tmp \
  DEV_HYGIENE_CHROME_RUNNING=0 \
    sh "$root/scripts/dev-hygiene.sh" "$@"
}

# Chrome 运行中：跳过 code_sign_clone 清理
skip_out=$(DEV_HYGIENE_HOME=$fake_home DEV_HYGIENE_TMP=$fake_tmp DEV_HYGIENE_VAR_BASE=$fake_tmp \
  DEV_HYGIENE_CHROME_RUNNING=1 sh "$root/scripts/dev-hygiene.sh" --dry-run)
echo "$skip_out" | grep -q 'skip: Chrome 正在运行'
[ -d "$fake_tmp/X/com.google.Chrome.code_sign_clone/code_sign_clone.old" ]

# dry-run：列出但不删除
dry_out=$(run --dry-run)
echo "$dry_out" | grep -q 'old-test.log'
echo "$dry_out" | grep -q 'codegenie.zip'
echo "$dry_out" | grep -q '0.9.9'
echo "$dry_out" | grep -q 'code_sign_clone.old'
[ -f "$fake_tmp/deveco/old-test.log" ]
[ -d "$fake_home/.local/share/deveco/versions/0.9.9" ]

# apply：过期项删除
run --apply >/dev/null 2>&1
[ ! -f "$fake_home/mylogs/DevEcoStudio26.0.0.851/old.log" ]
[ ! -f "$fake_home/Library/Caches/Huawei/DevEcoStudio26.0/plugins/codegenie.zip" ]
[ ! -f "$fake_home/Library/Logs/Huawei/idea.log" ]
[ ! -f "$fake_tmp/deveco/old-test.log" ]
[ ! -f "$fake_tmp/hdc-old.log" ]
[ ! -d "$fake_tmp/X/com.google.Chrome.code_sign_clone/code_sign_clone.old" ]
[ ! -d "$fake_home/.local/share/deveco/versions/0.9.9" ]

# apply：保留项完好
[ -f "$fake_home/mylogs/DevEcoStudio26.0.0.851/fresh.log" ]
[ -f "$fake_tmp/deveco/fresh.log" ]
[ -f "$fake_tmp/hdc.log" ]
[ -d "$fake_home/.local/share/deveco/versions/0.2.5" ]
[ -d "$fake_tmp/X/com.google.Chrome.code_sign_clone/code_sign_clone.fresh" ]

# apply：自身运行日志生成
[ -f "$fake_home/Library/Logs/dev-hygiene.log" ]

# 非法参数报错
if run --bogus >/dev/null 2>&1; then
  printf 'bogus arg should fail\n' >&2
  exit 1
fi

printf 'dev-hygiene: PASS\n'
