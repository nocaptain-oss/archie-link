#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

archive="$out/archie-link.zip"
"$root/scripts/package-skill.sh" "$archive" >/dev/null

unzip -t "$archive" >/dev/null
unzip -l "$archive" | grep -q 'archie-link/SKILL.md'
unzip -l "$archive" | grep -q 'archie-link/agents/openai.yaml'

if unzip -l "$archive" | grep -Eq '__pycache__|\.pyc|\.DS_Store|credentials\.json|wgcf-account\.toml|warp\.conf|snapshot\.json|runtime\.json|install-id|\.log'; then
	printf 'forbidden artifact found in package\n' >&2
	exit 1
fi

relative_work=$(mktemp -d)
(
	cd "$relative_work"
	"$root/scripts/package-skill.sh" output/archie-link.zip >/dev/null
)
test -f "$relative_work/output/archie-link.zip"
rm -rf "$relative_work"

printf 'package-skill: PASS\n'
