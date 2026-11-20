#!/bin/sh
# cleanroom-rebuild.sh — 在干净用户环境下从零重建 release，用于双环境可复现验证。
#
# 设计：以另一个 macOS 用户账户运行（独立 $HOME 与全部 Go 缓存），
# 从源仓库或 git bundle 克隆固定 commit，用全新 GOCACHE/GOMODCACHE/GOPATH/GOTMPDIR
# 重建，输出目录与主账户构建逐字节比对（scripts/verify-release-reproducible.sh）。
#
# 推荐用 git bundle 作源：主账户家目录通常对其他用户不可穿越（drwxr-x---），
# bundle 放 /tmp 后干净账户无需读主账户任何路径。
#
# 主账户准备（无需 sudo）：
#   git bundle create /tmp/archie-cleanroom.bundle <branch>
#   cp scripts/cleanroom-rebuild.sh /tmp/
#
# 干净账户侧调用（需要管理员 sudo，交互式执行）：
#   sudo -u archie-build -H sh /tmp/cleanroom-rebuild.sh \
#     /tmp/archie-cleanroom.bundle /tmp/archielink-cleanroom \
#     <key-id> <public-key> <version>
#
# 主账户需提前用相同 key-id/public-key/version 与同一 commit 完成一次构建。
# 本脚本不改产物字节；两次构建的 release-manifest.json 必须记录同一 commit。

set -eu

if [ "$#" -lt 4 ] || [ "$#" -gt 5 ]; then
	printf 'usage: %s <source-repo-or-bundle> <output-dir> <key-id> <public-key> [version]\n' "$0" >&2
	exit 2
fi
source=$1
output_dir=$2
key_id=$3
public_key=$4
version=${5:-0.4.0-dev}

# sudo 的安全 PATH 不含 /usr/local/bin；go 必须自行找回
if ! command -v go >/dev/null 2>&1; then
	for candidate in /usr/local/bin/go /opt/homebrew/bin/go /usr/local/go/bin/go; do
		if [ -x "$candidate" ]; then
			PATH="$(dirname "$candidate"):$PATH"
			export PATH
			break
		fi
	done
fi
if ! command -v go >/dev/null 2>&1; then
	printf 'go is not in PATH and not found in known locations\n' >&2
	exit 2
fi
if ! command -v git >/dev/null 2>&1; then
	printf 'git is not in PATH\n' >&2
	exit 2
fi

if [ -d "$source/.git" ]; then
	src_repo=$(CDPATH= cd -- "$source" && pwd)
	# 源仓库属主可能是主账户；在干净账户下读取会触发 git 的 dubious ownership 拦截。
	# 用一次性配置按次放行，不污染干净账户的全局 git 配置。
	export GIT_CONFIG_COUNT=1
	export GIT_CONFIG_KEY_0=safe.directory
	export GIT_CONFIG_VALUE_0="$src_repo"
	if [ -n "$(git -C "$src_repo" status --porcelain)" ]; then
		printf 'source worktree is dirty; commit or stash first\n' >&2
		exit 2
	fi
	commit=$(git -C "$src_repo" rev-parse HEAD)
elif [ -f "$source" ]; then
	# bundle 模式：单一 ref，commit 从 bundle 头解析
	commit=$(git bundle list-heads "$source" | awk 'NR==1{print $1}')
	if [ -z "$commit" ]; then
		printf 'bundle has no refs: %s\n' "$source" >&2
		exit 2
	fi
	src_repo=$source
else
	printf 'source is neither a git repository nor a bundle: %s\n' "$source" >&2
	exit 2
fi
case "$commit" in
	""|*[!0-9a-f]*)
		printf 'failed to resolve source commit\n' >&2
		exit 2
		;;
esac

home_dir=${HOME:-}
if [ -z "$home_dir" ] || [ "$home_dir" = "/" ]; then
	printf 'refusing to run without a per-user HOME\n' >&2
	exit 2
fi
workdir=$(mktemp -d "$home_dir/archielink-cleanroom.XXXXXX")
# Go module cache 落盘为只读目录，先恢复可写再删除
cleanup_workdir() {
	chmod -R u+w "$workdir" 2>/dev/null || true
	rm -rf "$workdir"
}
trap cleanup_workdir EXIT HUP INT TERM

git clone --quiet "$src_repo" "$workdir/repo"
clone_dir="$workdir/repo"
git -C "$clone_dir" checkout --quiet "$commit" 2>/dev/null || true

required_go=$(awk '$1 == "go" { print $2; exit }' "$clone_dir/go.mod")
go_version=$(go env GOVERSION)
go_version=${go_version#go}
if [ "$go_version" != "$required_go" ]; then
	printf 'Go toolchain mismatch: go.mod requires %s, current is %s\n' "$required_go" "$go_version" >&2
	exit 2
fi

mkdir -p "$workdir/gocache" "$workdir/gomodcache" "$workdir/gopath" "$workdir/gotmp"
export GOCACHE="$workdir/gocache"
export GOMODCACHE="$workdir/gomodcache"
export GOPATH="$workdir/gopath"
export GOTMPDIR="$workdir/gotmp"
export GOFLAGS=-mod=readonly
export GOTOOLCHAIN=local
export CCACHE_DISABLE=1

mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)
cd "$clone_dir"
ARCHIE_LINK_KEY_ID="$key_id" \
ARCHIE_LINK_PUBLIC_KEY="$public_key" \
ARCHIE_LINK_VERSION="$version" \
ARCHIE_LINK_COMMIT="$commit" \
	sh scripts/build-release.sh "$output_dir"

printf 'cleanroom build done: commit=%s output=%s\n' "$commit" "$output_dir"
