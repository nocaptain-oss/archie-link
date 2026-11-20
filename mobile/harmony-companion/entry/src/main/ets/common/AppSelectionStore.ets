import { common } from '@kit.AbilityKit';
import { fileIo as fs } from '@kit.CoreFileKit';
import { hilog } from '@kit.PerformanceAnalysisKit';

const STORE_FILE = 'vpn-apps.json';
const DISABLED_FILE = 'vpn-disabled-apps.json';
const SELF_BUNDLE = 'com.archielink.companion';

export function readSelectedApps(context: common.Context): string[] {
  return readBundleList(context, STORE_FILE);
}

export function writeSelectedApps(context: common.Context, apps: string[]): void {
  writeBundleList(context, STORE_FILE, apps);
}

export function readDisabledApps(context: common.Context): string[] {
  return readBundleList(context, DISABLED_FILE);
}

export function writeDisabledApps(context: common.Context, apps: string[]): void {
  writeBundleList(context, DISABLED_FILE, apps);
}

export function readEnabledApps(context: common.Context): string[] {
  const disabled = new Set(readDisabledApps(context));
  return readSelectedApps(context).filter((bundle: string): boolean => !disabled.has(bundle));
}

function readBundleList(context: common.Context, fileName: string): string[] {
  try {
    const raw = fs.readTextSync(`${context.filesDir}/${fileName}`);
    const value = JSON.parse(raw) as string[];
    return value.filter((item: string): boolean =>
      item.length > 0 && item !== SELF_BUNDLE
    );
  } catch (_error) {
    return [];
  }
}

function writeBundleList(context: common.Context, fileName: string, apps: string[]): void {
  const unique = Array.from(new Set(
    apps.filter((item: string): boolean =>
      item.length > 0 && item !== SELF_BUNDLE
    )
  )).sort();
  let file: fs.File | null = null;
  try {
    file = fs.openSync(
      `${context.filesDir}/${fileName}`,
      fs.OpenMode.READ_WRITE | fs.OpenMode.CREATE | fs.OpenMode.TRUNC
    );
    fs.writeSync(file.fd, JSON.stringify(unique));
  } catch (error) {
    hilog.error(0x4103, 'ArchieLinkVPN', 'write app selection failed: %{public}s', JSON.stringify(error));
  } finally {
    if (file !== null) {
      try {
        fs.closeSync(file);
      } catch (_error) {
        // Closing the app-selection file is best effort.
      }
    }
  }
}
