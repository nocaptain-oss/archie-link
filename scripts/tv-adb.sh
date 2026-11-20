#!/bin/sh
# tv-adb.sh —— 海信 TV adb 自愈封装
#
# 背景：伴侣 VPN 建立瞬间 Hisense ROM 会闪断 adbd 的 TCP 连接（error: closed），
# 手动 adb disconnect/connect 可恢复但每次都要人工干预。
# 本封装在失败时自动重连重试，调用方无感。
#
# 用法：
#   sh scripts/tv-adb.sh shell "pm list packages"
#   sh scripts/tv-adb.sh install -r /tmp/xxx.apk
#   sh scripts/tv-adb.sh shell am force-stop com.xxx

TV_SERIAL="${TV_SERIAL:-192.168.1.134:5555}"
MAX_RETRY=3

tv_connect() {
  adb disconnect "$TV_SERIAL" >/dev/null 2>&1
  sleep 1
  adb connect "$TV_SERIAL" >/dev/null 2>&1
  sleep 1
}

# 首次确保连接
adb get-state >/dev/null 2>&1 || tv_connect

i=0
while [ "$i" -lt "$MAX_RETRY" ]; do
  i=$((i + 1))
  out="$(adb -s "$TV_SERIAL" "$@" 2>&1)"
  rc=$?
  if [ "$rc" -eq 0 ]; then
    [ -n "$out" ] && printf '%s\n' "$out"
    exit 0
  fi
  echo "[tv-adb] 第${i}次失败(rc=$rc): $(printf '%s' "$out" | head -1)" >&2
  tv_connect
done

echo "[tv-adb] 重试${MAX_RETRY}次后仍失败，退出码 $rc" >&2
exit "$rc"
