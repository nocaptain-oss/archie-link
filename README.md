# 汪连 ArchieLink｜外网访问专家

跨平台外网访问、网络体检与连接恢复 CLI，配套鸿蒙 / Android 伴生应用。技术后端：Cloudflare WARP / WireGuard / wgcf（仅作技术后端，不替换产品名）。

Archie 是一只网络专家狗：先闻 DNS、再看路由、检查代理和本地服务；能连就安全连接，连不上就原路还原。

> **命名状态：** 暂定工作名，见 [NAMING.md](docs/product/NAMING.md)。

## 核心能力

- **网络体检**：`detect` / `plan` / `doctor` —— DNS、路由、代理、本地服务、外网连通判定
- **安全连接生命周期**：`up` / `status` / `down` / `rollback` —— 快照、共存 Gate、健康检查、自动回滚
- **配置导出**：`export`（文本/JSON/二维码）—— 供官方 WireGuard App 导入
- **授权购买**：`buy` —— 终端支付宝二维码，扫码付款自动激活，租约绑定设备
- **跨设备只读协作**：`remote serve` / `remote connect` —— TLS 1.3、一次性配对码、证书指纹 pin、只读 RPC 白名单
- **设备绑定授权**：`lease` —— 激活/续租/撤销，macOS 端支持 Secure Enclave 设备身份

## 支持平台

| 平台 | 状态 |
|---|---|
| macOS 12+（amd64/arm64） | 已实机验收 |
| Windows 10/11（amd64/arm64） | 已实机验收 |
| Linux（amd64，检测/引导） | 已实机验收（从源码构建） |
| HarmonyOS 伴生应用 | [HARMONYOS.md](docs/platform/HARMONYOS.md) |
| Android 伴生应用 | [mobile/android-companion](mobile/android-companion) |

## 安装

### CLI 二进制

从 [Releases](https://github.com/nocaptain-oss/archie-link/releases) 下载对应平台（v1.1.0+）：

| 附件 | 平台 |
|---|---|
| `archielink-darwin-arm64` | macOS 12+（Apple Silicon） |
| `archielink-darwin-amd64` | macOS 12+（Intel） |
| `archielink-windows-amd64.exe` | Windows 10/11（x64） |
| `archielink-windows-arm64.exe` | Windows 10/11（ARM64） |

macOS 示例（放到 PATH 并赋执行权限）：

```bash
chmod +x archielink-darwin-arm64
sudo mv archielink-darwin-arm64 /usr/local/bin/archielink
archielink detect --json
```

Linux 从源码构建：`go build ./cmd/archielink`（见下文「从源码构建」）。

完整性校验：下载同 Release 附带的 `SHA256SUMS`，执行 `shasum -a 256 -c --ignore-missing SHA256SUMS`；构建来源与产物清单见附件 `release-manifest.json` 与 `sbom.cdx.json`。服务端可自托管（附件 `controlplane-linux-amd64`，源码 `cmd/controlplane`）。

> 二进制暂未做 Developer ID 签名与公证（办理中）。macOS 首次运行如被 Gatekeeper 拦截：Finder 右键选「打开」，或 `xattr -d com.apple.quarantine <文件>`；Windows SmartScreen 选「仍要运行」。

### AI Agent 技能（Claude Code / Codex 等）

本仓库 [`skills/`](skills/) 内置技能定义，让 AI Agent 按用户手册直接操作 ArchieLink：

```bash
git clone https://github.com/nocaptain-oss/archie-link.git
mkdir -p ~/.agents/skills
cp -R archie-link/skills/archie-link ~/.agents/skills/
cp -R archie-link/skills/archie-link-doctor ~/.agents/skills/
```

把技能目录放进所用 Agent 的技能加载路径（如 `~/.agents/skills/`、`~/.codex/skills/`），重启会话后对 Agent 说「用 archie-link 带我完成外网连接」即可。

### 购买与激活

网络体检（`detect` / `doctor`）、状态查看与脱敏导出免费可用；一键安全连接与完整配置导出按授权提供：

```bash
archielink buy activation-task   # 按次安全激活 ¥19.9
archielink buy perpetual         # 个人永久版（2 设备）¥199
archielink buy family-annual-12  # 家庭年度（12 设备）¥69
```

终端展示支付宝二维码，扫码付款后自动激活，租约绑定当前设备（默认服务端 `https://api.uzphoto.com`，可用 `--server` 覆盖）。详见 [用户手册](docs/product/USER-GUIDE.md) 与 [商业逻辑](docs/product/BUSINESS-CASE.md)。

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
