#!/bin/sh
set -eu
export LC_ALL=C
export ZERO_AR_DATE=1
export SOURCE_DATE_EPOCH=0

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
output_dir=${1:-"$root/dist/v0.4"}
version=${ARCHIE_LINK_VERSION:-0.4.0-dev}
key_id=${ARCHIE_LINK_KEY_ID:-}
public_key=${ARCHIE_LINK_PUBLIC_KEY:-}
previous_key_id=${ARCHIE_LINK_PREVIOUS_KEY_ID:-}
previous_public_key=${ARCHIE_LINK_PREVIOUS_PUBLIC_KEY:-}
previous_valid_till=${ARCHIE_LINK_PREVIOUS_VALID_TILL:-}
codesign_identity=${ARCHIE_LINK_CODESIGN_IDENTITY:-}

if [ -z "$key_id" ] || [ -z "$public_key" ]; then
	printf 'ARCHIE_LINK_KEY_ID and ARCHIE_LINK_PUBLIC_KEY are required\n' >&2
	exit 2
fi
case "$version" in
	""|*[!A-Za-z0-9._+-]*)
		printf 'ARCHIE_LINK_VERSION contains unsupported characters\n' >&2
		exit 2
		;;
esac
case "$key_id" in
	""|*[!A-Za-z0-9._+-]*)
		printf 'ARCHIE_LINK_KEY_ID contains unsupported characters\n' >&2
		exit 2
		;;
esac

required_go=$(awk '$1 == "go" { print $2; exit }' "$root/go.mod")
go_version=$(go env GOVERSION)
go_version=${go_version#go}
if [ "$go_version" != "$required_go" ]; then
	printf 'Go toolchain mismatch: go.mod requires %s, current is %s\n' "$required_go" "$go_version" >&2
	exit 2
fi

commit=${ARCHIE_LINK_COMMIT:-$(git -C "$root" rev-parse HEAD)}
case "$commit" in
	""|*[!0-9a-f]*)
		printf 'ARCHIE_LINK_COMMIT must be a full lowercase git SHA\n' >&2
		exit 2
		;;
esac
dirty=false
if [ -n "$(git -C "$root" status --porcelain)" ]; then
	if [ "${ARCHIE_LINK_ALLOW_DIRTY:-0}" != "1" ]; then
		printf 'release build requires a clean worktree; set ARCHIE_LINK_ALLOW_DIRTY=1 only for local verification\n' >&2
		exit 2
	fi
	dirty=true
fi
build_id="$version+$commit"
key_fingerprint=$(printf '%s' "$public_key" | shasum -a 256 | awk '{print $1}')
host_os=$(go env GOOS)
darwin_se_arch=${ARCHIE_LINK_DARWIN_SE_ARCH:-}
if [ -z "$darwin_se_arch" ] && [ "$host_os" = "darwin" ]; then
	darwin_se_arch="amd64,arm64"
fi
case "$darwin_se_arch" in
	""|amd64|arm64|amd64,arm64|arm64,amd64) ;;
	*)
		printf 'ARCHIE_LINK_DARWIN_SE_ARCH must be empty, amd64, arm64, or both comma-separated\n' >&2
		exit 2
		;;
esac
if [ -n "$darwin_se_arch" ] && [ "$host_os" != "darwin" ]; then
	printf 'Secure Enclave builds require a macOS build host with the macOS SDK\n' >&2
	exit 2
fi
if [ "$host_os" = "darwin" ]; then
	export MACOSX_DEPLOYMENT_TARGET=12.0
fi
if [ -n "$codesign_identity" ]; then
	if [ "$host_os" != "darwin" ]; then
		printf 'ARCHIE_LINK_CODESIGN_IDENTITY requires a macOS build host\n' >&2
		exit 2
	fi
	if ! command -v codesign >/dev/null 2>&1; then
		printf 'codesign is unavailable\n' >&2
		exit 2
	fi
fi

mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)

ldflags="-s -w -X github.com/nocaptain-oss/archie-link/internal/entitlement.embeddedKeyID=$key_id"
ldflags="$ldflags -X github.com/nocaptain-oss/archie-link/internal/entitlement.embeddedPublicKey=$public_key"
ldflags="$ldflags -X github.com/nocaptain-oss/archie-link/internal/cli.Version=$version"
ldflags="$ldflags -X github.com/nocaptain-oss/archie-link/internal/cli.BuildCommit=$commit"
ldflags="$ldflags -X github.com/nocaptain-oss/archie-link/internal/cli.BuildID=$build_id"
if [ -n "$previous_key_id" ] || [ -n "$previous_public_key" ] || [ -n "$previous_valid_till" ]; then
	if [ -z "$previous_key_id" ] || [ -z "$previous_public_key" ] || [ -z "$previous_valid_till" ]; then
		printf 'previous key rotation requires ID, public key, and RFC3339 validity\n' >&2
		exit 2
	fi
	ldflags="$ldflags -X github.com/nocaptain-oss/archie-link/internal/entitlement.embeddedPreviousKeyID=$previous_key_id"
	ldflags="$ldflags -X github.com/nocaptain-oss/archie-link/internal/entitlement.embeddedPreviousPublicKey=$previous_public_key"
	ldflags="$ldflags -X github.com/nocaptain-oss/archie-link/internal/entitlement.embeddedPreviousValidTill=$previous_valid_till"
fi

build() {
	target=$1
	arch=$2
	suffix=$3
	cgo=$4
	link_flags=
	if [ "$target" = "darwin" ]; then
		link_flags="-buildid=$build_id -extldflags=-Wl,-no_uuid,-dead_strip,-dead_strip_dylibs"
	else
		link_flags="-buildid=$build_id"
	fi
	if [ "$cgo" = "1" ]; then
		if [ "$arch" = "amd64" ]; then
			clang_arch=x86_64
		else
			clang_arch=arm64
		fi
		binary="$output_dir/archielink-$target-$arch$suffix"
		link_attempt=0
		while :; do
			CGO_ENABLED=1 GOOS=$target GOARCH=$arch \
				CC="clang -arch $clang_arch" \
				CGO_CFLAGS="-arch $clang_arch -mmacosx-version-min=12.0" \
				CGO_LDFLAGS="-arch $clang_arch -mmacosx-version-min=12.0" \
				GOTOOLCHAIN=local GOFLAGS=-mod=readonly go build \
				-buildvcs=false \
				-trimpath \
				-ldflags "$ldflags $link_flags" \
				-o "$binary" \
				"$root/cmd/archielink"
			if ! otool -L "$binary" | grep -q '/usr/lib/libobjc.A.dylib'; then
				break
			fi
			link_attempt=$((link_attempt+1))
			if [ "$link_attempt" -ge 20 ]; then
				printf 'Apple ld64 keeps emitting the nondeterministic libobjc.A.dylib dependent for darwin/%s after %d links; if libobjc became a real dependency, revisit this check\n' "$arch" "$link_attempt" >&2
				exit 1
			fi
		done
	else
		CGO_ENABLED=0 GOOS=$target GOARCH=$arch GOTOOLCHAIN=local GOFLAGS=-mod=readonly go build \
			-buildvcs=false \
			-trimpath \
			-ldflags "$ldflags $link_flags" \
			-o "$output_dir/archielink-$target-$arch$suffix" \
			"$root/cmd/archielink"
	fi
}

case ",$darwin_se_arch," in
	*,amd64,*) build darwin amd64 "" 1 ;;
	*) build darwin amd64 "" 0 ;;
esac
case ",$darwin_se_arch," in
	*,arm64,*) build darwin arm64 "" 1 ;;
	*) build darwin arm64 "" 0 ;;
esac
build windows amd64 ".exe" 0
build windows arm64 ".exe" 0
GOOS=linux GOARCH=amd64 CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-mod=readonly go build \
	-buildvcs=false \
	-trimpath \
	-ldflags "-s -w -buildid=$build_id" \
	-o "$output_dir/controlplane-linux-amd64" \
	"$root/cmd/controlplane"
cp "$root/THIRD_PARTY_NOTICES.md" "$output_dir/THIRD_PARTY_NOTICES.md"

for binary in "$output_dir"/archielink-* "$output_dir/controlplane-linux-amd64"; do
	if strings "$binary" | grep -Eq 'ARCHIE_LINK_TEST_LICENSE|ARCHIE_LINK_PUBLIC_KEY|ARCHIE_LINK_FAULT'; then
		printf 'release binary contains a test or fault-injection marker: %s\n' "$binary" >&2
		exit 1
	fi
done

if [ -n "$codesign_identity" ]; then
	for binary in "$output_dir/archielink-darwin-amd64" "$output_dir/archielink-darwin-arm64"; do
		codesign --force --identifier com.nocaptain.archielink \
			--options runtime --timestamp \
			--sign "$codesign_identity" "$binary"
		codesign --verify --strict "$binary"
	done
fi

(
	cd "$output_dir"
	shasum -a 256 archielink-darwin-amd64 archielink-darwin-arm64 \
		archielink-windows-amd64.exe archielink-windows-arm64.exe \
		controlplane-linux-amd64 THIRD_PARTY_NOTICES.md > SHA256SUMS
)

components_file=$(mktemp)
trap 'rm -f "$components_file"' EXIT HUP INT TERM
go list -mod=readonly -m -f '{{if not .Main}}{{.Path}}|{{.Version}}{{end}}' all > "$components_file"
components=
while IFS='|' read -r module_path module_version; do
	[ -n "$module_path" ] || continue
	entry=$(printf '    {"type":"library","name":"%s","version":"%s"}' "$module_path" "$module_version")
	if [ -n "$components" ]; then
		components="$components,
$entry"
	else
		components=$entry
	fi
done < "$components_file"

{
	printf '{\n'
	printf '  "bomFormat": "CycloneDX",\n'
	printf '  "specVersion": "1.6",\n'
	printf '  "version": 1,\n'
	printf '  "metadata": {\n'
	printf '    "component": {\n'
	printf '      "type": "application",\n'
	printf '      "name": "archielink",\n'
	printf '      "version": "%s",\n' "$version"
	printf '      "bom-ref": "pkg:golang/github.com/nocaptain-oss/archie-link@%s"\n' "$version"
	printf '    },\n'
	printf '    "properties": [\n'
	printf '      {"name": "go.version", "value": "%s"},\n' "$go_version"
	printf '      {"name": "git.commit", "value": "%s"},\n' "$commit"
	printf '      {"name": "trust.key_id", "value": "%s"},\n' "$key_id"
	printf '      {"name": "trust.key_fingerprint_sha256", "value": "%s"}\n' "$key_fingerprint"
	printf '    ]\n'
	printf '  },\n'
	printf '  "components": [\n'
	if [ -n "$components" ]; then
		printf '%s\n' "$components"
	fi
	printf '  ]\n'
	printf '}\n'
} > "$output_dir/sbom.cdx.json"

file_size() {
	wc -c < "$1" | tr -d ' '
}

file_sha() {
	shasum -a 256 "$1" | awk '{print $1}'
}

darwin_se_build() {
	case ",$darwin_se_arch," in
		*",$1,"*) printf true ;;
		*) printf false ;;
	esac
}

{
	printf '{\n'
	printf '  "schema_version": "1",\n'
	printf '  "product": "archielink",\n'
	printf '  "version": "%s",\n' "$version"
	printf '  "build_id": "%s",\n' "$build_id"
	printf '  "commit": "%s",\n' "$commit"
	printf '  "dirty": %s,\n' "$dirty"
	if [ -n "$codesign_identity" ]; then
		printf '  "codesign": {"signed": true, "identity": "%s", "hardened_runtime": true, "notarized": false},\n' "$codesign_identity"
	else
		printf '  "codesign": {"signed": false, "identity": "", "hardened_runtime": false, "notarized": false},\n'
	fi
	printf '  "go_version": "%s",\n' "$go_version"
	printf '  "build_tags": [],\n'
	printf '  "build_flags": ["-trimpath", "-buildvcs=false", "linker:-buildid=<build_id>", "darwin:-extldflags=-Wl,-no_uuid,-dead_strip,-dead_strip_dylibs", "ZERO_AR_DATE=1", "SOURCE_DATE_EPOCH=0"],\n'
	printf '  "trust_key_id": "%s",\n' "$key_id"
	printf '  "trust_key_fingerprint_sha256": "%s",\n' "$key_fingerprint"
	printf '  "darwin_secure_enclave_arch": "%s",\n' "$darwin_se_arch"
	printf '  "third_party_notices_sha256": "%s",\n' "$(file_sha "$output_dir/THIRD_PARTY_NOTICES.md")"
	printf '  "artifacts": [\n'
	printf '    {"name": "archielink-darwin-amd64", "goos": "darwin", "goarch": "amd64", "secure_enclave_build": %s, "size": %s, "sha256": "%s"},\n' \
		"$(darwin_se_build amd64)" \
		"$(file_size "$output_dir/archielink-darwin-amd64")" "$(file_sha "$output_dir/archielink-darwin-amd64")"
	printf '    {"name": "archielink-darwin-arm64", "goos": "darwin", "goarch": "arm64", "secure_enclave_build": %s, "size": %s, "sha256": "%s"},\n' \
		"$(darwin_se_build arm64)" \
		"$(file_size "$output_dir/archielink-darwin-arm64")" "$(file_sha "$output_dir/archielink-darwin-arm64")"
	printf '    {"name": "archielink-windows-amd64.exe", "goos": "windows", "goarch": "amd64", "size": %s, "sha256": "%s"},\n' \
		"$(file_size "$output_dir/archielink-windows-amd64.exe")" "$(file_sha "$output_dir/archielink-windows-amd64.exe")"
	printf '    {"name": "archielink-windows-arm64.exe", "goos": "windows", "goarch": "arm64", "size": %s, "sha256": "%s"},\n' \
		"$(file_size "$output_dir/archielink-windows-arm64.exe")" "$(file_sha "$output_dir/archielink-windows-arm64.exe")"
	printf '    {"name": "controlplane-linux-amd64", "goos": "linux", "goarch": "amd64", "size": %s, "sha256": "%s"}\n' \
		"$(file_size "$output_dir/controlplane-linux-amd64")" "$(file_sha "$output_dir/controlplane-linux-amd64")"
	printf '  ]\n'
	printf '}\n'
} > "$output_dir/release-manifest.json"

printf '%s\n' "$output_dir"
