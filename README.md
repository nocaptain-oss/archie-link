# 汪连 ArchieLink｜外网访问专家

跨平台外网访问、网络体检与连接恢复 CLI，配套鸿蒙 / Android 伴生应用。技术后端：Cloudflare WARP / WireGuard / wgcf（仅作技术后端，不替换产品名）。

Archie 是一只网络专家狗：先闻 DNS、再看路由、检查代理和本地服务；能连就安全连接，连不上就原路还原。

> **命名状态：** 暂定工作名，见 [NAMING.md](docs/product/NAMING.md)。

## 核心能力

- **网络体检**：`detect` / `plan` / `doctor` —— DNS、路由、代理、本地服务、外网连通判定
- **安全连接生命周期**：`up` / `status` / `down` / `rollback` —— 快照、共存 Gate、健康检查、自动回滚
- **配置导出**：`export`（文本/JSON/二维码）—— 供官方 WireGuard App 导入
- **跨设备只读协作**：`remote serve` / `remote connect` —— TLS 1.3、一次性配对码、证书指纹 pin、只读 RPC 白名单
- **设备绑定授权**：`lease` —— 激活/续租/撤销，macOS 端支持 Secure Enclave 设备身份

## 支持平台

| 平台 | 状态 |
|---|---|
| macOS 12+（amd64/arm64） | 已实机验收 |
| Windows 10/11（amd64/arm64） | 已实机验收 |
| Linux（amd64，检测/引导） | 已实机验收 |
| HarmonyOS 伴生应用 | [HARMONYOS.md](docs/platform/HARMONYOS.md) |
| Android 伴生应用 | [mobile/android-companion](mobile/android-companion) |

## 文档

- [产品规格](docs/product/SPEC.md) · [路线图](docs/product/ROADMAP.md) · [商业逻辑](docs/product/BUSINESS-CASE.md)
- [用户手册](docs/product/USER-GUIDE.md) · [故障排查](docs/product/TROUBLESHOOTING.md) · [支持矩阵](docs/product/SUPPORT-MATRIX.md) · [隐私政策](docs/product/PRIVACY.md)
- [商业信任与授权架构](docs/architecture/COMMERCIAL-TRUST.md) · [网络共存与断网防护](docs/architecture/COEXISTENCE.md) · [跨设备协作](docs/architecture/CROSS-DEVICE.md)
- [鸿蒙平台](docs/platform/HARMONYOS.md) · [Windows](docs/platform/WINDOWS.md) · [Linux](docs/platform/LINUX.md) · [TV](docs/platform/TV.md)
- [第三方声明](THIRD_PARTY_NOTICES.md)

## 安全边界

- 不读取 Cookie、支付信息、私钥或系统登录凭据
- 自动连接必须经过共存 Gate、快照、健康检查和自动回滚
- 正式 Release 不含测试授权、运行时公钥覆盖或故障注入代码
- 云端只承载订单、授权、设备、撤销和持续增值，不承载用户流量或本地秘密

## 从源码构建

要求 go 1.27+：

```bash
go build ./cmd/archielink
go test ./...
go vet ./...
```

## 许可

本仓库源码仅作透明度公开（source-available），保留所有权利，见 [LICENSE](LICENSE)。
