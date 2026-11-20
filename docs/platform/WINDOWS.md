# Windows 平台规格（汪连 ArchieLink｜外网访问专家）

状态：`implemented`（V0.5 已关闭：切片 1 引导模式 2026-11-18 验收 PASS 9/9；切片 2 Win11 自动模式**跳过**——2026-11-19 用户拍板，无 Win11 真机，Win11 维持引导报告 result=unsupported exit 3）
上游：`docs/product/ROADMAP.md` §7、`docs/product/SPEC.md`
计划：`docs/plans/2026-11-18-archie-link-v0.5-windows-linux.md`

## 平台边界

- Windows 11 x64（build ≥ 22000）：自动模式候选（V0.5 切片 2，未实现；当前与 Win10 同走引导模式输出）。
- Windows 10（build < 22000）：**仅引导模式**，不做任何网络写操作。
- Windows 10 以下 / 32 位 / ARM64：按能力检测进入 `guided` 或 `unsupported`，不尝试半自动修改。

## 能力矩阵与现状

- V0.2 检测层已交付：PowerShell runner（`runPowerShell`），`Detect`（OS 版本/arch，`NetworkMutation = build >= 22000`）、`Snapshot`、`Coexist`（`internal/adapter/windows/`）。
- 模式选择：`adapter.SelectMode`——Win10 → `guided`；Win11 → `auto` 能力（但自动生命周期未实现，生命周期命令仍输出引导报告）。
- V0.5 切片 1（2026-11-18）：
  - `up/down/status/rollback` 在 Windows 输出结构化引导报告（`model.GuidedReport`，schema_version 1，mode `guided`，exit 3），不再是无信息的 `unsupported_platform` 裸错误。
  - `export --format wireguard` 在 Windows 可用：`ARCHIE_LINK_WG_CONFIG` 指向配置后即可脱敏/完整导出（完整导出同样强制 `--output` 0600、禁 `--json`）。
  - 状态目录：`%LOCALAPPDATA%\ArchieLink`（`ARCHIE_LINK_STATE_DIR` 可覆盖）；配置路径仅认 `ARCHIE_LINK_WG_CONFIG`（Windows 无 CloudflareWARP `warp.conf` 稳定公开路径，不猜测默认路径）。
  - `lease` 命令维持不可用（exit 3）：Windows 引导模式不涉及授权生命周期。

## 引导流程（用户手动步骤）

1. 安装官方 WireGuard for Windows（https://www.wireguard.com/install/；MSI Authenticode 签名须为 WireGuard LLC）。
2. 准备 WireGuard 配置（`.conf`）；macOS/Windows 宿主可用 `archielink export --format wireguard --include-private-key --output <path>` 导出（0600，完整配置禁粘贴聊天/云端/日志）。
3. WireGuard App：Add Tunnel → Import tunnel(s) from file → 选择 `.conf`。
4. **激活前编辑隧道，取消勾选 "Block untunneled traffic"**（2026-11-18 实机事故沉淀：`AllowedIPs = 0.0.0.0/0` 时 WireGuard for Windows 默认开启 kill-switch 防火墙，机器对局域网 ping/SSH 完全无响应；CLI `/installtunnelservice` 同样触发，本地恢复用 `/uninstalltunnelservice <name>`）。
5. 在 App 内激活隧道，浏览器验证连通性。

## 切片 2：Windows 11 自动模式（**跳过**，2026-11-19 用户拍板——无 Win11 真机；真机到手按下述设计补做）

- Windows WireGuardNT 适配器（经官方 WireGuard App 的隧道服务管理）。
- PowerShell DNS / 路由快照与恢复。
- 孤立探针 → 共存 Gate → 快照 → 应用 → 健康检查 → 自动回滚，与 macOS 生命周期同序、同 JSON 契约、同退出码。
- **kill-switch 设计约束（2026-11-18 实机事故）**：WireGuard for Windows 对 `AllowedIPs = 0.0.0.0/0` 默认启用 "Block untunneled traffic" 防火墙——机器对局域网 ping/SSH 完全无响应（隧道本身可用）。自动模式必须显式管理防火墙规则：保留局域网可达性（与 SKILL.md "Keep private LAN ranges on their original route" 一致），并在回滚/卸载时清理规则；健康检查不可依赖局域网通道在 kill-switch 生效期间可达。
- 验收：Win11 x64 实机三次生命周期 + 三类故障回滚（与 macOS V0.3 同标准）。

## 安全边界

- 引导模式不做网络写、不提权、不修改 DNS/路由/防火墙；只读检测与结构化说明。
- 与 macOS 相同红线：未识别默认路由/DNS/VPN/透明代理或快照不完整 → 不自动；探针先行；私钥/令牌不入日志。

## 验收记录

- 切片 1（2026-11-18，PASS 9/9）：deskdesk-windows（Windows 10.0.19045.6282 x64）——`up --yes --json` guided 4 步 rc=3（真实检测数据填充，Win10 network_mutation=false 判定正确）；down/status/rollback 单步 rc=3；export roundtrip（脱敏 rc=0、`--include-private-key` 无 `--output` rc=2、`--json` 互斥 rc=2、`--output` 落盘私钥逐字节一致 rc=0、拒覆盖 rc=2）；doctor 回归 rc=0；lease 守卫 rc=3。证据 `artifacts/v0.5-windows/2026-11-18-s1-acceptance/RESULT.md`（本地，不入库）。
- 切片 2：**跳过**（2026-11-19 用户拍板，无 Win11 真机），无验收；真机到手后按 §切片 2 设计补做并验收。

## 已知限制

- Windows 无 Cloudflare WARP `warp.conf` 稳定公开路径，配置须用户提供（`ARCHIE_LINK_WG_CONFIG`）。
- Win11 自动模式跳过（无真机）：Win11 与 Win10 行为一致（引导报告 result=unsupported exit 3）。
