# 汪连 ArchieLink｜外网访问专家 — 网络共存与断网防护规格

**状态：** Implemented（共存发现与 Gate 已交付并三平台验收；自动回滚链经 macOS V0.3 与 Linux V0.5 容器全链路实机/容器验收）  
**日期：** 2026-11-05（定稿）；2026-11-19（状态对账）  
**产品规格：** [SPEC.md](../product/SPEC.md)
**实现计划：** [2026-11-05-archie-link-coexistence.md](../plans/2026-11-05-archie-link-coexistence.md)
**后续计划：** [2026-11-05-archie-link-v0.3-macos-auto.md](../plans/2026-11-05-archie-link-v0.3-macos-auto.md)

## 1. 目标

定义 汪连 ArchieLink 与系统代理、透明代理、VPN、DNS、网络扩展、本地缓存代理、防火墙、虚拟网络和本地服务共存时的安全边界。

本规格不承诺识别所有代理软件。核心策略是：

> 能解释、快照和恢复的网络状态才允许自动修改；不能解释的状态一律禁止自动接管。

## 2. 对历史事故的准确定义

本机案例包含两类不同故障：

### 2.1 整机断网

第一次 `wg-quick up` 在 WireGuard 握手成功前已经接管默认路由和 DNS，但端点只解析到不可用的 IPv6 地址。

根因：

- 未隔离验证端点
- IPv6 地址被优先使用
- 默认路由和 DNS 过早变更
- 缺少自动回滚

### 2.2 本地服务掉线

`cache-doctor` 的本地 `127.0.0.1:8688` 始终可达。表面掉线来自 WARP DNS 把 `api.deepseek.com` 解析到不适配该 API 的 CloudFront 边缘，返回 403。

根因：

- 本地缓存代理依赖特定 DNS 结果
- DNS 变更后没有按业务端点验证
- 本地端口可达不等于上游服务可用

因此 `cache-doctor` 只是“本地代理/缓存服务”类别的一个实例，不应写成永久白名单特例。

## 3. 断网与网络异常分类

| 代码 | 场景 | 典型表现 |
|---|---|---|
| `global_route_hijack` | 隧道未就绪就接管默认路由 | 全部域名超时，本地网络也异常 |
| `dns_override_breakage` | DNS 被替换后域名解析到错误边缘 | 某些 API 返回 403 或证书错误 |
| `dns_missing` | 没有可用 DNS | 域名无法解析 |
| `dns_family_mismatch` | IPv4 / IPv6 解析选择错误 | IPv6 黑洞或只返回不可达地址 |
| `dns_negative_cache` | 失败结果被缓存 | 修复网络后仍持续失败 |
| `system_proxy_conflict` | 系统 HTTP/HTTPS/SOCKS 代理与新路线叠加 | 请求循环、认证失败或超时 |
| `pac_conflict` | PAC 路由规则与 CLI 直连行为不一致 | 浏览器可用、CLI 不可用，或相反 |
| `environment_proxy_conflict` | `HTTP_PROXY` 等环境变量仍指向旧代理 | 单进程可用、Agent 不可用 |
| `transparent_proxy_conflict` | TUN/TAP 或透明代理先于 WARP 接管流量 | 双重代理、循环或全部超时 |
| `route_precedence_conflict` | 多条默认路由、`/1` 路由或接口跃点冲突 | 部分流量走错路径 |
| `existing_vpn_conflict` | 已存在 VPN、kill switch 或 Always-on VPN | 新 VPN 无法连接或破坏旧 VPN |
| `ipv6_blackhole` | 无 IPv6 通路但优先使用 IPv6 | 握手收包为 0 或连接超时 |
| `mtu_fragmentation` | MTU 不匹配 | 小请求可用，大响应卡住 |
| `local_port_collision` | `1080`、`7890`、`8688` 等端口冲突 | 本地代理启动失败或服务被绕过 |
| `localhost_family_mismatch` | 服务监听 `127.0.0.1`，客户端访问 `::1` | 本地连接被拒绝 |
| `local_proxy_upstream_breakage` | cache-doctor、模型网关或本地反向代理上游失效 | 本地端口可达但业务 API 失败 |
| `hosts_override` | `/etc/hosts` 或 Windows hosts 覆盖 DNS | 特定域名固定走错地址 |
| `firewall_block` | 系统防火墙、EDR 或企业策略拦截 | 端点、DNS 或本地端口不可达 |
| `captive_portal` | 酒店、机场或公共 Wi-Fi 登录页 | HTTP 重定向、证书异常 |
| `network_roaming` | Wi-Fi 切换、休眠唤醒、网卡变化 | 旧路由或 DNS 失效 |
| `stale_managed_state` | 上次异常退出留下路由、DNS 或进程 | 重启后网络异常 |
| `tls_interception` | 企业 MITM 或自定义根证书 | 证书校验失败 |
| `virtualized_network_mismatch` | Docker、WSL、VM 或远程宿主网络隔离 | Agent 无法修改实际宿主网络 |
| `multicast_regression` | mDNS、DLNA、投屏或局域网发现被全隧道截获 | 本地设备无法互相发现 |

## 4. 网络所有权图

每个网络对象必须标记 owner：

```text
system
archielink
known_third_party
unknown
```

对象包括：

- 网络接口
- 默认路由和主机路由
- 路由策略和接口优先级
- 系统 DNS resolver
- scoped DNS 和 split DNS
- HTTP / HTTPS / SOCKS 代理
- PAC URL 和自动发现
- 环境代理
- hosts 覆盖
- 监听端口
- VPN / TUN / TAP 服务和进程
- 防火墙、kill switch 和网络扩展

### 4.1 自动模式门槛

`auto` 只允许满足全部条件：

1. 所有将被修改的对象都有 owner。
2. 所有原始值都可读取和恢复。
3. 不存在无法解释的 TUN/TAP 或透明代理。
4. 不存在指向未知进程的默认路由。
5. 不存在未知全局 DNS resolver。
6. 受保护端点基线全部完成。
7. 本地锁可用。

### 4.2 降级规则

以下任一情况必须返回 `guided`：

- 发现未知透明代理
- 发现未知 VPN 或网络扩展
- 无法读取完整 DNS resolver 链
- 无法读取代理绕过列表
- 无法识别默认路由 owner
- 受保护端点存在基线失败
- 防火墙或 MDM 阻止网络快照

无法读取基础网络状态时返回 `unsupported`。

## 5. 读取范围

### 5.1 macOS

只读检查：

```text
route -n get default
netstat -nr -f inet
netstat -nr -f inet6
ifconfig
scutil --proxy
scutil --dns
networksetup -listallnetworkservices
networksetup -getwebproxy
networksetup -getsecurewebproxy
networksetup -getsocksfirewallproxy
networksetup -getautoproxyurl
networksetup -getproxybypassdomains
cat /etc/hosts
lsof -nP -iTCP -sTCP:LISTEN
pgrep -fl wireguard-go
pgrep -fl ShadowsocksX
pgrep -fl Clash
pgrep -fl Surge
pfctl -s info
```

权限不足时降级，不得使用猜测值。

### 5.2 Windows

只读检查：

```text
Get-NetRoute
Get-NetIPInterface
Get-DnsClientServerAddress
Get-DnsClientNrptPolicy
Get-NetAdapter
Get-NetTCPConnection -State Listen
Get-VpnConnection
Get-NetFirewallProfile
Get-Process
Get-Content C:\Windows\System32\drivers\etc\hosts
netsh winhttp show proxy
```

代理配置同时检查：

- WinHTTP proxy
- WinINET proxy
- PAC 配置
- ProxyOverride
- WSL 代理环境

### 5.3 Linux

只读检查：

```text
ip route show
ip route show table all
ip -6 route show
ip rule show
ip link show
cat /etc/resolv.conf
resolvectl status
nmcli device show
cat /etc/hosts
ss -lnt
systemctl is-active
iptables-save
nft list ruleset
```

`iptables-save` 和 `nft list ruleset` 只能在权限允许时读取；失败时标记 unknown。

## 6. 受保护端点注册表

受保护端点不再使用固定单个域名。每个条目至少包含：

```json
{
  "id": "local-cache-api",
  "kind": "local_proxy_upstream",
  "direct_url": "http://127.0.0.1:8688/responses",
  "upstream_url": "https://api.deepseek.com",
  "expected": "not_403",
  "required_route": "local_dns",
  "baseline_required": true
}
```

支持的 kind：

```text
loopback_service
local_proxy_upstream
private_api
public_api
developer_callback
payment
identity
streaming
dlna_mdns
user_defined
```

### 6.1 自动发现

可以自动发现但不得读取秘密：

- 正在监听的 localhost 端口
- 当前系统代理和绕过列表
- 当前可解析的本地域名
- 当前浏览器或 Agent 已知的本地回调端口
- 用户配置的业务域名

不自动读取：

- Cookie
- Authorization header
- `.env`
- 支付凭证
- SSH / VPN 私钥

### 6.2 基线检查

启动前记录每个端点的：

- DNS 解析结果
- IPv4 / IPv6 可用性
- TCP 是否可达
- HTTP 状态或明确错误
- 是否经过代理
- 是否依赖 hosts 或本地 DNS

启动后必须逐项比较，不能只检查 WARP 和 Google。

## 7. 快照模型

快照至少包含：

```json
{
  "schema_version": "1",
  "created_at": "RFC3339",
  "platform": {},
  "interfaces": [],
  "routes": [],
  "route_rules": [],
  "dns": {
    "resolvers": [],
    "scoped": [],
    "hosts_digest": "",
    "hosts_entries_managed": []
  },
  "proxies": {
    "system": {},
    "pac": {},
    "environment": {},
    "bypass": []
  },
  "listeners": [],
  "vpn": [],
  "firewall": {},
  "protected_endpoints": [],
  "ownership": {},
  "incomplete_layers": []
}
```

任何 `incomplete_layers` 非空时，禁止自动修改对应层。

## 8. 变更与恢复

### 8.1 启动顺序

```text
读取快照
识别 owner
检查未知层
运行受保护端点基线
隔离握手
写入受控 DNS 例外
建立 WARP
验证全部受保护端点
提交 ownership
```

### 8.2 回滚顺序

```text
停止 owned 隧道进程
删除 managed resolver
恢复 hosts 中 managed 条目
恢复 DNS
恢复路由和路由策略
恢复系统代理和 PAC
验证原默认网关
验证原受保护端点
```

### 8.3 只改变自己拥有的资源

- 不删除没有 `managed by archielink` 标记的 resolver
- 不停止不是本 Skill 启动的进程
- 不覆盖用户原有代理配置
- 不静默修改全局 PAC
- 不删除用户 hosts

## 9. 健康检查

### 9.1 全局

- WARP trace 为 `warp=on`
- 至少一个外部 HTTPS 目标可达
- 默认路由与预期一致
- VPN 守护进程存活

### 9.2 DNS

- IPv4 和 IPv6 解析结果符合预期
- 不发生意外 DNS 泄漏
- 本地例外域名仍使用预期 resolver
- hosts 覆盖未被破坏

### 9.3 本地服务

- 每个受保护 localhost 端口可达
- 每个本地代理上游返回可接受状态
- `127.0.0.1` 与 `::1` 均按监听情况检查

### 9.4 代理

- 系统代理和 PAC 状态与快照一致
- 环境代理标记与快照一致
- 不出现代理循环
- 代理绕过列表未丢失

### 9.5 局域网

- 网关可达
- 私有 CIDR 可达
- mDNS / DLNA 场景按需验证
- 投屏和本地设备发现不被默认破坏

## 10. 失败码

```text
default_route_unknown_owner
transparent_proxy_detected
existing_vpn_conflict
system_proxy_conflict
pac_conflict
environment_proxy_conflict
dns_resolver_unknown
dns_family_mismatch
dns_negative_cache
hosts_override_detected
localhost_family_mismatch
local_port_conflict
local_service_unreachable
local_proxy_upstream_unreachable
ipv6_blackhole
mtu_fragmentation
firewall_block
captive_portal_detected
network_changed_during_startup
stale_managed_state
tls_interception_detected
snapshot_incomplete
rollback_incomplete
```

## 11. 平台差异

| 平台 | 自动模式要求 |
|---|---|
| macOS | 可读取代理、DNS、路由、监听端口；未知 Network Extension 或透明代理则降级 |
| Windows | 可读取 WinHTTP/WinINET/PAC、VPN、路由和 DNS；未知 WFP/EDR 策略则降级 |
| Linux | 可读取 route/rule/resolver/firewall；无法读取 iptables/nft 或 network manager 状态则降级 |
| HarmonyOS / TV | 由伴侣应用报告能力和活动 VPN；无法读取完整宿主状态时只允许引导 |

## 12. 测试矩阵

### 12.1 固定 fixture

- 无 DNS
- 本地 resolver
- scoped DNS
- PAC 代理
- 系统 HTTP 代理
- 环境代理
- hosts 覆盖
- 已有 TUN 接口
- 多个 VPN 进程
- 默认路由 owner 未知
- 本地端口冲突
- `127.0.0.1` / `::1` 不一致
- IPv6-only 解析但无 IPv6 路由
- MTU 黑洞
- 快照不完整
- 旧隧道残留

### 12.2 集成故障注入

- 隔离握手失败
- DNS 切换后 API 403
- 本地 cache proxy 上游 timeout
- 路由写入失败
- DNS 恢复失败
- 守护进程崩溃
- 用户在启动中断开 Wi-Fi
- 启动中切换网络

### 12.3 实机组合

- macOS + Shadowsocks
- macOS + 系统代理
- macOS + PAC
- Windows + WinHTTP/WinINET 代理
- Windows + 已有 VPN
- Linux + systemd-resolved
- Linux + NetworkManager
- Linux + 容器

## 13. V0.3 验收

1. 启动前发现未知透明代理或未知路由 owner 时，不执行网络变更。
2. 已有代理配置不被覆盖；失败后逐项恢复。
3. 本地缓存代理场景中，端口可达且上游非 403。
4. DNS、hosts、PAC、system proxy 和 route 都有快照与恢复测试。
5. IPv6 黑洞、MTU 和 localhost family mismatch 都能被诊断。
6. `incomplete_layers` 非空时自动降级。
7. 整机断网事故的故障注入连续三次回滚成功。
8. 至少完成一个系统代理、一个透明代理和一个本地缓存代理场景。

## 14. 非目标

- 不保证识别所有代理软件
- 不自动修改第三方代理配置
- 不绕过企业防火墙或 MDM
- 不接管用户已有的全局 VPN
- 不把未知状态当成安全状态

产品允许“无法自动支持”，不允许“不了解仍然强行修改”。
