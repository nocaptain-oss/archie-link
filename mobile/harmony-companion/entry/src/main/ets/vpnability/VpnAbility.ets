import { Want } from '@kit.AbilityKit';
import { BusinessError } from '@kit.BasicServicesKit';
import { fileIo } from '@kit.CoreFileKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { connection, VpnExtensionAbility, vpnExtension } from '@kit.NetworkKit';
import { writeVpnState } from '../common/VpnState';
import { readEnabledApps } from '../common/AppSelectionStore';
import { NativeSocket } from '../utils/NativeSocketFFI';
import { base64Decode } from '../wireguard/crypto/Base64';
import { chacha20Poly1305Decrypt, chacha20Poly1305Encrypt } from '../wireguard/crypto/ChaCha20Poly1305';
import { x25519 } from '../wireguard/crypto/X25519';
import {
  NoiseHandshake,
  SessionKey,
  readU32,
  writeU32
} from '../wireguard/tunnel/Handshake';

const DOMAIN = 0x4103;
const TAG = 'ArchieLinkVPN';
const MSG_TYPE_RESPONSE = 2;
const MSG_TYPE_COOKIE_REPLY = 3;
const MSG_TYPE_TRANSPORT = 4;
const DEFAULT_DNS = '1.1.1.1';
const KEEPALIVE_MS = 10_000;
const HANDSHAKE_RETRY_MS = 5_000;
const REKEY_AFTER_MS = 120_000;
const REKEY_RETRY_MS = 5_000;
const TUNNEL_STALL_MS = 90_000;
const PREV_SESSION_GRACE_MS = 20_000;
const CONTAINER_BUNDLES: string[] = [
  'com.droi.tong',
  'com.easy.hmos.abroad',
  'com.gspace.android'
];
const NAT_PORT_BASE = 10000;
const NAT_PORT_MAX = 19999;
const NAT_FLOW_LIMIT = 4096;
const ICMP_ECHO_REPLY = 0;
const ICMP_ECHO_REQUEST = 8;

class NatFlowEntry {
  proto: number;
  origSrcIp: number[];
  origSrcPort: number;
  natPort: number;
  flowKey: string;
  replyKey: string;
  lastUsedMs: number;

  constructor(
    proto: number,
    origSrcIp: number[],
    origSrcPort: number,
    natPort: number,
    flowKey: string,
    replyKey: string
  ) {
    this.proto = proto;
    this.origSrcIp = origSrcIp;
    this.origSrcPort = origSrcPort;
    this.natPort = natPort;
    this.flowKey = flowKey;
    this.replyKey = replyKey;
    this.lastUsedMs = Date.now();
  }
}

class DnsRedirectEntry {
  origDstIp: number[];
  lastUsedMs: number;

  constructor(origDstIp: number[]) {
    this.origDstIp = origDstIp;
    this.lastUsedMs = Date.now();
  }
}

export default class VpnAbility extends VpnExtensionAbility {
  private vpnConnection: vpnExtension.VpnConnection | null = null;
  private nativeSock: NativeSocket | null = null;
  private tunFd: number = -1;
  private handshake: NoiseHandshake | null = null;
  private privateKey: string = '';
  private publicKey: string = '';
  private presharedKey: string = '';
  private serverIP: string = '';
  private serverPort: number = 0;
  private clientIP: string = '';
  private dnsServer: string = '';
  private running: boolean = false;
  private handshakeComplete: boolean = false;
  private handshakeAttempts: number = 0;
  private handshakeTimer: number = -1;
  private keepaliveTimer: number = -1;
  private transportRxBytes: number = 0;
  private transportTxBytes: number = 0;
  private natByFlow: Map<string, NatFlowEntry> = new Map<string, NatFlowEntry>();
  private natByReply: Map<string, NatFlowEntry> = new Map<string, NatFlowEntry>();
  private icmpByReply: Map<string, number[]> = new Map<string, number[]>();
  private nextNatPort: number = NAT_PORT_BASE;
  private seenSourceIps: Set<string> = new Set<string>();
  private natLogCount: number = 0;
  private dnsByReply: Map<string, DnsRedirectEntry> = new Map<string, DnsRedirectEntry>();
  private dnsLogCount: number = 0;
  private seenDnsDomains: Set<string> = new Set<string>();
  private seenFlows: Set<string> = new Set<string>();
  private flowSeenCount: number = 0;
  private pendingHandshake: NoiseHandshake | null = null;
  private pendingAttempts: number = 0;
  private pendingTimer: number = -1;
  private rekeyTimer: number = -1;
  private prevSessionKeys: SessionKey | null = null;
  private prevLocalIndex: number = 0;
  private prevExpiresMs: number = 0;
  private lastTransportRxMs: number = 0;

  onCreate(_want: Want): void {
    hilog.info(DOMAIN, TAG, 'VPN extension created');
  }

  onRequest(want: Want, _startId: number): void {
    if (this.running) {
      return;
    }
    const params = want.parameters;
    this.serverIP = String(params?.['serverIP'] ?? '');
    this.serverPort = Number(params?.['serverPort'] ?? 0);
    const clientAddress = String(params?.['clientAddress'] ?? '');
    const clientPrefixLength = Number(params?.['clientPrefixLength'] ?? 32);
    const configuredDns = String(params?.['dnsServer'] ?? '').trim();
    const dnsServer = configuredDns.length > 0 ? configuredDns : DEFAULT_DNS;
    this.privateKey = String(params?.['privateKey'] ?? '');
    this.publicKey = String(params?.['publicKey'] ?? '');
    this.presharedKey = String(params?.['presharedKey'] ?? '');
    const allowedIPs = String(params?.['allowedIPs'] ?? '0.0.0.0/0');
    if (!validIPv4(this.serverIP) || this.serverPort < 1 || this.serverPort > 65535 ||
      !validIPv4(clientAddress) || clientPrefixLength < 0 || clientPrefixLength > 32 ||
      this.privateKey.length === 0 || this.publicKey.length === 0) {
      this.fail('WireGuard configuration is incomplete');
      return;
    }
    this.clientIP = clientAddress;
    this.dnsServer = dnsServer;
    this.setupVpn(
      clientAddress,
      clientPrefixLength,
      dnsServer,
      allowedIPs
    ).catch((error: Object): void => {
      const businessError = error as BusinessError;
      const message = this.errorMessage(error);
      if (businessError.code === 2203002 || message.includes('VPN exist already')) {
        hilog.warn(
          DOMAIN,
          TAG,
          'VPN conflict (code %{public}d): another VPN is running, map to conflict state',
          businessError.code
        );
        writeVpnState(
          this.context,
          'conflict',
          '检测到其他 VPN 正在运行。鸿蒙同一时刻仅允许一个 VPN，请先断开现有 VPN（系统 VPN 或其他应用）后重试。'
        );
        this.cleanup();
        return;
      }
      this.fail(message);
    });
  }

  onDestroy(): void {
    this.cleanup();
    writeVpnState(this.context, 'stopped', 'HarmonyOS VPN 已停止，资源已释放。');
  }

  private async setupVpn(
    clientAddress: string,
    clientPrefixLength: number,
    dnsServer: string,
    allowedIPs: string
  ): Promise<void> {
    this.running = true;
    writeVpnState(this.context, 'preparing', '正在创建 HarmonyOS VPN 接口。');
    const selectedApps = readEnabledApps(this.context);
    if (selectedApps.length === 0) {
      throw new Error('请先在设置中添加需要通过 VPN 的应用');
    }
    const containerMode = selectedApps.some((bundle: string): boolean =>
      CONTAINER_BUNDLES.includes(bundle)
    );
    this.vpnConnection = vpnExtension.createVpnConnection(this.context);

    const vpnConfig: vpnExtension.VpnConfig = {
      addresses: [{
        address: { address: clientAddress, family: 1, port: 0 },
        prefixLength: clientPrefixLength
      }],
      routes: this.buildRoutes(allowedIPs),
      dnsAddresses: dnsServer.length > 0 ? [dnsServer] : [],
      mtu: 1500,
      isIPv4Accepted: true,
      isIPv6Accepted: false
    };
    if (!containerMode) {
      vpnConfig.trustedApplications = selectedApps;
    }
    this.tunFd = await this.vpnConnection.create(vpnConfig);
    try {
      await this.vpnConnection.protectProcessNet();
    } catch (error) {
      hilog.warn(DOMAIN, TAG, 'protectProcessNet unavailable: %{public}s', JSON.stringify(error));
    }
    writeVpnState(
      this.context,
      'tun_ready',
      containerMode
        ? `容器兼容模式，全部应用经 VPN，TUN fd=${this.tunFd}。`
        : `VPN 接口已创建，TUN fd=${this.tunFd}。`
    );

    this.nativeSock = new NativeSocket();
    const socketFd = this.nativeSock.create();
    if (socketFd < 0) {
      throw new Error('native UDP socket creation failed');
    }
    if (!this.nativeSock.startRecvThread(
      (data: ArrayBuffer, _address: string, _port: number): void => {
        this.onWireGuardMessage(new Uint8Array(data));
      }
    )) {
      throw new Error('native UDP receive thread failed');
    }
    await this.sendHandshake();
    this.scheduleHandshakeRetry();
  }

  private buildRoutes(allowedIPs: string): connection.RouteInfo[] {
    const routes: connection.RouteInfo[] = [];
    for (const raw of allowedIPs.split(',')) {
      const cidr = raw.trim();
      if (cidr.length === 0 || cidr.includes(':')) {
        continue;
      }
      const parts = cidr.split('/');
      if (parts.length !== 2 || !validIPv4(parts[0])) {
        continue;
      }
      const prefix = Number(parts[1]);
      if (prefix < 0 || prefix > 32) {
        continue;
      }
      routes.push({
        interface: 'vpn-tun',
        destination: {
          address: { address: parts[0], family: 1, port: 0 },
          prefixLength: prefix
        },
        gateway: {
          address: '0.0.0.0',
          family: 1,
          port: 0
        },
        hasGateway: false,
        isDefaultRoute: prefix === 0
      });
    }
    if (routes.length === 0) {
      routes.push({
        interface: 'vpn-tun',
        destination: {
          address: { address: '0.0.0.0', family: 1, port: 0 },
          prefixLength: 0
        },
        gateway: {
          address: '0.0.0.0',
          family: 1,
          port: 0
        },
        hasGateway: false,
        isDefaultRoute: true
      });
    }
    return routes;
  }

  private async sendHandshake(): Promise<void> {
    if (!this.nativeSock || !this.running) {
      return;
    }
    this.handshakeAttempts++;
    this.handshakeComplete = false;
    const privateBytes = base64Decode(this.privateKey);
    this.handshake = new NoiseHandshake();
    this.handshake.init(privateBytes, x25519(privateBytes, 9n), base64Decode(this.publicKey));
    if (this.presharedKey.length > 0) {
      this.handshake.setPresharedKey(base64Decode(this.presharedKey));
    }
    await this.sendToServer(this.handshake.buildInitiation());
    writeVpnState(
      this.context,
      'handshaking',
      `正在与 WireGuard 端点握手（第 ${this.handshakeAttempts} 次）。`
    );
  }

  private scheduleHandshakeRetry(): void {
    if (this.handshakeTimer >= 0) {
      clearTimeout(this.handshakeTimer);
    }
    this.handshakeTimer = setTimeout((): void => {
      if (this.running && !this.handshakeComplete && this.handshakeAttempts < 6) {
        this.sendHandshake()
          .then((): void => this.scheduleHandshakeRetry())
          .catch((error: Object): void => this.fail(this.errorMessage(error)));
      } else if (!this.handshakeComplete) {
        this.fail('WireGuard handshake timed out');
      }
    }, HANDSHAKE_RETRY_MS);
  }

  private async sendToServer(packet: Uint8Array): Promise<void> {
    if (!this.nativeSock) {
      return;
    }
    const data = packet.buffer.slice(
      packet.byteOffset,
      packet.byteOffset + packet.byteLength
    ) as ArrayBuffer;
    if (this.nativeSock.sendTo(data, this.serverIP, this.serverPort) <= 0) {
      throw new Error('native UDP send failed');
    }
  }

  private onWireGuardMessage(packet: Uint8Array): void {
    if (packet.length < 4) {
      return;
    }
    const type = readU32(packet, 0);
    if (type === MSG_TYPE_RESPONSE) {
      const pending = this.pendingHandshake;
      if (pending !== null && pending.processResponse(packet)) {
        this.completeRekey();
        return;
      }
      const initial = this.handshake;
      if (pending === null && initial !== null && initial.processResponse(packet)) {
        this.handshakeComplete = true;
        if (this.handshakeTimer >= 0) {
          clearTimeout(this.handshakeTimer);
          this.handshakeTimer = -1;
        }
        this.lastTransportRxMs = Date.now();
        writeVpnState(this.context, 'connected', 'WireGuard 握手成功，正在转发流量。');
        this.startTunForwarding();
        this.startKeepalive();
        this.scheduleRekey();
      }
      return;
    }
    if (type === MSG_TYPE_TRANSPORT) {
      hilog.info(DOMAIN, TAG, 'WireGuard transport received: %{public}d bytes', packet.length);
      this.processTransportPacket(packet);
      return;
    }
    if (type === MSG_TYPE_COOKIE_REPLY) {
      hilog.warn(DOMAIN, TAG, 'WireGuard cookie reply received');
    }
  }

  private processTransportPacket(packet: Uint8Array): void {
    if (packet.length < 32) {
      return;
    }
    const receiverIndex = readU32(packet, 4);
    let plain: Uint8Array | null = null;
    const active = this.handshake;
    if (active !== null && active.sessionKeys !== null &&
      active.localIndex === receiverIndex) {
      plain = this.decryptTransportWith(active.sessionKeys, packet);
    }
    if (plain === null && this.prevSessionKeys !== null &&
      this.prevLocalIndex === receiverIndex && Date.now() < this.prevExpiresMs) {
      plain = this.decryptTransportWith(this.prevSessionKeys, packet);
    }
    if (plain === null) {
      hilog.warn(DOMAIN, TAG, 'WireGuard transport decrypt failed');
      return;
    }
    this.lastTransportRxMs = Date.now();
    if (plain.length === 0) {
      return;
    }
    this.transportRxBytes += plain.length;
    this.restoreDnsSrc(plain);
    this.natInboundPacket(plain);
    fileIo.write(this.tunFd, plain.buffer as ArrayBuffer).catch((error: Object): void => {
      hilog.error(DOMAIN, TAG, 'TUN write failed: %{public}s', this.errorMessage(error));
    });
  }

  private decryptTransportWith(session: SessionKey, packet: Uint8Array): Uint8Array | null {
    const counter = readU32(packet, 8) + readU32(packet, 12) * 0x100000000;
    const expected = session.recvCounter;
    if (counter + 2000 < expected) {
      return null;
    }
    const nonce = new Uint8Array(12);
    nonce.set(packet.subarray(8, 16), 4);
    const plain = chacha20Poly1305Decrypt(
      session.recvKey,
      nonce,
      packet.subarray(16),
      new Uint8Array(0)
    );
    if (plain === null) {
      return null;
    }
    if (counter >= expected) {
      session.recvCounter = counter + 1;
    }
    return plain;
  }

  private startTunForwarding(): void {
    const fd = this.tunFd;
    const readBuffer = new ArrayBuffer(65535);
    const loop = async (): Promise<void> => {
      while (this.running && this.handshakeComplete && fd >= 0 && this.tunFd === fd) {
        try {
          const length = await fileIo.read(fd, readBuffer);
          if (length > 0) {
            const packet = new Uint8Array(readBuffer.slice(0, length));
            if (packet.length >= 20 && (packet[0] >> 4) === 4) {
              if (this.transportTxBytes === 0) {
                hilog.info(DOMAIN, TAG, 'First IPv4 packet read from TUN: %{public}d bytes', length);
              }
              await this.sendTransportPacket(packet);
            }
          }
        } catch (_error) {
          await new Promise<void>((resolve: () => void): void => {
            setTimeout(resolve, 10);
          });
        }
      }
    };
    loop().catch((error: Object): void => this.fail(this.errorMessage(error)));
  }

  private async sendTransportPacket(ipPacket: Uint8Array): Promise<void> {
    if (!this.handshake?.sessionKeys) {
      return;
    }
    this.logFlow(ipPacket);
    this.natOutboundPacket(ipPacket);
    this.rewriteDnsDst(ipPacket);
    const counter = this.handshake.sessionKeys.sendCounter++;
    const nonce = new Uint8Array(12);
    writeU64LE(nonce, 4, BigInt(counter));
    const encrypted = chacha20Poly1305Encrypt(
      this.handshake.sessionKeys.sendKey,
      nonce,
      ipPacket,
      new Uint8Array(0)
    );
    const packet = new Uint8Array(16 + encrypted.ciphertext.length + encrypted.tag.length);
    writeU32(packet, 0, MSG_TYPE_TRANSPORT);
    writeU32(packet, 4, this.handshake.remoteIndex);
    writeU64LE(packet, 8, BigInt(counter));
    packet.set(encrypted.ciphertext, 16);
    packet.set(encrypted.tag, 16 + encrypted.ciphertext.length);
    this.transportTxBytes += ipPacket.length;
    await this.sendToServer(packet);
  }

  private natOutboundPacket(packet: Uint8Array): void {
    if (packet.length < 20 || (packet[0] >> 4) !== 4) {
      return;
    }
    const srcStr = ipv4AtToString(packet, 12);
    if (this.seenSourceIps.size < 10 && !this.seenSourceIps.has(srcStr)) {
      this.seenSourceIps.add(srcStr);
      hilog.info(DOMAIN, TAG, 'TUN source seen: %{public}s', srcStr);
    }
    if (srcStr === this.clientIP) {
      return;
    }
    const clientParts = this.clientIP.split('.').map((part: string): number => Number(part));
    if (clientParts.length !== 4) {
      return;
    }
    const protocol = packet[9];
    const headerLength = (packet[0] & 0x0F) * 4;
    const fragment = (packet[6] << 8) + packet[7];
    const isFragment = (fragment & 0x3FFF) !== 0;

    if (protocol === 1) {
      if (isFragment || packet.length < headerLength + 6 ||
        packet[headerLength] !== ICMP_ECHO_REQUEST) {
        return;
      }
      const id = (packet[headerLength + 4] << 8) + packet[headerLength + 5];
      this.icmpByReply.set(`icmp:${id}`, [packet[12], packet[13], packet[14], packet[15]]);
      if (this.icmpByReply.size > 256) {
        this.icmpByReply.clear();
      }
      this.rewriteIpv4At(packet, 12, clientParts, false);
      return;
    }
    if (protocol !== 6 && protocol !== 17) {
      return;
    }
    if (packet.length < headerLength + 4) {
      return;
    }
    const origSrcPort = (packet[headerLength] << 8) + packet[headerLength + 1];
    const dstStr = ipv4AtToString(packet, 16);
    const dstPort = (packet[headerLength + 2] << 8) + packet[headerLength + 3];
    const flowKey = `${protocol}:${srcStr}:${origSrcPort}:${dstStr}:${dstPort}`;
    let entry = this.natByFlow.get(flowKey);
    if (entry === undefined) {
      const natPort = this.allocateNatPort();
      const replyKey = `${protocol}:${dstStr}:${dstPort}:${natPort}`;
      entry = new NatFlowEntry(
        protocol,
        [packet[12], packet[13], packet[14], packet[15]],
        origSrcPort,
        natPort,
        flowKey,
        replyKey
      );
      this.natByFlow.set(flowKey, entry);
      this.natByReply.set(replyKey, entry);
      this.evictNatFlows();
      if (this.natLogCount < 5) {
        this.natLogCount++;
        hilog.info(
          DOMAIN,
          TAG,
          'NAT flow: %{public}s %{public}s:%{public}d -> %{public}s:%{public}d (nat %{public}d)',
          protocol === 6 ? 'TCP' : 'UDP',
          srcStr,
          origSrcPort,
          dstStr,
          dstPort,
          natPort
        );
      }
    }
    entry.lastUsedMs = Date.now();
    if (isFragment) {
      this.rewriteIpv4At(packet, 12, clientParts, false);
      return;
    }
    this.rewriteIpv4At(packet, 12, clientParts, false);
    if (entry.natPort !== origSrcPort) {
      packet[headerLength] = (entry.natPort >> 8) & 0xFF;
      packet[headerLength + 1] = entry.natPort & 0xFF;
    }
    this.recomputeTransportChecksum(packet);
  }

  private natInboundPacket(packet: Uint8Array): void {
    if (packet.length < 20 || (packet[0] >> 4) !== 4) {
      return;
    }
    const dstStr = ipv4AtToString(packet, 16);
    if (dstStr !== this.clientIP) {
      return;
    }
    const protocol = packet[9];
    const headerLength = (packet[0] & 0x0F) * 4;
    const fragment = (packet[6] << 8) + packet[7];
    const isFragment = (fragment & 0x3FFF) !== 0;

    if (protocol === 1) {
      if (isFragment || packet.length < headerLength + 6 ||
        packet[headerLength] !== ICMP_ECHO_REPLY) {
        return;
      }
      const id = (packet[headerLength + 4] << 8) + packet[headerLength + 5];
      const origSrc = this.icmpByReply.get(`icmp:${id}`);
      if (origSrc === undefined) {
        return;
      }
      this.rewriteIpv4At(packet, 16, origSrc, false);
      return;
    }
    if (protocol !== 6 && protocol !== 17) {
      return;
    }
    if (packet.length < headerLength + 4) {
      return;
    }
    const srcStr = ipv4AtToString(packet, 12);
    const srcPort = (packet[headerLength] << 8) + packet[headerLength + 1];
    const dstPort = (packet[headerLength + 2] << 8) + packet[headerLength + 3];
    const entry = this.natByReply.get(`${protocol}:${srcStr}:${srcPort}:${dstPort}`);
    if (entry === undefined) {
      return;
    }
    entry.lastUsedMs = Date.now();
    if (this.flowSeenCount < 130 && !this.seenFlows.has(`ret:${entry.replyKey}`)) {
      this.seenFlows.add(`ret:${entry.replyKey}`);
      hilog.info(DOMAIN, TAG, 'NAT return matched: %{public}s', entry.replyKey);
    }
    this.rewriteIpv4At(packet, 16, entry.origSrcIp, false);
    if (isFragment) {
      return;
    }
    if (entry.origSrcPort !== dstPort) {
      packet[headerLength + 2] = (entry.origSrcPort >> 8) & 0xFF;
      packet[headerLength + 3] = entry.origSrcPort & 0xFF;
    }
    this.recomputeTransportChecksum(packet);
  }

  private rewriteDnsDst(packet: Uint8Array): void {
    if (this.dnsServer.length === 0 || packet.length < 24 || (packet[0] >> 4) !== 4) {
      return;
    }
    const protocol = packet[9];
    if (protocol !== 6 && protocol !== 17) {
      return;
    }
    const fragment = (packet[6] << 8) + packet[7];
    if ((fragment & 0x3FFF) !== 0) {
      return;
    }
    const headerLength = (packet[0] & 0x0F) * 4;
    if (headerLength + 4 > packet.length) {
      return;
    }
    const dstPort = (packet[headerLength + 2] << 8) + packet[headerLength + 3];
    if (dstPort !== 53) {
      return;
    }
    const srcStr = ipv4AtToString(packet, 12);
    const srcPort = (packet[headerLength] << 8) + packet[headerLength + 1];
    const origDstStr = ipv4AtToString(packet, 16);
    if (protocol === 17 && this.seenDnsDomains.size < 500) {
      const qname = this.parseDnsQname(packet.subarray(headerLength + 8));
      if (qname.length > 0 && !this.seenDnsDomains.has(qname)) {
        this.seenDnsDomains.add(qname);
        hilog.info(DOMAIN, TAG, 'DNS query: %{public}s (to %{public}s)', qname, origDstStr);
      }
    }
    const key = `${protocol}:${srcStr}:${srcPort}`;
    this.dnsByReply.set(key, new DnsRedirectEntry([packet[16], packet[17], packet[18], packet[19]]));
    this.evictDnsRedirects();
    if (this.dnsLogCount < 10) {
      this.dnsLogCount++;
      hilog.info(
        DOMAIN,
        TAG,
        'DNS redirect: %{public}s:%{public}d %{public}s -> %{public}s',
        srcStr,
        srcPort,
        origDstStr,
        this.dnsServer
      );
    }
    if (origDstStr === this.dnsServer) {
      return;
    }
    const dnsParts = this.dnsServer.split('.').map((part: string): number => Number(part));
    if (dnsParts.length !== 4 || !dnsParts.every((part: number): boolean =>
      Number.isInteger(part) && part >= 0 && part <= 255
    )) {
      return;
    }
    this.rewriteIpv4At(packet, 16, dnsParts, false);
    this.recomputeTransportChecksum(packet);
  }

  private restoreDnsSrc(packet: Uint8Array): void {
    if (this.dnsServer.length === 0 || packet.length < 24 || (packet[0] >> 4) !== 4) {
      return;
    }
    const protocol = packet[9];
    if (protocol !== 6 && protocol !== 17) {
      return;
    }
    const fragment = (packet[6] << 8) + packet[7];
    if ((fragment & 0x3FFF) !== 0) {
      return;
    }
    const headerLength = (packet[0] & 0x0F) * 4;
    if (headerLength + 4 > packet.length) {
      return;
    }
    const srcStr = ipv4AtToString(packet, 12);
    if (srcStr !== this.dnsServer) {
      return;
    }
    const srcPort = (packet[headerLength] << 8) + packet[headerLength + 1];
    if (srcPort !== 53) {
      return;
    }
    const dstStr = ipv4AtToString(packet, 16);
    const dstPort = (packet[headerLength + 2] << 8) + packet[headerLength + 3];
    const entry = this.dnsByReply.get(`${protocol}:${dstStr}:${dstPort}`);
    if (entry === undefined) {
      return;
    }
    entry.lastUsedMs = Date.now();
    this.rewriteIpv4At(packet, 12, entry.origDstIp, false);
    this.recomputeTransportChecksum(packet);
    if (protocol === 17 && this.seenDnsDomains.size < 500) {
      const qname = this.parseDnsQname(packet.subarray(headerLength + 8));
      if (qname.length > 0 && !this.seenDnsDomains.has(`reply:${qname}`)) {
        this.seenDnsDomains.add(`reply:${qname}`);
        hilog.info(DOMAIN, TAG, 'DNS reply restored: %{public}s', qname);
      }
    }
  }

  private parseDnsQname(payload: Uint8Array): string {
    if (payload.length < 13) {
      return '';
    }
    const qdcount = (payload[4] << 8) + payload[5];
    if (qdcount !== 1) {
      return '';
    }
    let off = 12;
    let name = '';
    while (off < payload.length) {
      const len = payload[off];
      if (len === 0) {
        break;
      }
      if ((len & 0xC0) !== 0 || off + 1 + len >= payload.length) {
        return '';
      }
      for (let i = 0; i < len; i++) {
        name += String.fromCharCode(payload[off + 1 + i]);
      }
      name += '.';
      off += 1 + len;
      if (name.length > 253) {
        return '';
      }
    }
    return name.length > 0 ? name.slice(0, name.length - 1) : '';
  }

  private evictDnsRedirects(): void {
    if (this.dnsByReply.size <= 512) {
      return;
    }
    const cutoff = Date.now() - 60_000;
    const stale: string[] = [];
    this.dnsByReply.forEach((entry: DnsRedirectEntry, key: string): void => {
      if (entry.lastUsedMs < cutoff) {
        stale.push(key);
      }
    });
    for (const key of stale) {
      this.dnsByReply.delete(key);
    }
    if (this.dnsByReply.size > 512) {
      this.dnsByReply.clear();
    }
  }

  private logFlow(packet: Uint8Array): void {
    if (this.flowSeenCount >= 100 || packet.length < 24) {
      return;
    }
    const protocol = packet[9];
    if (protocol !== 6 && protocol !== 17) {
      return;
    }
    const headerLength = (packet[0] & 0x0F) * 4;
    if (headerLength + 4 > packet.length) {
      return;
    }
    const srcStr = ipv4AtToString(packet, 12);
    const dstStr = ipv4AtToString(packet, 16);
    const dstPort = (packet[headerLength + 2] << 8) + packet[headerLength + 3];
    const key = `${protocol}:${srcStr}:${dstStr}:${dstPort}`;
    if (this.seenFlows.has(key)) {
      return;
    }
    this.seenFlows.add(key);
    this.flowSeenCount++;
    hilog.info(
      DOMAIN,
      TAG,
      'FLOW %{public}s %{public}s -> %{public}s:%{public}d',
      protocol === 6 ? 'TCP' : 'UDP',
      srcStr,
      dstStr,
      dstPort
    );
  }

  private allocateNatPort(): number {
    const port = this.nextNatPort;
    this.nextNatPort = this.nextNatPort >= NAT_PORT_MAX ? NAT_PORT_BASE : this.nextNatPort + 1;
    return port;
  }

  private evictNatFlows(): void {
    if (this.natByReply.size <= NAT_FLOW_LIMIT) {
      return;
    }
    const cutoff = Date.now() - 60_000;
    const stale: string[] = [];
    this.natByReply.forEach((entry: NatFlowEntry, key: string): void => {
      if (entry.lastUsedMs < cutoff) {
        stale.push(key);
      }
    });
    for (const key of stale) {
      const entry = this.natByReply.get(key);
      if (entry !== undefined) {
        this.natByFlow.delete(entry.flowKey);
      }
      this.natByReply.delete(key);
    }
    if (this.natByReply.size > NAT_FLOW_LIMIT) {
      this.natByFlow.clear();
      this.natByReply.clear();
    }
  }

  private rewriteIpv4At(packet: Uint8Array, offset: number, newIp: number[], fixTransport: boolean): void {
    packet[offset] = newIp[0];
    packet[offset + 1] = newIp[1];
    packet[offset + 2] = newIp[2];
    packet[offset + 3] = newIp[3];
    this.recomputeIpHeaderChecksum(packet);
    if (fixTransport) {
      this.recomputeTransportChecksum(packet);
    }
  }

  private recomputeIpHeaderChecksum(packet: Uint8Array): void {
    if (packet.length < 20) {
      return;
    }
    packet[10] = 0;
    packet[11] = 0;
    const headerLength = (packet[0] & 0x0F) * 4;
    let sum = 0;
    for (let index = 0; index < headerLength; index += 2) {
      sum += (packet[index] << 8) + packet[index + 1];
    }
    while (sum > 0xFFFF) {
      sum = (sum & 0xFFFF) + (sum >>> 16);
    }
    const checksum = (~sum) & 0xFFFF;
    packet[10] = (checksum >> 8) & 0xFF;
    packet[11] = checksum & 0xFF;
  }

  private recomputeTransportChecksum(packet: Uint8Array): void {
    const protocol = packet[9];
    if ((protocol !== 6 && protocol !== 17) || packet.length < 20) {
      return;
    }
    const fragment = (packet[6] << 8) + packet[7];
    if ((fragment & 0x3FFF) !== 0) {
      return;
    }
    const headerLength = (packet[0] & 0x0F) * 4;
    if (headerLength + 4 > packet.length) {
      return;
    }
    const isUdp = protocol === 17;
    const checksumOffset = isUdp ? headerLength + 6 : headerLength + 16;
    if (isUdp && (packet[headerLength + 6] << 8) + packet[headerLength + 7] === 0) {
      return;
    }
    const ipTotal = (packet[2] << 8) + packet[3];
    const end = ipTotal >= headerLength + 4 && ipTotal <= packet.length ? ipTotal : packet.length;
    packet[checksumOffset] = 0;
    packet[checksumOffset + 1] = 0;
    let sum = 0;
    for (let index = 12; index < 20; index += 2) {
      sum += (packet[index] << 8) + packet[index + 1];
    }
    sum += protocol;
    sum += end - headerLength;
    for (let index = headerLength; index + 1 < end; index += 2) {
      sum += (packet[index] << 8) + packet[index + 1];
    }
    if ((end - headerLength) % 2 === 1) {
      sum += packet[end - 1] << 8;
    }
    while (sum > 0xFFFF) {
      sum = (sum & 0xFFFF) + (sum >>> 16);
    }
    let checksum = (~sum) & 0xFFFF;
    if (checksum === 0 && isUdp) {
      checksum = 0xFFFF;
    }
    packet[checksumOffset] = (checksum >> 8) & 0xFF;
    packet[checksumOffset + 1] = checksum & 0xFF;
  }

  private scheduleRekey(): void {
    if (this.rekeyTimer >= 0) {
      clearTimeout(this.rekeyTimer);
    }
    this.rekeyTimer = setTimeout((): void => {
      this.rekeyTimer = -1;
      if (!this.running || !this.handshakeComplete) {
        return;
      }
      this.startRekey('scheduled');
    }, REKEY_AFTER_MS);
  }

  private startRekey(reason: string): void {
    if (!this.running || !this.handshakeComplete || this.pendingHandshake !== null) {
      return;
    }
    hilog.info(DOMAIN, TAG, 'WireGuard rekey started (%{public}s)', reason);
    this.pendingAttempts = 0;
    this.sendRekeyInitiation()
      .then((): void => this.schedulePendingRetry())
      .catch((): void => this.schedulePendingRetry());
  }

  private async sendRekeyInitiation(): Promise<void> {
    if (!this.nativeSock || !this.running || !this.handshakeComplete) {
      return;
    }
    const privateBytes = base64Decode(this.privateKey);
    const pending = new NoiseHandshake();
    pending.init(privateBytes, x25519(privateBytes, 9n), base64Decode(this.publicKey));
    if (this.presharedKey.length > 0) {
      pending.setPresharedKey(base64Decode(this.presharedKey));
    }
    this.pendingHandshake = pending;
    this.pendingAttempts++;
    await this.sendToServer(pending.buildInitiation());
    hilog.info(DOMAIN, TAG, 'WireGuard rekey initiation sent (attempt %{public}d)', this.pendingAttempts);
  }

  private schedulePendingRetry(): void {
    if (this.pendingTimer >= 0) {
      clearTimeout(this.pendingTimer);
    }
    this.pendingTimer = setTimeout((): void => {
      this.pendingTimer = -1;
      if (!this.running || this.pendingHandshake === null) {
        return;
      }
      this.sendRekeyInitiation()
        .then((): void => this.schedulePendingRetry())
        .catch((): void => this.schedulePendingRetry());
    }, REKEY_RETRY_MS);
  }

  private completeRekey(): void {
    const pending = this.pendingHandshake;
    if (pending === null || pending.sessionKeys === null) {
      return;
    }
    if (this.handshake !== null && this.handshake.sessionKeys !== null) {
      this.prevSessionKeys = this.handshake.sessionKeys;
      this.prevLocalIndex = this.handshake.localIndex;
      this.prevExpiresMs = Date.now() + PREV_SESSION_GRACE_MS;
    }
    this.handshake = pending;
    this.pendingHandshake = null;
    this.pendingAttempts = 0;
    if (this.pendingTimer >= 0) {
      clearTimeout(this.pendingTimer);
      this.pendingTimer = -1;
    }
    this.lastTransportRxMs = Date.now();
    this.scheduleRekey();
    hilog.info(DOMAIN, TAG, 'WireGuard rekey complete, session rotated');
  }

  private startKeepalive(): void {
    if (this.keepaliveTimer >= 0) {
      clearInterval(this.keepaliveTimer);
    }
    this.keepaliveTimer = setInterval((): void => {
      if (!this.running || !this.handshakeComplete) {
        return;
      }
      const silentMs = Date.now() - this.lastTransportRxMs;
      if (this.pendingHandshake === null && silentMs > TUNNEL_STALL_MS) {
        hilog.warn(DOMAIN, TAG, 'WireGuard tunnel stalled for %{public}d ms, forcing rekey', silentMs);
        this.startRekey('stalled');
        return;
      }
      this.sendTransportPacket(new Uint8Array(0)).catch((): void => {
        // The next interval retries keepalive traffic.
      });
    }, KEEPALIVE_MS);
  }

  private fail(message: string): void {
    hilog.error(DOMAIN, TAG, 'VPN failed: %{public}s', message);
    writeVpnState(this.context, 'error', message);
    this.cleanup();
  }

  private cleanup(): void {
    this.running = false;
    this.handshakeComplete = false;
    if (this.handshakeTimer >= 0) {
      clearTimeout(this.handshakeTimer);
      this.handshakeTimer = -1;
    }
    if (this.keepaliveTimer >= 0) {
      clearInterval(this.keepaliveTimer);
      this.keepaliveTimer = -1;
    }
    if (this.rekeyTimer >= 0) {
      clearTimeout(this.rekeyTimer);
      this.rekeyTimer = -1;
    }
    if (this.pendingTimer >= 0) {
      clearTimeout(this.pendingTimer);
      this.pendingTimer = -1;
    }
    this.pendingHandshake = null;
    this.pendingAttempts = 0;
    this.prevSessionKeys = null;
    this.prevExpiresMs = 0;
    if (this.nativeSock) {
      this.nativeSock.close();
      this.nativeSock = null;
    }
    if (this.vpnConnection) {
      this.vpnConnection.destroy().catch((): void => {
        // Process teardown also closes the VPN interface.
      });
      this.vpnConnection = null;
    }
    this.tunFd = -1;
    this.handshake = null;
    this.natByFlow.clear();
    this.natByReply.clear();
    this.icmpByReply.clear();
    this.dnsByReply.clear();
    this.seenSourceIps.clear();
    this.natLogCount = 0;
    this.dnsServer = '';
    this.dnsLogCount = 0;
    this.seenFlows.clear();
    this.flowSeenCount = 0;
  }

  private errorMessage(error: Object): string {
    const businessError = error as BusinessError;
    return businessError.message ?? JSON.stringify(error);
  }
}

function validIPv4(value: string): boolean {
  const parts = value.split('.');
  if (parts.length !== 4) {
    return false;
  }
  return parts.every((part: string): boolean => {
    const number = Number(part);
    return Number.isInteger(number) && number >= 0 && number <= 255;
  });
}

function ipv4AtToString(packet: Uint8Array, offset: number): string {
  return `${packet[offset]}.${packet[offset + 1]}.${packet[offset + 2]}.${packet[offset + 3]}`;
}

function writeU64LE(buffer: Uint8Array, offset: number, value: bigint): void {
  for (let index = 0; index < 8; index++) {
    buffer[offset + index] = Number((value >> BigInt(index * 8)) & 0xFFn);
  }
}
