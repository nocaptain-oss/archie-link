#!/bin/sh
# notarize-release.sh — 将已签名的 Darwin 产物提交 Apple 公证并等待结果。
#
# 前置条件（缺一不可）：
#   1. Apple Developer 账号已开通，本机钥匙串已存公证凭据：
#      xcrun notarytool store-credentials <profile> --apple-id <id> --team-id <team> --password <app-specific-password>
#   2. release 目录由 build-release.sh 以 ARCHIE_LINK_CODESIGN_IDENTITY 签名产出
#      （manifest 的 codesign.signed 必须为 true，产物须带 Hardened Runtime）。
#
# 用法：
#   ARCHIE_LINK_NOTARY_PROFILE=<profile> sh scripts/notarize-release.sh <release-dir>
#
# 说明：
#   - 公证提交的是 Darwin 产物 zip；Apple 返回 Accepted 后，
#     Gatekeeper 首次运行会在线校验票据（裸 Mach-O 不支持 stapler）。
#   - 公证不会修改产物字节；SHA256SUMS 仍然有效。
#   - 失败时打印完整 JSON 诊断并以非零退出，不静默吞错。

set -eu

if [ "$#" -ne 1 ]; then
	printf 'usage: %s <release-dir>\n' "$0" >&2
	exit 2
fi
profile=${ARCHIE_LINK_NOTARY_PROFILE:-}
if [ -z "$profile" ]; then
	printf 'ARCHIE_LINK_NOTARY_PROFILE is required (xcrun notarytool store-credentials)\n' >&2
	exit 2
fi
if ! command -v xcrun >/dev/null 2>&1; then
	printf 'xcrun is unavailable; notarization requires macOS with Xcode command line tools\n' >&2
	exit 2
fi

release_dir=$(CDPATH= cd -- "$1" && pwd)
manifest="$release_dir/release-manifest.json"
if [ ! -f "$manifest" ]; then
	printf 'release-manifest.json not found in %s; build with scripts/build-release.sh first\n' "$release_dir" >&2
	exit 2
fi
if ! grep -q '"signed": true' "$manifest"; then
	printf 'release is not codesigned; rebuild with ARCHIE_LINK_CODESIGN_IDENTITY before notarizing\n' >&2
	exit 2
fi

version=$(sed -n 's/.*"version": "\([^"]*\)".*/\1/p' "$manifest" | head -1)
commit=$(sed -n 's/.*"commit": "\([^"]*\)".*/\1/p' "$manifest" | head -1)
zip_name="archielink-darwin-$version+$commit.zip"

for binary in archielink-darwin-amd64 archielink-darwin-arm64; do
	if [ ! -f "$release_dir/$binary" ]; then
		printf 'missing artifact: %s\n' "$release_dir/$binary" >&2
		exit 2
	fi
	if ! codesign --verify --strict "$release_dir/$binary" >/dev/null 2>&1; then
		printf 'signature verification failed: %s\n' "$binary" >&2
		exit 2
	fi
	if ! codesign -d -v "$release_dir/$binary" 2>&1 | grep -q 'flags=.*runtime'; then
		printf 'Hardened Runtime is not enabled: %s\n' "$binary" >&2
		exit 2
	fi
done

workdir=$(mktemp -d "${TMPDIR:-/tmp}/archielink-notarize.XXXXXX")
trap 'rm -rf "$workdir"' EXIT HUP INT TERM
mkdir "$workdir/darwin"
cp "$release_dir/archielink-darwin-amd64" "$release_dir/archielink-darwin-arm64" "$workdir/darwin/"
ditto -c -k --keepParent "$workdir/darwin" "$workdir/$zip_name"

submission=$(xcrun notarytool submit "$workdir/$zip_name" --keychain-profile "$profile" --wait --format json 2>"$workdir/submit.err") || {
	cat "$workdir/submit.err" >&2
	printf 'notarytool submit failed\n' >&2
	exit 1
}

printf '%s\n' "$submission"
status=$(printf '%s' "$submission" | sed -n 's/.*"status": *"\([^"]*\)".*/\1/p' | tail -1)
case "$status" in
	Accepted)
		printf 'notarization accepted for %s\n' "$zip_name"
		;;
	*)
		printf 'notarization did not complete: status=%s\n' "$status" >&2
		exit 1
		;;
esac
