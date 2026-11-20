#!/bin/sh
set -eu
export LC_ALL=C

if [ "$#" -ne 2 ]; then
	printf 'usage: %s <release-dir-a> <release-dir-b>\n' "$0" >&2
	exit 2
fi

left=$(CDPATH= cd -- "$1" && pwd)
right=$(CDPATH= cd -- "$2" && pwd)

for dir in "$left" "$right"; do
	(
		cd "$dir"
		shasum -a 256 -c SHA256SUMS
	)
done

for name in SHA256SUMS sbom.cdx.json release-manifest.json THIRD_PARTY_NOTICES.md; do
	if ! cmp -s "$left/$name" "$right/$name"; then
		printf 'release metadata differs: %s\n' "$name" >&2
		exit 1
	fi
done

printf 'reproducible release: PASS\n'
