import { bundleManager, common, Want } from '@kit.AbilityKit';
import { display } from '@kit.ArkUI';
import { BusinessError } from '@kit.BasicServicesKit';
import { fileIo, picker } from '@kit.CoreFileKit';
import { hilog } from '@kit.PerformanceAnalysisKit';
import { connection, http, vpnExtension } from '@kit.NetworkKit';
import {
  readDisabledApps,
  readSelectedApps,
  writeDisabledApps,
  writeSelectedApps
} from '../common/AppSelectionStore';
import { startVpnBackgroundTask, stopVpnBackgroundTask } from '../common/LongTermTaskModel';
import { startVpnWatchdog, stopVpnWatchdog } from '../common/VpnWatchdog';
import { readVpnState, writeVpnState } from '../common/VpnState';
import { parseWgConfig, WgParsedConfig } from '../utils/WgConfigParser';

const VPN_WANT: Want = {
  deviceId: '',
  bundleName: 'com.archielink.companion',
  abilityName: 'VpnAbility'
};
const SELF_BUNDLE = 'com.archielink.companion';
const DELETE_THRESHOLD = -52;
const CONFIG_FILE = 'wireguard.conf';
const CONTAINER_BUNDLES: string[] = [
  'com.droi.tong',
  'com.easy.hmos.abroad',
  'com.gspace.android'
];

interface GeoInfo {
  success?: boolean;
  city?: string;
  country?: string;
  country_name?: string;
}

interface AppCatalogItem {
  bundle: string;
  label: string;
}

const COMMON_APPS: AppCatalogItem[] = [
  { bundle: 'com.huawei.shell_assistant', label: '应用容器（出境易/卓易通等容器内应用的外网流量）' },
  { bundle: 'com.huawei.hmos.browser', label: '浏览器' }
];

@Entry
@Component
struct Index {
  @State page: string = 'main';
  @State status: string = 'stopped';
  @State detail: string = '';
  @State configured: boolean = false;
  @State locationText: string = '正在获取位置';
  @State selectedApps: string[] = [];
  @State disabledApps: string[] = [];
  @State draggingApp: string = '';
  @State dragOffset: number = 0;
  @State swipeLocked: boolean = false;
  @State deletingApp: string = '';
  @State deletingCollapsed: boolean = false;
  @State addAppVisible: boolean = false;
  @State splashVisible: boolean = true;
  @State splashOpacity: number = 1;
  @State showManualAdd: boolean = false;
  @State newBundleName: string = '';
  @State addAppError: string = '';
  @State glowOpacity: number = 0.055;
  @State glowScale: number = 0.93;
  @State haloScale: number = 0.90;
  @State haloOpacity: number = 0.75;
  @State orbitScale: number = 0.96;
  @State orbitAngle: number = 0;
  @State connectPressed: boolean = false;
  @State pendingSwitch: string = '';
  private refreshTimer: number = -1;
  private wideLayout: boolean = false;
  private settingsScroller: Scroller = new Scroller();
  private swipeUnlockTimer: number = -1;
  private observedVpnState: string = '';
  private locationGeneration: number = 0;
  private serverHost: string = '';
  private serverPort: number = 0;
  private clientAddress: string = '';
  private clientPrefixLength: number = 32;
  private dnsServer: string = '';
  private privateKey: string = '';
  private publicKey: string = '';
  private presharedKey: string = '';
  private allowedIPs: string = '0.0.0.0/0';

  aboutToAppear(): void {
    try {
      this.getUIContext().getFont().registerFont({
        familyName: 'WangLianRounded',
        familySrc: $rawfile('wanglian_rounded.otf')
      });
      this.getUIContext().getFont().registerFont({
        familyName: 'WangLianSerif',
        familySrc: $rawfile('wanglian_serif.ttf')
      });
    } catch (error) {
      hilog.warn(0x04103, 'ArchieLinkUI', `register brand font failed: ${JSON.stringify(error)}`);
    }
    this.wideLayout = this.diagonalInches() >= 8.5;
    this.loadStoredConfig();
    this.reloadApps();
    this.refreshStatus();
    this.refreshTimer = setInterval((): void => {
      this.refreshStatus();
    }, 1000);
    setTimeout((): void => {
      this.getUIContext().animateTo({ duration: 250, curve: Curve.EaseOut }, (): void => {
        this.splashOpacity = 0;
      });
      setTimeout((): void => {
        this.splashVisible = false;
      }, 280);
    }, 800);
  }

  aboutToDisappear(): void {
    if (this.refreshTimer >= 0) {
      clearInterval(this.refreshTimer);
      this.refreshTimer = -1;
    }
    if (this.swipeUnlockTimer >= 0) {
      clearTimeout(this.swipeUnlockTimer);
      this.swipeUnlockTimer = -1;
    }
  }

  private diagonalInches(): number {
    try {
      const d = display.getDefaultDisplaySync();
      const widthInch = d.width / d.densityDPI;
      const heightInch = d.height / d.densityDPI;
      return Math.sqrt(widthInch * widthInch + heightInch * heightInch);
    } catch (_error) {
      return -1;
    }
  }

  private transitionDuration(): number {
    const diagonal = this.diagonalInches();
    if (diagonal < 0) {
      return 550;
    }
    if (diagonal < 8.5) {
      return 450;
    }
    if (diagonal <= 12) {
      return 550;
    }
    return 650;
  }

  private goToPage(target: string): void {
    if (this.page === target) {
      return;
    }
    this.getUIContext().animateTo(
      { duration: this.transitionDuration(), curve: Curve.FastOutSlowIn },
      (): void => {
        this.page = target;
      }
    );
  }

  build() {
    Stack({ alignContent: Alignment.Center }) {
      if (this.page === 'settings') {
        this.settingsPage()
      } else {
        this.mainPage()
      }
      if (this.addAppVisible) {
        this.addAppDialog()
      }
      if (this.splashVisible) {
        this.splashPage()
      }
    }
    .width('100%')
    .height('100%')
    .backgroundColor('#ffffff')
  }

  @Builder
  splashPage() {
    Stack({ alignContent: Alignment.Center }) {
      Column() {
        Image($r('app.media.archielink_icon'))
          .width(125)
          .height(125)
          .objectFit(ImageFit.Contain)
          .borderRadius(62.5)
          .margin({ bottom: 30 })
          .translate({ y: -80 })

        Row({ space: 13 }) {
          Text('汪连')
            .fontSize(38)
            .fontWeight(FontWeight.Medium)
            .fontFamily('WangLianRounded')
            .fontColor('#7e868e')
            .letterSpacing(2)
          Text('ArchieLink')
            .fontSize(38)
            .fontWeight(FontWeight.Medium)
            .fontFamily('WangLianRounded')
            .fontColor('#a8afb6')
            .letterSpacing(0.5)
        }
        .alignItems(VerticalAlign.Bottom)
        .translate({ y: -80 })

        Text('外网访问专家')
          .fontSize(24)
          .fontWeight(FontWeight.Medium)
          .fontFamily('WangLianSerif')
          .fontColor('#7e868e')
          .letterSpacing(11)
          .margin({ top: 30 })
          .translate({ x: 5.5, y: -85 })
      }
      .alignItems(HorizontalAlign.Center)
      .translate({ y: -30 })
    }
    .width('100%')
    .height('100%')
    .backgroundColor('#ffffff')
    .opacity(this.splashOpacity)
  }

  @Builder
  mainPage() {
    Stack({ alignContent: Alignment.TopStart }) {
      Column()
        .width('100%')
        .height('100%')
        .backgroundColor('#ffffff')

      Column() {
        Row() {
          Column({ space: 5 }) {
            Text('汪连')
              .fontSize(31)
              .fontWeight(FontWeight.Medium)
              .fontFamily('WangLianRounded')
              .fontColor('#848b93')
              .letterSpacing(-1)
            Text('ArchieLink')
              .fontSize(11)
              .fontWeight(FontWeight.Medium)
              .fontColor('#a3aab1')
              .letterSpacing(1.3)
          }
          .alignItems(HorizontalAlign.Start)

          Blank()

          Button({ type: ButtonType.Circle }) {
            Image($r('app.media.settings_symbol'))
              .width(39)
              .height(39)
              .objectFit(ImageFit.Contain)
          }
          .width(46)
          .height(46)
          .padding(0)
          .backgroundColor(Color.Transparent)
          .onClick((): void => {
            this.goToPage('settings');
          })
        }
        .width('100%')
        .height(56)
        .alignItems(VerticalAlign.Top)

        Column() {
          Stack({ alignContent: Alignment.Center }) {
            Column()
              .width(370)
              .height(370)
              .borderRadius(185)
              .radialGradient({
                center: [185, 185],
                radius: 185,
                colors: [
                  [this.signalColor(1), 0],
                  [this.signalColor(0.85), 0.18],
                  [this.signalColor(0.55), 0.38],
                  [this.signalColor(0.3), 0.55],
                  [this.signalColor(0.14), 0.7],
                  [this.signalColor(0.05), 0.84],
                  [Color.Transparent, 1]
                ]
              })
              .opacity(this.glowOpacity)
              .scale({ x: this.glowScale, y: this.glowScale })
            Circle()
              .width(238)
              .height(238)
              .fill(Color.Transparent)
              .stroke(this.signalColor(0.14))
              .strokeWidth(1)
              .strokeDashArray([74.8, 299.1, 74.8, 299.1])
              .rotate({ angle: this.orbitAngle + 18 })
              .scale({ x: this.orbitScale, y: this.orbitScale })
            Column()
              .width(205)
              .height(205)
              .borderRadius(103)
              .backgroundColor(this.signalColor(0.035))
              .opacity(this.haloOpacity)
              .scale({ x: this.haloScale, y: this.haloScale })
            Button('', { type: ButtonType.Circle })
              .width(164)
              .height(164)
              .borderRadius(82)
              .backgroundColor('#f8fbfa')
              .border({ width: 1, color: this.signalColor(0.30) })
              .shadow({
                radius: 34,
                color: this.signalColor(0.12),
                offsetX: 0,
                offsetY: 16
              })
              .stateEffect(false)
              .onTouch((event: TouchEvent): void => {
                if (event.type === TouchType.Down) {
                  this.connectPressed = true;
                } else if (event.type === TouchType.Up || event.type === TouchType.Cancel) {
                  this.connectPressed = false;
                }
              })
              .onClick((): void => {
                this.toggleConnection();
              })
            Column()
              .width(164)
              .height(164)
              .borderRadius(82)
              .radialGradient({
                center: [82, 82],
                radius: 82,
                colors: [
                  [withAlpha(this.pressTintColor(), 0.22), 0],
                  [withAlpha(this.pressTintColor(), 0.08), 0.55],
                  [Color.Transparent, 0.85]
                ]
              })
              .hitTestBehavior(HitTestMode.None)
              .opacity(this.connectPressed || this.pendingSwitch !== '' ? 1 : 0)
              .animation({ duration: 220, curve: Curve.EaseOut })
          }
          .width('100%')
          .height(270)
          .margin({ top: 110 })
          .onAppear((): void => {
            this.startIdleAnimations();
          })

          Text(this.locationText)
            .width('100%')
            .fontSize(16.5)
            .fontWeight(FontWeight.Medium)
            .fontColor('#63716e')
            .textAlign(TextAlign.Center)
            .maxLines(2)
            .lineHeight(22)
            .margin({ top: 34 })
        }
        .width('100%')
        .layoutWeight(1)
      }
      .width('100%')
      .height('100%')
      .padding({ left: 28, right: 28, top: 28, bottom: 34 })
    }
    .width('100%')
    .height('100%')
    .backgroundColor('#ffffff')
    .transition(TransitionEffect.OPACITY)
  }

  @Builder
  settingsPage() {
    Column() {
      Row({ space: 7 }) {
        Button({ type: ButtonType.Circle }) {
          Stack({ alignContent: Alignment.Center }) {
            Row()
              .width(13)
              .height(1.7)
              .backgroundColor('#5f706b')
              .borderRadius(2)
              .rotate({ angle: -45 })
              .translate({ x: -2, y: -4 })
            Row()
              .width(13)
              .height(1.7)
              .backgroundColor('#5f706b')
              .borderRadius(2)
              .rotate({ angle: 45 })
              .translate({ x: -2, y: 4 })
          }
          .width(20)
          .height(20)
        }
          .width(40)
          .height(40)
          .padding(0)
          .backgroundColor(Color.Transparent)
          .onClick((): void => {
            this.goToPage('main');
          })
        Text('设置')
          .fontSize(20)
          .fontWeight(FontWeight.Medium)
          .fontColor('#5f706b')
          .letterSpacing(-0.3)
        Blank()
      }
      .width('100%')
      .height(44)
      .alignItems(VerticalAlign.Center)

      Scroll(this.settingsScroller) {
        Column({ space: 24 }) {
          Column() {
            Row() {
              Text('连接配置')
                .fontSize(13)
                .fontWeight(FontWeight.Medium)
                .fontColor('#344c46')
              Blank()
            }
            .width('100%')
            .height(44)
            .padding({ left: 16, right: 16 })
            .backgroundColor('#dfeae5')
            .borderRadius({ topLeft: 21, topRight: 21 })
            .border({ width: { bottom: 1 }, color: '#cad8d2' })

            Row({ space: 16 }) {
              Column({ space: 5 }) {
                Text('WireGuard')
                  .fontSize(16.5)
                  .fontWeight(FontWeight.Medium)
                  .fontColor('#0a1513')
                Text(this.configured ? '配置已就绪，可更换配置文件' : '导入或更换配置文件')
                  .fontSize(11.5)
                  .fontColor('#63716e')
              }
              .alignItems(HorizontalAlign.Start)
              .layoutWeight(1)

              Button('导入配置')
                .height(32)
                .padding({ left: 13, right: 13 })
                .fontSize(12.5)
                .fontWeight(FontWeight.Medium)
                .fontColor('#0a1513')
                .backgroundColor('#ffffff')
                .border({ width: 1, color: '#d3ded9' })
                .borderRadius(12)
                .onClick((): void => {
                  this.importConfig();
                })
            }
            .width('100%')
            .height(78)
            .padding({ left: 18, right: 18 })
          }
          .width('100%')
          .backgroundColor(this.wideLayout ? '#f2f7f4' : '#fbfdfc')
          .border({ width: 1, color: this.wideLayout ? '#ccdcd5' : '#dce6e2' })
          .borderRadius(22)

          Column() {
            Row() {
              Text('已加入清单')
                .fontSize(13)
                .fontWeight(FontWeight.Medium)
                .fontColor('#344c46')
              Blank()
              Button('添加应用')
                .height(30)
                .padding({ left: 12, right: 12 })
                .fontSize(12.5)
                .fontWeight(FontWeight.Medium)
                .fontColor('#0a1513')
                .backgroundColor('#ffffff')
                .border({ width: 1, color: '#d3ded9' })
                .borderRadius(10)
                .onClick((): void => {
                  this.settingsScroller.scrollTo({ xOffset: 0, yOffset: 0 });
                  this.addAppVisible = true;
                  this.showManualAdd = false;
                  this.addAppError = '';
                })
            }
            .width('100%')
            .height(44)
            .padding({ left: 16, right: 12 })
            .backgroundColor('#dfeae5')
            .borderRadius({ topLeft: 21, topRight: 21 })
            .border({ width: { bottom: 1 }, color: '#cad8d2' })

            if (this.usesContainerMode()) {
              Text('容器兼容模式：连接后全部应用经 VPN')
                .width('100%')
                .padding({ left: 16, right: 16, top: 10, bottom: 10 })
                .fontSize(11)
                .fontColor('#876a3c')
                .backgroundColor('#fbf6ea')
            }

            if (this.selectedApps.length === 0) {
              Text('还没有应用，点击“添加应用”加入清单')
                .width('100%')
                .height(92)
                .fontSize(13)
                .fontColor('#7a8985')
                .textAlign(TextAlign.Center)
            } else {
              ForEach(this.selectedApps, (bundle: string, index: number) => {
                this.appRow(bundle, index === this.selectedApps.length - 1)
              }, (bundle: string): string => bundle)
            }
          }
          .width('100%')
          .backgroundColor(this.wideLayout ? '#f2f7f4' : '#fbfdfc')
          .border({ width: 1, color: this.wideLayout ? '#ccdcd5' : '#dce6e2' })
          .borderRadius(22)
          .clip(false)
          .layoutWeight(this.wideLayout ? 1 : 0)
        }
        .width('100%')
        .constraintSize({ minHeight: this.wideLayout ? '100%' : 0 })
        .justifyContent(FlexAlign.Start)
        .alignItems(HorizontalAlign.Start)
        .clip(false)
        .padding({ top: 16, bottom: 36 })
      }
      .layoutWeight(1)
      .scrollBar(BarState.Off)
      .align(Alignment.TopStart)
      .clip(false)
    }
    .width('100%')
    .height('100%')
    .padding({ left: 24, right: 24, top: 24 })
    .backgroundColor('#ffffff')
    .transition(TransitionEffect.OPACITY.combine(TransitionEffect.translate({ x: 40 })))
  }

  @Builder
  appRow(bundle: string, isLast: boolean) {
    Column() {
      Row({ space: 14 }) {
        Text(appLabel(bundle))
          .fontSize(16.5)
          .fontWeight(FontWeight.Medium)
          .fontColor('#0a1513')
          .maxLines(1)
          .textOverflow({ overflow: TextOverflow.Ellipsis })
          .layoutWeight(1)
        Toggle({ type: ToggleType.Switch, isOn: !this.disabledApps.includes(bundle) })
          .selectedColor('#0ebd7d')
          .onChange((enabled: boolean): void => {
            this.setAppEnabled(bundle, enabled);
          })
      }
      .width('100%')
      .height(66)
      .padding({ left: 18, right: 10 })
      .backgroundColor('#fbfdfc')
      .borderRadius(isLast ? { bottomLeft: 21, bottomRight: 21 } : undefined)
      .border({ width: { bottom: isLast ? 0 : 1 }, color: '#dce6e2' })
      .shadow(this.draggingApp === bundle || this.deletingApp === bundle
        ? { radius: 20, color: '#240a1513', offsetX: -8, offsetY: 9 }
        : { radius: 0, color: '#00000000' })
      .offset({
        x: this.draggingApp === bundle || this.deletingApp === bundle ? this.dragOffset : 0
      })
      .gesture(
        PanGesture({ direction: PanDirection.Horizontal, distance: 1 })
          .onActionStart((): void => {
            if (this.swipeLocked || this.deletingApp.length > 0) {
              return;
            }
            this.draggingApp = bundle;
            this.dragOffset = 0;
          })
          .onActionUpdate((event: GestureEvent): void => {
            if (this.swipeLocked || this.deletingApp.length > 0) {
              return;
            }
            this.draggingApp = bundle;
            this.dragOffset = Math.min(0, event.offsetX);
            if (this.dragOffset <= DELETE_THRESHOLD) {
              this.deleteApp(bundle);
            }
          })
          .onActionEnd((): void => {
            if (this.swipeLocked) {
              this.unlockSwipeSoon();
              return;
            }
            if (this.deletingApp.length > 0) {
              return;
            }
            this.getUIContext().animateTo({ duration: 180, curve: Curve.FastOutSlowIn }, (): void => {
              this.dragOffset = 0;
            });
            setTimeout((): void => {
              if (this.deletingApp.length === 0) {
                this.draggingApp = '';
              }
            }, 190);
          })
          .onActionCancel((): void => {
            if (this.swipeLocked) {
              this.unlockSwipeSoon();
              return;
            }
            if (this.deletingApp.length > 0) {
              return;
            }
            this.getUIContext().animateTo({ duration: 180, curve: Curve.FastOutSlowIn }, (): void => {
              this.dragOffset = 0;
            });
            setTimeout((): void => {
              if (this.deletingApp.length === 0) {
                this.draggingApp = '';
              }
            }, 190);
          })
      )
    }
    .width('100%')
    .height(this.deletingApp === bundle && this.deletingCollapsed ? 0 : 66)
    .clip(this.deletingApp === bundle && this.deletingCollapsed)
  }

  @Builder
  addAppDialog() {
    Stack({ alignContent: Alignment.Top }) {
      Column()
        .width('100%')
        .height('100%')
        .backgroundColor('#55000000')
        .onClick((): void => {
          this.closeAddApp();
        })

      Column() {
        Column({ space: 14 }) {
          Text('添加应用')
            .width('100%')
            .fontSize(22)
            .fontWeight(FontWeight.Medium)
            .fontColor('#0a1513')

          Scroll() {
            Column() {
              ForEach(COMMON_APPS, (item: AppCatalogItem) => {
                this.appCatalogRow(item)
              }, (item: AppCatalogItem): string => item.bundle)
            }
            .width('100%')
          }
          .width('100%')
          .constraintSize({ maxHeight: 300 })
          .scrollBar(BarState.Off)
          .align(Alignment.TopStart)

          Button(this.showManualAdd ? '收起其他应用' : '找不到应用？通过包名添加')
            .width('100%')
            .height(34)
            .fontSize(12.5)
            .fontColor('#5f706b')
            .backgroundColor(Color.Transparent)
            .onClick((): void => {
              this.showManualAdd = !this.showManualAdd;
            })

          if (this.showManualAdd) {
            Column({ space: 8 }) {
              TextInput({ placeholder: '输入应用包名（自动验证是否已安装）', text: this.newBundleName })
                .height(42)
                .fontSize(14)
                .backgroundColor('#f3f7f5')
                .borderRadius(10)
                .onChange((value: string): void => {
                  this.newBundleName = value;
                  this.addAppError = '';
                })
              if (this.addAppError.length > 0) {
                Text(this.addAppError)
                  .width('100%')
                  .fontSize(12)
                  .fontColor('#ef4e52')
              }
              Button('加入清单')
                .width('100%')
                .height(38)
                .fontSize(13)
                .fontWeight(FontWeight.Medium)
                .fontColor('#ffffff')
                .backgroundColor('#0a1513')
                .borderRadius(10)
                .onClick((): void => {
                  this.addApp();
                })
            }
            .width('100%')
          }
        }
        .width('100%')
        .padding(20)
        .backgroundColor('#ffffff')
        .borderRadius(22)
        .shadow({ radius: 28, color: '#33000000', offsetY: 14 })
      }
      .width('100%')
      .padding({ left: 24, right: 24 })
      .margin({ top: 233 })
    }
    .width('100%')
    .height('100%')
  }

  @Builder
  appCatalogRow(item: AppCatalogItem) {
    Row({ space: 12 }) {
      Column({ space: 3 }) {
        Text(item.label)
          .fontSize(15)
          .fontWeight(FontWeight.Medium)
          .fontColor('#0a1513')
        Text(item.bundle)
          .fontSize(10.5)
          .fontColor('#81908c')
          .maxLines(1)
          .textOverflow({ overflow: TextOverflow.Ellipsis })
      }
      .alignItems(HorizontalAlign.Start)
      .layoutWeight(1)

      if (this.selectedApps.includes(item.bundle)) {
        Text('已添加')
          .fontSize(12)
          .fontColor('#0ebd7d')
          .padding({ right: 12 })
      } else {
        Text('+')
          .width(42)
          .height(42)
          .fontSize(24)
          .fontWeight(FontWeight.Lighter)
          .fontColor('#344c46')
          .textAlign(TextAlign.Center)
      }
    }
    .width('100%')
    .constraintSize({ minHeight: 52 })
    .padding({ top: 8, bottom: 8 })
    .opacity(this.selectedApps.includes(item.bundle) ? 0.52 : 1)
    .backgroundColor('#ffffff')
    .border({ width: { bottom: 1 }, color: '#e3ebe7' })
    .onClick((): void => {
      if (!this.selectedApps.includes(item.bundle)) {
        this.addCatalogApp(item.bundle);
      }
    })
  }

  private reloadApps(): void {
    const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
    this.selectedApps = readSelectedApps(context);
    this.disabledApps = readDisabledApps(context);
  }

  private persistApps(): void {
    const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
    writeSelectedApps(context, this.selectedApps);
    writeDisabledApps(context, this.disabledApps);
  }

  private setAppEnabled(bundle: string, enabled: boolean): void {
    if (enabled) {
      this.disabledApps = this.disabledApps.filter((item: string): boolean => item !== bundle);
    } else if (!this.disabledApps.includes(bundle)) {
      this.disabledApps = [...this.disabledApps, bundle];
    }
    this.persistApps();
  }

  private usesContainerMode(): boolean {
    return this.selectedApps.some((bundle: string): boolean =>
      !this.disabledApps.includes(bundle) && CONTAINER_BUNDLES.includes(bundle)
    );
  }

  private async addApp(): Promise<void> {
    const bundle = this.newBundleName.trim();
    if (!bundle.includes('.') || bundle === SELF_BUNDLE) {
      this.addAppError = '请输入有效的应用包名。';
      return;
    }
    if (this.selectedApps.includes(bundle)) {
      this.closeAddApp();
      return;
    }
    const label = await this.lookupAppLabel(bundle);
    if (label.length === 0) {
      this.addAppError = '此设备上未找到该应用，请检查包名。';
      return;
    }
    this.selectedApps = [...this.selectedApps, bundle].sort();
    this.persistApps();
    this.closeAddApp();
  }

  private async lookupAppLabel(bundle: string): Promise<string> {
    try {
      const info = await bundleManager.getBundleInfo(
        bundle,
        bundleManager.BundleFlag.GET_BUNDLE_INFO_WITH_APPLICATION
      );
      const label = info.appInfo?.label;
      if (typeof label === 'string' && label.length > 0 && !label.startsWith('$')) {
        return label;
      }
      return bundle;
    } catch (error) {
      const businessError = error as BusinessError;
      hilog.warn(0x04103, 'ArchieLinkUI',
        'lookupAppLabel %{public}s failed: %{public}d %{public}s',
        bundle, businessError.code, businessError.message ?? '');
      if (businessError.code === 17700001) {
        return '';
      }
      return bundle;
    }
  }

  private addCatalogApp(bundle: string): void {
    if (!this.selectedApps.includes(bundle)) {
      this.selectedApps = [...this.selectedApps, bundle].sort();
      this.persistApps();
    }
  }

  private closeAddApp(): void {
    this.addAppVisible = false;
    this.showManualAdd = false;
    this.newBundleName = '';
    this.addAppError = '';
  }

  private deleteApp(bundle: string): void {
    if (this.deletingApp.length > 0) {
      return;
    }
    this.lockSwipe();
    this.deletingApp = bundle;
    this.deletingCollapsed = false;
    this.getUIContext().animateTo({ duration: 360, curve: Curve.FastOutSlowIn }, (): void => {
      this.dragOffset = -440;
    });
    setTimeout((): void => {
      this.getUIContext().animateTo({ duration: 260, curve: Curve.FastOutSlowIn }, (): void => {
        this.deletingCollapsed = true;
      });
    }, 110);
    setTimeout((): void => {
      this.selectedApps = this.selectedApps.filter((item: string): boolean => item !== bundle);
      this.disabledApps = this.disabledApps.filter((item: string): boolean => item !== bundle);
      this.persistApps();
      this.deletingApp = '';
      this.deletingCollapsed = false;
      this.draggingApp = '';
      this.dragOffset = 0;
    }, 420);
  }

  private lockSwipe(): void {
    this.swipeLocked = true;
    if (this.swipeUnlockTimer >= 0) {
      clearTimeout(this.swipeUnlockTimer);
    }
    this.swipeUnlockTimer = setTimeout((): void => {
      this.swipeLocked = false;
      this.swipeUnlockTimer = -1;
    }, 3000);
  }

  private unlockSwipeSoon(): void {
    if (!this.swipeLocked) {
      return;
    }
    if (this.swipeUnlockTimer >= 0) {
      clearTimeout(this.swipeUnlockTimer);
    }
    this.swipeUnlockTimer = setTimeout((): void => {
      this.swipeLocked = false;
      this.swipeUnlockTimer = -1;
    }, 300);
  }

  private async refreshLocation(): Promise<void> {
    const generation = ++this.locationGeneration;
    const expectedConnected = this.status === 'connected';
    let lookupIP = '';
    if (expectedConnected && this.serverHost.length > 0) {
      try {
        lookupIP = await resolveServer(this.serverHost);
      } catch (_error) {
        lookupIP = '';
      }
    }
    for (let attempt = 0; attempt < 4; attempt++) {
      if (generation !== this.locationGeneration ||
        (this.status === 'connected') !== expectedConnected) {
        return;
      }
      const location = await this.fetchLocation(lookupIP);
      if (location.length > 0) {
        this.locationText = location;
        return;
      }
      await new Promise<void>((resolve: () => void): void => {
        setTimeout(resolve, 1400);
      });
    }
    if (generation === this.locationGeneration) {
      this.locationText = '位置暂不可用';
    }
  }

  private async fetchLocation(lookupIP: string): Promise<string> {
    const endpoints: string[] = lookupIP.length > 0
      ? [
        `https://ipwho.is/${lookupIP}`,
        `https://ipapi.co/${lookupIP}/json/`
      ]
      : [
        'https://ipwho.is/',
        'https://ipapi.co/json/',
        'https://ifconfig.co/json'
      ];
    for (const endpoint of endpoints) {
      const request = http.createHttp();
      try {
        const response = await new Promise<http.HttpResponse>(
          (resolve: (value: http.HttpResponse) => void, reject: (reason: Object) => void): void => {
            request.request(endpoint, {
              method: http.RequestMethod.GET,
              connectTimeout: 4000,
              readTimeout: 4000
            }, (error: BusinessError, data: http.HttpResponse): void => {
              if (error) {
                reject(error);
              } else {
                resolve(data);
              }
            });
          }
        );
        if (response.responseCode !== 200) {
          continue;
        }
        const info = JSON.parse(String(response.result)) as GeoInfo;
        const city = info.city?.trim() ?? '';
        const rawCountry = info.country_name ?? info.country ?? '';
        const country = countryLabel(rawCountry);
        if (city.length > 0 && country.length > 0) {
          return `${city}，${country}`;
        }
      } catch (_error) {
        // Try the next geo endpoint.
      } finally {
        request.destroy();
      }
    }
    return '';
  }

  private signalColor(alpha: number): string {
    return this.isConnected() ? withAlpha('#0ebd7d', alpha) : withAlpha('#ef4e52', alpha);
  }

  private pressTintColor(): string {
    if (this.pendingSwitch === 'connect') {
      return '#0ebd7d';
    }
    if (this.pendingSwitch === 'disconnect') {
      return '#ef4e52';
    }
    return this.isConnected() || this.isBusy() ? '#ef4e52' : '#0ebd7d';
  }

  private updatePendingSwitch(): void {
    if (this.pendingSwitch === 'connect') {
      if (this.isConnected() || !this.isBusy()) {
        this.pendingSwitch = '';
      }
    } else if (this.pendingSwitch === 'disconnect') {
      if (!this.isConnected() && !this.isBusy()) {
        this.pendingSwitch = '';
      }
    }
  }

  private startIdleAnimations(): void {
    this.glowOpacity = 0.10;
    this.glowScale = 0.76;
    this.haloScale = 0.90;
    this.haloOpacity = 0.75;
    this.orbitScale = 0.96;
    this.orbitAngle = 0;
    this.getUIContext().animateTo({
      duration: 2600,
      curve: Curve.EaseInOut,
      iterations: -1,
      playMode: PlayMode.Alternate
    }, (): void => {
      this.glowOpacity = 0.28;
      this.glowScale = 1.08;
      this.haloScale = 1.08;
      this.haloOpacity = 1;
      this.orbitScale = 1.04;
    });
    this.getUIContext().animateTo({
      duration: 16000,
      curve: Curve.Linear,
      iterations: -1
    }, (): void => {
      this.orbitAngle = 360;
    });
  }

  private isConnected(): boolean {
    return this.status === 'connected';
  }

  private isBusy(): boolean {
    return this.status === 'handshaking' || this.status === 'tun_ready' ||
      this.status === 'preparing';
  }

  private toggleConnection(): void {
    if (this.isConnected() || this.isBusy()) {
      this.pendingSwitch = 'disconnect';
      this.stopExtension();
      return;
    }
    this.connectVPN();
  }

  private async importConfig(): Promise<void> {
    try {
      const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
      const documentPicker = new picker.DocumentViewPicker(context);
      const uris = await documentPicker.select({
        fileSuffixFilters: ['.conf', '.wg', '.txt'],
        maxSelectNumber: 1
      });
      if (!uris || uris.length === 0) {
        return;
      }
      const file = await fileIo.open(uris[0]);
      const buffer = new ArrayBuffer(64 * 1024);
      const length = await fileIo.read(file.fd, buffer);
      fileIo.closeSync(file);
      const raw = decodeUTF8(buffer, length);
      const config = parseWgConfig(raw);
      if (config.error) {
        this.detail = config.error;
        return;
      }
      this.persistRawConfig(raw);
      this.applyConfig(config);
      this.detail = `${config.serverHost}:${config.serverPort} · ${config.clientAddress}`;
    } catch (error) {
      this.detail = this.errorMessage(error);
    }
  }

  private loadStoredConfig(): void {
    try {
      const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
      const raw = fileIo.readTextSync(`${context.filesDir}/${CONFIG_FILE}`);
      const config = parseWgConfig(raw);
      if (!config.error) {
        this.applyConfig(config);
      }
    } catch (_error) {
      this.configured = false;
    }
  }

  private persistRawConfig(raw: string): void {
    const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
    let file: fileIo.File | null = null;
    try {
      file = fileIo.openSync(
        `${context.filesDir}/${CONFIG_FILE}`,
        fileIo.OpenMode.READ_WRITE | fileIo.OpenMode.CREATE | fileIo.OpenMode.TRUNC
      );
      fileIo.writeSync(file.fd, raw);
    } catch (_error) {
      this.detail = '配置已载入，但本地保存失败。';
    } finally {
      if (file !== null) {
        fileIo.closeSync(file);
      }
    }
  }

  private applyConfig(config: WgParsedConfig): void {
    this.serverHost = config.serverHost;
    this.serverPort = Number(config.serverPort);
    this.clientAddress = config.clientAddress;
    this.clientPrefixLength = config.clientPrefixLength;
    this.dnsServer = config.dnsServer;
    this.privateKey = config.privateKey ?? '';
    this.publicKey = config.publicKey ?? '';
    this.presharedKey = config.presharedKey ?? '';
    this.allowedIPs = config.allowedIPs ?? '0.0.0.0/0';
    this.configured = this.privateKey.length > 0 && this.publicKey.length > 0;
  }

  private async connectVPN(): Promise<void> {
    if (!this.configured) {
      this.goToPage('settings');
      this.detail = '请先导入有效的 WireGuard .conf 文件。';
      return;
    }
    if (this.selectedApps.length === 0) {
      this.goToPage('settings');
      this.detail = '请先添加需要通过 VPN 的应用。';
      return;
    }
    this.pendingSwitch = 'connect';
    this.status = 'preparing';
    try {
      const serverIP = await resolveServer(this.serverHost);
      const want: Want = {
        deviceId: '',
        bundleName: 'com.archielink.companion',
        abilityName: 'VpnAbility',
        parameters: {
          serverIP: serverIP,
          serverPort: this.serverPort,
          clientAddress: this.clientAddress,
          clientPrefixLength: this.clientPrefixLength,
          dnsServer: this.dnsServer,
          privateKey: this.privateKey,
          publicKey: this.publicKey,
          presharedKey: this.presharedKey,
          allowedIPs: this.allowedIPs
        }
      };
      await vpnExtension.startVpnExtensionAbility(want);
      const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
      startVpnBackgroundTask(context);
      startVpnWatchdog(context);
      this.refreshStatus();
    } catch (error) {
      const businessError = error as BusinessError;
      this.status = businessError.code === 2203002 ? 'conflict' : 'error';
      this.pendingSwitch = '';
      this.detail = businessError.code === 2203002
        ? '请先在系统或其他应用中断开现有 VPN。'
        : this.errorMessage(error);
    }
  }

  private stopExtension(): void {
    stopVpnWatchdog();
    vpnExtension.stopVpnExtensionAbility(VPN_WANT)
      .then((): void => {
        const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
        stopVpnBackgroundTask(context);
        writeVpnState(context, 'stopped', 'HarmonyOS VPN 已停止，资源已释放。');
        this.status = 'stopped';
        this.pendingSwitch = '';
      })
      .catch((error: BusinessError): void => {
        this.status = 'error';
        this.pendingSwitch = '';
        this.detail = `错误码 ${error.code}: ${error.message}`;
      });
  }

  private refreshStatus(): void {
    const context = this.getUIContext().getHostContext() as common.UIAbilityContext;
    const current = readVpnState(context);
    this.status = current[0];
    this.detail = current[1];
    this.updatePendingSwitch();
    if (current[0] !== this.observedVpnState) {
      const previousState = this.observedVpnState;
      this.observedVpnState = current[0];
      if (current[0] === 'conflict' && previousState.length > 0) {
        this.getUIContext().showAlertDialog({
          title: 'VPN 冲突',
          message: current[1],
          confirm: {
            value: '知道了',
            action: (): void => {}
          },
          cancel: (): void => {}
        });
      } else if (current[0] === 'connected') {
        this.locationText = '正在获取 VPN 位置';
        setTimeout((): void => {
          this.refreshLocation();
        }, 800);
      } else if (current[0] === 'stopped' || current[0] === 'error' || current[0] === 'idle') {
        this.locationText = '正在获取位置';
        setTimeout((): void => {
          this.refreshLocation();
        }, 250);
      }
    }
  }

  private errorMessage(error: Object): string {
    const businessError = error as BusinessError;
    return businessError.message ?? JSON.stringify(error);
  }
}

function appLabel(bundle: string): string {
  for (const item of COMMON_APPS) {
    if (item.bundle === bundle) {
      return item.label;
    }
  }
  const parts = bundle.split('.');
  return parts.length > 0 ? parts[parts.length - 1] : bundle;
}

function countryLabel(country: string): string {
  switch (country.toUpperCase()) {
    case 'US':
    case 'UNITED STATES':
      return '美国';
    case 'GB':
    case 'UNITED KINGDOM':
      return '英国';
    case 'JP':
    case 'JAPAN':
      return '日本';
    case 'SG':
    case 'SINGAPORE':
      return '新加坡';
    case 'DE':
    case 'GERMANY':
      return '德国';
    case 'NL':
    case 'NETHERLANDS':
      return '荷兰';
    case 'CA':
    case 'CANADA':
      return '加拿大';
    case 'AU':
    case 'AUSTRALIA':
      return '澳大利亚';
    case 'FR':
    case 'FRANCE':
      return '法国';
    case 'HK':
    case 'HONG KONG':
      return '中国香港';
    case 'TW':
    case 'TAIWAN':
      return '中国台湾';
    case 'CN':
    case 'CHINA':
      return '中国';
    default:
      return country;
  }
}

function withAlpha(color: string, alpha: number): string {
  const channel = Math.max(0, Math.min(255, Math.round(alpha * 255)))
    .toString(16)
    .padStart(2, '0');
  return `#${channel}${color.slice(1)}`;
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

function decodeUTF8(buffer: ArrayBuffer, length: number): string {
  const bytes = new Uint8Array(buffer, 0, length);
  let result = '';
  for (let index = 0; index < bytes.length; index++) {
    result += String.fromCharCode(bytes[index]);
  }
  return result;
}
