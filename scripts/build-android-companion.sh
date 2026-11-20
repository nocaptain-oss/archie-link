#!/bin/sh
set -eu

root=$(CDPATH= cd -- "$(dirname "$0")/.." && pwd)
sdk=${ANDROID_SDK_ROOT:-$HOME/Library/Caches/archie-link/android-sdk}
platform="$sdk/platforms/android-28/android.jar"
build_tools="$sdk/build-tools/35.0.0"
source_dir="$root/mobile/android-companion"
build_dir=${ARCHIE_ANDROID_BUILD_DIR:-/private/tmp/archie-android-build}
version=$(sed -n 's/.*android:versionName="\([^"]*\)".*/\1/p' "$source_dir/AndroidManifest.xml")
output=${1:-"$root/dist/android/archielink-companion-v$version-debug.apk"}

test -f "$platform"
test -x "$build_tools/aapt2"
test -x "$build_tools/apksigner"
test -x "$build_tools/zipalign"

rm -rf "$build_dir"
mkdir -p "$build_dir/classes" "$build_dir/dex" "$build_dir/lib/armeabi-v7a" "$(dirname "$output")"

CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 go build \
	-trimpath \
	-ldflags="-s -w -X golang.zx2c4.com/wireguard/ipc.socketDirectory=/data/user/0/com.archielink.companion/files/wireguard" \
	-o "$build_dir/lib/armeabi-v7a/libwireguard.so" \
	./cmd/wireguard-android

"$build_tools/aapt2" compile \
	--dir "$source_dir/res" \
	-o "$build_dir/compiled-res.zip"

"$build_tools/aapt2" link \
	-o "$build_dir/unsigned.apk" \
	-I "$platform" \
	--manifest "$source_dir/AndroidManifest.xml" \
	--min-sdk-version 28 \
	--target-sdk-version 28 \
	"$build_dir/compiled-res.zip"

openssl req -x509 -newkey rsa:2048 -nodes \
	-keyout "$build_dir/archielink-rpc-key.pem" \
	-out "$build_dir/archielink-rpc-cert.pem" \
	-days 2 \
	-subj "/CN=archielink-rpc/O=ArchieLink Debug/C=CN" \
	-addext "subjectAltName=IP:127.0.0.1,DNS:localhost" >/dev/null 2>&1
openssl pkcs12 -export -legacy \
	-inkey "$build_dir/archielink-rpc-key.pem" \
	-in "$build_dir/archielink-rpc-cert.pem" \
	-name rpc \
	-out "$build_dir/archielink-rpc.p12" \
	-passout pass:archielink-debug-rpc >/dev/null 2>&1
mkdir -p "$build_dir/assets"
cp "$build_dir/archielink-rpc.p12" "$build_dir/assets/archielink-rpc.p12"
(cd "$build_dir" && zip -q "$build_dir/unsigned.apk" assets/archielink-rpc.p12)

javac --release 8 \
	-cp "$platform" \
	-d "$build_dir/classes" \
	"$source_dir"/src/com/archielink/companion/*.java

mkdir -p "$build_dir/config-test"
javac --release 8 \
	-d "$build_dir/config-test" \
	"$source_dir/src/com/archielink/companion/WireGuardConfig.java" \
	"$source_dir/src/com/archielink/companion/Ipv4Routes.java" \
	"$source_dir/src/com/archielink/companion/VpnAppPolicy.java" \
	"$source_dir/test/com/archielink/companion/WireGuardConfigTest.java" \
	"$source_dir/test/com/archielink/companion/Ipv4RoutesTest.java" \
	"$source_dir/test/com/archielink/companion/VpnAppPolicyTest.java"
java -cp "$build_dir/config-test" com.archielink.companion.WireGuardConfigTest
java -cp "$build_dir/config-test" com.archielink.companion.Ipv4RoutesTest
java -cp "$build_dir/config-test" com.archielink.companion.VpnAppPolicyTest

"$sdk/cmdline-tools/latest/bin/d8" \
	--lib "$platform" \
	--min-api 28 \
	--output "$build_dir/dex" \
	"$build_dir"/classes/com/archielink/companion/*.class

(cd "$build_dir/dex" && zip -q -j "$build_dir/unsigned.apk" classes.dex)
(cd "$build_dir" && zip -q "$build_dir/unsigned.apk" lib/armeabi-v7a/libwireguard.so)
"$build_tools/zipalign" -f 4 "$build_dir/unsigned.apk" "$build_dir/aligned.apk"

keystore=${ARCHIE_ANDROID_DEBUG_KEYSTORE:-$HOME/Library/Caches/archie-link/android-debug.keystore}
if [ ! -f "$keystore" ]; then
	keytool -genkeypair \
		-keystore "$keystore" \
		-storepass android \
		-keypass android \
		-alias androiddebugkey \
		-keyalg RSA \
		-keysize 2048 \
		-validity 3650 \
		-dname "CN=ArchieLink Debug,O=ArchieLink,C=CN" \
		-noprompt >/dev/null 2>&1
fi

"$build_tools/apksigner" sign \
	--ks "$keystore" \
	--ks-pass pass:android \
	--key-pass pass:android \
	--ks-key-alias androiddebugkey \
	--out "$output" \
	"$build_dir/aligned.apk"

"$build_tools/apksigner" verify --verbose "$output"
printf '%s\n' "$output"
