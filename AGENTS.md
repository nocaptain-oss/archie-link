# AGENTS.md

面向在本仓库工作的 coding agent（Codex、Claude Code 等）的最小指引。

## 构建 / 测试

要求 Go 1.27+：

```bash
go build ./cmd/archielink
go test ./...
go vet ./...
```

shell 脚本自测：`sh scripts/package-skill_test.sh`、`sh scripts/package-doctor_test.sh` 等（`scripts/*_test.sh`）。

## 目录速览

- `cmd/archielink` — CLI 入口；`cmd/controlplane` — 可自托管授权服务端；`cmd/wireguard-android` — Android WireGuard 集成
- `internal/` — 领域包：`doctor`（诊断）、`coexist`（共存 Gate）、`state`（快照/回滚）、`entitlement`（授权）、`remote`（跨设备只读 RPC）、`cli`、`inspect`、`export` 等
- `skills/` — Agent 技能（`archie-link`、`archie-link-doctor`）；改动后用 `scripts/package-skill.sh` 重新打包
- `mobile/` — 鸿蒙 / Android 伴生应用
- `docs/` — 产品（`product/`）、平台（`platform/`）、架构（`architecture/`）文档
- `tests/acceptance/` — 实机验收（需 `ARCHIE_LINK_ACCEPTANCE=1`）

## 约定

- 修改产品行为前，先更新 `docs/` 内对应规格文档，再改代码
- 自动连接必须走 共存 Gate → 快照 → 健康检查 → 自动回滚 路径，不得绕过
- 不读取 Cookie、支付信息、私钥或系统登录凭据
- 正式 Release 不引入测试授权、运行时公钥覆盖或故障注入代码
- 提交信息用简短祈使句（风格见 `git log`）
