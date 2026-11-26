# 汪连 ArchieLink｜外网访问专家 — 用户手册

**状态：** Draft v1.0（V1.0 切片 3，2026-11-20）
**上游：** [SUPPORT-MATRIX.md](SUPPORT-MATRIX.md)（模式与平台支持范围）、[PRIVACY.md](PRIVACY.md)（数据处理）
**平台细节：** [WINDOWS.md](../platform/WINDOWS.md) · [LINUX.md](../platform/LINUX.md) · [HARMONYOS.md](../platform/HARMONYOS.md) · [TV.md](../platform/TV.md)

## 1. 产品是什么

ArchieLink 是一个**外网访问修复与建立工具**：它检测你的网络环境（DNS、路由、代理、VPN 共存、本地服务），在确认安全后建立 Cloudflare WARP 隧道，持续健康检查，出问题自动回滚到原始状态。核心承诺：**单次错误配置不会让你整机断网**。

## 2. 三态模式

| 模式 | 含义 | 适用 |
|---|---|---|
| auto | 自动连接：快照 → 共存检查 → 孤立探针 → 应用 → 健康检查 → 自动回滚 | macOS、Linux |
| guided | 引导模式：结构化引导报告，你按步骤在官方应用手动操作 | Windows、受限环境 |
| zero-companion | 零伴侣轨道：宿主导出配置，你在设备端官方 App 导入 | iOS/iPadOS |
| unsupported | 不支持：结构化说明后退出 | — |

平台支持范围与验证状态见 [SUPPORT-MATRIX.md](SUPPORT-MATRIX.md)。

## 3. 安装

- **macOS**（12.0+，x86_64/arm64）：获取 `archielink` 二进制。当前未签名版本首次运行需在 Finder 右键打开或系统设置中允许（正式签名随 V1.0 发布提供）。
- **Windows 10**（guided 模式）：获取 `archielink.exe`，另装官方 WireGuard MSI（校验签名 `WireGuard LLC`）。
- **Linux**（Ubuntu 22.04+/Debian 12+）：获取对应架构二进制。

## 4. 日常使用（auto 模式）

连接前先体检，确认无误再连接：

```bash
archielink detect --json     # 环境检测（只读）
archielink plan --json       # 连接计划（只读）
archielink status --json     # 当前状态（只读）
archielink up --yes --json   # 建立连接（需要你明确确认后执行）
archielink down --json       # 断开并恢复快照
archielink rollback --json   # 启动失败后的确定性回滚
```

只有退出码 `0` 且 `connected=true`、`healthy=true` 才代表连接成功。任何失败都会自动回滚；`down`/`rollback` 无需联网、无需授权始终可用。

**退出码**：

| 码 | 含义 | 你的动作 |
|---:|---|---|
| 0 | 成功 | — |
| 2 | 参数错误 | 检查命令 |
| 3 | 平台不支持自动模式 | 按 guided 报告手动操作 |
| 4 | 授权缺失/过期/撤销 | 见 §5 激活 |
| 5 | 预检/共存/快照/探针阻断 | 按 findings 处置（见故障排查） |
| 6 | 启动失败已回滚 | 查看回滚结果 |
| 7 | 健康检查失败已回滚 | 查看失败项 |
| 8 | 回滚未完成 | 按手动恢复提示操作 |
| 9 | 另一进程持有锁 | 不要并发重试 |

## 5. 授权与付费

```bash
# 统一购买入口（三产品，支付宝 aipay 网页收银台）
archielink buy activation-task --server https://api.uzphoto.com --json   # 按次 ¥19.9
archielink buy perpetual --server https://api.uzphoto.com --json         # 个人永久版 ¥199
archielink buy family-annual-12 --server https://api.uzphoto.com --json  # 家庭年度 ¥69/年

# 兼容通道（Machine Pay 402，存量按次）
archielink lease identity --json
archielink lease pay-intent --request-id <稳定请求ID> --plan activation-task \
  --server https://api.uzphoto.com --json
archielink lease activate --order <order_id> --server https://api.uzphoto.com --json
archielink lease status --json
```

- `buy` 下单后在终端内渲染支付二维码（即刻收 `pay_qr_code`/`pay_url`），用支付宝「扫一扫」扫码完成支付，无需跳出终端打开浏览器；CLI 自动轮询支付状态并在支付完成后走激活流程。扫码不便时也可复制输出的 `alipays://` 链接或 URL 到支付宝打开。
- 按次激活（`activation-task`，¥19.9）：执行窗口 14 天（7 天租约 + 7 天离线宽限），期内可多次 `up`；1 台设备，不自动续费。
- 个人永久版（`perpetual`，¥199）：2 台设备，当前大版本永久使用，完整导出/二维码权益（`export_private`）。
- 家庭年度订阅（`family-annual-12`，¥69/年）：12 台设备池（激活限速 3 台/7 天），设备绑定设备密钥，离线宽限 30 天；完整导出/二维码权益。
- 付款意图绑定你的安装 ID 与设备公钥；**不要在不同设备复用请求 ID**。

## 6. 诊断报告

`detect` / `doctor` 支持 `--output <path>`：JSON 报告以 0600 权限写入文件（不覆盖已存在文件），内容与 `--json` 输出逐字节一致，适合归档或附给支持工单。

## 7. 设备端导入

### iOS / iPadOS（零伴侣轨道）

1. 从 App Store 安装官方 WireGuard App（免费）。**注**：中国区已下架，需港区/日区/美区账号（免费 App 付款方式可选"无"）；App 要求 iOS/iPadOS 15+。
2. 宿主导出配置（二选一，均需永久版/家庭版权益 `export_private`，未授权退出码 4）：
   - 配置文件：`archielink export --format wireguard --include-private-key --output <路径>`（0600 文件），经 AirDrop/文件 App 传输；
   - 二维码：`archielink export --format qrcode --output <路径>`（PNG，0600），手机端"从二维码创建"扫码。
3. App 内导入 → 批准 VPN 权限 → 激活 → Safari 验证出口。
4. **安全提醒**：完整配置含私钥，只经你选择的通道传输；不要粘贴进聊天、云盘或日志。

### Windows（guided 模式）

1. 安装官方 WireGuard MSI（`https://www.wireguard.com/install/`，校验签名；该站点在国内需先有可用通道）。
2. macOS 宿主导出 `.conf`，经自选通道传输到 Windows。
3. App 内"Add Tunnel → Import tunnel(s) from file"。
4. **关键**：`AllowedIPs = 0.0.0.0/0` 时 App 默认勾选 "Block untunneled traffic"（kill-switch），会导致机器对局域网完全无响应——**激活前取消勾选**。若已中招：`wireguard.exe /uninstalltunnelservice <隧道名>` 恢复。
5. 激活后浏览器验证出口；随时在 App 内断开。

### HarmonyOS 手机/平板、Android TV、Android 手机

使用各自伴侣应用（见平台文档）；宿主侧 `device vpn-probe` 可检测设备端 VPN 槽位状态。

Android TV 首次连接需先在电视上开启 ADB 调试（海信/Vidda 通常经「商场模式」或「声音平衡 1969 工厂模式」进入，详尽步骤见 [TV.md §5.3](../platform/TV.md)），随后电脑执行 `adb connect <电视IP>:5555`，并在电视端弹窗中允许调试授权。

## 8. 跨设备只读体检

在目标设备开启一次性会话，从控制端做只读诊断（局域网内，TLS 1.3）：

```bash
# 目标设备
archielink remote serve --yes --json          # 输出 endpoint、fingerprint、配对码
# 控制端
archielink remote connect <ip:port> <配对码> --json
```

- 配对码 8 位一次性，仅显示一次；会话默认 10 分钟，只接受一个连接。
- 只读白名单（检测/计划/体检）；无 shell、无写操作、无文件访问。
- 防火墙提示：Windows Defender 会静默拦截入站（需临时放行规则）；macOS 会弹"允许传入连接"对话窗，点允许。

## 9. 安全注意事项

- 不要绕过 `up` 的确认语义；`--yes` 只在你明确决定修改网络后使用。
- 完整配置（含私钥）不进聊天、云盘、日志与剪贴板共享。
- 支付、登录、身份验证操作不要经 WARP 隧道路由。
- 遇到问题先看 [TROUBLESHOOTING.md](TROUBLESHOOTING.md)。
