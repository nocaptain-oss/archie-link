import { common, wantAgent, WantAgent } from '@kit.AbilityKit';
import { BusinessError } from '@kit.BasicServicesKit';
import { backgroundTaskManager } from '@kit.BackgroundTasksKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { readVpnState, isVpnActiveState } from './VpnState';

const DOMAIN = 0x4103;
const TAG = 'ArchieLinkTask';
const RETRY_DELAY_MS = 5_000;
const MAX_RETRY = 3;

let started: boolean = false;
let taskContext: common.UIAbilityContext | null = null;
let cancelListenerRegistered: boolean = false;
let cancelRequeueTimes: number[] = [];
const MAX_CANCEL_REQUEUE_PER_HOUR = 20;

export function startVpnBackgroundTask(context: common.UIAbilityContext): void {
  taskContext = context;
  registerCancelListener();
  if (started) {
    return;
  }
  requestTask(0);
}

export function stopVpnBackgroundTask(context: common.UIAbilityContext): void {
  started = false;
  backgroundTaskManager.stopBackgroundRunning(context)
    .then((): void => {
      hilog.info(DOMAIN, TAG, 'VPN background task stopped');
    })
    .catch((error: BusinessError): void => {
      hilog.warn(DOMAIN, TAG, 'stop background task failed: %{public}s', error.message);
    });
}

/**
 * Called from the watchdog tick: re-request the keep-alive task when the VPN
 * is up but our bookkeeping says the task is gone (e.g. cancel callback was
 * delayed or lost).
 */
export function ensureVpnBackgroundTask(context: common.UIAbilityContext): void {
  if (started) {
    return;
  }
  const state = readVpnState(context)[0];
  if (!isVpnActiveState(state)) {
    return;
  }
  hilog.warn(DOMAIN, TAG, 'keep-alive task missing while VPN state=%{public}s, re-requesting', state);
  startVpnBackgroundTask(context);
}

/**
 * Periodic verification: query the system for live continuous tasks so a
 * stale `started` flag cannot leave the VPN unprotected.
 */
export async function verifyVpnBackgroundTask(context: common.UIAbilityContext): Promise<void> {
  const state = readVpnState(context)[0];
  if (!isVpnActiveState(state)) {
    return;
  }
  try {
    const tasks: backgroundTaskManager.ContinuousTaskInfo[] =
      await backgroundTaskManager.getAllContinuousTasks(context);
    if (tasks.length === 0) {
      hilog.warn(DOMAIN, TAG, 'keep-alive task verified missing while VPN state=%{public}s', state);
      started = false;
      startVpnBackgroundTask(context);
    }
  } catch (error) {
    const err = error as BusinessError;
    hilog.warn(DOMAIN, TAG, 'verify continuous task failed: %{public}s', err.message);
  }
}

function requestTask(attempt: number): void {
  const context = taskContext;
  if (context === null) {
    return;
  }
  const info: wantAgent.WantAgentInfo = {
    wants: [{
      bundleName: 'com.archielink.companion',
      abilityName: 'VpnAbility'
    }],
    actionType: wantAgent.OperationType.START_ABILITY,
    requestCode: 0,
    wantAgentFlags: [wantAgent.WantAgentFlags.UPDATE_PRESENT_FLAG]
  };
  try {
    wantAgent.getWantAgent(info)
      .then((agent: WantAgent): void => {
        backgroundTaskManager.startBackgroundRunning(
          context,
          backgroundTaskManager.BackgroundMode.DATA_TRANSFER,
          agent
        ).then((): void => {
          started = true;
          hilog.info(DOMAIN, TAG, 'VPN background task started');
        }).catch((error: BusinessError): void => {
          hilog.error(DOMAIN, TAG, 'start background task failed (attempt %{public}d): %{public}s',
            attempt, error.message);
          scheduleRetry(attempt);
        });
      })
      .catch((error: BusinessError): void => {
        hilog.error(DOMAIN, TAG, 'create background want agent failed: %{public}s', error.message);
        scheduleRetry(attempt);
      });
  } catch (error) {
    hilog.error(DOMAIN, TAG, 'background task setup failed: %{public}s', String(error));
    scheduleRetry(attempt);
  }
}

function scheduleRetry(attempt: number): void {
  if (attempt >= MAX_RETRY) {
    return;
  }
  setTimeout((): void => {
    requestTask(attempt + 1);
  }, RETRY_DELAY_MS);
}

function registerCancelListener(): void {
  if (cancelListenerRegistered) {
    return;
  }
  cancelListenerRegistered = true;
  try {
    backgroundTaskManager.on(
      'continuousTaskCancel',
      (info: backgroundTaskManager.ContinuousTaskCancelInfo): void => {
        hilog.warn(DOMAIN, TAG,
          'continuous task cancelled (id=%{public}d, reason=%{public}d)',
          info.id, info.reason);
        started = false;
        const context = taskContext;
        if (context === null) {
          return;
        }
        const state = readVpnState(context)[0];
        if (!isVpnActiveState(state)) {
          hilog.info(DOMAIN, TAG,
            'VPN state=%{public}s inactive, keeping continuous task cancelled', state);
          return;
        }
        if (!allowCancelRequeue()) {
          hilog.warn(DOMAIN, TAG, 'cancel re-request rate limit reached, giving up keep-alive re-request');
          return;
        }
        requestTask(0);
      }
    );
  } catch (error) {
    hilog.error(DOMAIN, TAG, 'register cancel listener failed: %{public}s', String(error));
  }
}

function allowCancelRequeue(): boolean {
  const now = Date.now();
  cancelRequeueTimes = cancelRequeueTimes.filter((t: number): boolean => now - t < 3_600_000);
  if (cancelRequeueTimes.length >= MAX_CANCEL_REQUEUE_PER_HOUR) {
    return false;
  }
  cancelRequeueTimes.push(now);
  return true;
}
