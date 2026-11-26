# 汪连 ArchieLink｜外网访问专家 — 支持矩阵

**状态：** Accepted v1.0（V1.0 切片 1，2026-11-19）
**上游：** [SPEC.md](SPEC.md)、[ROADMAP.md](ROADMAP.md)
**维护：** 每次版本收口或平台验收状态变化时更新本文件。

## 1. 模式定义

| 模式 | 含义 | 网络写操作 |
|---|---|---|
| auto | 自动连接：快照 → 共存 Gate → 孤立探针 → 应用 → 健康检查 → 自动回滚 | 是（受全链路保护） |
| guided | 引导模式：结构化引导报告（`GuidedReport`，exit 3），用户按步骤手动操作 | 否 |
| zero-companion | 零伴侣轨道：宿主导出 WireGuard 配置，用户在官方 App 导入并授权 | 否（宿主侧只导出） |
| unsupported | 不支持：结构化说明后退出 | 否 |

## 2. 验证层级

| 层级 | 含义 | 适用范围 |
|---|---|---|
| T1 | 自有真机全功能验收（含生命周期、数据面、长稳） | 最高可信度 |
| T2 | AGC 云真机（`compatible (cloud-verified)`）——仅覆盖安装/启动/授权 UI/兼容性；数据面、长稳保活、真实出口与分流永远 T1 | 华为实验室托管真机 |
| 容器 | 系统容器全链路验收（假对端，回滚与环境清洁逐项核验） | Linux |
| loopback | 本机回环协议/契约测试 | 跨设备 RPC |

## 3. 宿主平台矩阵（archielink CLI）

| 平台 | 最低版本 | 模式 | 验证 | 证据（本地归档） |
|---|---|---|---|---|
| macOS (x86_64/arm64) | 12.0 | auto + guided + 全部 CLI | T1：本机 macOS 12.7.6 x86_64——root 生命周期 3/3、故障注入 4/4、V0.2–V0.4 全验收、双环境可复现构建 | `artifacts/` 各版本目录 |
| Windows 10 x64 | 10.0.19045 | guided（auto 不做） | T1：deskdesk-windows 19045.6282 切片 1 PASS 9/9 | `artifacts/v0.5-windows/2026-11-18-s1-acceptance/` |
| Windows 11 x64 | build ≥ 22000 | guided（auto 跳过，见 §5） | 与 Win10 同源引导逻辑 | — |
| Linux（Ubuntu 22.04+、Debian 12+） | Ubuntu 22.04 / Debian 12 | auto + guided | 容器 T1 级：Ubuntu 22.04.5 容器全链路（probe→apply→verify→自动回滚→环境全净）+ 6 真 bug 修复回归；**正式发布前建议补实机回归** | `artifacts/v0.5-linux/2026-11-19-automatic-container/` |
| Linux（其他发行版） | — | 按能力检测 guided / unsupported | 只读检测 T1：Huawei Cloud EulerOS 2.0 PASS 7/7（生产服务器，只读验收） | `artifacts/v0.5-linux/2026-11-19-euleros-detection/` |

macOS 分发注记：Developer ID 签名/公证阻塞于 Apple Developer 账号（V1.0 切片 4）；未签名二进制首次运行需用户绕过 Gatekeeper。

## 4. 设备端矩阵

| 平台 | 最低版本 | 形态 | 验证 | 证据（本地归档） |
|---|---|---|---|---|
| HarmonyOS 手机/平板（伴侣 HAP `0.5.0-d3`，bundleName `com.archielink.companion`） | API 23+（minSdk 6.1.0(23)） | 伴侣应用（`VpnExtensionAbility` + ArkTS WireGuard 数据面） | T1：BRA-AL00（HarmonyOS 7.0.0.111, API 26）生产化收工（rekey/看门狗/保活/冲突检测/HRM-001 vpn-probe）；T2：MatePad Pro Max 悦享（授权流）、Mate 60 Pro（兼容 16 项 + UX）、Mate 90 Pro、Mate XT（转场 + 留白复测） | `mobile/harmony-companion/artifacts/harmony-companion/cloud-test/` |
| Android TV（TV companion `0.4.0`） | Android 9（API 28） | TV 伴侣 APK（`VpnService`；V1.1 增 TV-003 内容入口检测 + 无浏览器引导安装页，随调试包免费） | T1：海信 VIDAA（MTK9255，Android 9，armeabi-v7a，无 root）D2 整体验收——VPN、遥控器、YouTube 搜索、白名单；V1.1 引导安装按 TV.md §5.1 验收 | TV.md 验收记录 |
| Android 手机/平板（D1 伴侣 APK） | — | 伴侣应用（`VpnService` + AndroidKeyStore） | T1：真实 WARP、ADB 共存、应用白名单、断开清理、UI 验收 | PROJECT-MEMORY D1 条目 |
| iOS / iPadOS（零伴侣轨道 D4） | iOS/iPadOS 15（官方 WireGuard App 当前要求） | 官方 WireGuard App + 宿主 `export` 配置导入（conf/二维码） | T1：iPad 第 7 代 / iPadOS 18.7.1 / WireGuard 1.0.16（港区）——conf AirDrop 导入 + **iPad 扫码全链路 PASS**（V0.10：`export --format qrcode` → 扫码 → 导入 → 握手 → 外网）；手机档 iPhone 未验（流程同源） | `artifacts/d4-ios/`、`artifacts/v0.10-mobile/` |

注：中国区 App Store 已下架 WireGuard，需港区/日区/美区账号（免费 App 付款方式选"无"可注册）；详见 SKILL.md iOS 节。D4 零伴侣轨道的完整导出（`--include-private-key`）与二维码（`--format qrcode`）自 2026-11-23 起为永久版/家庭版权益（`export_private`）；脱敏导出（`--format wireguard`，无私钥）保持免费。

## 5. 明确不承诺清单

| 项 | 状态 | 触发条件（恢复实施） |
|---|---|---|
| Windows 11 自动模式 | 跳过（2026-11-19 拍板）：Win11 维持引导报告 result=unsupported exit 3 | Win11 x64 真机到手，按 WINDOWS.md §切片 2 设计补做（含 kill-switch 防火墙约束） |
| 鸿蒙 PC / 2in1（V0.8） | 跳过（保持）：AGC PC 远程真机无法上传 conf，剪贴板路径被否；无自有真机；伴侣 `deviceTypes` 保留 2in1 声明 | 鸿蒙 PC/2in1 真机到手，或 AGC 云真机开放文件导入 |
| HarmonyOS 智慧屏 | 条件性支持声明，未验证（无真机） | 智慧屏真机到手，按 TV.md §7 验证 `VpnExtensionAbility` |
| Google TV | 未验证（无真机；D2 仅验 VIDAA Android 9） | Google TV 真机到手 |
| Linux 真实 WARP up→healthy 完整验收 | 延后：华为云北京出口对 WARP 数据面 UDP 全面封锁；容器假对端全链路已认可为验收结论 | colima / 港美 VPS / 用户 Linux 实机任一可用时补验 |
| 自研 iOS 伴侣 | 结构性排除：`NEPacketTunnelProvider` entitlement 仅发给付费 Apple Developer Program | 购入付费 Apple Developer 账号后另行评估（与 macOS Developer ID 签名同一账号） |
| NetworkManager 连接级 DNS 适配器（nmcli） | 留待后续版本：无 resolved 的 NM 系统回退 resolv.conf 文件托管 | NM-only 环境成为正式支持目标时 |

## 6. 已知不兼容设备与外部限制

**不兼容设备**：

| 设备 | 现象 | 结论 |
|---|---|---|
| iPhone 6（iPhone7,2）/ iOS 12.5.8 / WireGuard 1.0.15 | 官方 WireGuard 扩展路径监控全天 76+ 次评估零 satisfied（`unsatisfied [en0]` → backend 永久暂停），握手零发出；扫码导入本身正常（字段经 UAPI 确认） | 已知不兼容设备；iOS 支持下限以官方 WireGuard App 要求为准（当前 iOS 15+），不要在 iOS 12/13/14 设备上引导用户尝试 |

**外部限制（不作为开发项）**：

| 项 | 说明 |
|---|---|
| TikTok on HarmonyOS | 应用层检测大陆 SIM/设备环境，VPN 层不可修；换境外节点仍无网络 |
| ChatGPT（部分 VPN 出口） | Cloudflare 按 VPN IP 拦截；WARP 免费出口 104.28.223.164 被标记中国河北时 Gemini/ChatGPT 拒服务 |
| 华为云北京出口 | 对 WARP 数据面 UDP 全面封锁（注册 API TCP 可达） |
| download.wireguard.com | 被墙，Windows MSI 下载需先有可用外网通道 |
| WireGuard for Windows kill-switch | `AllowedIPs = 0.0.0.0/0` 默认启用 "Block untunneled traffic"，机器对局域网无响应；引导流程强制提醒取消勾选 |

## 7. 维护周期承诺

- **大版本窗口**：正式发布后，当前大版本与前一大版本并行维护；仅前一版本收安全修复。
- **永久版授权**：1 人、2 台自有设备、购买时大版本、12 个月更新与支持（V0.4 定位，不随本矩阵改变）。
- **平台退出**：移除任一正式支持平台前，提前至少一个大版本在本矩阵公告。
- **矩阵更新义务**：每次版本收口、平台验收状态变化或新增不兼容设备时更新本文件；漂移以最新验收记录为准。

## 8. 证据归档说明

验收证据（RESULT.md、JSON、日志、截图）统一归档于本地 `artifacts/` 与 `mobile/harmony-companion/artifacts/`（`.gitignore` 排除，不入库）；本矩阵只记指针与结论。云测试证据仅本地归档（2026-11-17 拍板）。
