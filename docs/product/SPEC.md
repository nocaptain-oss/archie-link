# 汪连 ArchieLink｜外网访问专家 — 产品规格

**状态：** Accepted v0.3；交付版本表 V0.2–V0.10 全部收口（跳过项见 [SUPPORT-MATRIX.md](SUPPORT-MATRIX.md) §5），V1.0 进行中
**日期：** 2026-11-06（v0.2）；2026-11-19（状态对账）
**版本路线图：** [ROADMAP.md](ROADMAP.md)  
**鸿蒙支持规格：** [HARMONYOS.md](../platform/HARMONYOS.md)
**电视支持规格：** [TV.md](../platform/TV.md)
**跨设备协作规格：** [CROSS-DEVICE.md](../architecture/CROSS-DEVICE.md)
**网络共存规格：** [COEXISTENCE.md](../architecture/COEXISTENCE.md)
**商业信任与授权架构：** [COMMERCIAL-TRUST.md](../architecture/COMMERCIAL-TRUST.md)
**商业逻辑：** [BUSINESS-CASE.md](BUSINESS-CASE.md)  
**当前阶段计划：** [V0.3 macOS 自动回退](../plans/2026-11-05-archie-link-v0.3-macos-auto.md)
**V0.4 计划：** [商业信任实现](../plans/2026-11-06-archie-link-v0.4-commercial-trust.md)

## 1. 目标

构建一个可付费分发的跨平台网络回退 Skill：识别用户设备、Agent 能力和现有网络状态，在不破坏本地服务的前提下建立 Cloudflare WARP WireGuard 连接；启动失败时恢复原网络；成功后提供诊断、状态和关闭能力。

首期收费对象是“自动诊断、安装、连接守卫和故障恢复”，不是转售 WARP 带宽。产品文案不得承诺匿名、固定国家、固定 IP、绕过平台风控或适合高价值账户登录。

## 2. 用户与场景

### 2.1 编程 Agent 用户

需要访问 GitHub、包仓库、文档或模型 API，但当前网络的 DNS、超时或 TLS 连接失败。Agent 可以执行命令，但不应自行猜测网络状态或反复切换 VPN。

### 2.2 办公 Agent / 个人 Agent 用户

Agent 可能只能执行受限命令、生成文件或操作 GUI。产品必须降级为引导模式，而不是假装已经修改系统网络。

### 2.3 移动端用户

Agent 无法静默安装系统 VPN。产品只生成 WireGuard 配置文件和导入说明，由用户手动完成。

### 2.4 鸿蒙用户

HarmonyOS 手机、平板、折叠屏、鸿蒙 PC 和 2in1 设备需要单独的鸿蒙伴侣应用。Agent 负责诊断、配置和启动意图；伴侣应用通过 `VpnExtensionAbility` 建立系统 VPN，并处理用户授权、路由、DNS、应用分流和状态展示。

### 2.5 电视用户

Android TV、Google TV、国内 Android 电视和 HarmonyOS 智慧屏用户希望在电视上直接访问 YouTube 和其他外网视频。电视端需要遥控器交互、VPN 伴侣应用、内容入口检查和最小浏览器入口。专门的大屏浏览器体验作为 V1.0 后的独立产品阶段。

### 2.6 多设备用户

用户可能同时拥有 macOS、Windows、Linux、鸿蒙设备和电视，希望由一个 Agent 协助检查另一台设备。产品通过同局域网一次性配对和语义化只读 RPC 实现跨设备诊断，不要求普通用户配置 SSH 或共享密码。

### 2.7 跨设备与开发者辅助用户门槛

跨设备安装、调试和远程指导不面向完全无动手能力的普通小白。目标用户必须：

- 能在指导下打开官方开发者选项或调试模式。
- 能连接 USB、输入一次性配对码或在设备端确认授权。
- 愿意读取并确认安全提示，接受临时开启调试功能的风险。
- 信任 AI 工具和本产品，同时保留最终设备授权决定权。
- 在独自无法完成时，接受远程语音/屏幕指导或受控的语义化辅助流程。

用户信任不能替代技术控制。所有安装、启动和停止仍必须经过设备端确认、一次性配对和可撤销会话，不得因为“用户已经信任我们”就开放任意 shell、文件读写或静默 VPN 授权。

### 2.8 商业资产分层

产品不把“知道 WireGuard 命令”当作商业壁垒。资产按以下边界划分：

| 资产 | 归属 | 决策 |
|---|---|---|
| 免费诊断、错误解释、基础安装知识 | 公开层 | 用于获客和建立信任 |
| 共存 Gate、快照、隔离握手、健康检查、自动回滚、平台适配 | 本地商业核心 | 必须在本地执行，不能依赖云端修改网络 |
| 订单、租约、撤销、设备上限、退款/拒付、价格与功能分层、更新通道 | 云端控制面 | 这是收费和滥用的主要控制点 |
| 多设备面板、远程只读体检、电视/鸿蒙配对、企业策略、优先支持 | 可选云端增值 | 年度订阅的合理价值来源 |
| 流量、DNS 内容、Cookie、支付凭据、WireGuard 私钥、本地服务正文 | 绝不入云 | 只在本机处理 |

完整信任边界见 [COMMERCIAL-TRUST.md](../architecture/COMMERCIAL-TRUST.md)。

## 3. 商业与合规定位

| 项目 | 决策 |
|---|---|
| 免费层 | 只读诊断、风险解释、配置预览、脱敏配置导出和引导 |
| 按次付费单位 | 一次“安全激活/修复任务”（19.90 CNY），不按每次连接收费；执行窗口 14 天（7 天租约 + 7 天离线宽限），自激活（核销）时起算；1 台设备，不自动续费；未核销次数余额不设过期 |
| 个人永久版 | 1 名个人、2 台自有设备；当前大版本永久使用；12 个月更新与支持（199.00 CNY） |
| 家庭年度订阅 | 每个订阅年度最多绑定 12 台不同设备，最多 12 台同时激活（69.00 CNY/年，2026-11-23 定稿） |
| 不售卖 | WARP 免费额度、带宽、固定 IP、匿名性 |
| 支付入口 | 三产品统一走支付宝 aipay「AI网页应用收款」（`alipay.trade.page.pay`，个人收款场景）；现有 Machine Pay 402 流程降级为兼容通道 |
| 支付数据 | Skill 不保存支付密码、商户私钥或原始支付凭证 |
| 高价值操作 | 登录、支付、API Key 创建不得通过 WARP 隧道路由 |
| 合规前置 | 上架前核对 Cloudflare WARP 商业使用条款和 SkillHub 审核规则 |

## 4. 范围

### 4.1 本期范围

- macOS 12 及以上，`amd64` 与 `arm64`
- 一个确定性 `archielink` CLI
- Cloudflare WARP + WireGuard 后端
- 自动、引导、不支持三态能力降级
- 网络快照、隔离探测、全隧道启动、健康检查和自动回滚
- 授权 Provider 抽象与本地测试 Provider
- 经过脱敏的 WireGuard 配置导出
- Agent 可读的 JSON 输出

### 4.2 后续范围

- Windows 11 x64 适配器
- Linux 适配器（systemd-resolved / NetworkManager）
- iOS、Android 与 HarmonyOS 移动端二维码导入
- HarmonyOS 手机、平板和鸿蒙 PC/2in1 伴侣应用
- Android TV 与 HarmonyOS 智慧屏电视伴侣应用
- 电视端最小系统浏览器打开和手机辅助输入
- 同局域网跨设备一次性配对和只读诊断
- Host-Assisted Device Bridge：宿主安装/启动、设备端伴侣应用和语义化 RPC
- Android 手机、Android TV、HarmonyOS 条件性开发者辅助；iOS 走零伴侣官方 WireGuard App 配置导入（D4）
- 正式 SkillHub Pay Skill Provider
- 签名升级包和远程撤销

### 4.3 非目标

- 不构建自有 VPN 节点或流量计费系统
- 不提供匿名网络服务
- 不绕过验证码、访问控制或平台登录限制
- 不在失败时保留部分路由、DNS 或后台进程
- 不由多个 subagent 并发控制同一条全局隧道

### 4.4 交付版本

最终能力按场景拆成独立版本，不在单个版本中一次交付：

| 版本 | 场景 | 交付结果 |
|---|---|---|
| V0.1 | 规格 | 产品边界、路线图和阶段计划 |
| V0.2 | 免费网络体检 | macOS / Windows / Linux 只读诊断 |
| V0.3 | macOS 自动回退 Beta | 自动连接、测试授权、回滚 |
| V0.4 | macOS 付费正式版 | SkillHub Pay、签名授权、退款撤销 |
| V0.5 | Windows / Linux 自动回退 | 跨平台网络适配器 |
| V0.6 | HarmonyOS 手机 / 平板 | 伴侣 HAP、VPN Extension、用户授权 |
| V0.7 | Android TV / HarmonyOS 智慧屏 | 电视 VPN 核心、遥控器交互、最小浏览入口 |
| V0.8 | 鸿蒙 PC / 2in1 | PC 适配、窗口生命周期、条件性自动启动 |
| V0.9 | 跨设备协作 | 一次性配对、远程只读诊断、自动清理 |
| V0.10 | 办公 / 个人 / 移动端 | 引导模式、配置导出、二维码 |
| V1.0 | 跨平台稳定版 | 完整升级、恢复和支持矩阵 |
| V1.1 | 电视浏览器增强 | 大屏浏览器、遥控器搜索、视频播放 |

详细范围与退出条件见 [ROADMAP.md](ROADMAP.md)。

### 4.5 Developer-Assisted Device Bridge（D-track）

D-track 是独立的开发者辅助交付轨道，不占用 V0.x 消费者版本号：

| 阶段 | 目标 | 默认用户 | 发布状态 |
|---|---|---|---|
| D0 | 规格、安全模型、RPC 契约 | 项目开发者 | 内部 |
| D1 | Android 手机/平板 | 能开启 USB/无线调试的技术用户 | 内测 |
| D2 | Android TV / Google TV | 能完成局域网 ADB 配对的技术用户 | 内测 |
| D3 | HarmonyOS 手机/平板/PC/智慧屏 | 能安装签名调试 HAP 并信任 VPN 的用户 | 条件内测 |
| D4 | iOS | 能安装 App Store 应用并完成配置导入的 iPhone/iPad 用户 | 重定义：零伴侣轨道 |

2026-11-18 决定：D4 改为零伴侣轨道。不自研 iOS 伴侣，宿主导出脱敏/完整 WireGuard 配置（`export --format wireguard`，后续补二维码），用户从 App Store 安装官方 WireGuard App（免费、自带 Network Extension 权限）后导入并完成系统 VPN 授权。2026-11-23 决定：零伴侣轨道的完整导出（`--include-private-key`）与二维码（`--format qrcode`）为永久版/家庭版权益（`export_private`），脱敏导出保持免费。已排除的自研替代路线：GitHub 开源 + agent 引导构建侧载——`NEPacketTunnelProvider` entitlement 只发给付费 Apple Developer Program（约 ¥688/年），免费 Apple ID 侧载无法启用该 entitlement 且签名 7 天过期、限 3 个应用，"绕开 Apple 开发者账户自研伴侣"技术上不可行。零伴侣轨道不提供桥 RPC、逐应用分流（iOS 仅 MDM 可做 per-app VPN）和远程状态；若未来因 macOS Developer ID 签名购入付费账号，可另行评估恢复自研伴侣轨道。

Host 只负责设备发现、安装、启动和语义化控制，不接管目标设备网络。VPN 必须由设备端伴侣应用和系统 VPN API 建立，用户必须完成系统授权。

## 5. 核心不变量

1. 任何网络变更前必须先完成能力检测、网络快照和授权检查。
2. 隔离握手测试通过前，不得接管默认路由或系统 DNS。
3. 启动失败必须回滚到快照；回滚失败必须返回最高级错误并保留诊断日志。
4. `up`、`down`、`status` 必须幂等。
5. 同一台设备同一时间只允许一个控制进程持有全局网络锁。
6. 所有秘密文件权限为 `0600`，状态目录权限为 `0700`。
7. 支付、登录和身份验证流量不得经过 WARP。
8. Agent 只能读取结构化结果，不得从自然语言日志猜测是否成功。
9. 无法识别、快照或恢复的网络状态不得被自动接管。
10. 正式二进制不得包含测试授权 Provider 或可替换的信任公钥。
11. 本地安全恢复能力在离线宽限期内不得被云端故障阻断。
12. 云端只签发权益，不下发任意 shell、路由命令或可执行脚本。
13. WireGuard 配置中的 root hook 指令一律拒绝。
14. 商业代码的拆分以信任边界和故障责任为依据，不以“防止抄袭”为由把数据面搬到云端。

## 6. 功能需求

### FR-001 能力检测

`archielink detect` 输出：

- 操作系统、版本、架构
- 是否 root / 管理员
- 是否具备命令执行、提权、系统网络修改和 GUI 能力
- 当前网络接口、路由、DNS、系统代理、PAC、hosts、监听端口和防火墙线索
- 已存在的 WireGuard、Cloudflare WARP、Shadowsocks 或同类隧道
- 支持的运行模式：`auto`、`guided`、`unsupported`
- `--output <path>` 可将 JSON 报告写入文件（0600 权限，已存在文件拒绝覆盖；stdout 输出与文件内容为同一 schema）

### FR-002 网络快照

`archielink up` 在变更前记录：

- 默认路由和重点主机路由
- DNS resolver 文件及活动网络服务 DNS
- HTTP / HTTPS / SOCKS 代理、PAC、环境代理和绕过列表
- hosts 覆盖和 scoped resolver
- 本地监听端口和受保护端点基线
- 现有 utun / WireGuard 接口
- 本地回环关键端口
- 原始网络是否原本可用

快照写入状态目录，供 `rollback` 和 `doctor` 使用。

### FR-003 授权检查

所有会改变网络状态的命令必须经过 `EntitlementProvider`：

- 未授权：只允许 `detect`、`plan`、`doctor`、脱敏 `export`（`--format wireguard` 不带 `--include-private-key`）
- 完整导出（`--include-private-key`）与二维码（`--format qrcode`）需要租约含 `export_private` 权益（永久版/家庭版签发；按次与免费用户不签发）
- 已授权：允许 `up`、`down`、`status`
- 授权过期：停止新的 `up`，但保留 `down` 和 `rollback`

V0.3 的测试授权只存在于 `licensetest` 构建。V0.4 正式授权必须使用编译期固定的信任公钥、设备密钥和短期签名租约；不得从用户环境变量替换信任根。

### FR-004 计划生成

`archielink plan` 不修改系统，只输出：

- 推荐模式
- 将要执行的变更
- 需要的权限
- 预计影响的流量
- DNS / 本地服务例外
- 无法自动支持时的引导步骤

### FR-005 隔离握手

在 `Table=off` 且不修改系统 DNS 的隔离模式下：

- 测试 Cloudflare 注册端点和候选 WireGuard 端点
- 确认至少收到一次握手
- 仅在明确端口集合内测试，不做全网扫描
- 记录可用端点，不把某个 IP 当成永久常量

### FR-006 全隧道启动

隔离握手成功后：

- 启用 IPv4 全隧道
- 设置受控 DNS
- 启动持久守护进程
- 记录进程和接口所有权

### FR-007 本地服务例外

支持按域名、IP、端口和服务类型配置直连或本地 DNS：

- `127.0.0.1`、`localhost`、私有网段始终绕过隧道
- 本地缓存代理和模型网关的上游域名使用原本地 DNS
- 用户配置的业务域名、开发回调和内网域名进入受保护端点注册表
- 支付、身份验证和平台 API 默认列入保护列表
- 例外规则必须由启动快照恢复

### FR-008 健康检查

启动成功后至少验证：

- `https://1.1.1.1/cdn-cgi/trace` 返回 `warp=on`
- 一个外部 HTTPS 目标可访问
- 一个本地回环服务仍可访问（该服务在场时才检查：启动时检测到本地回环服务监听才要求其保持可达，未运行本地服务的机器跳过该项，与受保护端点注册表的检测语义一致）
- 配置的本地例外仍返回预期状态
- 启动前每个受保护端点的基线状态与启动后一致
- 守护进程仍在运行

### FR-009 自动回滚

以下任一情况触发回滚：

- 端点握手超时
- 全隧道启动失败
- DNS 更新失败
- 守护进程退出
- 健康检查失败
- 用户中断启动

回滚必须恢复快照中的路由、DNS、代理和进程状态。

### FR-010 状态、诊断与关闭

- `status`：返回结构化连接状态和健康结果
- `doctor`：区分本机端口、DNS、路由、端点和上游服务故障；`--output <path>` 可将 JSON 报告写入文件（0600 权限，已存在文件拒绝覆盖；与 stdout 为同一 schema）
- `down`：停止本 Skill 启动的隧道并清理例外，不影响启动前已存在的隧道
- `rollback`：从最近快照恢复，不要求当前隧道仍存活

### FR-011 配置导出

`export` 在 MVP 中输出经过脱敏的标准 WireGuard 配置：

- 不包含账户令牌
- 默认遮盖私钥，只有显式 `--include-private-key` 才输出
- 含私钥的完整导出必须落盘到显式指定的文件：`--include-private-key` 必须与 `--output <path>` 同用，文件以 0600 权限创建，已存在文件拒绝覆盖；完整配置不得经 stdout、`--json` 或管道输出（Agent stdout 等同日志，私钥不得进入）
- 传输通道由用户选择（AirDrop / 文件 / 二维码），不进入日志和云端
- 完整导出（`--include-private-key`）与二维码（`--format qrcode`）为永久版/家庭版付费权益：租约必须含 `export_private` feature，未授权时退出码 4（`entitlement_failed`）；脱敏导出（默认 wireguard 格式，无私钥）保持免费；门禁为摩擦型，不锁定用户磁盘上的配置文件本身
- 移动端二维码（V0.10）：`export --format qrcode` 生成 PNG 二维码文件，内容为完整配置（含私钥），供官方 WireGuard App 扫码导入；必须与 `--output <path>` 同用，文件以 0600 权限创建，已存在文件拒绝覆盖；二维码不得经 stdout、`--json` 或管道输出（二维码编码含私钥的完整配置，与文本完整导出同规则）；无 `--output` 时参数错误退出

### FR-012 Agent 降级

当 Agent 缺少执行或提权能力时：

- 返回 `guided`
- 输出分步骤安装说明和可下载配置
- 不声称已启动 VPN
- 不把 shell 文本当作成功状态

### FR-013 鸿蒙伴侣应用

HarmonyOS 手机、平板和鸿蒙 PC 必须通过签名伴侣应用使用系统 VPN：

- 检测 HarmonyOS、API level 和设备类型
- 检测伴侣应用安装、版本和 VPN 信任状态
- 首次启动请求系统用户授权
- 使用 `VpnExtensionAbility` 创建虚拟网卡、路由和 DNS
- 支持应用允许列表或阻止列表
- 与已有 VPN 冲突时提示用户切换，不并发抢占
- 伴侣应用退出、系统撤销授权或 VPN 服务停止后返回明确状态

鸿蒙能力细节见 [HARMONYOS.md](../platform/HARMONYOS.md)。

智能手表、手环、车机、音箱、摄像头、其他 IoT 和通用 OpenHarmony 设备明确返回 `unsupported`，不纳入正式支持矩阵。

### FR-014 电视核心与浏览器

Android TV 和 HarmonyOS 智慧屏的核心交付包括：

- 电视伴侣应用和遥控器操作
- VPN 启动、停止、状态和授权
- 检测 YouTube 客户端、系统浏览器和 Google 服务
- 国内视频应用、投屏和局域网直连保护
- 打开系统浏览器和指定 URL
- 手机二维码或局域网辅助输入网址

V1.0 前不重做完整浏览器。V1.1 再交付遥控器专用地址栏、搜索、历史、全屏视频、User-Agent 和网页兼容增强。

电视能力细节见 [TV.md](../platform/TV.md)。

### FR-015 跨设备只读协作

跨设备首期只允许语义化只读能力：

- 目标设备主动启动一次性 helper
- 控制端通过配对码连接
- 双方确认设备身份
- 只允许 `hello`、`capabilities`、`detect`、`plan`、`doctor`、`close`
- 不提供任意 shell、文件读写或下载
- 会话结束关闭端口并清理临时密钥
- 配对失败、超时、重复使用或指纹不一致时失败关闭

完整安全边界见 [CROSS-DEVICE.md](../architecture/CROSS-DEVICE.md)。

### FR-016 网络共存与断网防护

自动模式必须建立网络所有权图，并满足：

- 默认路由、DNS、代理、PAC、hosts、监听端口和防火墙对象可解释
- 未知透明代理、未知 VPN、未知全局 resolver 或未知路由 owner 时降级
- 所有将被修改的对象都有快照和恢复路径
- 本地代理、缓存代理、模型网关和用户服务进入受保护端点注册表
- 启动后逐项验证端口、上游、DNS 和代理绕过状态
- `incomplete_layers` 非空时禁止自动修改对应层

完整分类和测试矩阵见 [COEXISTENCE.md](../architecture/COEXISTENCE.md)。

### FR-017 Host-Assisted Device Bridge

当受限 Agent 或用户需要从宿主电脑辅助配置手机、平板或电视时：

- Skill 必须先执行能力和风险说明，不得自动打开隐藏工程菜单或使用未公开工厂码。
- 宿主 CLI 只调用官方调试工具：Android 使用 `adb`，HarmonyOS 使用 `hdc`；iOS 走零伴侣轨道，宿主仅导出配置，不操作设备（D4）。
- 宿主可以安装和启动已签名伴侣应用。默认必须完成设备端系统 VPN 授权；若厂商删除了授权 UI，
  只允许技术用户显式执行 `device authorize-vpn --yes`，为当前调试包设置
  `ACTIVATE_VPN` AppOp，并提供 `revoke-vpn`。不得静默设置。
- 宿主与伴侣应用只通过一次性配对、设备指纹和短时 TLS 会话调用白名单 RPC。
- 允许的 RPC 为 `hello`、`capabilities`、`prepare`、`start`、`status`、`stop`、`close`。
- 禁止任意 shell、文件浏览、文件写入、下载执行和未经确认的系统设置修改。
- VPN 私钥与配置由设备端安全存储持有，宿主只传临时加密配置，不保留长期秘密。
- 会话结束必须停止本轮辅助通道、清理临时密钥，并提示用户关闭不再需要的调试功能。

开发者辅助路径不适用于不愿开启调试模式或无法完成设备端确认的用户；此类用户继续使用 `guided` 配置导出和官方客户端导入。

### FR-018 统一收款通道

三产品（按次 / 个人永久版 / 家庭年度）统一经支付宝 aipay Vibe Pay「即刻收」直连模式（`alipay.aipay.nowpaydirect.*`，个人收款场景）收款（2026-11-24 拍板切换）：

- 商品与价格在支付宝「即刻收」管理小程序配置（三个 `out_product_id`），控制面经环境变量绑定 plan → `out_product_id`；价格不由客户端传入
- `archielink buy <plan>`：向控制面 `POST /v1/pay/page-intent` 下单（plan ∈ `activation-task` / `perpetual` / `family-annual-12`，路径兼容保留），按收费模式获取购买入口：
  - 按次（次数包 `QUANTITY`/COUNT）：`purchase.create` 生成购买链接，下单时基线余额记录在订单；支付确认以 `quota.query` 余额超过基线 + `quota.verify` 核销 1 次（幂等消费请求号）为准；14 天执行窗口自核销起算，未核销余额不设过期
  - 个人永久版（永久买断 `ONE_TIME`）与家庭年度（时长包 `ONE_TIME_DURATION`，周期 YEAR）：`purchase.consult` 查询访问决策；`PURCHASE_REQUIRED` 返回购买入口，`ALLOW` 视为已持有权益（幂等下单直接落账）
- CLI 在终端直接渲染购买二维码（`purchase_qr_code` 优先，缺省用 `purchase_url` 生成），用户手机支付宝扫码支付，无需跳转浏览器；购买链接保留为后备展示
- 支付确认由 CLI 轮询驱动（服务端 consult / quota 查询），无异步通知依赖；`external_buyer_id` 使用客户端 InstallID
- 退款与撤销：权益由支付宝托管，平台退款/拒付/撤销事件后授权停止续租；管理员 chargeback 通道（状态撤销 + epoch++，全订单类型可用，2026-11-26 三档实测）；管理员 refund API 仅覆盖有 `provider_trade_no` 的订单（Machine Pay/PagePay），nowPay 直连订单退款能力缺口另立计划（[2026-11-26-nowpay-refund-capability](../plans/2026-11-26-nowpay-refund-capability.md)，购款进商户账户 12 个月内可手动退）
- 原 `alipay.trade.page.pay` 通道（`FAST_INSTANT_TRADE_PAY`）代码保留为后备；「电脑网站支付」签约已提交（2026-11-24 SUBMITTED 等待生效），生效前不可用（2026-11-24 线上 40006 `isv.insufficient-isv-permissions` 现场）
- 现有 Machine Pay 402 流程（`lease pay-intent`，服务单价上限 50 CNY）保留为兼容通道，新购买引导统一走 `buy`
- 支付宝 AI 付款产品准入对比（2026-11-23 调研，2026-11-24 补充）：Machine Pay 服务注册单价硬上限 ¥50，无法承载 ¥69/¥199；PC Agent Pay 为买家侧产品，数字资源商家侧绕回 Machine Pay；SkillPay 无商家 webhook，无法自动签发租约；Token Pay 仅限企业账号；即刻收直连三种收费模式（买断/时长包/次数包）与三产品 1:1 对齐，托管购买页免自建电商页面，权益与核销由支付宝托管。

## 7. CLI 契约

```text
archielink version [--json]
archielink detect [--json] [--output <path>]
archielink plan [--json]
archielink doctor [--json] [--output <path>]
archielink up --yes [--json]
archielink down [--json]
archielink status [--json]
archielink rollback [--json]
archielink export --format wireguard [--include-private-key --output <path>] [--json]   # --include-private-key 需租约含 export_private（永久/家庭）
archielink export --format qrcode --output <path>    # 需租约含 export_private（永久/家庭）
archielink buy <plan> [--server <url>] [--request-id <id>] [--timeout <dur>] [--json]  # plan: activation-task | perpetual | family-annual-12
archielink lease identity [--json]
archielink lease pay-intent --request-id <id> --plan activation-task [--server <url>] [--json]
archielink lease activate --order <id> [--server <url>] [--json]
archielink lease status [--json]
archielink device discover [--tool adb|hdc|auto] [--json]
archielink device install --serial <serial> --package <path> --yes [--json]
archielink device pair --serial <serial> --yes [--json]
archielink device launch --serial <serial> [--tool adb|hdc] [--json]
archielink device status --serial <serial> [--json]
archielink device capabilities --serial <serial> [--json]
archielink device prepare --serial <serial> --config <path> --yes [--profile <name>] [--json]
archielink device authorize-vpn --serial <serial> --yes [--json]
archielink device revoke-vpn --serial <serial> --yes [--json]
archielink device start --serial <serial> [--json]
archielink device stop --serial <serial> [--json]
archielink device close --serial <serial> [--json]
archielink device vpn-probe --serial <serial> [--json]
```

规则：

- 机器可读结果写 stdout；日志写 stderr。
- `--json` 输出必须稳定、可版本化。
- 网络变更命令必须要求 `--yes`。
- Device Bridge 安装命令必须显式指定本地 `.apk` / `.hap` 和目标序列号，并要求
  `--yes`；`--apk` 仅保留为 Android 兼容别名。不提供任意 `adb` / `hdc shell` 入口。
- 退出码：

| 退出码 | 含义 |
|---:|---|
| 0 | 成功 |
| 2 | 参数错误 |
| 3 | 当前平台不支持 |
| 4 | 未授权或授权过期 |
| 5 | 预检失败 |
| 6 | 隧道启动失败，已回滚 |
| 7 | 健康检查失败，已回滚 |
| 8 | 回滚失败，需要人工处理 |
| 9 | 已有一个控制进程持锁 |

## 8. 核心数据模型

```json
{
  "schema_version": "1",
  "mode": "auto",
  "platform": {
    "os": "darwin",
    "version": "12.7.6",
    "arch": "amd64"
  },
  "capabilities": {
    "shell": true,
    "elevation": true,
    "network_mutation": true,
    "gui": false
  },
  "network": {
    "default_interface": "en0",
    "gateway": "192.168.1.1",
    "dns": ["192.168.1.1"],
    "proxy_enabled": false,
    "existing_tunnels": ["ShadowsocksX-NG"]
  },
  "protection": {
    "always_direct_cidrs": ["127.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16"],
    "local_dns_domains": ["api.deepseek.com"],
    "local_ports": [8688]
  },
  "result": "ready"
}
```

状态机：

```text
detected -> planned -> licensed -> probing -> applying -> verifying
   |           |           |          |          |           |
   v           v           v          v          v           v
 blocked     guided     denied     failed    rollback     active
                                                 |
                                                 v
                                        restored | rollback_failed
```

## 9. 平台适配接口

```go
type Adapter interface {
	Name() string
	Detect(context.Context) (model.PlatformReport, error)
	Snapshot(context.Context) (model.NetworkSnapshot, error)
	Probe(context.Context, model.ProbePlan) (model.ProbeResult, error)
	Apply(context.Context, model.ApplyPlan) error
	Verify(context.Context) (model.HealthReport, error)
	Rollback(context.Context, model.NetworkSnapshot) error
}
```

平台代码不得泄漏到核心状态机。核心层只依赖 `Adapter`、`EntitlementProvider` 和 `StateStore`。

## 10. 平台矩阵

| 平台 | 自动模式 | 引导模式 | 关键限制 |
|---|---|---|---|
| macOS 12+ | 是 | 是 | 管理员权限；system resolver 与 route 必须恢复 |
| Windows 11 x64 | 后续 | 是 | 管理员权限；WireGuardNT、PowerShell DNS/route |
| Linux | 后续 | 是 | root；resolved / NetworkManager 差异 |
| Android | 条件性（D1 桥） | 是 | 调试 APK 伴侣；`VpnService`；用户确认 VPN |
| iOS | 否 | 是 | 零伴侣：导出配置给官方 WireGuard App；无桥 RPC 与逐应用分流 |
| HarmonyOS 手机 / 平板 | 否 | 是 | 签名伴侣 HAP；API 11+；用户首次 VPN 授权 |
| Android TV / Google TV | 条件性 | 是 | 固件、GMS、遥控器和应用安装策略差异 |
| HarmonyOS 智慧屏 | 条件性 | 是 | 需设备验证 VpnExtension 和可用内容入口 |
| 鸿蒙 PC / 2in1 | 条件性 | 是 | 需单独验证 VPN Extension、窗口和生命周期 |
| 云沙箱 | 否 | 否 | 无法修改宿主网络，明确返回 unsupported |

## 11. Agent 能力矩阵

| Agent 能力 | 运行模式 |
|---|---|
| Shell + 提权 + 本地文件 | `auto` |
| Shell + 本地文件，无提权 | `guided` |
| 只能读写文件 | `guided` |
| 只能输出文字 | 引导说明 |
| 鸿蒙宿主 + 伴侣应用可用 | `guided`，进入伴侣应用完成授权和连接 |
| 云端 Agent | `unsupported` |

Skill 本身不得通过 Agent 名称判断能力，必须调用 `archielink detect`。

## 12. 授权与 Pay Skill

### 12.1 Provider 接口

```go
type EntitlementProvider interface {
	Verify(context.Context, model.ActivationRequest) (model.Entitlement, error)
}
```

`Entitlement` 至少包含：

- `skill_id`
- `license_id`
- `install_id`
- `features`
- `issued_at`
- `expires_at`
- `signature`

V0.4 租约还必须包含：

- `device_public_key`
- `plan`
- `lease_id`
- `nonce`
- `not_before`
- `offline_grace_until`
- `revocation_epoch`
- `max_devices`
- `max_new_devices_per_year`
- `max_new_devices_per_7_days`
- `device_registration_quota_remaining`

### 12.2 本地与云端职责

| 本地执行器 | 云端控制面 |
|---|---|
| 设备密钥生成与签名 | 订单、退款、拒付和撤销 |
| 租约签名与时间窗校验 | 设备注册、设备上限和异常共享 |
| 离线宽限期内执行安全恢复 | 短期租约续签和 revocation epoch |
| 网络快照、路由、DNS、健康检查和回滚 | 价格、功能分层、兼容策略和更新通道 |

本地执行器不得上传流量内容、DNS 查询内容、Cookie、支付凭据或 WireGuard 私钥。

### 12.3 V0.4 硬门槛

- 测试 Provider 仅在 `licensetest` 构建中存在。
- 正式构建固定公钥，禁止运行时公钥覆盖。
- 设备私钥使用 Secure Enclave；不支持时使用 macOS Keychain。
- 服务端签发短期设备绑定租约，支持撤销、设备上限和离线宽限期。
- 支付通知必须验签、幂等并防重放。
- 退款/拒付后停止续租，但保留 `down` 和 `rollback`。
- 发布包必须签名、公证、可复现并附 SHA-256/SBOM。
- aipay「AI网页应用收款」API、异步通知验签、退款和撤销行为必须取得官方文档并完成沙箱与线上验证；Machine Pay 402 兼容通道维持既有验证。

### 12.4 家庭年度订阅

家庭年度订阅用于个人、家庭成员和朋友的非商业设备，不用于商业代装或转售。

- 每个订阅年度最多注册 12 台不同设备，最多同时激活 12 台。
- 同一设备、同一设备密钥重装不重复占名额。
- 更换设备或设备密钥重置消耗新的年度名额。
- 每个滚动 7 天最多新增 3 台，超出进入冷却或人工复核。
- macOS、Windows、Linux、鸿蒙 PC 和电视伴侣共用同一设备池。
- 虚拟机、容器和临时服务器默认不纳入家庭设备池。
- 账号持有人负责移除设备；撤销只影响后续租约，`down` 和 `rollback` 永远可用。
- 禁止收费代装、转售、出租、公开共享账号和批量部署。

### 12.5 个人永久许可证

- 面向个人 B2C，不是 B2B 默认方案。
- 一名实名个人、2 台本人日常使用设备；每年允许 1 次设备更换。
- 当前大版本永久使用；购买后 12 个月功能、安全更新和基础支持。
- 更新期结束后保留最后可用版本，但新系统和新服务兼容不保证。
- 不含家庭 12 台设备池、云端管理、电视/鸿蒙远程配对、企业策略和优先支持。
- 不得用于公司、机构、收费代装、转售或批量部署。
- 企业客户使用独立 B2B 年度席位授权；离线/私有部署另签企业永久授权与年度维护。
- 免费诊断、`down` 和 `rollback` 不因许可证到期失效。

## 13. 错误与日志

- 错误码稳定，文案可变。
- 自动遮盖 `PrivateKey`、`PublicKey`、Token、Cookie、账户 ID。
- 日志只记录动作和状态，不记录完整配置正文。
- 回滚失败时保留快照文件并输出人工恢复指引。

## 14. 非功能需求

- `detect` 在 5 秒内返回。
- `up` 在 45 秒内成功或明确失败。
- 同一操作连续执行两次不得产生重复路由、DNS 或进程。
- 核心逻辑在无图形界面环境可运行。
- 所有平台适配器使用相同 JSON schema。
- Agent 技能包不得包含缓存、密钥、导出图片或本机状态。
- 网络变更前必须有本地锁；崩溃后锁可被安全识别和回收。

## 15. 测试与验收

### 15.1 单元测试

- 能力三态判定
- 状态机合法和非法迁移
- 授权有效、过期、篡改、退款
- 平台检测与 JSON schema
- 回滚顺序和幂等

### 15.2 集成测试

- 隔离握手成功
- 隔离握手失败但不改默认路由
- 全隧道成功
- DNS 失败自动回滚
- 守护进程退出自动回滚
- 本地 `8688` 服务保持可达
- `api.deepseek.com` 不返回 403
- 重复 `up`、重复 `down`
- 回滚失败进入人工处理状态

### 15.3 退出验收

1. macOS 12 x86_64 实机完成三次 `up -> status -> down`。
2. 模拟三次启动失败，DNS、默认路由、本地服务均恢复。
3. 已存在的 Shadowsocks 配置不被改写。
4. 未授权状态不能启动隧道。
5. 引导模式不执行任何提权命令。
6. Skill 包通过结构、安全和秘密扫描。
7. V0.6 在 HarmonyOS 手机和平板完成伴侣应用授权、启动、停止和冲突测试。
8. V0.7 在 Android TV 和 HarmonyOS 智慧屏完成遥控器生命周期、内容入口和最小浏览入口测试。
9. V0.8 在鸿蒙 PC/2in1 完成窗口、休眠、网络切换和 VPN Extension 能力测试。
10. V0.9 完成 macOS、Windows、Linux 之间至少一组双向只读配对。
11. V0.9 验证错误配对、超时、重复使用和 helper 崩溃后无残留。
12. V1.1 完成电视浏览器增强的专项验收。
13. V0.3 在未知透明代理、已有 VPN、PAC 和快照不完整场景中拒绝自动接管。
14. V0.3 在系统代理、透明代理和本地缓存代理场景完成启动与回滚验证。

## 16. 已知风险

| 风险 | 影响 | 缓解 |
|---|---|---|
| WARP 端点随网络变化 | 启动失败 | 动态探测、限定候选、可回滚 |
| `wg-quick` 先改路由后握手 | 整机断网 | 先隔离探测，再申请全隧道 |
| 境外 DNS 返回错误边缘 | API 403 | 本地 DNS 例外和保护列表 |
| 多个 Agent 并发控制 | 路由互相覆盖 | 单实例锁和 root agent 所有权 |
| 免费 WARP 商业使用边界不清 | 上架或合规风险 | 售卖自动化，不售卖带宽；上架前核实条款 |
| 平台 Pay API 未知 | 无法形成正式收费闭环 | Pay Provider 延迟到官方文档确认后 |
| 二进制分发被平台限制 | 安装失败 | 源码构建 + 签名包双通道；上架前验证 |
| 鸿蒙只支持单 VPN 实例 | 与其他 VPN 冲突 | 启动前检测并提示用户切换 |
| 鸿蒙隧道协议需应用自实现 | VPN Extension 只提供网卡和路由 | 伴侣 HAP 内置并验证 WireGuard 数据路径 |
| 鸿蒙 PC 能力可能与手机不同 | PC 自动模式失效 | PC/2in1 单独验证，失败时降级 |
| AppGallery VPN 审核不确定 | 无法分发伴侣应用 | 开发前核对签名、权限和审核要求 |
| 电视没有 YouTube 或 GMS | VPN 成功但目标内容不可用 | 连接后检查内容入口并区分错误 |
| 智慧屏不开放 VpnExtension | 无法建立系统 VPN | 真机验证，不支持时降级 |
| 电视浏览器兼容和遥控输入困难 | 用户无法完成搜索和播放 | V1.0 只做最小入口，V1.1 专门适配 |
| 跨设备远程能力被误用为任意控制 | 严重安全风险 | 只允许语义化 RPC、双端确认和短时会话 |
| 配对码或临时密钥泄漏 | 未授权访问 | 单次配对、过期、指纹确认和自动清理 |
| 未知透明代理或网络扩展 | 双重路由、断网或服务掉线 | owner 未知时禁止自动模式 |
| 快照不完整 | 回滚后残留配置 | `incomplete_layers` 阻断对应层自动修改 |
| 本地代理上游依赖特定 DNS | 端口可达但业务持续失败 | 基线检查 direct 和 upstream 两层 |

## 17. 已确认假设

- 当前 `warp-fallback` 只作为参考实现，不直接在其上迭代成付费版。
- 首个自动模式只支持 macOS。
- 鸿蒙支持需要独立伴侣 HAP，纯 Skill 不能建立系统 VPN。
- HarmonyOS 手机/平板、智慧屏与鸿蒙 PC/2in1 分开验收。
- TV 核心 VPN 在 V1.0 前交付，完整 TV 浏览器在 V1.1 交付。
- 跨设备首期只读，不开放任意远程 shell 或远程网络写操作。
- 正式收费前必须完成 Pay Skill 和平台审核调研。
- 产品使用结构化 CLI，不让每种 Agent 重复实现网络逻辑。

## 18. 证据

- 当前规则：[warp-fallback SKILL.md](/Users/nocaptain/.agents/skills/warp-fallback/SKILL.md)
- 当前控制脚本：[warp-vpn](/usr/local/bin/warp-vpn)
- 当前实现说明：[CloudflareWARP README.md](</Users/nocaptain/Library/Application Support/CloudflareWARP/README.md>)
- 2026-10-26 安装与排障会话：
  `~/.codex/sessions/2026/10/26/rollout-2026-10-26T17-05-05-01a17774-ca95-7ee1-81d3-df7b11ac8ebe.jsonl`
- 本机实测环境：macOS 12.7.6、x86_64
