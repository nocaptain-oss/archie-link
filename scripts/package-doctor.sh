#!/bin/sh
set -eu

if [ "$#" -ne 1 ]; then
	printf 'usage: %s OUTPUT.zip\n' "$0" >&2
	exit 2
fi

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
output=$1
output_dir=$(CDPATH= cd -- "$(dirname "$output")" && pwd)
output="$output_dir/$(basename "$output")"
stage=$(mktemp -d)
trap 'rm -rf "$stage"' EXIT

mkdir -p "$stage/archie-link-doctor"
cp -R "$root/skills/archie-link-doctor/." "$stage/archie-link-doctor/"

find "$stage" -name '__pycache__' -type d -prune -exec rm -rf {} +
command -v xattr >/dev/null 2>&1 && xattr -cr "$stage"
find "$stage" -type f \( \
	-name '*.pyc' -o \
	-name '.DS_Store' -o \
	-name '*.zip' -o \
	-name 'credentials.json' -o \
	-name 'report.json' -o \
	-name '*.log' \
	\) -delete

rm -f "$output"
(
	cd "$stage"
	zip -Xqr "$output" archie-link-doctor
)
printf '%s\n' "$output"
