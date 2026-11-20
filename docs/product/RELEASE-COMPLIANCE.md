# 汪连 ArchieLink｜外网访问专家 — 发布合规检查单

**状态：** Accepted v1.0（V1.0 切片 3，2026-11-20）
**用途：** 对外发布（公开仓库/上架/正式售卖）前逐项核对；切片 4 发布工程与后续版本发布复用。
**上游：** [NAMING.md](NAMING.md)、[COMMERCIAL-TRUST.md](../architecture/COMMERCIAL-TRUST.md)、V1.0 计划 §切片 3

## 1. 外部依赖项（阻塞状态记录）

| # | 项 | 状态 | 负责人 | 触发条件（何时必须完成） | 进展 |
|---|---|---|---|---|---|
| 1 | 名称知识产权检索：汪连/ArchieLink 商标、域名、同名软件 | **商标已完成（2026-11-20）**：中国商标网查询，风险可控（用户拍板） | 用户/委托代理 | 对外发布前（公开仓库、上架、正式售卖任一之前） | 域名/同名软件未系统检索；对外发布放行，发布物料沿用现名 |
| 2 | Cloudflare WARP 商业条款书面核对：售卖自动化而非带宽的边界书面化 | **未开始** | 用户 | 永久版/家庭版正式售卖前 | 个人按量通道已验证可用；书面化前正式售卖不启动 |
| 3 | SkillHub 永久版/云订阅收款边界官方确认 | **未开始** | 用户 | 永久版在 SkillHub 上架前 | 个人按量（activation-task 19.90）已验证；永久版/订阅形态待平台确认 |
| 4 | Apple Developer 账号（¥688/年） | **未购入** | 用户 | Developer ID 签名/公证（切片 4）与自研 iOS 伴侣评估（已排除） | 未签名二进制需用户绕 Gatekeeper（SUPPORT-MATRIX §3 注记） |
| 5 | 鸿蒙 AppGallery 海外发布（2026-11-20 拍板：个人开发者账号 + 海外市场，避开中国市场红线） | **材料就绪**：专用发布签名已验证（EC P-256 + `SHA256withECDSA` CLI 签名链） | 用户（AGC 账号操作）+ Codex（构建/签名） | 海外市场上架前 | 独立主体迁移取消；Mate XT 留白复测关闭（修复完成于 HAP `4f64c81d`） |

## 2. 发布前检查单

对外发布（V1.0 或任一正式版本）前逐项勾选：

- [x] 名称知识产权检索完成（§1 #1，2026-11-20：商标·中国商标网，风险可控）
- [ ] 隐私政策定稿（[PRIVACY.md](PRIVACY.md)：支持渠道、日志保留期正式数值回填）
- [ ] 用户手册/故障排查随版本更新（[USER-GUIDE.md](USER-GUIDE.md)、[TROUBLESHOOTING.md](TROUBLESHOOTING.md)）
- [ ] 支持渠道正式公布（购买页/工单邮箱/群）
- [ ] 商业条款书面核对（永久版/订阅售卖前，§1 #2/#3）
- [ ] Release 构建双环境可复现 + SBOM/manifest（复用 `scripts/build-release.sh`）
- [ ] macOS Developer ID 签名 + 公证（§1 #4）
- [ ] `go vet ./... && go test ./...`（grep FAIL）+ 三平台交叉编译 + package-* 脚本全绿
- [ ] 实机回归矩阵按 [REGRESSION-MATRIX.md](../dev/REGRESSION-MATRIX.md) 执行并回填记录
- [ ] 公开仓库前：module path 定案（2026-11-20：`github.com/nocaptain-oss/archie-link`，go.mod 无需改）+ 秘密扫描复核完成（gitleaks 155 commits 0 命中 + 手动扫描）；**待历史清理**（PROJECT-MEMORY/HANDOFF 全历史含 p12 密码与运营信息）后建仓推送

## 3. 记录规则

- 每项完成后在本文件回填日期与证据指针（本地 `artifacts/` 或外部文档）。
- 阻塞状态变化（完成/放弃/条件变更）需同步 HANDOFF.md 与 PROJECT-MEMORY.md。
- 本检查单不替代各文档自身的评审；仅做发布门禁汇总。
