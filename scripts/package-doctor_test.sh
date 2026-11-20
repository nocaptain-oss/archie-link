#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
out=$(mktemp -d)
trap 'rm -rf "$out"' EXIT

archive="$out/archie-link-doctor.zip"
"$root/scripts/package-doctor.sh" "$archive" >/dev/null

unzip -t "$archive" >/dev/null
unzip -l "$archive" | grep -q 'archie-link-doctor/SKILL.md'
unzip -l "$archive" | grep -q 'archie-link-doctor/agents/openai.yaml'

if unzip -l "$archive" | grep -Eq '__pycache__|\.pyc|\.DS_Store|credentials\.json|warp\.conf|report\.json|\.log'; then
	printf 'forbidden artifact found in package\n' >&2
	exit 1
fi

printf 'package-doctor: PASS\n'
