/**
 * WireGuard 配置文件解析器
 *
 * 支持解析标准 .conf 文件格式：
 *   [Interface] → Address, DNS, PrivateKey
 *   [Peer]      → Endpoint (host:port), PublicKey, AllowedIPs
 *   # 和 ; 开头的行是注释，跳过
 */

export interface WgParsedConfig {
  /** Endpoint 中的主机（IP 或域名） */
  serverHost: string;
  /** Endpoint 中的端口号 */
  serverPort: string;
  /** Interface 的 Address（取第一个 IP） */
  clientAddress: string;
  /** DNS 服务器（取第一个） */
  dnsServer: string;
  /** 完整的客户端私有 IP（含掩码） */
  clientAddressFull: string;
  /** 客户端私有 IP 前缀长度 */
  clientPrefixLength: number;
  /** PrivateKey */
  privateKey?: string;
  /** Peer PublicKey */
  publicKey?: string;
  /** AllowedIPs */
  allowedIPs?: string;
  /** PresharedKey */
  presharedKey?: string;
  /** Interface MTU（68-1500 内有效，未配置时缺省） */
  mtu?: number;
  /** 原始解析错误 */
  error?: string;
}

/**
 * 判断一行是否为「裸 WireGuard 密钥」（43 个 base64 字符 + 1 个补位 '='，即解码后 32 字节）
 */
function isBareWireGuardKeyToken(line: string): boolean {
  return /^[A-Za-z0-9+/]{43}=$/.test(line);
}

/**
 * 宽容处理常见复制粘贴破损：PrivateKey=/PublicKey=/PresharedKey= 值为空、
 * 紧随其后的行是裸 base64 密钥时，合并回一行（官方格式密钥与值同行）。
 * 兼容笔记类应用渲染/复制引入的换行（AGC 审核实测场景）。
 */
function joinSplitKeyLines(lines: string[]): string[] {
  const merged: string[] = [];
  for (let i = 0; i < lines.length; i++) {
    const line: string = lines[i].trim();
    const eqIdx: number = line.indexOf('=');
    if (eqIdx !== -1 && line.substring(eqIdx + 1).trim() === '') {
      const key: string = line.substring(0, eqIdx).trim().toLowerCase();
      if (key === 'privatekey' || key === 'publickey' || key === 'presharedkey') {
        if (i + 1 < lines.length && isBareWireGuardKeyToken(lines[i + 1].trim())) {
          merged.push(line + lines[i + 1].trim());
          i++;
          continue;
        }
      }
    }
    merged.push(lines[i]);
  }
  return merged;
}

/**
 * 校验 WireGuard 密钥格式（43 个 base64 字符 + '='，base64 解码后为 32 字节）
 */
function isValidWireGuardKey(value?: string): boolean {
  if (!value) {
    return false;
  }
  return isBareWireGuardKeyToken(value);
}

/**
 * 校验 DNS 服务器：IPv4 点分十进制（0-255）或含 ':' 的 IPv6 字面量
 */
function isValidDnsServer(value: string): boolean {
  if (value.includes(':')) {
    return true;
  }
  const parts: string[] = value.split('.');
  if (parts.length !== 4) {
    return false;
  }
  for (const part of parts) {
    if (!/^\d+$/.test(part)) {
      return false;
    }
    const num: number = parseInt(part, 10);
    if (num < 0 || num > 255) {
      return false;
    }
  }
  return true;
}

/**
 * 解析 WireGuard .conf 文件内容
 * @param content 文件文本内容
 * @returns 解析结果
 */
export function parseWgConfig(content: string): WgParsedConfig {
  const result: WgParsedConfig = {
    serverHost: '',
    serverPort: '51820',
    clientAddress: '',
    clientPrefixLength: 32,
    dnsServer: '',
    clientAddressFull: '',
  };

  const lines: string[] = joinSplitKeyLines(content.split(/\r?\n/));
  let currentSection: string = '';

  for (const rawLine of lines) {
    const line: string = rawLine.trim();

    // 跳过空行和注释
    if (line === '' || line.startsWith('#') || line.startsWith(';')) {
      continue;
    }

    // 章节标记 [Interface] / [Peer] / ...
    if (line.startsWith('[') && line.endsWith(']')) {
      currentSection = line.slice(1, -1).trim().toLowerCase();
      continue;
    }

    // 解析 key = value
    const eqIdx: number = line.indexOf('=');
    if (eqIdx === -1) {
      continue;
    }
    const key: string = line.substring(0, eqIdx).trim().toLowerCase();
    const value: string = line.substring(eqIdx + 1).trim();

    if (currentSection === 'interface') {
      switch (key) {
        case 'address': {
          result.clientAddressFull = value;
          // 取第一个 IP，去掉掩码
          const firstAddress: string = value.split(',')[0].trim();
          const firstParts: string[] = firstAddress.split('/');
          const firstIp: string = firstParts[0];
          result.clientAddress = firstIp;
          if (firstParts.length > 1) {
            const prefix: number = parseInt(firstParts[1], 10);
            if (prefix >= 0 && prefix <= 32) {
              result.clientPrefixLength = prefix;
            }
          }
          break;
        }
        case 'dns': {
          // 取第一个 DNS
          result.dnsServer = value.split(',')[0].trim();
          break;
        }
        case 'privatekey':
          result.privateKey = value;
          break;
        case 'mtu': {
          const parsedMtu = parseInt(value, 10);
          if (parsedMtu >= 68 && parsedMtu <= 1500) {
            result.mtu = parsedMtu;
          }
          break;
        }
      }
    } else if (currentSection === 'peer') {
      switch (key) {
        case 'endpoint': {
          // endpoint = host:port 或 [host]:port
          const ep: string = value.trim();
          const lastColon: number = ep.lastIndexOf(':');
          if (lastColon === -1) {
            result.serverHost = ep;
            result.serverPort = '51820';
          } else {
            // 检测 IPv6 格式 [::1]:port
            if (ep.startsWith('[')) {
              const closeBracket: number = ep.indexOf(']');
              if (closeBracket !== -1) {
                result.serverHost = ep.substring(0, closeBracket + 1);
                result.serverPort = ep.substring(closeBracket + 2); // skip "]:"
              }
            } else {
              result.serverHost = ep.substring(0, lastColon);
              result.serverPort = ep.substring(lastColon + 1);
            }
          }
          break;
        }
        case 'publickey':
          result.publicKey = value;
          break;
        case 'presharedkey':
          result.presharedKey = value;
          break;
        case 'allowedips':
          result.allowedIPs = value;
          break;
        case 'persistentkeepalive':
          break;
      }
    }
  }

  // 验证结果
  if (!result.serverHost) {
    result.error = '配置文件中未找到 [Peer] Endpoint';
  }
  if (!result.clientAddress) {
    result.error = (result.error ? result.error + '；' : '') + '配置文件中未找到 [Interface] Address';
  }
  if (!isValidWireGuardKey(result.privateKey)) {
    result.error = (result.error ? result.error + '；' : '') +
      '未找到有效的 [Interface] PrivateKey（密钥与值必须在同一行，如 PrivateKey = xxxx，请检查内容是否复制完整）';
  }
  if (!isValidWireGuardKey(result.publicKey)) {
    result.error = (result.error ? result.error + '；' : '') +
      '未找到有效的 [Peer] PublicKey（密钥与值必须在同一行，如 PublicKey = xxxx，请检查内容是否复制完整）';
  }
  if (!/^\d+$/.test(result.serverPort) ||
    parseInt(result.serverPort, 10) < 1 || parseInt(result.serverPort, 10) > 65535) {
    result.error = (result.error ? result.error + '；' : '') + '[Peer] Endpoint 端口无效（须为 1-65535）';
  }
  if (result.dnsServer && !isValidDnsServer(result.dnsServer)) {
    // DNS 非法则回退默认 DNS（VpnAbility 对空值回退 DEFAULT_DNS）
    result.dnsServer = '';
  }
  if (result.presharedKey && !isValidWireGuardKey(result.presharedKey)) {
    // 预共享密钥非法则按未配置处理（VpnAbility 对空值跳过 setPresharedKey）
    result.presharedKey = '';
  }

  return result;
}
