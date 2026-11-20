# Linux 平台规格（汪连 ArchieLink｜外网访问专家）

状态：`implemented`（V0.5 已关闭：检测层/引导/切片 3 自动模式均已实现并验收；切片 2 Win11 部分不在本文件；真实 WARP up→healthy 验收延后，见已知限制）
上游：`docs/product/ROADMAP.md` §7、`docs/product/SPEC.md`
计划：`docs/plans/2026-11-18-archie-link-v0.5-windows-linux.md`

## 平台边界

- Ubuntu 22.04+、Debian 12+：自动模式（V0.5 切片 3 已实现，容器验收 PASS；正式发布前建议补实机回归）。
- 其他发行版：按适配器能力检测决定 `guided` / `unsupported`，不尝试半自动修改。

## 现状（已交付）

- V0.2 检测层：`internal/adapter/linux/`——`Detect`（`/etc/os-release`、`uname -m`、`id -u`、`sudo`/`ip`/`wg`/`wireguard-go` 能力探测）、`Snapshot`、`Coexist`。
- 能力判定：`Elevation = root 或有 sudo`；`NetworkMutation = elevation && ip && (wg || wireguard-go)`；`SelectMode` 据此给 `auto` 能力 / `guided`。
- V0.5 切片 1 起，`up/down/status/rollback` 输出结构化引导报告（`model.GuidedReport`，exit 3）；`export` 自切片 3 生命周期装配落地后可用（`ARCHIE_LINK_WG_CONFIG` 指向配置即可脱敏/完整导出，完整导出强制 `--output` 0600、禁 `--json`）。

## 验收记录

- 检测层 + 引导输出（2026-11-19，PASS 7/7）：Huawei Cloud EulerOS 2.0（生产服务器，**只读验收专用——禁自动模式/网络变更测试**，非 root 账户）——`doctor` rc=0 guided 判定正确（无 wg/wireguard-go → network_mutation=false、gui=false、eth0/华为云内网 DNS 检出）；`up` GuidedReport rc=3 四步、`down/status/rollback` 单步 rc=3；`lease` rc=3；`export` 结构化 `config_not_configured` rc=5。证据 `artifacts/v0.5-linux/2026-11-19-euleros-detection/RESULT.md`（本地，不入库）。
- **规格目标系统验收（2026-11-19，PASS，Ubuntu 22.04.5 容器 @EulerOS 宿主，docker-engine + ubuntu-base rootfs import + systemd/NM/resolved/wireguard-tools）**——发现并修复真 bug：detect 旧写法经 runner 执行 `command -v`，而 `command` 是 shell 内建非二进制（EulerOS 恰带 shim 碰巧可用，标准 Ubuntu 四项探测全空漏报 network_mutation）；修复为 Go 原生 `exec.LookPath`（`53ffc25`）。修复后 Ubuntu 22.04 实测 `mode=auto`（首次真机检出 auto 能力）、`network_mutation=true`；up/down/status/rollback/lease 全 rc=3、export rc=5。证据 `artifacts/v0.5-linux/2026-11-19-ubuntu-container-detection/RESULT.md`（本地，不入库）。宿主遗留：镜像 ×2 + 容器 `archielink-test`（切片 3 开发期间保留）+ 验收账户加入 docker 组（等效 root，可 `gpasswd -d` 收回）。
- 切片 3 自动模式容器验收（2026-11-19，**PASS，用户拍板认可容器验收**）：华为云 Ubuntu 22.04 容器 + 假对端（独立容器 kernel WireGuard NAT 服务器）走通 probe→apply（握手/隧道路由/保护路由全过）→verify（假端点 warp_trace 预期失败）→自动回滚→环境全净（无接口/规则 30500/表 51820/进程/resolv.conf 标记，默认路由完好）；status/rollback/down/coexist inspect 全 rc=0。过程修 6 真 bug（`b83230e`，回归测试入 automatic_test.go）。证据 `artifacts/v0.5-linux/2026-11-19-automatic-container/RESULT.md`（本地，不入库）。**真实 WARP up→healthy 验收延后**（华为云北京出口对 WARP 数据面 UDP 全面封锁；候选 colima/港美 VPS/用户 Linux 实机，待需要时拍板）。验收后宿主已清理：两容器 + 两镜像已删、宿主/本机临时二进制已清（docker 组成员保留，可自行 `gpasswd -d` 收回）。

## 切片 3：自动模式（已实现，2026-11-19 完成，`9cb1367`/`b83230e`）

- 生命周期装配（`lifecycle_linux.go`）：状态目录 `ARCHIE_LINK_STATE_DIR` > `$XDG_STATE_HOME/ArchieLink` > `~/.local/state/ArchieLink`（0700）；配置只认 `ARCHIE_LINK_WG_CONFIG`（不猜测默认路径）；授权沿用 macOS 契约（licensetest / `ARCHIE_LINK_LICENSE_FILE` 签名授权），设备级租约绑定延后（依赖 deviceid 的 Linux 实现）。
- 数据面：kernel WireGuard 优先（`ip link add <iface> type wireguard`），失败回退 `wireguard-go`（用户态 TUN，进程随接口删除退出）；配置经 `wg setconf` 注入（过滤 wg-quick 专有键，拒绝 Pre/PostUp 等钩子）；`Address`/`MTU` 由 `ip` 单独应用。
- 路由：wg-quick 式策略路由——`wg set fwmark`、独立路由表（51820 起探空自增）、`table main suppress_prefixlength 0` + `not fwmark <table> table <table>` 两条规则（显式 priority，保证求值顺序）；主表默认路由不动，端点 /32 与直连保护路由（AlwaysDirectCIDRs、解析后的保护域名主机）显式 `ip route add via <原网关>`；全部记入 `Ownership.ManagedRoutes` 供回滚。
- DNS 适配器：`systemd-resolved`（`resolvectl`）优先——`resolvectl dns <iface> 1.1.1.1` + `resolvectl domain <iface> '~.'`，随接口删除自清理；无 resolved 时回退 `/etc/resolv.conf` 字节级备份/恢复（备份文件入 ManagedFiles）。NetworkManager 连接级（nmcli）适配器留待 V0.6，本轮仅探测在场性。
- 健康检查：`warp_trace`（1.1.1.1/cdn-cgi/trace warp=on，带重试）、`google`、`deepseek`（非 403）、`wireguard_handshake`、`tunnel_route`（`ip route get` 经隧道接口）、`protected_route:<host>`（保护主机不走隧道）；无 `local_8688`（Linux 无常驻本地服务承诺）。
- 回滚：删策略规则 → `ip link del <iface>`（连带用户态进程退出）→ 删主表保护路由 → DNS 恢复（resolved 模式随接口清理；resolv.conf 模式字节级还原）→ 校验默认路由与快照一致；任一步失败返回 `rollback_incomplete`（exit 8）。
- 孤立探针：独立接口 `archieprobe`（不装路由），替换 Endpoint 后等待真实握手，握手失败即失败，探测后彻底拆除。
- 探针/应用共享 `Automatic` 装配，`app.Service` 平台门从 darwin 专判改为 darwin+linux 能力门。
- 验收：Ubuntu 22.04+ / Debian 12+ 各一台实机，三次生命周期 + 三类故障回滚；容器验收已获用户拍板认可（见验收记录），实机回归为正式发布前建议项。

## 安全边界

- 与 macOS/Windows 相同红线：只读检测先行；未识别网络状态不自动；快照不完整不自动；私钥不入日志。
- 生产服务器（`uzphoto`，控制面部署环境）**禁止**用作生命周期/回滚测试对象。

## 已知限制

- 无 Ubuntu/Debian 测试实机；切片 3 验收经用户拍板采用容器验收（见验收记录），正式发布前建议补 Ubuntu 22.04+ / Debian 12+ 实机回归。华为云北京出口对 WARP 数据面 UDP 全面封锁（注册 API TCP 可达，9 端口×容器/宿主网络栈均无握手），真实 WARP 完整生命周期验收在该环境不可行，已延后。
- 探针强制 `PersistentKeepalive = 5`（10s 探测窗口内可靠发起握手）；apply 对缺失 keepalive 的配置补 5，用户已配置值保留。wgcf 生成的配置无 keepalive，若不补必然探测超时（WireGuard 懒握手：无流量/无 keepalive 不发起）。
- IPv6 全禁系统（`/proc/net/if_inet6` 为空，Docker 默认）跳过 v6 隧道地址与路由；EulerOS 5.10 内核查不存在表报 "FIB table does not exist" 被视为空表可用。
- Ubuntu/Debian 的 wireguard-go 用户态二进制名为 `/usr/bin/wireguard`（非 wireguard-go），已双名回退。
- NetworkManager 连接级 DNS 适配器（nmcli）未实现，留待 V0.6；无 resolved 的 NM 系统回退 resolv.conf 文件托管（NM 重连可能覆写，回滚仍可恢复）。
- Linux 授权仅支持签名授权/测试授权，设备级租约绑定（deviceid）依赖后续 Linux 实现延后。
