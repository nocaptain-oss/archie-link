#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
	printf 'usage: %s <output.zip>\n' "$0" >&2
	exit 2
fi

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
output_dir=$(dirname "$1")
mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)
output="$output_dir/$(basename "$1")"

stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

mkdir -p "$stage/archie-link"
cp -R "$root/skills/archie-link/." "$stage/archie-link/"

find "$stage" -name '__pycache__' -type d -prune -exec rm -rf {} +
command -v xattr >/dev/null 2>&1 && xattr -cr "$stage"
find "$stage" -type f \( \
	-name '.DS_Store' -o \
	-name 'credentials.json' -o \
	-name 'wgcf-account.toml' -o \
	-name 'warp.conf' -o \
	-name 'snapshot.json' -o \
	-name 'runtime.json' -o \
	-name 'install-id' -o \
	-name '*.log' \
\) -delete

rm -f "$output"
(
	cd "$stage"
	zip -Xqr "$output" archie-link
)
printf '%s\n' "$output"
