# 汪连 ArchieLink｜外网访问专家 — 鸿蒙设备与鸿蒙 PC 支持规格

**状态：** Implemented（D3 全部收工：BRA-AL00 T1 生产化验收 + AGC T2 云真机多机型验证；V0.6 退出条件已对账收口；伴侣 `0.5.0-d3`，签名链独立化完成；V0.8 鸿蒙 PC/2in1 跳过——见 §V0.8 节）
**日期：** 2026-11-05（定稿）；2026-11-19（状态对账）  
**产品规格：** [SPEC.md](../product/SPEC.md)
**路线图：** [ROADMAP.md](../product/ROADMAP.md)

## 1. 结论

鸿蒙手机、平板、智慧屏和鸿蒙 PC/2in1 必须作为独立平台族支持。

纯文本 Skill 不能直接创建系统 VPN。根据 HarmonyOS 官方文档：

- 三方 VPN 需要应用实现 `VpnExtensionAbility`。
- `VpnExtensionAbility` 首批接口从 API 11 开始，仅支持 Stage 模型。
- 应用需要 `ohos.permission.INTERNET`。
- 首次启动 VPN 前，系统会要求用户信任 VPN 应用并确认连接。
- 系统同一时间只支持一个 VPN 连接服务。
- 系统提供虚拟网卡、路由、DNS 和应用分流配置；隧道协议与流量处理由应用自己实现。
- 调用启动接口的控制应用退出时，VPN 服务可能被系统停止。

因此鸿蒙端的实现形式必须是：

```text
Agent Skill
    ↓ 生成配置、诊断和启动意图
签名鸿蒙伴侣应用（HAP）
    ├── ArkUI 用户界面
    ├── VpnExtensionAbility
    ├── WireGuard 隧道实现
    ├── VPN 路由 / DNS / 应用分流
    └── 本地状态与用户授权
```

Agent 负责准备和解释，伴侣应用负责真正建立系统 VPN。

## 2. 目标平台

| 平台族 | 设备 | 首期支持级别 | 说明 |
|---|---|---|---|
| HarmonyOS 手机 | Phone | 伴侣应用引导 | 用户安装并授权后启动 |
| HarmonyOS 平板 | Tablet | 伴侣应用引导 | 与手机共用核心能力 |
| 折叠屏 / 2in1 形态 | Foldable / 2in1 | 伴侣应用引导 | UI 需适配窗口和折叠状态 |
| HarmonyOS 智慧屏 | TV | 伴侣应用引导，条件性自动 | 需验证 VpnExtension 和可用内容入口 |
| 鸿蒙 PC | PC / 2in1 | 伴侣应用引导，条件性自动 | 需验证该设备与 API 是否支持 VpnExtension |
| 云沙箱 / 容器 | 任意 | `unsupported` | 无法修改宿主网络 |

鸿蒙 PC 不得仅因为与平板同属 HarmonyOS 就默认为完全一致；必须单独验证 VPN Extension、窗口生命周期、通知栏、键盘鼠标交互和进程保活。

以下设备明确不在支持范围：

- 智能手表和手环
- 车机
- 音箱、摄像头和其他 IoT 设备
- 非 HarmonyOS 手机、平板、PC、2in1 的通用 OpenHarmony 设备

这些设备如果被检测到，统一返回 `unsupported`，不进入测试矩阵，也不承担 VPN 网关用途。

具体设备型号的支持标记遵循 5.3 的验证分级：仅有 AGC 云真机验证的型号标记为
`compatible (cloud-verified)`，不标记 `supported`。

## 3. 运行模式

### 3.1 未安装伴侣应用

返回 `guided`：

- 说明鸿蒙端需要安装签名伴侣应用。
- 不执行命令。
- 不声称 VPN 已开启。
- 可提供 WireGuard 配置的脱敏预览和导入说明。

### 3.2 已安装但未授权

返回 `guided`：

- 提供打开伴侣应用的深链接或操作步骤。
- 用户必须完成系统 VPN 信任授权。
- Agent 不得尝试绕过授权对话框。

### 3.3 已授权

返回 `app_assisted` 工作流，但在总体模式上仍归类为 `guided`：

- Agent 准备连接配置。
- 伴侣应用启动 `VpnExtensionAbility`。
- 用户确认启动。
- 伴侣应用建立隧道并报告状态。

只有已验证的鸿蒙 PC 才能在能力矩阵中标记为 `auto`，且仍保留系统首次授权。

## 4. 功能需求

### HRM-001 设备检测

检测并输出：

- HarmonyOS
- API level
- Stage 模型
- 设备类型：Phone、Tablet、PC、2in1、Foldable、TV
- CPU 架构
- 屏幕与窗口形态
- 伴侣应用是否安装
- 伴侣应用版本
- VPN 信任授权状态
- 当前是否已有其他 VPN

宿主侧探针（`archielink device vpn-probe`，实现注记，BRA-AL00 真机验证于 2026-11-18）：

- 仅执行固定参数只读命令：`shell ifconfig`、`shell hidumper -s NetsysNative`、`shell ps -ef`，不执行任何写命令，不回传调用方可控文本给 shell。
- VPN 判定 = ifconfig 接口名或 NetsysNative 网络实例中出现 tun/vpn 类接口（如 `vpn-tun`）。`hidumper -s NetsysNative` 为必需探测源：真机证实卓易通（Anco）容器内 VPN（v2VPN）占用系统 VPN 槽位（伴侣侧错误码 2203002 被拒），但其 tun 接口可能不出现在宿主 `ifconfig`（仅 NetsysNative 实例可见，如 `NetId:278  interfaces: {vpn-tun, }`）。漏掉该探测源会把容器 VPN 误报为无 VPN。
- 归因：`self` = VPN 活动且存在 `com.archielink.companion:vpn` 扩展进程、且（ifconfig 探测不可用或宿主 netns 存在 tun 类接口）；`other` = VPN 活动且无该进程，或有该进程但 tun 类接口不在宿主 netns（伴侣扩展被 2203002 拒绝后进程会残留，且容器 VPN 的 tun 可能留在容器 netns）；`none` = 无 VPN 活动；`unknown` = ps 探测失败。
- 已知限制：伴侣扩展进程残留期间，若其他 VPN（含容器 VPN 重连后 tun 迁入宿主 netns 的情况）恰好占位，固定只读命令无法区分，探针会误报 `self`；槽位归属的最终仲裁是伴侣侧 `startVpnExtensionAbility` 错误码 2203002。
- 真机实测（BRA-AL00）：无 VPN → `none`；伴侣连接 → `self`（tun 10.2.0.2/32）；容器 VPN 无伴侣进程 → `other`；容器 VPN + 伴侣残留（tun 在容器 netns）→ `other`。
- 零售 BRA-AL00 无 `netcfg` 命令、VPNManager 服务无 hidumper 输出，故均不使用。

### HRM-002 伴侣应用

伴侣应用至少包含：

- ArkUI 启动和停止界面
- VPN 状态与连接时长
- 首次信任授权流程
- 通知栏或状态栏状态
- `VpnExtensionAbility`
- WireGuard 隧道实现
- 路由和 DNS 配置
- 应用允许/阻止分流
- 本地日志和脱敏诊断

### HRM-003 分流

使用 HarmonyOS VPN 配置支持：

- 默认路由
- 指定 CIDR 路由
- IPv4 / IPv6
- DNS 地址
- `trustedApplications`
- `blockedApplications`

保护以下应用或服务不被错误代理：

- SkillHub Pay
- 微信 / 支付宝
- 系统登录和身份验证
- 本地伴侣应用
- 用户配置的本地服务

### HRM-004 生命周期

- 用户明确启动后才创建 VPN。
- 控制应用进程退出前提示用户 VPN 可能停止。
- 支持从伴侣应用主动停止。
- 异常退出后重新打开应用时检测残留状态。
- 检测到其他 VPN 已运行时，提示用户先切换，不能并发抢占。
- 长任务（持续任务）通知与 VPN 状态绑定：仅当 VPN 处于开启状态
  （`preparing`/`tun_ready`/`handshaking`/`connected`）时允许持有长任务；
  应用退后台、系统取消通知后的重新请求都必须先校验 VPN 状态。
- VPN 处于非开启状态时，长任务通知必须下线；守护（watchdog）检测到
  VPN 已停止时负责下线长任务通知并自行停止，守护仅在 VPN 开启状态期间运行。

### HRM-005 Agent 接口

Agent 可调用：

- 打开伴侣应用
- 打开诊断页
- 传入脱敏配置摘要
- 查询伴侣应用状态

Agent 不得：

- 静默安装 HAP
- 绕过 VPN 信任授权
- 直接读写系统密钥
- 在未签名或调试构建中处理真实支付授权

### 当前实现（2026-11-08）

- 宿主支持 HDC 设备发现、HAP 安装和固定 Activity 启动，默认不开放任意 shell。
- `mobile/harmony-companion` 使用 API 23 Stage 模型，已注册
  `VpnExtensionAbility`，支持自动调试签名和签名 HAP。
- HarmonyOS 7 `BRA-AL00` 真机已验证签名、安装、调试启动和首屏渲染。
  `VpnExtensionAbility` 可通过独立 `:vpn` 进程启动并在停止后清理。
- HarmonyOS 7 对普通 `aa start` 返回 `10106105` AppStore 管控；开发宿主必须使用
  `aa start -D` 调试标志启动。
- `VpnConnection.create` 已验证：保留测试路由 `198.18.0.0/15`、地址
  `198.18.0.1/15`、MTU 1280 的 `vpn-tun` 可创建并返回 TUN fd，不接管默认路由
  和 DNS；停止后 TUN、路由和扩展进程均清理。
- 应用图标直接复用 Android TV 圆角图标原始字节，不做分层重绘。
- WireGuard 数据面已接入 TUN fd：Noise IKpsk2、ChaCha20-Poly1305、Transport、
  扩展进程 NAPI UDP 和 WARP 外网均已真机通过。
- 下一步将全局 VPN 改为应用白名单 VPN，仅用户添加的应用进入隧道；新 UI
  需先通过 CSS Inspector 人工定稿。
- 2026-11-09 真机已验证应用白名单界面：设置页支持应用开关和手动新增包名；
  应用卡左滑使名称越过画布后自动滑出删除，行高收拢并带动下方应用行上移。
  `BRA-AL00` 长距离滑动单次仅删除一项，清单和禁用状态均持久化。连续帧证据位于
  `artifacts/harmony-companion/native-swipe-verification/`。
- 其他 VPN 共存提示（2026-11-17）：`vpnConnection.create` 冲突由扩展进程映射为
  `conflict` 状态并写入跨进程状态文件（"检测到其他 VPN 正在运行…"），UI 轮询
  观测到该状态迁移时经 `UIContext.showAlertDialog` 弹出明确提示（冷启动残留的
  `conflict` 状态不弹）。实测（BRA-AL00 + V2VPN 共存）系统侧错误消息固定为
  `VPN exist already, please execute destroy first`（未文档化错误码，故按
  netmanager 2203002 错误码 + 消息双重判定）；BRA-AL00 本地真机实测 PASS
  （2026-11-17，与 V2VPN 共存，弹窗与按钮回落均验证，证据位于
  `artifacts/harmony-companion/conflict-verification/`）；满足 HRM-004"提示
  用户先切换，不能并发抢占"与 V0.6 退出条件"存在其他 VPN 时给出明确提示"。
  V0.6 退出条件的实机范围同步修正为 API 23+（minSdk `6.1.0(23)`）。
- 本地真机验证签名约束（2026-11-17 实测）：release 发布签名 HAP 无法侧载
  （`hdc install` 报 `9568322 not trusted app source`），本地真机一律
  `devecocli run --uninstall` 走 debug 调试签名；release 仅经 AGC 渠道分发。
  `run --skip-build` 按相同 `versionCode` 判定不重装（代码变更未升版本时不会
  部署新包），换装新代码必须带 `--uninstall` 或升 versionCode。
- 全屏页面转场时长（2026-11-17 AGC UX 规范 + 复验实测）：main↔settings 全屏
  切换的转场动效时长按屏幕物理尺寸分档——8.5 英寸以下 ≥200ms、8.5 至 12 英寸
  ≥250ms、12 英寸以上 ≥300ms（AGC 质量标准）。实现：`goToPage` 经
  `display.getDefaultDisplaySync()` 宽高/DPI 计算对角英寸动态取
  450/550/650ms（`Curve.FastOutSlowIn` + `TransitionEffect`）；实测依据：
  260ms 设计时长在 AGC 帧差检测下仅识别 142ms（FastOutSlowIn 前段完成主要
  变化，检测值约为设计值的 55%），三档预测检测值 246/300/355ms，各档余量
  18–23%。首次复验（HAP `dec9646d`）：24 项仅 1 警告（转场 142ms），图标
  分层项 PASS。分档实测：Mate XT 非凡大师三屏展开（10.2"，8.5-12" 档 550ms）
  复验 PASS，转场警告关闭（`c8cab921`）；手机档与 >12" 档待下次上传顺带复验。
- 大屏/折叠屏布局适配（2026-11-17 Mate XT 云测试警告，2026-11-18 截图证据
  定案）：AGC 质量标准要求页面留白率在 [0.1, 0.7]（设计元素 box 占窗口
  ≥30%）；Mate XT 三屏展开（3184×2232）下设置页留白 0.9819。截图取证结论
  （subagent 像素分析）：渲染正常、全宽铺满、非动画态、应用全屏——根因是
  审核按**元素 box 面积求和**口径统计：白底页面（#ffffff）上的近白卡片
  （#fbfdfc，差异 1.6%）不可检测，仅剩文字+小按钮 ≈1.8%，且内容只占上半屏
  44% 高度。修复方案（覆盖元素面积与包围盒两种口径）：
  ① 卡片可检测化——背景 #fbfdfc → #f2f7f4、边框 #dce6e2 → #ccdcd5，
  使大块卡片区域可被识别为设计元素；
  ② 高度铺满——Scroll 内容列 `constraintSize({ minHeight: '100%' })` +
  已加入清单卡片 `layoutWeight(1)` 撑满剩余高度，使内容包围盒覆盖全屏。
  不采用双栏分栏（元素面积口径无增益、包围盒口径反而减半）。
  生效范围（2026-11-18 用户拍板）：仅对角线 ≥8.5 英寸大屏生效（折叠屏
  展开态/大平板；`diagonalInches() >= 8.5`，与转场分档共用对角计算），
  手机与折叠态、小平板维持原布局原色值；审核警告疑似机器误判口径，
  不阻塞上架（用户确认），修复为尽力达标而非硬性要求。
- 发布签名链独立化（2026-11-18 切换）：独立 EC P-256 材料链在
  `~/.ohos/config/archielink/`——`archielink_release.p12`（私钥，alias
  `archielink`）、`archielink_release_chain.pem`（叶证书 + Huawei CBG
  Developer Relations CA G2 中间链，PEM）、`ArchieLinkRelease.p7b`（release
  Profile，`com.archielink.companion`，从 p7b 提取的 distribution-certificate
  公钥与 p12 私钥闭环验证）。构建流程：hvigor 构建产出 unsigned 包（SignHap
  失败属预期，见下）→ `hap-sign-tool.jar sign-app` 手动签名。关键坑：
  ① build-profile.json5 密码字段必须是 ≥32 字符的 DevEco 加密串，CLI 手写
  明文报 "Invalid initialization vector"（00303242），故 CLI 走手动签名，
  DevEco Studio GUI 配置签名可自动加密；② AGC CSR 校验只收 EC P-256，
  RSA 4096（含 SHA256withRSA 重签）一律报"上传的CSR文件无效"；
  ③ `sign-app` 参数名：`-mode localSign`、`-appCertFile`、`-keyPwd`、
  `-keystorePwd`、`-compatibleVersion 23`，密码字段长度 <32 报 00303116；
  ④ `appCertFile` 必须是证书链文件（单证书报 11013004 Profile cert must
  a cert chain），链 = 叶证书（从 p7b 内 distribution-certificate 提取）
  + 中间 CA（Huawei CBG Developer Relations CA G2）；⑤ keystore 同目录
  必须存在 `material/` 子目录（ENOENT stat 报 00308018）。首签产物
  `c8594d89b7ac99b5`（代码同 `c8cab921`，签名链独立）。

### 生产化加固（2026-11-10 实机验收）

五个根因与修复，全部真机验证：

1. **双向 NAT**：容器 VM 原始流量（如 `172.17.1.2`）经 NAT 流表改写为隧道
   地址（`natByFlow`/`natByReply`，NAT 端口 10000–19999，TCP/UDP/ICMP echo，
   全量校验和重算，4096 条 LRU）。缺它时 Instagram 等依赖回包的应用全部失败。
2. **应用分流**：容器场景实际生效的是 `trustedApplications` 白名单
   （`shell_assistant` + `hmos_browser`），国内应用保持直连；全隧道模式会让
   国内应用不可用。
3. **隧道内 DNS 劫持重定向**：容器 DNS 查询（任意 53 端口）重定向到隧道 DNS
   （`rewriteDnsDst`/`restoreDnsSrc`，512 条 + 60s TTL），否则走物理网络被
   污染。DNS 查询/应答域名日志（QNAME 解析，500 独立域名上限）用于诊断。
4. **WireGuard rekey 必须实现**：手写协议只握手一次时，会话在 180s
   （REJECT_AFTER_TIME）后被对端静默丢弃，表现为"连接 3 分钟必死"。实现为
   120s 定时轮换、5s 重试、90s 无收包停滞强制 rekey、旧会话 20s 收包宽限
   （双路解密）、rekey 期间转发循环不中断。
5. **保活通知防划掉**：实测死因链为"用户划掉通知（`continuousTaskCancel`
   reason 23）→ 主进程后台冻结（`FROZEN_AFTER_ENTER_BG`）→ 扩展被 LMK
   杀死无人重启 → 掉线"。修复为监听 cancel 回调立即重新申请长时任务（限速
   20 次/时）、`getAllContinuousTasks` 系统核验（`ensure/verifyVpnBackgroundTask`）、
   主进程看门狗 5s/30s 巡检，扩展被杀后 15s 冷却自动重连，冷启动自愈。

进程模型事实：`:vpn` 扩展无法避免被 LMK 杀死（Stage 模型限制），必须由主
进程长时任务 + 看门狗负责检测与重连；主进程自身的存活依赖保活通知不被移除。

### 出口 IP 归因（2026-11-10）

- WARP 免费出口 `104.28.223.164` 被主流 geo 库标记为中国河北石家庄。
- TikTok 边缘拒绝中国 IP 连接（App 显示"没有网络"）；Gemini/ChatGPT 按区域
  封锁。Instagram 不封中国来源 IP，因此同一隧道下正常。
- 换 ProtonVPN 日本节点（标准 WireGuard conf 导入）后 Gemini/ChatGPT 恢复。
- TikTok 换节点后仍无网络：DNS 解析、TCP 连接、隧道均正常，判定为 TikTok
  应用层检测（大陆 SIM/设备环境），不属于 VPN 层可修复范围，已放弃并记录为
  外部限制。
- 验证出口 geo 的方法：容器内浏览器访问 IP 回显站点（如 ipinfo.io）。

### HRM-006 鸿蒙 PC

鸿蒙 PC 额外要求：

- 键盘、鼠标和触控板操作
- 可调整窗口和最小窗口布局
- 多任务切换
- 通知中心行为
- PC/2in1 VPN Extension 支持验证
- 休眠、唤醒、网络切换
- 企业受管设备策略

### HRM-007 鸿蒙智慧屏

鸿蒙智慧屏额外要求：

- 在真实设备上验证 `VpnExtensionAbility`
- 遥控器方向键、确认键和返回键操作
- 大字号和强焦点态
- 检测系统浏览器和可用内容入口
- 允许已有视频应用直接受益
- 无浏览器或无可用视频入口时明确返回 `needs_browser`

电视 VPN 与最小浏览入口的完整要求见 [TV.md](TV.md)。

## 5. 技术边界

### 5.1 已验证的官方能力

官方文档确认：

- `VpnExtensionAbility` API 11+，Stage 模型
- `ohos.permission.INTERNET`
- 首次用户 VPN 授权
- 单 VPN 实例限制
- VPN 虚拟网卡、路由、DNS 和应用分流
- 隧道协议需应用内部实现

### 5.2 尚需设备验证

- 各 HarmonyOS API level 对 WireGuard 隧道实现的兼容性
- 鸿蒙 PC / 2in1 是否完整开放 VpnExtension 能力
- HarmonyOS 智慧屏是否开放 VpnExtension 能力
- 伴侣应用退出后的系统停止时机
- 鸿蒙 PC 休眠、唤醒和网络切换行为
- AppGallery 对 VPN 类应用和网络扩展权限的审核要求
- SkillHub Pay 与应用内购买、微信或支付宝支付的组合限制

### 5.3 设备验证分级与 AGC 云真机

自有真机不足时，部分设备验证可借 AGC 云测试（华为实验室托管的云真机）推进。
设备型号的支持标记按三级区分，不得混淆：

| 级别 | 验证环境 | 可标记 | 覆盖范围 |
|---|---|---|---|
| T1 自有真机 | 本地 USB/网络连接的自有设备 | `supported` | 安装、启动、授权、隧道数据面、长稳保活、VPN 冲突、host HDC bridge |
| T2 云真机 | AGC 云测试 / 远程真机 | `compatible (cloud-verified)` | 安装、启动、授权 UI 流、多机型兼容、性能基线 |
| T3 未验证 | 无 | 不标记 | — |

T2 规则：

- AGC 云真机可计入兼容性验证，但不足以支撑以下结论，相关项保持
  `deferred` 并等待 T1：
  - host 侧 HDC bridge（discover/pair/install/launch）——云设备不与本地宿主连接；
  - 长稳与保活——云测试会话时长受平台限制，无法等效多天稳定性；
  - 真实出口与分流效果——云设备网络环境受控，WARP 出口和国内直连保护不可等效验证。
- AGC 实际可用机型、API level 与自动化能力以控制台为准；某机型或能力缺失时
  该项保持 `deferred`，不得以推断补齐，也不得因此标记 `unsupported` 以外的
  支持级别。
- T2 结果（截图、日志、机型清单）归档 `artifacts/harmony-companion/cloud-test/`，
  并回写机型矩阵；支持列表中的云验证型号必须带 `cloud-verified` 标记。
- 云测试上传的 HAP、自动化脚本与配置不得包含生产信任根私钥、真实 WireGuard
  配置或用户数据；测试配置使用专用占位端点。

## 6. 分发与签名

鸿蒙支持需要两类产物：

| 产物 | 用途 |
|---|---|
| `archie-link` Agent Skill | 诊断、配置准备、状态解释 |
| `汪连 ArchieLink HarmonyOS` HAP | VPN 授权、隧道、状态和 UI |

要求：

- 生产 HAP 使用正式签名。
- Debug 签名不得进入付费发行。
- Skill 包不包含私钥、用户配置和支付凭证。
- HAP 与 Skill 使用明确的兼容版本表。
- 伴侣应用升级后不得破坏已有授权和路由配置。

## 7. 版本安排

### V0.6：HarmonyOS 手机与平板

交付：

- 设备与 API 检测
- 签名伴侣 HAP
- VpnExtensionAbility
- WireGuard 隧道
- 引导启动与用户授权
- 状态、停止和错误码

退出条件：

- API 23+（应用 minSdk `6.1.0(23)`）的 Phone 和 Tablet 实机完成启动、状态、停止。
- 首次授权流程符合系统要求。
- 存在其他 VPN 时给出明确提示。
- 控制应用退出和异常恢复有可观察结果。

### V0.7：HarmonyOS 智慧屏

交付：

- 智慧屏设备与 VpnExtension 能力检测
- 遥控器优先的伴侣界面
- 系统浏览器和内容入口检查
- 最小浏览入口

退出条件：

- 至少一台智慧屏实机完成授权、启动、停止和内容入口测试。
- 不支持 VpnExtension 或无浏览器时准确降级。

完整阶段定义见 [TV.md](TV.md)。

### V0.8：鸿蒙 PC / 2in1

**状态：跳过（2026-11-19 用户拍板，保持原决定）。** 真实卡点：AGC 云调试有 PC 形态远程真机，但无法上传 WireGuard conf 配置文件到远程真机（云真机无文件导入通道），唯一的剪贴板路径被用户否决；无自有鸿蒙 PC/2in1 真机。伴侣 `deviceTypes` 保留 2in1 声明，设备到手或 AGC 开放文件导入后直接开验，无需改代码。

交付：

- 桌面窗口和键鼠交互
- PC/2in1 VPN Extension 适配
- 休眠唤醒和网络切换
- 鸿蒙 PC 专属诊断
- 条件性 Agent 自动启动

退出条件：

- 至少一台鸿蒙 PC/2in1 实机完成三次生命周期。
- 窗口、通知、休眠和网络切换测试通过。
- 若设备或系统不开放 VPN Extension，自动降级为明确引导。

## 8. Agent 适配

| Agent 能力 | 鸿蒙行为 |
|---|---|
| 可执行命令并连接鸿蒙设备 | 可调用伴侣应用状态接口 |
| 只能读写文件 | 生成诊断 JSON 和安装说明 |
| 只能输出文字 | 提供分步引导 |
| 宿主为鸿蒙设备 | 通过伴侣应用和用户授权启动 |
| 云沙箱 Agent | `unsupported` |

不得通过 Agent 名称决定行为；必须检测宿主和伴侣应用能力。

## 9. 测试矩阵

### 设备

- HarmonyOS Phone
- HarmonyOS Tablet
- Foldable
- HarmonyOS Smart Screen / TV
- HarmonyOS PC
- 2in1

### 系统

- API 11 及以上
- 当前 HarmonyOS 稳定主版本
- 当前 HarmonyOS PC/2in1 稳定版本

### 网络

- Wi-Fi
- 蜂窝网络（手机）
- IPv4 / IPv6
- 已有其他 VPN
- DNS over HTTPS
- 休眠唤醒
- 网络切换
- 企业代理或受限网络

### 权限

- 首次拒绝 VPN 授权
- 首次接受后授权
- 系统设置撤销授权
- 其他 VPN 已运行
- 伴侣应用被卸载或升级

## 10. 验收

1. 未安装伴侣应用时，Skill 明确返回 `guided`，不声称 VPN 已启动。
2. 安装后首次启动出现系统 VPN 信任授权。
3. 用户拒绝授权后不建立 VPN。
4. 用户接受后可完成启动、状态和停止。
5. 系统中已有其他 VPN 时不抢占，不并发启动。
6. 伴侣应用异常退出后能检测并报告残留状态。
7. 鸿蒙 PC/2in1 若不支持 VpnExtension，明确进入 `guided` 或 `unsupported`。
8. 支付、登录和身份验证不会被 VPN 配置错误代理。

## 11. 官方文档依据

本地 DevEco 文档，读取日期 2026-11-05：

- `开发指南/网络/Network_Kit_网络服务/连接网络/连接VPN/net-vpnextension`
- `API参考/网络/Network_Kit_网络服务/ArkTS_API/ohos_app_ability_VpnExtensionAbility_三方VPN能力_/js-apis-vpnextensionability`
- `API参考/网络/Network_Kit_网络服务/ArkTS_API/ohos_net_vpnExtension_VPN增强管理_/js-apis-net-vpnextension`
- `FAQ/网络/网络_Network/应用切换到后台后VPN功能保活的解决方案/faqs-network-134`
- `FAQ/网络/网络_Network/HarmonyOS下VPN的DNS解析顺序及域名拦截失效的处理方案/faqs-network-126`
