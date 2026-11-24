# Cloudflare WARP 商业条款书面核对

**状态：** Accepted v1.0（2026-11-23 用户批准）
**用途：** [RELEASE-COMPLIANCE.md](RELEASE-COMPLIANCE.md) §1 #2 的书面核对交付物——把"售卖本地自动化工具，而非售卖 WARP 带宽/账号"的边界逐条款书面化。
**触发条件：** 永久版/家庭版正式售卖前必须完成（本文档成文并经用户评审即视为完成；规模化售卖或模式变更时复核）。
**注意：** 本文件位于 `docs/product/`，会随公开仓同步进入公开树（github.com/nocaptain-oss/archie-link）——**2026-11-23 用户确认可公开**。

## 1. 核对范围与文件版本

| 文件 | 版本 | 核对日期 | 结论 |
|---|---|---|---|
| Cloudflare Self-Serve Subscription Agreement（SSA，`cloudflare.com/terms`） | Last Updated 2025-09-12 | 2026-11-23 | 全文核对，关键条款见 §3 |
| Cloudflare Service-Specific Terms（`cloudflare.com/service-specific-terms-*`） | Last Updated 2026-09-28 | 2026-11-23 | **无 WARP 专项条款**（Application/Network/Zero Trust/Developer Platform/Other 五节均无 WARP 专属条款；唯一 "warp" 命中为 Mobile SDK 节复用的图标文件名）。WARP 消费级服务由 SSA 一般条款约束 |

版本漂移规则：Cloudflare 可随时修改条款（SSA §14）。每次正式售卖动作（新 SKU 上架、规模化推广）前复核一次条款版本号；条款实质变更（尤其 §2.2.1）时更新本文档。

## 2. ArchieLink 商业模式声明（核对基准）

- ArchieLink 售卖的是**本地软件许可与自动化工具**：网络体检、共存保护、配置导入、隧道编排、健康检查、自动回滚。
- ArchieLink **不售卖**：带宽、线路、WARP 账号、Cloudflare 服务访问权、任何"代配/代注册/共享配置"服务。
- 隧道由**终端用户自有的** WireGuard 配置（含但不限于 WARP）建立；WARP 账号是用户与 Cloudflare 之间的直接关系，ArchieLink 不经手用户与 Cloudflare 之间的凭据。
- 云端控制面只承载订单、授权、设备、撤销（[COMMERCIAL-TRUST.md](../architecture/COMMERCIAL-TRUST.md)），**不承载用户流量**。
- 自定义 WireGuard 节点是产品的一等公民（HarmonyOS 已真机验证 ProtonVPN 标准 conf 导入即用），WARP 只是可选后端之一。

## 3. SSA 关键条款逐条核对（2025-09-12 版）

### 3.1 §2.2.1(a) — 禁止租售/代注册服务访问

> "rent, lease, loan, export, or sell access to the Services to any third party, or sign up for the Services on behalf of a third party"

**核对结论：不触碰。**

- ArchieLink 不向用户提供 Cloudflare 服务访问——用户自己拥有 WARP 账号或自己注册；一设备一密钥（多设备互抢是产品文档明确提示并要求用户自备配置的技术事实，见 [TROUBLESHOOTING.md](TROUBLESHOOTING.md) §1）。
- ArchieLink 不代任何第三方注册 WARP 账号（"sign up on behalf of"），不共享、不出租、不内置任何共享 WARP 配置或密钥。
- 收费对应物是本地工具的授权（lease），不是 Cloudflare 服务的访问权——用户不买 ArchieLink 也照样可以免费用 WARP。

### 3.2 §2.2.1(e) — 禁止自动化脚本批量造号/挖数据

> "introduce software or automated agents or scripts into the Services so as to produce multiple accounts, generate automated searches, requests or queries, or to strip or mine data from the Services"

**核对结论：产品本身不触碰；wgcf 相关存在灰区，如实记录如下。**

- ArchieLink CLI、伴侣应用与云端控制面**不调用** Cloudflare 注册 API，不捆绑、不执行 wgcf 或任何自动注册工具；产品只消费用户提供的现成 WireGuard 配置文件。
- **灰区**：产品文档（TROUBLESHOOTING.md §1"一设备一密钥：每台设备用 wgcf 注册自己的配置"）向用户推荐了 wgcf——一个通过非官方 API 自动注册 WARP 账号的第三方工具。用户自行使用 wgcf 是用户与 Cloudflare 之间的关系，但**官方文档推荐该工具构成轻度关联**，如实记录为灰区。
- 缓解措施：
  1. 文档口径保持"推荐用户使用 WARP 官方客户端/自行注册"为主路径，wgcf 仅作为多设备密钥冲突的排障提示；
  2. 自定义 WireGuard 节点（ProtonVPN 等商业 VPN 的标准 conf）作为同等替代路径在文档中并列呈现；
  3. 永不将 wgcf 自动注册做成产品功能或付费服务。

### 3.3 §2.2.1(j) — 禁止用服务提供 VPN/代理

> "use the Services to provide a virtual private network or other similar proxy services"

**核对结论：不触碰。**

- 该条款针对"把 Cloudflare 服务（如 Workers、Spectrum）用作后端向他人提供 VPN/代理"的情形。
- ArchieLink 不向任何用户提供 VPN 服务：隧道是 Cloudflare 自家的 WARP 产品按其设计用途（终端用户客户端）被使用，或用户自选的其他 WireGuard 节点；ArchieLink 云端零流量承载（COMMERCIAL-TRUST 边界），不存在"用服务提供代理"的技术路径。

### 3.4 §2.6 — Free Services 无担保、可随时终止

> Free Services "until … (c) termination of the Free Service by Cloudflare in our sole discretion"，且 "We will have no liability for any harm or damage arising out of or in connection with any Free Services."

**核对结论：对产品承诺的硬约束。**

- 免费 WARP 可被 Cloudflare 随时终止且不承担责任。因此：
  1. 营销与文档**不得承诺** WARP 可用性、永久性或质量（NAMING.md 用词边界已禁止"绝对不会断""永久免费线路"类承诺，本条款是该禁令的法律依据）；
  2. 卖点必须落在**工具能力**（体检/共存/回滚/多后端编排），不落在任何特定后端的可用性上；
  3. 自定义 WireGuard 节点支持是本风险的架构级缓解——WARP 不可用时工具价值不归零。

### 3.5 §8 — 违规可暂停/终止

> 违反 §2.2/§2.7 可立即 Suspend 或终止；也可"at any time, with or without notice for any reason"终止账户。

**核对结论：同 §3.4，属终端用户侧风险，需在文档如实提示。**

- 用户 WARP 账号被终止的风险由用户承担；产品不承诺替用户恢复 Cloudflare 侧账号状态，只承诺本地环境可完整回滚（`down`/`rollback` 永不依赖云端，包括 Cloudflare）。

## 4. 行为红线（营销、功能与运营约束）

以下为本次核对固化的红线，违反任一条即触发本文档复核：

1. **不代配**：不提供任何"帮你注册/配置 WARP 账号"的付费或免费服务。
2. **不共享**：不内置、不分发任何共享 WARP 配置或密钥（技术上同时违反一设备一密钥事实，会造成互抢）。
3. **不造号**：不将 wgcf 或任何自动注册能力做成产品功能。
4. **不卖线路**：营销文案不得把 ArchieLink 描述为"线路""机场""节点服务商"；WARP/Cloudflare 仅作为技术后端名称出现（[NAMING.md](NAMING.md)）。
5. **不背书可用性**：不对 WARP 的可用性、出口质量、地区解锁能力做任何承诺。
6. **不借名**：不暗示与 Cloudflare 存在合作、授权或背书关系。

## 5. 残留风险与结论

- **残留风险**（如实记录，不构成阻塞）：
  1. 本文档为工程侧书面核对，**不构成法律意见**；规模化售卖或出现 Cloudflare 侧问询时，建议由执业律师复核本文档结论；
  2. wgcf 文档推荐的灰区（§3.2）保持监控；若 Cloudflare 明确禁止非官方注册通道且影响用户主路径，按版本漂移规则更新文档口径；
  3. Cloudflare 条款单方可改（SSA §14），本文档结论的有效性以 §1 的复核节奏维持。
- **结论**：ArchieLink"售卖本地自动化工具"的商业模式与 SSA §2.2.1 各项限制不冲突；关键依赖是 §4 红线在营销与功能两侧持续执行。书面核对完成（2026-11-23 用户批准）。
