---
name: archie-link
description: "Safely diagnose and establish a Cloudflare WARP/VPN fallback with snapshots, coexistence checks, health verification, deterministic rollback, and optional paid activation. Use when the user explicitly asks to connect, repair foreign network access, or restore a failed connection. 中文触发场景：外网访问、科学上网、连不上外网、帮我连外网、Google 打不开、YouTube 连不上、GitHub 连接失败、修复网络、恢复外网连接、免费VPN、开通VPN、VPN 连不上、VPN 修复、VPN 激活、VPN专家。"
metadata:
  short-description: "Safe network fallback with automatic rollback"
  triggers:
    - 外网访问
    - 科学上网
    - 连不上外网
    - 帮我连外网
    - Google 打不开
    - YouTube 连不上
    - GitHub 连接失败
    - 修复网络
    - 恢复外网连接
    - 免费VPN
    - 开通VPN
    - VPN 连不上
    - VPN 修复
    - VPN 激活
    - VPN专家
---

# ArchieLink

Use the installed `archielink` binary. The CLI is the only source of truth for network state and success.

## Prerequisites: the `archielink` CLI

Check first: `command -v archielink`. This skill ships without the binary. If missing, install from the official GitHub repository before any skill step:

1. Download the platform binary from <https://github.com/nocaptain-oss/archie-link/releases> (v1.1.0+): `archielink-darwin-arm64` / `archielink-darwin-amd64` / `archielink-windows-amd64.exe` / `archielink-windows-arm64.exe`. Linux: build from source, `go build ./cmd/archielink`.
2. Verify against the Release's `SHA256SUMS`: `shasum -a 256 -c --ignore-missing SHA256SUMS`.
3. Put it on `PATH` with exec permission (`chmod +x`).
4. macOS Gatekeeper may block the unsigned binary: right-click → Open, or `xattr -d com.apple.quarantine <file>`. Windows SmartScreen: "Run anyway".

No other download source. Full install guide: <https://github.com/nocaptain-oss/archie-link#安装>. Do not proceed until `archielink` is on `PATH`.

## Related skill: archie-link-doctor

`archie-link-doctor` is the read-only subset of this skill: the same `detect`/`plan`/`doctor`/`coexist` checks, no network writes, no activation. Use it when the user asks for diagnosis only (网络体检/网络排查); use this skill when they ask to connect, repair, or restore (连接/修复/恢复). If it is not installed, it comes from the same GitHub repository and SkillHub (`archie-link-doctor`).

## Ownership

- The root agent owns the global network lock. Never run `up`/`down`/`rollback` concurrently; never hand tunnel control to a subagent.
- Never edit DNS, routes, proxies, or local DNS files yourself. Rule: never kill wireguard-go directly.

## Before changing the network

```bash
archielink detect --json
archielink plan --json
archielink coexist gate --json
archielink status --json
```

Explain the observed problem and the proposed change. Start a connection only after the user explicitly confirms network modification.

## Paid activation

Three products, paid by Alipay QR code rendered in the terminal (no browser jump):

| Plan | Price | Entitlement |
|---|---|---|
| `activation-task` | CNY 19.90 | 14-day window (7+7 days), 1 device, no auto-renew |
| `perpetual` | CNY 199 | personal perpetual, 2 devices, `export_private` |
| `family-annual-12` | CNY 69/year | 12-device family pool, `export_private` |

```bash
archielink buy <plan> --request-id <stable-request-id> --server https://api.uzphoto.com --json
```

The CLI renders an Alipay QR code in the terminal; the user scans it with the Alipay app (扫一扫). The CLI polls the order and activates the lease once paid. If the QR does not render, give the user the printed `pay_url` to open manually. Never build payment cryptography. Orders bind `install_id` + `device_public_key`; never reuse a request ID across devices.

## Start

```bash
archielink up --yes --json
```

`--yes` is not a substitute for human confirmation; never add it to exploratory or diagnostic requests. Only exit code `0` with `connected=true` and `healthy=true` means the connection is active. On failure, read the error code, then:

```bash
archielink rollback --json
```

## Status and stop

```bash
archielink status --json
archielink down --json
```

`status` never modifies the network. `down` removes only ArchieLink-owned resources and restores the snapshot.

## Exit codes

| Code | Meaning | Agent action |
|---:|---|---|
| 0 | Success | Report result |
| 2 | Bad arguments / missing `--yes` | Fix the command; no confirmation bypass |
| 3 | Platform unsupported | Stop automatic setup |
| 4 | Entitlement missing/expired/revoked | No network changes |
| 5 | Preflight/coexistence/snapshot/probe blocked | Explain findings; improvise no writes |
| 6 | Startup failed, rolled back | Report restored state |
| 7 | Health failed, rolled back | Report state and failed check |
| 8 | Rollback incomplete | Give manual recovery warning |
| 9 | Another process holds the lock | Do not retry concurrently |

## Modes

- `auto`: ArchieLink may run the guarded lifecycle after confirmation.
- `guided`: explain the missing privilege or unsupported network state; run no elevation or network-write commands.
- `unsupported`: stop and recommend another host; never claim a tunnel is connected.

## File reports

`detect` and `doctor` accept `--output <path>`: the JSON report is written to a 0600 file (existing files are never overwritten), byte-identical to `--json` stdout.

## iOS devices (zero-companion track)

No bridge RPC, no per-app split tunneling (MDM only), no remote status. The host only exports a config for the official WireGuard App (App Store, free).

1. Export on explicit request (needs `export_private`, perpetual/family plans; exit 4 otherwise): `archielink export --format wireguard --include-private-key --output <path>` (0600 file), or `archielink export --format qrcode --output <path>` for a PNG (0600) the phone scans via "Create from QR code".
2. Move the file via a user-chosen channel (AirDrop / Files) or show the PNG on screen.
3. In the app: add tunnel from file or QR code → approve the VPN permission prompt → activate → verify the exit IP in Safari.

Never paste the full config into chat or cloud storage; the private key stays in the 0600 file/PNG and the user-chosen channel only.

## Windows devices (guided mode)

No automatic lifecycle: `up`/`down`/`status`/`rollback` print a structured guided report and exit `3`. The official WireGuard for Windows app manages the tunnel; `export` works once `ARCHIE_LINK_WG_CONFIG` points to a `.conf`.

1. Install the official WireGuard MSI from https://www.wireguard.com/install/ (verify the Authenticode signer is `WireGuard LLC`).
2. Export a `.conf` on a macOS host and transfer it over a user-chosen channel; in the app: "Add Tunnel" → "Import tunnel(s) from file".
3. Kill-switch gotcha (real incident): with `AllowedIPs = 0.0.0.0/0` the app enables "Block untunneled traffic" by default and the machine stops answering LAN ping/SSH although the tunnel works. Untick it before activating; recover with `wireguard.exe /uninstalltunnelservice <name>`.
4. Activate, verify the exit IP in a browser; deactivate from the app anytime.

## Remote read-only sessions (cross-device)

`archielink remote` runs a target device's read-only diagnostics from a controller over the LAN: TLS 1.3, temporary self-signed certificate, one-time 8-digit pairing code shown once on the target.

Target: `archielink remote serve --yes --json` (prints endpoint, fingerprint, pairing_code, expires_at).

Controller:

```bash
archielink remote connect <ip:port> <code> --json                          # full checkup
archielink remote connect <ip:port> <code> --rpc detect --json             # single RPC
archielink remote connect <ip:port> <code> --fingerprint <sha256> --json   # pin the certificate
```

Rules:

- `serve` answers exactly one connection — right or wrong code — then exits (fail closed). Default TTL 10 minutes (`--ttl`).
- RPC whitelist `hello|capabilities|detect|plan|doctor`; no shell, no write RPC, no file access.
- The full pairing code exists only on the target's stdout; relay it verbally or through a user-chosen channel (logs carry a redacted form like `41****01`).
- Exit 5 with `error.code` (`connect_failed`, `pairing_invalid`, `pairing_expired`, `pairing_rejected`, `fingerprint_mismatch`, `confirm_timeout`) means the session never established; do not retry with guessed codes.
- Host firewalls: Windows Defender silently drops inbound (add a temporary named rule, remove after); macOS shows an "accept incoming connections" prompt the user must Allow. The endpoint hint skips link-local addresses; confirm the reachable LAN IP.

## Hard safety rules

- Unknown default route, DNS resolver, VPN, transparent proxy, or incomplete snapshot means no automatic setup.
- The isolated probe must succeed before the full tunnel is applied.
- Payment, login, identity verification, and account recovery must not route login, payment, or identity verification through WARP.
- Keep `127.0.0.1`, private LAN ranges, `.local`, and configured local services on their original route; verify the local proxy port and its upstream business endpoint separately.
- Never print, upload, or commit VPN private keys, tokens, cookies, account IDs, or full configs.
- `archielink export --format wireguard --json` gives a redacted config (free). Add `--include-private-key` only on explicit user request, always paired with `--output <path>` (0600 file). `--format qrcode` likewise requires `--output <path>` (PNG, 0600, encodes the private key). Full/QR export needs `export_private` (perpetual/family plans; exit 4 otherwise). Full configs never enter stdout, JSON, logs, or chat.
- Never claim success from a log line or a READY banner. Require structured JSON and exit code `0`.
