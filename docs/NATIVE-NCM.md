# 原生 USB 以太网模式（实验性）

本方案已在 OnePlus 一加 15T（PLZ110，Android 16）与这台 macOS 27 Mac 上实测。
它使用手机的 NCM 功能和 Mac 内置 AppleUSBNCM 驱动；USB 数据流不经过 BetterTether 的 Go/libusb/utun 转发器。

## 已验证结果

- 系统“网络”页显示“一加 15T — 已连接”，类型为 Ethernet，当前接口为 `en12`。
- Mac 通过手机 DHCP 获取 IPv4 地址、网关、DNS；当前服务顺序高于 Wi-Fi。
- Surge 的网络摘要显示 `Primary interface type: Wired`、接口 `en12`。
- 使用原有 `Surge-Mini` 配置，开启系统代理、关闭增强模式，测试成功。
- 关闭 Mac Wi-Fi 后，直连 Apple 网站及通过 Surge 代理访问 Google 均为 HTTP 200。测试前后确认 Wi-Fi 均为 Off，默认路由为 `en12`。测试完成后恢复了 Wi-Fi。
- 从手机“文件传输”模式重新运行连接脚本，再次取得 NCM DHCP 网关、USB 直连 HTTP 200；复测 Mac Wi-Fi 关闭时，原 Surge 配置的代理请求正常完成（Google 返回地区跳转 HTTP 302）。

地址和接口编号不是固定值。以上是一次设备实测，不代表所有 Android 厂商或固件都兼容。

## 为什么之前只有网卡但不能上网

`svc usb setFunctions ncm` 只负责 USB 功能切换。这台手机产生 `usb0`，但其 NCM 共享配置匹配的是 `ncm\\d`；直接启动 `TETHERING_NCM` 后仍没有 DHCP。
Android 以太网服务实际识别了 `usb0`。辅助程序用已经授权的 ADB shell 身份申请 `TETHERING_ETHERNET`、`CONNECTIVITY_SCOPE_GLOBAL`，才成功启动 DHCP 与互联网转发。
没有修改 Android 持久属性、授予额外应用权限、绕过运营商共享检查或获取 root。

## 本机重新连接

1. 手机接入 USB，解锁，开启 USB 调试并授权这台 Mac。
2. 双击仓库根目录的 `启动原生USB共享.command`，或运行：

   ```sh
   python3 scripts/native-ncm.py start
   ```

3. 脚本会停止旧 BetterTether 守护进程以释放 USB（必要时要求管理员密码），切换手机到 NCM，启动以太网共享，并等待 DHCP 网关。
   双击入口会在缺少辅助程序时先执行本机构建，需要 ADB 与 OpenJDK。
4. 保持旧 BetterTether 转发器停止。它的界面尚未整合 NCM 状态，因此其中的“已停止”不代表原生 USB 网络断开。
5. 在 macOS 网络设置确认手机服务已连接，并排在 Wi-Fi 前面。脚本不会修改服务顺序、Wi-Fi 或 Surge 配置。
6. 使用原来的 Surge 配置，不要使用仍绑定旧 `utun4` 的专用配置。

若 ADB 找不到手机，先停止旧 BetterTether，并在手机的 USB 通知中临时切到“文件传输 / Android Auto”，保持 USB 调试开启后再运行脚本。Mac 的 USB 设备在 RNDIS 共享模式下可能仍阻止 ADB 打开；更换为文件传输通常会重新枚举 USB。脚本会为这台手机重启 ADB 的 native 后端。
如果 `adb devices` 仍为空，且 ADB 日志提示 `0xe00002be` USB 资源不足，应确认手机设置里的“USB 网络共享”开关确实关闭，暂时不要打开 BetterTether 界面或再次开启共享；必要时拔插 Mac 端 USB 线并换一个接口。这是 Mac 无法向手机发送切换指令，不能靠给已断开的 `enN` 手动设置 IP 修复。

USB 拔插、手机重启或再次点击手机“USB 网络共享”可能让手机恢复 RNDIS。此方案尚未提供自动重连后台服务，需要再次运行上述脚本。不要将一次联网成功理解为跨重启持久设置。

`python3 scripts/native-ncm.py status` 可查看当前原生网卡是否已取得 DHCP 网关及系统默认路由。

## 从源码构建辅助程序

需要 ADB、Python 3、JDK。默认 JDK 路径为 Apple Silicon Homebrew 的 OpenJDK；其他路径通过 `JAVA_BIN_DIR` 指定。

```sh
bash scripts/android/build-native-tether.sh
```

脚本从 Google 官方仓库获取固定版本 R8 并校验 SHA-256，然后生成 `build/ncm-helper/native-tether.jar`。连接脚本将此文件复制到手机 `/data/local/tmp`，通过 ADB shell 运行；不会安装 Android APK。

辅助程序依赖 Android 隐藏接口，未来系统升级可能使其失效。回调成功仅说明系统接受请求；连接脚本还会检查实际 DHCP 网关。联网仍须以访问测试为准。

## 恢复原来的 RNDIS 模式

先使用 Wi-Fi 或其他可用网络，确保操作期间不会失联。手机连接且 ADB 已授权时：

```sh
adb -d shell 'CLASSPATH=/data/local/tmp/bettertether-native.jar app_process / NativeTether stop-ethernet'
adb -d shell svc usb setFunctions rndis
```

也可以在手机上关闭并重新开启 USB 网络共享，确认回到 RNDIS。随后再启动 BetterTether。若继续使用 `interface_only = true`，需使用对应当前 `utunN` 的 Surge 专用配置；原生 NCM 方式则无需该绑定。

## Surge 与 aTrust 的边界

原生网卡解决的是系统物理出站选择，不改变 Surge 的分流规则。已验证原配置的系统代理模式。
原配置的增强模式与 aTrust 同时工作仍需单独验证：aTrust 占用 `198.18.0.0/16` 时可能与 Surge 虚拟 DNS 冲突。不要据此宣称所有应用都被 Surge 接管，或企业内网已通过验证。

## 参考

- [Apple 网络服务顺序](https://support.apple.com/en-lamr/guide/mac-help/mchlp2711/mac)
- [Android USB 功能命令](https://android.googlesource.com/platform/frameworks/base/+/61f01fe56bd8464acf3141212371a9176f3d6c9b/cmds/svc/src/com/android/commands/svc/UsbCommand.java)
- [Android TetheringManager](https://android.googlesource.com/platform/packages/modules/Connectivity/+/refs/heads/main/Tethering/common/TetheringLib/src/android/net/TetheringManager.java)
