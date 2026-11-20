# 汪连 ArchieLink 验收记录

## 自动化检查

```bash
go test ./...
sh scripts/package-doctor_test.sh
python3 ~/.codex/skills/.system/skill-creator/scripts/quick_validate.py \
  skills/archie-link-doctor
```

## 实机矩阵

| 平台 | 状态 | 备注 |
|---|---|---|
| macOS 12.7.6 x86_64 | PASS | `detect`、`plan`、`doctor` 返回 schema v1 |
| macOS 12.7.6 x86_64 自动回退 | PASS | `TestMacOSLifecycle -count=3`：23.28s、19.44s、24.76s |
| macOS 12.7.6 x86_64 四类故障注入 | PASS | `TestMacOSFaultInjection`：18.55s，四项全过（2026-11-06） |
| Windows 10.0.19045 x64 | PASS | 管理员账号远程只读验收，正确返回 `guided` |
| Alibaba Cloud Linux 3.2104 U13.2 x86_64 | PASS | 远端只读验收，正确返回 `guided` |

## macOS 观察结果

- `mode=auto`
- 系统 DNS 通过 `scutil --dns` 回退读取
- 系统 `utun0..2` 不再被误报为多个用户 VPN
- 当前网络访问 Google 超时，`doctor` 正确报告
  `foreign_endpoint_unreachable`
- 未执行 DNS、路由、代理或 VPN 写命令

## Linux 观察结果

- 平台：Alibaba Cloud Linux 3.2104 U13.2，x86_64
- `mode=guided`
- 默认接口和网关读取正确
- 阿里云 DNS 读取正确
- 未检测到已有 WireGuard 隧道
- 服务器未安装 WireGuard 工具，因此 `network_mutation=false`
- Google HTTPS 超时被正确报告为 `foreign_endpoint_unreachable`
- 远端验收二进制使用固定 SHA-256，执行后已删除

## Windows 观察结果

- 主机：`DESKDESK\administrator`，Windows 10.0.19045，AMD64
- `mode=guided`
- 默认接口 `WLAN`、网关和 DNS 读取正确
- 未检测到已有 WireGuard 或 WARP 隧道
- 未安装 WireGuard 工具，因此 `network_mutation=false`
- Google HTTPS 超时被正确报告为 `foreign_endpoint_unreachable`
- 首次验收发现 `archielink` 进程被误判为 WARP；已修复为只匹配
  `wireguard|wireguard-go|warp-svc|openvpn` 并重新验收通过
- 临时验收程序已删除；受限 SSH 验收配置按用户要求保留

## 验收命令

```bash
ARCHIE_LINK_ACCEPTANCE=1 go test -tags=acceptance ./tests/acceptance -v
```

## 网络共存与 Gate 验收

| 平台 | 状态 | 备注 |
|---|---|---|
| macOS 12.7.6 x86_64 | PASS | `coexist inspect/gate` 返回 schema v1；firewall 层不可读，Gate 正确拒绝 auto |
| Alibaba Cloud Linux 3.2104 x86_64 | PASS | 路由、DNS、策略路由和监听端口可读；Gate 因 baseline 未完成而拒绝 auto |
| Windows 10.0.19045 x64 | PASS | 受限公钥验收通过；Gate 因 baseline 未完成而拒绝 auto |

Windows 本地验收：

```powershell
.\archielink-windows-amd64.exe coexist inspect --json
.\archielink-windows-amd64.exe coexist gate --json
```

验收构建产物 SHA-256：

```text
Windows x64:
e0b36c2f3dfd84ec623aef72d9cd9b2145e3e87ca26849ce3f850ea9829bdfed

Windows ARM64:
5d490cd9325a3f318b556e153eb92731f6389f0e7d3f3c2816b66dd0fc261da5

Linux amd64:
1e63bee7cd984afae019ad6ef50992717860b5885cc74048f8f77600d4aef104
```

Windows 验收中发现 PowerShell 中文网卡名乱码，已统一 PowerShell 采集为 UTF-8
输出并重新验收通过。专用 SSH 验收密钥和 `deskdesk-windows` 别名按用户要求保留。
三台实机上的临时验收程序均已删除。

## V0.3 macOS 自动回退

自动化单元测试已完成。非 root 实机检查确认：

- `detect` 返回 `elevation=false`，模式降级为 `guided`
- `up --yes` 在快照、探测和网络写入前返回退出码 `5`
- 错误码为 `elevation_required`

root 生命周期验收：**PASS 3/3**

```bash
ARCHIE_LINK_ACCEPTANCE=1 \
ARCHIE_LINK_WG_CONFIG="$HOME/Library/Application Support/CloudflareWARP/warp.conf" \
go test -tags=acceptance ./tests/acceptance -run TestMacOSLifecycle -count=3 -v
```

三条记录均通过：

```text
PASS 23.28s
PASS 19.44s
PASS 24.76s
总耗时 67.49s
```

每次结束均恢复默认路由且 Wi-Fi 无显式 DNS 残留。

故障注入使用专用 `faultinjection,licensetest` 构建，生产二进制不包含注入开关：

```bash
sudo -E env \
ARCHIE_LINK_ACCEPTANCE=1 \
ARCHIE_LINK_WG_CONFIG="$HOME/Library/Application Support/CloudflareWARP/warp.conf" \
go test -tags=acceptance ./tests/acceptance -run TestMacOSFaultInjection -v
```

覆盖探针不可用、正式写入后失败、WireGuard 守护进程退出和健康检查 403；每项均检查 DNS、默认路由、resolver 和进程残留。

root 实机结果：**PASS 4/4**（2026-11-06，18.55s）

```text
PASS unavailable_endpoint 2.12s
PASS dns_write_failure    3.73s
PASS daemon_exit          2.11s
PASS health_403           5.82s
```

首跑曾全 FAIL，定位并修复三处：

1. 探针注入被配置端点补充逻辑覆盖，`unavailable_endpoint` 实际连通真实端点。
2. macOS `wg-quick` DNS monitor 在停止后异步回写 `1.1.1.1`；现由 ArchieLink 显式设置和恢复 DNS，wg-quick 配置剥离 `DNS`。
3. 守护进程被杀后 `wg-quick down` 找不到接口，残留 `.name`/`.sock` 阻断后续启动；回滚现在兜底销毁接口并清理文件。

V0.3 开发产物 SHA-256：

```text
macOS amd64:
ba13c9775ef764a1275fe3d4b85e58c8a4c6046080c9ea2fa5b4d10e2ff976f7

macOS arm64:
b0cd0bf0f9ea5b91aa2f51ef0fa952919f1110ef8a81ab48a52e5cbcbd520e7f

Windows amd64:
098571b92d7b369978683f1b843e1c9ae094663786c259d65461e4abd4bd84b7

Windows arm64:
206e0ed7c530fac11019c2f26a2efee7c8852959c9e289bdb4fe1ac63a1194d5

ArchieLink Skill:
4f64acfc9faaa6f10093925fcd2d0180c70b91d6dc707fb84761b8141be59ba3
```
