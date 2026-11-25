---
name: archie-link-doctor
description: "Diagnose network reachability and WARP fallback eligibility without changing system settings. Use when a person or agent needs read-only DNS, route, proxy, tunnel, local-service, or foreign-endpoint diagnosis before deciding whether to install a network fallback. 中文触发场景：网络体检、网络诊断、检查网络环境、网络排查、外网访问诊断、GitHub 连不上排查、连不上外网先检查。"
metadata:
  short-description: "Read-only network diagnosis for agent environments"
  triggers:
    - 网络体检
    - 网络诊断
    - 检查网络环境
    - 网络排查
    - 外网访问诊断
    - GitHub 连不上排查
    - 连不上外网先检查
---

# ArchieLink Doctor

Run the installed `archielink` binary. Do not reproduce its network checks with ad-hoc commands.

## Prerequisites: the `archielink` CLI

Check first: `command -v archielink`. This skill ships without the binary. If it is missing, install it from the official GitHub repository — download the platform binary from <https://github.com/nocaptain-oss/archie-link/releases> (v1.1.0+), verify against the Release's `SHA256SUMS`, and put it on `PATH`; see <https://github.com/nocaptain-oss/archie-link#安装>. Do not run substitute commands; if the CLI cannot be installed, report that read-only diagnosis is unavailable on this host.

The tool is read-only. It must not modify DNS, routes, proxies, or VPN state.

## Workflow

1. Run:

```bash
archielink detect --json
archielink plan --json
archielink doctor --json
archielink coexist inspect --json
archielink coexist gate --json
```

2. Inspect the `mode`, `allowed`, and `findings` fields.

## Modes

- `auto`: the host has the capabilities needed for a later guarded setup. Do not install or start anything in this read-only skill.
- `guided`: provide the manual path and explain the missing privilege, platform, or network capability.
- `unsupported`: stop. Do not claim that setup is possible on this host.

`coexist gate` is authoritative for later automatic setup. If `allowed` is false, unknown network ownership must not enter auto mode.

## Safety

- Never execute network write commands.
- Never read `.env`, credentials, cookies, payment data, or VPN private keys.
- Never describe `ready` as connected.
- Report the stable finding codes before suggesting the next step.
- Treat `foreign_endpoint_unreachable` as evidence of a network problem, not proof that WARP will fix it.
- Route any implementation or setup request to the full `archie-link` skill — same CLI, adds guarded connect/rollback and optional paid activation — only after the user explicitly asks to continue. If it is not installed, get it from the same GitHub repository or SkillHub (`archie-link`).
