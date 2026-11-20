#!/bin/sh
set -eu

gateway=${ALIPAY_SANDBOX_PROBE_URL:-https://openapi-sandbox.dl.alipaydev.com/gateway.do}
tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/archielink-alipay-probe.XXXXXX")
trap 'rm -rf "$tmpdir"' EXIT HUP INT TERM

timestamp=$(date '+%Y-%m-%d %H:%M:%S')
status=$(
	curl -sS --max-time 25 \
		-D "$tmpdir/headers" \
		-o "$tmpdir/body" \
		-w '%{http_code}' \
		-X POST "$gateway" \
		--data-urlencode 'app_id=0000000000000000' \
		--data-urlencode 'method=alipay.trade.precreate' \
		--data-urlencode 'format=JSON' \
		--data-urlencode 'charset=utf-8' \
		--data-urlencode 'sign_type=RSA2' \
		--data-urlencode 'sign=invalid' \
		--data-urlencode "timestamp=$timestamp" \
		--data-urlencode 'version=1.0' \
		--data-urlencode 'biz_content={"out_trade_no":"archie-sandbox-probe","total_amount":"0.01","subject":"probe"}'
)

if grep -q 'alipay_trade_precreate_response' "$tmpdir/body"; then
	printf 'alipay sandbox API: PASS http=%s json_response=yes\n' "$status"
	exit 0
fi

content_type=$(awk 'BEGIN{IGNORECASE=1} /^content-type:/ {sub(/\r$/, ""); print $2; exit}' "$tmpdir/headers")
printf 'alipay sandbox API: BLOCKED http=%s content_type=%s json_response=no\n' \
	"$status" "${content_type:-unknown}"
exit 2
