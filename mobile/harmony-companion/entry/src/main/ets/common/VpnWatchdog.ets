import { Want, appManager, common } from '@kit.AbilityKit';
import { fileIo } from '@kit.CoreFileKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { connection, vpnExtension } from '@kit.NetworkKit';
import { readVpnState, isVpnActiveState } from './VpnState';
import { parseWgConfig } from '../utils/WgConfigParser';
import { ensureVpnBackgroundTask, verifyVpnBackgroundTask, stopVpnBackgroundTask } from './LongTermTaskModel';

const DOMAIN = 0x4103;
const TAG = 'ArchieLinkVPN';
const VPN_PROCESS_NAME = 'com.archielink.companion:vpn';
const SELF_BUNDLE = 'com.archielink.companion';
const CONFIG_FILE = 'wireguard.conf';
const POLL_INTERVAL_MS = 5_000;
const RESTART_COOLDOWN_MS = 15_000;
const VERIFY_INTERVAL_TICKS = 6;

let timerId: number = -1;
let lastRestartMs: number = 0;
let tickCount: number = 0;
let watchdogContext: common.UIAbilityContext | null = null;

export function startVpnWatchdog(context: common.UIAbilityContext): void {
  watchdogContext = context;
  if (timerId >= 0) {
    return;
  }
  timerId = setInterval((): void => {
    tick();
  }, POLL_INTERVAL_MS);
  hilog.info(DOMAIN, TAG, 'VPN watchdog started');
}

export function stopVpnWatchdog(): void {
  if (timerId >= 0) {
    clearInterval(timerId);
    timerId = -1;
    hilog.info(DOMAIN, TAG, 'VPN watchdog stopped');
  }
  watchdogContext = null;
}

async function tick(): Promise<void> {
  const context = watchdogContext;
  if (context === null) {
    return;
  }
  const state = readVpnState(context)[0];
  if (!isVpnActiveState(state)) {
    hilog.info(DOMAIN, TAG, 'watchdog: VPN state=%{public}s inactive, taking down task and stopping', state);
    stopVpnBackgroundTask(context);
    stopVpnWatchdog();
    return;
  }
  ensureVpnBackgroundTask(context);
  tickCount++;
  if (tickCount % VERIFY_INTERVAL_TICKS === 0) {
    verifyVpnBackgroundTask(context).catch((): void => {
      // Verification failures are logged inside verifyVpnBackgroundTask.
    });
  }
  let vpnAlive = false;
  try {
    const infos: appManager.ProcessInformation[] = await appManager.getRunningProcessInformation();
    for (const info of infos) {
      if (info.processName === VPN_PROCESS_NAME) {
        vpnAlive = true;
        break;
      }
    }
  } catch (error) {
    hilog.warn(DOMAIN, TAG, 'watchdog process check failed: %{public}s', JSON.stringify(error));
    return;
  }
  if (vpnAlive) {
    return;
  }
  if (Date.now() - lastRestartMs < RESTART_COOLDOWN_MS) {
    return;
  }
  hilog.warn(
    DOMAIN,
    TAG,
    'watchdog: VPN extension dead while state=%{public}s, restarting',
    state
  );
  lastRestartMs = Date.now();
  await restartVpnExtension(context);
}

async function restartVpnExtension(context: common.UIAbilityContext): Promise<void> {
  try {
    const raw = fileIo.readTextSync(`${context.filesDir}/${CONFIG_FILE}`);
    const config = parseWgConfig(raw);
    if (config.error) {
      hilog.error(DOMAIN, TAG, 'watchdog: stored config invalid: %{public}s', config.error);
      return;
    }
    const serverIP = await resolveServer(config.serverHost);
    const want: Want = {
      deviceId: '',
      bundleName: SELF_BUNDLE,
      abilityName: 'VpnAbility',
      parameters: {
        serverIP: serverIP,
        serverPort: Number(config.serverPort),
        clientAddress: config.clientAddress,
        clientPrefixLength: config.clientPrefixLength,
        dnsServer: config.dnsServer,
        privateKey: config.privateKey ?? '',
        publicKey: config.publicKey ?? '',
        presharedKey: config.presharedKey ?? '',
        allowedIPs: config.allowedIPs ?? '0.0.0.0/0',
        tunnelMtu: config.mtu ?? 0
      }
    };
    await vpnExtension.startVpnExtensionAbility(want);
    hilog.info(DOMAIN, TAG, 'watchdog: VPN extension restarted');
  } catch (error) {
    hilog.error(DOMAIN, TAG, 'watchdog restart failed: %{public}s', JSON.stringify(error));
  }
}

async function resolveServer(host: string): Promise<string> {
  if (/^[0-9.]+$/.test(host)) {
    return host;
  }
  const addresses: connection.NetAddress[] = await connection.getAddressesByName(host);
  for (const address of addresses) {
    if (!address.address.includes(':')) {
      return address.address;
    }
  }
  throw new Error('WireGuard 端点没有解析到 IPv4 地址');
}
