---
name: archie-link
description: "Safely diagnose and establish a Cloudflare WARP/VPN fallback with snapshots, coexistence checks, health verification, and deterministic rollback. Use when the user explicitly asks to connect, repair foreign network access, or restore a failed connection. 中文触发场景：外网访问、科学上网、连不上外网、帮我连外网、Google 打不开、YouTube 连不上、GitHub 连接失败、修复网络、恢复外网连接、免费VPN、开通VPN、VPN 连不上、VPN 修复、VPN 激活、VPN专家、翻墙、GFW、破网。"
metadata:
  version: "1.1.0"
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
    - 翻墙
    - GFW
    - 破网
---

# ArchieLink

Use the installed `archielink` CLI. The CLI is the only source of truth for network state and success.

## Prerequisites: the `archielink` CLI

Check `command -v archielink` first; the CLI does not ship with this skill. If it is missing, stop and send the user to the official install guide (<https://github.com/nocaptain-oss/archie-link#安装> — GitHub Releases with `SHA256SUMS`; no other download source). The user installs the CLI themselves; do not download or install the CLI for the user. Do not proceed until `archielink` runs from `PATH`.

## Related skill: archie-link-doctor

`archie-link-doctor` is the read-only subset: same checks, no network writes, no activation. Use it for diagnosis only (网络体检/网络排查); use this skill to connect, repair, or restore (连接/修复/恢复).

## Ownership

- The root agent owns the global network lock. Never run `up`/`down`/`rollback` concurrently; never hand tunnel control to a subagent.
- Never edit DNS, routes, proxies, or local DNS files yourself; never kill wireguard-go directly.

## Before changing the network

```bash
archielink detect --json
archielink plan --json
archielink coexist gate --json
archielink status --json
```

Explain the observed problem and the proposed change. Start a connection only after the user explicitly confirms network modification.

## Config file (precheck before up/buy)

A usable config must exist before you suggest `buy` or run `up`; never let the user pay first. Precheck in order, avoiding the official client whenever possible:

1. `ARCHIE_LINK_WG_CONFIG` if set; else macOS default `~/Library/Application Support/CloudflareWARP/warp.conf` (Windows: env var required).
2. Else ask for any WireGuard or commercial-VPN conf (zero install; point the env var at it).
3. Else official-client fallback, CLI first, GUI last. Say it installs the official Cloudflare WARP client as registration backend and the user registers themselves for compliance (the skill never registers, runs wgcf, generates, or shares configs/keys; one device, one key). With consent install it for them: probe `command -v brew` → `brew install --cask cloudflare-warp`; no brew → download page (no reachability promises). Walk one step at a time and stand by: the USER runs the bundled CLI registration command themselves (probe `warp-cli` first; GUI stays closed), then verify the file. CLI missing or file absent → GUI fallback: open the app once, toggle on to register, toggle off immediately (keep app and config; a live client tunnel makes coexist gate refuse `up`). DNS pollution → suggest another network (hotspot). Finish with `coexist inspect`, then continue.

A missing config surfaces late as `probe_failed` — precheck. Config present but probe fails → suspect missing `PersistentKeepalive` or a polluted endpoint. Never induce or discourage refunds.

## Paid activation (optional, user-initiated only)

Activation is optional; never suggest, upsell, or start it yourself. Only when the user explicitly asks to activate:

```bash
archielink buy <plan> --request-id <stable-request-id> --server https://api.uzphoto.com --json
```

Plans: `activation-task` (single activation), `perpetual` (personal), `family-annual-12` (family pool). The CLI prints the order amount before payment and renders an Alipay QR code in the terminal; the user scans and pays (the agent never handles payment). If the QR does not render, give the printed `pay_url` to open manually. Never build payment cryptography. Orders bind `install_id` + `device_public_key`; never reuse a request ID across devices.

## Start

```bash
archielink up --yes --json
```

`--yes` is not a substitute for human confirmation; never add it to exploratory or diagnostic runs. Only exit code `0` with `connected=true` and `healthy=true` means the connection is active. On failure, read the error code, then:

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

`detect`/`doctor` accept `--output <path>`: 0600 file, never overwrites, byte-identical to `--json` stdout.

## iOS devices (zero-companion)

No bridge RPC, no per-app split tunneling (MDM only), no remote status. The host only exports a config for the official WireGuard App (App Store, free).

1. Export on explicit request (needs `export_private`; exit 4 otherwise): `archielink export --format wireguard --include-private-key --output <path>`, or `--format qrcode --output <path>` (PNG) the phone scans via "Create from QR code".
2. Move the file via a user-chosen channel (AirDrop / Files) or show the PNG on screen.
3. In the app: add tunnel from file or QR code → approve the VPN permission prompt → activate → verify the exit IP in Safari.

Never paste the full config into chat or cloud storage.

## Windows devices (guided)

No automatic lifecycle: `up`/`down`/`status`/`rollback` print a structured guided report and exit `3`. The official WireGuard for Windows app manages the tunnel; `export` works once `ARCHIE_LINK_WG_CONFIG` points to a `.conf`.

1. Install the official WireGuard MSI from https://www.wireguard.com/install/ (verify the Authenticode signer is `WireGuard LLC`).
2. Export a `.conf` on a macOS host and transfer it over a user-chosen channel; in the app: "Add Tunnel" → "Import tunnel(s) from file".
3. Kill-switch gotcha (real incident): with `AllowedIPs = 0.0.0.0/0` the app enables "Block untunneled traffic" by default and the machine stops answering LAN ping/SSH although the tunnel works. Untick it before activating; recover with `wireguard.exe /uninstalltunnelservice <name>`.
4. Activate, verify the exit IP in a browser; deactivate from the app anytime.

## Android TV companion

Installing the TV companion needs LAN ADB, which most Chinese TVs ship disabled. A human enables it with the remote — do not invent key sequences. Walk one path below (Hisense/Vidda differs by firmware), then take the workflow back:

1. Newer Vidda (JUUI, e.g. Vidda X/R): 遥控器「设置」→「全部设置」→「通用」→「商场模式」→ 开；再「关于」→「版本号」→ 连按「确定」5 次开启开发者模式 → 返回 →「开发者模式/选项」→「ADB/网络调试」→ 开。
2. Older Hisense VIDAA: 「设置」→「声音」→ 关闭杜比全景声（如有）→ 进入「声音平衡」界面 → 外接键盘输入 `1969`（或无数字键遥控器反复按「菜单+OK」）→ 屏显 `M` 进入工厂模式 → 按「菜单」→ 将 Tofac 由 U 改 M → 重启 → 设置「关于」上连按「菜单」4–5 次开隐藏菜单 →「版本号」连按「确定」→「开发者选项」→「USB 调试」→ 开。警告：M 模式下投屏等功能可能异常，安装完成后切回 U 模式。
3. Generic Android TV: 「设置」→「设备偏好设置」→「关于」→ 连点「版本号」7 次 →「开发者选项」→「USB 调试/网络 ADB 调试」→ 开。

Handback: the human confirms "ADB 已开启，电视 IP 是 <ip>"; then run `adb connect <ip>:5555`, have them approve the on-TV「允许 USB 调试」prompt (prefer 始终允许), verify with `adb devices`, and continue with `archielink device discover --json`. If the on-screen menus match none of the paths, ask the user what they see instead of guessing.

## Remote read-only sessions

`archielink remote` runs a target device's read-only diagnostics from a LAN controller: TLS 1.3, temporary self-signed certificate, one-time 8-digit pairing code shown on the target.

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
- The full pairing code exists only on the target's stdout; relay it verbally or through a user-chosen channel (logs carry a redacted form).
- Exit 5 with `error.code` (`connect_failed`, `pairing_invalid`, `pairing_expired`, `pairing_rejected`, `fingerprint_mismatch`, `confirm_timeout`) means the session never established; do not retry with guessed codes.
- Host firewalls: Windows Defender silently drops inbound (add a temporary named rule, remove after); macOS shows an "accept incoming connections" prompt the user must Allow.

## Hard safety rules

- Unknown default route, DNS resolver, VPN, transparent proxy, or incomplete snapshot means no automatic setup.
- The isolated probe must succeed before the full tunnel is applied.
- Payment, login, identity verification, and account recovery must not route login, payment, or identity verification through WARP.
- Keep `127.0.0.1`, private LAN ranges, `.local`, and configured local services on their original route; verify the local proxy port and its upstream business endpoint separately.
- Never print, upload, or commit VPN private keys, tokens, cookies, account IDs, or full configs.
- `archielink export --format wireguard --json` gives a redacted config (free). `--include-private-key` or `--format qrcode` only on explicit request, always with `--output <path>` (0600; the QR PNG encodes the private key); needs `export_private` (perpetual/family; exit 4 otherwise). Full configs never enter stdout, JSON, logs, or chat.
- Never claim success from a log line or a READY banner. Require structured JSON and exit code `0`.
