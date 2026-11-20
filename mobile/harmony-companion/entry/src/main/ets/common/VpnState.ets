import { common } from '@kit.AbilityKit';
import { fileIo as fs } from '@kit.CoreFileKit';
import { hilog } from '@kit.PerformanceAnalysisKit';

const STATE_FILE = 'vpn-state.json';
const ACTIVE_STATES: string[] = ['preparing', 'tun_ready', 'handshaking', 'connected'];

export function isVpnActiveState(state: string): boolean {
  return ACTIVE_STATES.includes(state);
}

export function writeVpnState(context: common.Context, state: string, detail: string): void {
  let file: fs.File | null = null;
  try {
    file = fs.openSync(
      `${context.filesDir}/${STATE_FILE}`,
      fs.OpenMode.READ_WRITE | fs.OpenMode.CREATE | fs.OpenMode.TRUNC
    );
    fs.writeSync(file.fd, JSON.stringify({ state: state, detail: detail }));
  } catch (error) {
    hilog.error(0x4103, 'ArchieLinkVPN', 'write VPN state failed: %{public}s', JSON.stringify(error));
  } finally {
    if (file !== null) {
      try {
        fs.closeSync(file);
      } catch (error) {
        hilog.error(0x4103, 'ArchieLinkVPN', 'close VPN state failed: %{public}s', JSON.stringify(error));
      }
    }
  }
}

export function readVpnState(context: common.Context): string[] {
  try {
    const raw = fs.readTextSync(`${context.filesDir}/${STATE_FILE}`);
    const value = JSON.parse(raw) as Record<string, string>;
    return [value.state, value.detail];
  } catch (error) {
    return ['idle', '尚未创建 HarmonyOS VPN 测试接口。'];
  }
}
