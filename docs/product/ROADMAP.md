# 汪连 ArchieLink｜外网访问专家 — 版本与场景路线图

**状态：** Draft v0.1  
**日期：** 2026-11-05  
**产品规格：** [SPEC.md](SPEC.md)

## 1. 分期原则

每个版本必须同时满足三个条件才算完成：

1. 对应一个明确用户场景，而不是只完成技术模块。
2. 有独立、可观察的验收结果。
3. 不依赖后续版本才能工作。

共享核心可以跨版本演进，但平台适配、支付接入和 Agent 降级不能混成一个不可验收的大版本。

## 2. 场景与版本映射

| 版本 | 主场景 | 平台 | Agent 能力 | 收费状态 |
|---|---|---|---|---|
| V0.1 | 规格与可行性确认 | 文档 | 不适用 | 不收费 |
| V0.2 | 免费网络体检 | macOS / Windows / Linux 的只读检测 | 有文件或 shell 能力 | 免费获客 |
| V0.3 | macOS 自动回退 Beta | macOS 12+ | shell + 管理员权限 | 测试授权，不公开收款 |
| V0.4 | macOS 付费正式版 | macOS 12+ | shell + 管理员权限 | SkillHub Pay |
| V0.5 | Windows / Linux 自动回退 | Windows 11、主流 Linux | shell + 管理员权限 | 按平台授权 |
| V0.6 | HarmonyOS 手机 / 平板 | HarmonyOS 手机、平板、折叠屏 | 伴侣应用 + 用户授权 | 引导 / 伴侣应用授权 |
| V0.7 | Android TV / HarmonyOS 智慧屏 | Android TV、Google TV、HarmonyOS 智慧屏 | 遥控器 + 伴侣应用 | 核心功能授权 |
| V0.8 | 鸿蒙 PC / 2in1 | HarmonyOS PC、2in1 | 伴侣应用 + 条件性自动 | 按平台授权 |
| V0.9 | 跨设备只读协作 | macOS / Windows / Linux 同局域网 | 一次性配对 + Controller / Target | 多设备增值授权 |
| V0.10 | 办公 Agent / 个人 Agent / 移动端 | 受限 Agent、iOS、Android | 文件、GUI 或仅提示 | 引导模式授权 |
| V1.0 | 跨平台稳定版 | 所有正式支持环境 | 自动 / 引导 / 不支持三态 | 完整授权与续期 |
| V1.1 | 电视浏览器增强 | Android TV、HarmonyOS 智慧屏 | 遥控器优先的大屏浏览器 | 高级版或增值包 |

## 3. V0.1：规格基线

### 目标

锁定产品边界、目标场景、失败回滚原则和付费架构。

### 交付物

- `SPEC.md`
- `ROADMAP.md`
- V0.2 与 V0.3 的独立实现计划

### 退出条件

- 产品负责人确认“售卖自动化，不售卖 WARP 带宽”。
- 确认首期自动模式只支持 macOS。
- 确认鸿蒙采用“Skill + 签名伴侣 HAP”架构。
- 确认 HarmonyOS 手机/平板、TV 核心与鸿蒙 PC/2in1 分开验收。
- 确认电视浏览器增强放在 V1.0 后。
- 确认正式 Pay Provider 在获得平台文档前不进入开发。

## 4. V0.2：免费网络体检

### 用户场景

用户不确定问题是 DNS、路由、代理、端点还是本地服务；希望先得到可读诊断，再决定是否安装或付费。

### 功能

- `archielink detect`
- `archielink plan`
- `archielink doctor`
- 结构化 JSON 与人类可读摘要
- 自动判断 `auto`、`guided`、`unsupported`
- 不修改任何网络状态
- 不上传诊断内容

### Agent 适配

- 有 shell：执行只读 CLI。
- 只有文件读写：读取导出的诊断 JSON。
- 只有文字能力：按固定模板说明检测项。

### 明确不做

- 不安装 WireGuard
- 不修改 DNS、路由或代理
- 不启动隧道
- 不验证付费权限

### 退出条件

- 在 macOS、Windows、Linux 各至少一台机器上输出正确的能力三态。
- 所有命令均可证明没有执行网络写操作。
- 同一输入生成稳定 JSON schema。
- Skill 包不超过 8KB 的 Agent 主说明，详细说明外置。

## 5. V0.3：macOS 自动回退 Beta

**实现状态：** 连续三次真实生命周期验收通过（2026-11-05）；独立故障注入仍待执行。

### 用户场景

macOS 用户已确认需要外网备用线路，允许安装工具并提权；要求出现故障时绝不把整机网络留在半配置状态。

### 功能

- 授权 Provider 抽象与测试授权
- 网络快照
- 网络所有权图与未知状态防线
- PAC / system proxy / 环境代理检测
- hosts、scoped DNS 和受保护端点注册表
- 隔离握手
- 全隧道启动
- 本地 DNS 与回环保护
- 健康检查
- 自动回滚
- `up`、`down`、`status`、`rollback`
- 脱敏 WireGuard 配置导出

### 明确不做

- 不接真实微信 / 支付宝支付
- 不提供 Windows / Linux 自动模式
- 不提供移动端二维码
- 不做自动更新

### 实施计划

[2026-11-05-archie-link-v0.3-macos-auto.md](../plans/2026-11-05-archie-link-v0.3-macos-auto.md)

### 安全前置

[COEXISTENCE.md](../architecture/COEXISTENCE.md)

### 退出条件

- macOS 12 x86_64 连续三次 `up -> status -> down`。
- 三类故障注入均在时限内回滚。
- 本地 `127.0.0.1:8688` 保持可达。
- `api.deepseek.com` 不返回 CloudFront 403。
- 现有 Shadowsocks 配置不变。
- 未知透明代理、未知全局 DNS 或未知默认路由 owner 时拒绝自动模式。
- 系统代理、透明代理和本地缓存代理三类场景完成启动与回滚。
- DNS、PAC、hosts、proxy bypass 和 route 均有快照/恢复证据。

## 6. V0.4：macOS 付费正式版

### 用户场景

普通用户通过 SkillHub Pay 使用微信或支付宝购买 macOS 自动能力。

### 功能

- 正式 SkillHub Pay Provider
- 平台订单、退款、撤销和续期语义
- 编译期固定信任公钥
- Secure Enclave/Keychain 设备密钥
- 7 天短期租约、30 天离线宽限和 48 小时续租窗口
- 设备安装 ID、授权设备上限和 revocation epoch
- 家庭年度订阅：12 台不同设备/订阅年、最多 12 台同时激活、3 台/7 天新增上限
- 个人永久版：1 人、2 台自有设备、当前大版本、12 个月更新与支持
- B2B 企业年度授权：5 席起、设备回收、发票、企业策略与可选 SLA
- 设备绑定、订单幂等、支付通知防重放
- 退款/拒付后停止续租，但保留 `down` 和 `rollback`
- 生产签名、公证和安装包校验
- `-trimpath`、可复现构建、SBOM 和发布哈希
- 支付、登录、身份验证的直连保护

### 进入开发条件

- 已取得 SkillHub Pay Skill 的正式接口文档。
- 已验证回调签名和退款撤销行为。
- 已确认 Cloudflare WARP 与平台审核允许该收费形态。
- 已确认“安全激活/修复任务”作为按次计费单元的展示与退款口径。

### 退出条件

- 测试订单完成购买、激活、退款、撤销、续期全链路。
- 未授权、过期和撤销状态下都无法启动网络变更。
- 正式二进制不存在 `licensetest`、`faultinjection` 代码或运行时可替换公钥。
- 设备租约无法通过复制文件、替换公钥或回拨时钟绕过。
- 12 台家庭设备池支持同设备重装不重复计数、换机消耗新名额、账号持有人在云端移除设备。
- 亲友非商业使用条款、收费代装/转售限制和异常设备轮换复核可执行。
- 个人永久版不包含家庭设备池和云管理；企业采购不得使用个人永久版。
- 云端故障或离线宽限期内，`down` 和 `rollback` 始终可用。
- 支付流量不经过 WARP。
- 正式包通过平台安全扫描。

## 7. V0.5：Windows / Linux 自动回退

### 用户场景

本地开发 Agent、办公电脑和 Linux 服务器需要与 macOS 相同的安全启动和回滚能力。

### 平台边界

- Windows 11 x64
- Windows 10 仅引导模式
- Ubuntu 22.04+、Debian 12+ 为自动模式候选
- 其他 Linux 发行版按适配器能力检测决定

### 功能

- Windows WireGuardNT 适配器
- PowerShell DNS / 路由快照与恢复
- Linux kernel WireGuard 或 `wireguard-go`
- `systemd-resolved` / NetworkManager 两套 DNS 适配器
- 平台无关 JSON 契约

### 退出条件

- 每个正式支持平台完成三次生命周期和三类故障回滚。
- Windows 和 Linux 的退出码、状态 schema 与 macOS 一致。
- 不支持的系统明确进入 `guided`，不尝试半自动修改。

## 8. V0.6：HarmonyOS 手机与平板

### 用户场景

用户在 HarmonyOS 手机、平板或折叠屏上需要使用网络回退能力。Agent 不能绕过系统 VPN 授权，需要把配置和启动意图交给签名伴侣应用。

### 功能

- HarmonyOS 设备检测
- API level、Stage 模型和设备类型检测
- 签名伴侣 HAP
- `VpnExtensionAbility`
- WireGuard 隧道实现
- 系统 VPN 信任授权
- 路由、DNS、应用允许/阻止列表
- 启动、状态、停止和错误码
- 与已有 VPN 冲突检测

### 明确不做

- 不静默安装 HAP
- 不绕过系统 VPN 授权
- 不在鸿蒙手机上宣称完全无感自动
- 不与已有 VPN 并发抢占
- 不支持智能手表、车机、IoT 和通用 OpenHarmony 设备

### 规格

[HARMONYOS.md](../platform/HARMONYOS.md)

### 退出条件

- API 11+ Phone 和 Tablet 实机完成启动、状态和停止。
- 首次接受、拒绝和撤销 VPN 授权均有明确结果。
- 伴侣应用退出后系统 VPN 状态可被检测。
- 存在其他 VPN 时不并发启动。

## 9. V0.7：Android TV 与 HarmonyOS 智慧屏

### 用户场景

用户在电视上直接访问 YouTube 或其他外网视频，不希望手机投屏，也不希望研究系统 VPN 和浏览器兼容。

### 功能

- Android TV / Google TV 伴侣应用
- Android `VpnService`
- HarmonyOS 智慧屏伴侣应用
- 条件性使用 `VpnExtensionAbility`
- 遥控器方向键、确认键和返回键
- 大字号、强焦点态
- YouTube 应用、系统浏览器和 Google 服务检测
- 打开系统浏览器和指定 URL
- 手机二维码或局域网辅助输入
- 国内视频应用、投屏和局域网直连保护
- 待机、唤醒和网络切换

### 明确不做

- 不在 V1.0 前开发完整电视浏览器
- 不承诺所有 Android TV 固件都能安装应用
- 不承诺没有 GMS 或浏览器的设备可以直接看 YouTube
- 不在 HarmonyOS 智慧屏未开放 VpnExtension 时伪装为支持

### 规格

[TV.md](../platform/TV.md)

### 退出条件

- 遥控器完成首次授权、启动、停止和状态查看。
- 至少一种内容入口可用：YouTube 应用或系统浏览器。
- 无 GMS、无浏览器或无 VPN Extension 时给出准确原因。
- 国内视频和投屏不被误代理。

## 10. V0.8：鸿蒙 PC / 2in1

**状态：跳过（2026-11-19 用户拍板保持）。** 无自有鸿蒙 PC/2in1 真机；AGC 云调试 PC 远程真机无法上传 conf 配置文件，唯一剪贴板路径被否决。伴侣 `deviceTypes` 保留 2in1 声明，设备可得后直接开验。

### 用户场景

用户在鸿蒙 PC 或 2in1 设备上运行编程、办公或个人 Agent，需要桌面窗口、键鼠操作和条件性自动启动。

### 功能

- PC / 2in1 设备检测
- 桌面窗口和最小窗口布局
- 键鼠与触控板操作
- 通知中心状态
- 休眠、唤醒和网络切换
- VPN Extension 能力验证
- 企业受管设备策略读取
- 条件性 Agent 自动启动

### 明确不做

- 不假定鸿蒙 PC 与手机具有完全相同的 VPN 能力。
- 不在设备不支持 VpnExtension 时伪装为自动模式。
- 不绕过 MDM 或企业策略。

### 退出条件

- 至少一台鸿蒙 PC/2in1 实机完成三次生命周期。
- 窗口、通知、休眠和网络切换通过。
- VPN Extension 不可用时明确降级为 `guided` 或 `unsupported`。

## 11. V0.9：跨设备只读协作

### 用户场景

用户拥有两台或更多设备，希望主设备上的 Agent 协助检查同 Wi-Fi 下的 Windows、Linux 或 macOS，但不知道 IP、SSH 用户名和公钥配置。

### 功能

- `archielink remote serve --once`
- `archielink remote connect <pairing-code>`
- 二维码、短配对码和过期时间
- 双端设备确认
- 一次性会话
- 只读 `detect`、`plan`、`doctor` RPC
- 结构化 JSON 回传
- 会话结束关闭监听并清理临时密钥

### 明确不做

- 不提供任意远程 shell
- 不提供远程文件读写
- 不做无人值守常驻
- 不做跨公网中继
- 不修改远端 DNS、路由、代理或 VPN

### 规格

[CROSS-DEVICE.md](../architecture/CROSS-DEVICE.md)

### 退出条件

- macOS、Windows、Linux 之间完成至少一组双向只读配对。
- 错误配对码、超时、重复使用和指纹不一致均失败关闭。
- 不确认目标设备时无法执行 RPC。
- 非白名单方法被拒绝。
- 会话结束后无监听端口、临时密钥和授权残留。

## 12. V0.10：办公 Agent、个人 Agent 与移动端

### 用户场景

Agent 没有提权、不能修改系统网络，或用户只在手机上使用。

### 功能

- 引导模式
- 可复制的安装步骤
- WireGuard 配置导出
- 移动端二维码
- 文件型诊断报告
- Agent 能力探测与明确降级

### 明确不做

- 不通过自动化偷偷开启系统 VPN
- 不控制手机 VPN 设置
- 不绕过系统权限或平台审核

### 退出条件

- 受限 Agent 不会声称已安装或已启动。
- 用户可拿配置在官方 WireGuard App 中独立完成导入。
- 引导模式不产生提权命令和后台进程。

## 13. V1.0：跨平台稳定版

### 目标

完成自动、引导、不支持三态的统一体验，形成可维护的正式产品。

### 功能

- 所有正式平台适配器
- HarmonyOS 手机、平板、智慧屏和鸿蒙 PC/2in1
- Android TV 与 Google TV
- 稳定授权、续期与撤销
- 签名更新和版本回退
- 结构化支持报告
- 崩溃恢复与残留检测
- 兼容矩阵和维护周期

### 退出条件

- 所有正式场景有独立回归套件。
- 升级和回滚均不破坏现有网络配置。
- 新 Agent 只需要实现文件或命令能力，不再复制网络逻辑。
- 支付、合规、隐私和支持文档完成。

## 14. V1.1：电视浏览器引导安装

2026-11-26 范围精修（用户拍板）：V1.1 从"自研 10-foot 浏览器增强"重定向为
"无浏览器电视的通用引导安装方案"。决策依据：TV Bro 等第三方电视浏览器在
VIDAA 实机长期稳定；内嵌浏览器内核在该固件存在空白页/崩溃，成本高收益低。
浏览器增强包降级为可选候选阶段（真机方案不满足时重新立项）。

### 功能（本轮）

- TV-003 内容入口检测（五态：`ready_with_app` / `ready_with_browser` /
  `needs_browser` / `needs_google_services` / `unsupported`）
- `needs_browser` 时 TV 端引导安装页：市场一键跳转 / 无市场分步指引，
  不自动下载、不静默安装
- 安装完成返回伴侣自动重检（`onResume`）
- 主界面"打开浏览器"快捷入口
- 随调试包免费；商业化延后到 TV 正式发布

### 明确不做（降级为可选候选，另行立项）

- 遥控器专用地址栏
- 手机扫码输入搜索词和网址
- YouTube 网页快捷入口
- 视频全屏和播放控制
- 历史、收藏和主页
- User-Agent 切换
- 页面兼容和崩溃恢复
- DRM、证书和区域限制诊断

### 明确不做（边界）

- 不在 V1.0 前冒进替换系统浏览器
- 不承诺所有网页和流媒体服务兼容
- 不嵌入未经授权的内容源
- 不自动下载或静默安装任何 APK

### 退出条件

- 无浏览器状态被正确识别并给出可执行引导。
- 引导安装页遥控器全程可操作。
- 安装成功后返回伴侣自动识别新浏览器。
- "打开浏览器"可启动浏览器并返回伴侣。
- 安装权限受限的固件给出明确提示，不卖"可能能用"。

## 15. D-track：Developer-Assisted Device Bridge

该轨道用于有动手能力、信任 AI 工具并接受远程指导的技术用户。它不是普通消费者的一键安装方案，也不占用 V0.x 版本号。

| 阶段 | 平台 | 宿主能力 | 设备端要求 |
|---|---|---|---|
| D0 | 通用契约 | `device discover/pair/install/launch/status/stop/cleanup` | 一次性配对和语义化 RPC |
| D1 | Android 手机/平板 | ADB 安装、启动、状态查询 | 调试 APK、`VpnService`、用户确认 VPN |
| D2 | Android TV / Google TV | 局域网 ADB 配对、安装、启动 | 遥控器授权界面、`VpnService` |
| D3 | HarmonyOS 手机/平板/PC/智慧屏 | HDC 安装签名调试 HAP、启动 | `VpnExtensionAbility`、系统 VPN 信任 |
| D4 | iOS | 导出脱敏/完整 WireGuard 配置 + 导入引导（无宿主工具链依赖） | App Store 官方 WireGuard App、用户导入配置并授权 VPN |

D3 已进入开发：HDC 发现、HAP 安装/启动和 VPN Extension 编译骨架已完成，调试签名与真机 VPN 验收尚未完成。

2026-11-18 决定：D4 改为零伴侣轨道——不自研 iOS 伴侣、不依赖 Apple Developer 账号。原因：自研 WireGuard 伴侣必须使用 `NEPacketTunnelProvider` entitlement，该权限只发给付费 Apple Developer Program；免费 Apple ID 侧载（含 GitHub 开源 + agent 引导构建安装）无法获得该 entitlement 且签名 7 天过期。用户改从 App Store 安装官方 WireGuard App，宿主导出配置（`export --format wireguard`，二维码随 V0.10）并引导导入。无桥 RPC、逐应用分流与远程状态。

详细实施与安全边界见 [Host-Assisted Device Bridge 计划](../plans/2026-11-07-archie-link-host-assisted-device-bridge.md)。

## 16. 依赖与阻塞

| 依赖 | 最早阻塞版本 | 处理方式 |
|---|---|---|
| SkillHub Pay API 文档 | V0.4 | 未取得文档前只用测试 Provider |
| Cloudflare WARP 商业条款 | V0.4 | 上架前完成书面核对 |
| Windows / Linux 测试设备 | V0.5 | 每个正式支持版本至少一台实机 |
| HarmonyOS API 11+ 手机和平板实机 | V0.6 | 覆盖首次授权、拒绝、撤销和 VPN 冲突 |
| Android TV / Google TV 实机 | V0.7 | 覆盖 GMS、遥控器、待机和国内视频 |
| HarmonyOS 智慧屏实机 | V0.7 | 验证 VpnExtension、浏览器和遥控器 |
| 鸿蒙 PC / 2in1 实机 | V0.8 | 单独验证 VpnExtension、窗口和休眠 |
| 同局域网跨设备实机 | V0.9 | macOS、Windows、Linux 至少完成一组双向配对 |
| 跨设备威胁模型与安全测试 | V0.9 | 验证重放、过期、指纹和清理 |
| 鸿蒙正式签名与 AppGallery 审核 | V0.6 | 付费发行前完成 VPN 类权限与签名核对 |
| Android ADB / TV 网络调试设备 | D1–D2 | 至少一台手机和一台 TV，覆盖授权、拒绝和断连 |
| HarmonyOS HDC 与调试签名 HAP | D3 | 覆盖首次信任、拒绝、撤销和 VPN 冲突 |
| Apple Developer 账号与 macOS 构建机 | V0.4（macOS Developer ID 签名/公证） | D4 零伴侣轨道不再依赖该账号；仅 macOS 发布签名与未来自研 iOS 伴侣需要 |
| 生产代码签名和公证 | V0.4 | macOS 发布前完成 |
| 平台二进制文件限制 | V0.3 | 打包测试时提前验证，避免 V0.4 才发现 |

## 17. 当前执行顺序

1. 完成并批准 V0.1。
2. 编写 V0.2 独立实现计划。
3. 实现并验收 V0.2。
4. 执行 V0.3 计划。
5. 取得 Pay 文档和合规结论后编写 V0.4。
6. V0.5 完成后编写 V0.6 鸿蒙手机/平板计划。
7. V0.6 完成后编写 V0.7 电视核心计划。
8. V0.7 完成后编写 V0.8 鸿蒙 PC/2in1 计划。
9. V0.8 完成后编写 V0.9 跨设备只读协作计划。
10. V1.0 稳定后再启动 V1.1 电视浏览器增强。
11. D0、D1 Android 和 D2 Android TV 已完成；当前推进 D3 HarmonyOS，D3 依赖调试签名 HAP，D4 iOS 已重定义为零伴侣轨道（官方 WireGuard App 配置导入），可随移动端导出能力一并交付，不依赖 Apple Developer 账号。

V0.4 至 V1.1 不提前写代码；每个版本在进入开发前必须有独立规格和计划。鸿蒙手机/平板、电视和 PC/2in1 不合并开发，避免生命周期和输入方式差异互相掩盖。
