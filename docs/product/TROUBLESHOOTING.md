# 汪连 ArchieLink｜外网访问专家 — 故障排查与 FAQ

**状态：** Draft v1.0（V1.0 切片 3，2026-11-20）
**上游：** [USER-GUIDE.md](USER-GUIDE.md)、[SUPPORT-MATRIX.md](SUPPORT-MATRIX.md)（已知不兼容与外部限制）

## 1. 速查表

| 症状 | 常见原因 | 处置 |
|---|---|---|
| `up` 退出码 5，findings 提示已有 VPN/代理 | 共存 Gate 拦截（另一 VPN 或代理在运行） | 先断开现有 VPN/代理再试；或按 guided 报告手动操作 |
| `up` 退出码 5，探针超时 | WARP endpoint UDP 被封锁或污染 | 换 endpoint（如 `162.159.192.1:2408`）；仍不通则当前网络不可用 |
| `up` 退出码 6（已回滚），DNS 写入失败 | resolver 写入被拒或系统代理接管 | 看 `error` 字段；系统代理开启时先关代理 |
| `up` 退出码 7，健康检查失败 | warp_trace / google / deepseek / 本地服务检查未过 | 见 §3 分项 |
| `up`/`down` 退出码 9 | 另一进程持有全局锁 | 等待或结束另一 archielink 进程；不要并发重试 |
| `up` 退出码 4 | 授权过期/撤销/未激活 | 见 USER-GUIDE §5 续费/激活；`down`/`rollback` 不受影响 |
| 授权突然失效 + 提示时钟回拨 | 系统时间倒退超 24 小时（`clock_review_required`） | 校准系统时间后联网续租，由控制面解除标记 |
| Windows 导入隧道后局域网失联 | WireGuard kill-switch 默认开启 | `wireguard.exe /uninstalltunnelservice <隧道名>` 恢复；下次导入前取消 "Block untunneled traffic" |
| iPhone/iPad 导入后不握手 | 设备低于官方 App 要求或已知不兼容 | 核对 iOS/iPadOS ≥15；iPhone 6 等老设备见 SUPPORT-MATRIX §6 |
| 多台设备互抢连接 | 同一 WARP 密钥多设备共用 | 一设备一密钥：每台设备用 wgcf 注册自己的配置 |
| 下载 WireGuard MSI 失败 | `download.wireguard.com` 在国内被墙 | 先在可用通道的设备下载再传输 |

## 2. 防火墙与网络拦截经验

| 环境 | 表现 | 处置 |
|---|---|---|
| Windows Defender | 静默丢弃未注册二进制的入站（无提示，表现为超时） | 添加临时命名入站规则，用完删除 |
| macOS 应用防火墙 | 弹"允许传入连接"对话窗 | 点允许 |
| 华为云安全组 | 默认只放 22/80/443，其他端口入站被拦 | 跨主机验收用 SSH 隧道或按需开端口 |
| 华为云北京出口 | WARP 数据面 UDP 全面封锁 | 换出口；该环境只支持只读检测 |
| 企业网络 | 未知默认路由/透明代理被 Gate 拦截 | 尊重拦截（exit 5）；企业网络优先 IT 部门处理 |

## 3. 健康检查分项（退出码 7）

| 检查项 | 含义 | 处置 |
|---|---|---|
| `warp_trace_off` | 隧道在但 WARP 未生效 | 等待 1-2 分钟重试；仍失败换 endpoint |
| `warp_trace_unreachable` / `google_unreachable` | 隧道不通 | 检查 endpoint 可达性；rollback 后换节点重试 |
| `deepseek_forbidden` | api.deepseek.com 返回 403（出口被识别） | 换 endpoint；部分出口对特定站点不可用属外部限制 |
| `local_service_unreachable` | 本地 8688 服务在场但不可达 | 本地服务故障排查；服务不在场时此项自动跳过（`local_8688_absent`） |
| `wireguard_process_missing` / `wireguard_handshake_missing` | 守护进程退出或无握手 | rollback 后重试；反复出现收集 `doctor` 报告 |
| `route_not_via_tunnel` / `protected_route_missing` | 路由未按预期 | rollback 后重试 |

## 4. FAQ

**Q: ArchieLink 和官方 Cloudflare WARP 客户端什么关系？**
A: WARP/WireGuard 是技术后端；ArchieLink 是建立与修复工具：检测环境、保护本地服务、健康检查、自动回滚、多平台引导。我们不替换 WARP，也不售卖带宽。

**Q: 断网了怎么办？**
A: `down` 和 `rollback` 不需要联网、不需要授权，永远可用。最坏情况（退出码 8 回滚未完成）按提示手动恢复；重启也可释放受管对象（锁是内核语义，进程死亡自动释放）。

**Q: 退款政策？**
A: 按次激活是一次性数字商品，履约后视为已消费；是否主动退款以购买页公示为准。平台退款/拒付/撤销事件后授权会停止续租。

**Q: 你们收集什么数据？**
A: 见 [PRIVACY.md](PRIVACY.md)：本地优先，云端只有订单/授权/设备注册记录，无流量、无 Cookie、无支付凭据、无私钥。

**Q: 旧设备能用吗？**
A: 查 [SUPPORT-MATRIX.md](SUPPORT-MATRIX.md) §4/§6：iOS 端以官方 WireGuard App 要求为准（当前 iOS 15+），iPhone 6 等已列为已知不兼容。

**Q: TikTok/ChatGPT 走 WARP 还是不通？**
A: 属应用层/站点层对 VPN 出口的限制（外部限制清单见 SUPPORT-MATRIX §6），VPN 层不可修；换出口节点是唯一尝试方向。

**Q: 支持哪些平台？会停止支持吗？**
A: 见 SUPPORT-MATRIX §7 维护周期承诺：移除正式支持平台前至少提前一个大版本公告。
