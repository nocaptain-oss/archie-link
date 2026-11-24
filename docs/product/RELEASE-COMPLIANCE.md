# 汪连 ArchieLink｜外网访问专家 — 发布合规检查单

**状态：** Accepted v1.0（V1.0 切片 3，2026-11-20）
**用途：** 对外发布（公开仓库/上架/正式售卖）前逐项核对；切片 4 发布工程与后续版本发布复用。
**上游：** [NAMING.md](NAMING.md)、[COMMERCIAL-TRUST.md](../architecture/COMMERCIAL-TRUST.md)、V1.0 计划 §切片 3

## 1. 外部依赖项（阻塞状态记录）

| # | 项 | 状态 | 负责人 | 触发条件（何时必须完成） | 进展 |
|---|---|---|---|---|---|
| 1 | 名称知识产权检索：汪连/ArchieLink 商标、域名、同名软件 | **商标已完成（2026-11-20）**：中国商标网查询，风险可控（用户拍板） | 用户/委托代理 | 对外发布前（公开仓库、上架、正式售卖任一之前） | 域名/同名软件未系统检索；对外发布放行，发布物料沿用现名 |
| 2 | Cloudflare WARP 商业条款书面核对：售卖自动化而非带宽的边界书面化 | **完成（2026-11-23 用户批准）**：[WARP-TERMS-REVIEW.md](WARP-TERMS-REVIEW.md) 逐条核对 SSA（2025-09-12 版）§2.2.1(a)(e)(j)/§2.6/§8，Service-Specific Terms 核实无 WARP 专项条款；wgcf 文档推荐灰区如实记录；六条行为红线固化 | 用户 | 永久版/家庭版正式售卖前 | 已批准定稿；规模化售卖时按文档 §1 版本漂移规则复核 |
| 3 | 三产品统一收款通道：支付宝 Vibe Pay「即刻收」直连模式（`alipay.aipay.nowpaydirect.*`）线上验证 | **进行中（2026-11-24 切换）**：三商品已在「即刻收」管理小程序创建（按次 COUNT ¥19.9 / 永久 ONE_TIME ¥199 / 家庭时长包 YEAR ¥69）；服务端 NowPayService + CLI 终端二维码已部署 api.uzphoto.com 并三档线上冒烟通过（quota.query/purchase.create/consult 全 10000，应用 `2021007117679181` 具备 nowpaydirect 权限）；原 `alipay.trade.page.pay` 方案签约已于 2026-11-24 提交「电脑网站支付」（SUBMITTED 等待生效，生效后 40006 消除、可生产验证，仍列后备），Machine Pay 402 保留按次兼容 | 用户（小程序配置/真实付款验证）+ Codex（服务端/CLI 实现） | 三产品正式售卖前 | 剩余：三档各一笔真实购买走通（HANDOFF 任务 20 待办 ①） |
| 4 | Apple Developer 账号（¥688/年） | **未购入** | 用户 | Developer ID 签名/公证（切片 4）与自研 iOS 伴侣评估（已排除） | 未签名二进制需用户绕 Gatekeeper（SUPPORT-MATRIX §3 注记） |
| 5 | 鸿蒙 AppGallery 海外发布（2026-11-20 拍板：个人开发者账号 + 海外市场，避开中国市场红线） | **已提审：预审通过（2026-11-23），等待正式审核**。上架材料齐备——中英双语介绍、4 张真机截图（1080×1920）、隐私政策网页（gh-pages `https://nocaptain-oss.github.io/archie-link/privacy/`）、隐私标签数据项 D011007、境外分发同意声明；支持设备仅手机（智慧屏未勾选，无 TV 真机截图，延后）；用户 2026-11-22/23 人工提交提审 | 用户（AGC 账号操作）+ Codex（构建/签名） | 海外市场上架前 | 独立主体迁移取消；Mate XT 留白复测关闭；审核被拒则逐项整改重提（AGC 网页端工程事实已登记 PROJECT-MEMORY）；智慧屏延后至后续版本（module.json5 已声明 `tv`，无需重打包） |

## 2. 发布前检查单

对外发布（V1.0 或任一正式版本）前逐项勾选：

- [x] 名称知识产权检索完成（§1 #1，2026-11-20：商标·中国商标网，风险可控）
- [x] 隐私政策定稿（[PRIVACY.md](PRIVACY.md)）——**2026-11-23 定稿（Accepted v1.2，用户批准）**：日志保留期回填正式数值 52 天（服务器 logrotate 配置核实）+ 支持邮箱 vcaptain@126.com 公布；AGC 上架用隐私政策网页（gh-pages）同步更新
- [x] 用户手册/故障排查随版本更新（2026-11-20：V1.0 切片 3 成文对应 1.0.0；后续版本发布时滚动更新）
- [x] 支持渠道正式公布（2026-11-23：**支持邮箱 vcaptain@126.com**，形式拍板=仅邮箱；已写入 PRIVACY.md §10、gh-pages 隐私政策网页；群聊等后续形式再议）
- [ ] 商业条款书面核对（永久版/订阅售卖前，§1 #2/#3）——**WARP 条款核对已完成**（2026-11-23 用户批准，[WARP-TERMS-REVIEW.md](WARP-TERMS-REVIEW.md)）；收款金额边界由统一 aipay Vibe Pay 方案化解（§1 #3，2026-11-23 拍板，待签约入驻+线上验证）
- [x] 决策记录：完整导出（`--include-private-key`）/二维码（`--format qrcode`）列为永久版/家庭版权益（`export_private`），脱敏导出保持免费；按次执行窗口收紧 14 天（7 天租约 + 7 天宽限）（2026-11-23 用户拍板；SPEC FR-003/FR-011、D4 注记、SUPPORT-MATRIX §4 注）
- [x] 决策记录：三产品统一收款走支付宝 aipay「AI网页应用收款」（Vibe Pay，`alipay.trade.page.pay`，个人收款场景）；家庭年度定价 ¥69/年定稿；Machine Pay 402 降级为兼容通道（2026-11-23 用户拍板；SPEC FR-018、BUSINESS-CASE 收款通道选型）
- [x] 决策记录：收款切换 Vibe Pay「即刻收」直连模式（`alipay.aipay.nowpaydirect.*`，2026-11-24 用户拍板）——绕开 page.pay 签约送审死结（40006 现场），商品托管即刻收小程序，服务端只绑定 plan→out_product_id，终端二维码扫码支付无浏览器跳转（SPEC FR-018、BUSINESS-CASE 收款通道选型、计划 docs/plans/2026-11-24-nowpay-direct.md）
- [x] Release 构建双环境可复现 + SBOM/manifest（2026-11-20：V1.0 Release **1.0.0**（`26fad16`）host/cleanroom 五产物+SBOM+manifest 逐字节一致，`verify-release-reproducible.sh` PASS；产物 `dist/v1.0-host/`，证据 `artifacts/2026-11-20-cleanroom-v1.0-reproducible/`）
- [ ] macOS Developer ID 签名 + 公证（§1 #4，待 Apple 账号购入）
- [x] `go vet ./... && go test ./...`（grep FAIL）+ 三平台交叉编译 + package-* 脚本全绿（2026-11-20：22 包全绿/vet 干净；SE darwin 双架构 cgo 交叉编译随 Release 构建；package-doctor/skill/dev-hygiene/debug-clean 脚本测试全绿）
- [x] 实机回归矩阵按 [REGRESSION-MATRIX.md](../dev/REGRESSION-MATRIX.md) 执行并回填记录（2026-11-20 切片 2：root 矩阵——生命周期 ×3、故障注入 ×4、跨版本双向、Win10 guided 回归；14 项场景→套件映射成文）
- [x] 公开仓库（2026-11-20 完成）：`github.com/nocaptain-oss/archie-link`（PUBLIC，orphan-squash 首提交 `abd81de`，280 文件，main）——秘密扫描复核（gitleaks 全历史 155 commits 0 命中 + 手动扫描）+ 历史清理（orphan 单提交不携带历史：剔除 HANDOFF/PROJECT-MEMORY/AGENTS/deploy/docs/plans/docs/dev/scripts/dev；LINUX.md 公开副本脱敏生产 IP/主机名/用户名；LICENSE 专有源码可见）

## 3. 记录规则

- 每项完成后在本文件回填日期与证据指针（本地 `artifacts/` 或外部文档）。
- 阻塞状态变化（完成/放弃/条件变更）需同步 HANDOFF.md 与 PROJECT-MEMORY.md。
- 本检查单不替代各文档自身的评审；仅做发布门禁汇总。
