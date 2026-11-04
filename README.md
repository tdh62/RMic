# RMic

把手机变成电脑的麦克风，也可以用手机耳机收听电脑声音。

RMic 在电脑上运行，手机通过浏览器或 Android 客户端连接，适合局域网和已互通的 Tailscale 等网络。音频不依赖云端服务；使用覆盖网络时，连接遵循该网络的直连或中继路径。

| 使用方式 | 声音方向 | 适合场景 |
| --- | --- | --- |
| 手机麦克风 | 手机 → 电脑 | 会议、语音聊天、录音 |
| 听电脑 | 电脑 → 手机 | 用手机耳机听音乐、视频或游戏声音 |
| 双向传音 | 两个方向同时工作 | 同时说话和收听，建议戴耳机 |

## 版本说明

当前 GitHub 版本：**v0.1**。正式版本发布后可从 [GitHub Releases](https://github.com/tdh62/RMic/releases/tag/v0.1) 下载；构建未完成时请查看 [Actions](https://github.com/tdh62/RMic/actions)。

| 文件名包含 | 用途 |
| --- | --- |
| `rmic-gui-…-windows-amd64.zip` | Windows 图形界面版，推荐普通用户使用 |
| `rmic-…-windows-amd64.zip` | Windows 终端界面版，支持键盘操作 |
| `rmic-gui-…-linux-amd64.tar.gz` | 常见 x86-64 Linux 电脑的图形界面版 |
| `rmic-…-linux-amd64.tar.gz` | 常见 x86-64 Linux 电脑的终端界面版 |
| `linux-arm64` | ARM64 Linux 设备，按需选择 GUI 或终端版 |
| `android-release.apk` | 已签名 Android 客户端 |
| `android-debug.apk` | Android 测试版 |

## 快速开始（Windows）

**只想听电脑声音**时，可以在 RMic 中明确选择扬声器或耳机对应的 `loopback:` 设备，采集该设备正在播放的声音，不必为此安装 VB-CABLE。

**想把手机当作电脑麦克风**时，需要虚拟音频设备。Windows 发布包在 `drivers/windows/` 中提供完整未修改的基础 VB-CABLE 原版 ZIP：

1. 将 `VBCABLE_Driver_Pack45.zip` 完整解压到独立文件夹。
2. 阅读其中的 `readme.txt`，按厂商说明以管理员身份运行 `VBCABLE_Setup_x64.exe`，在官方窗口安装并确认系统 UAC。
3. 按安装器提示重启电脑。
4. 打开 RMic，在驱动页面点击“检测全部”，或在终端版按 `r` 刷新。

也可以从 [VB-Audio 官方网站](https://vb-audio.com/Cable/) 下载。RMic 默认不会启动驱动安装器。卸载时也请按原版说明操作官方安装器。

双击 `rmic-gui.exe` 打开图形界面，或双击 `rmic.exe` 打开终端界面。RMic 主程序不需要以管理员身份运行。

首次使用按引导准备设备、选择用途，然后保存并启动。已有音频设备时可以选择“使用现有设备”跳过驱动准备。

| 用途 | RMic 中选择 | 其他电脑软件中选择 |
| --- | --- | --- |
| 手机麦克风 | 麦克风送入：`CABLE Input` | 会议、游戏或录音软件的麦克风：`CABLE Output` |
| 听电脑 | 电脑声音来源：`loopback:实际使用的扬声器或耳机` | 播放软件输出到这副扬声器或耳机 |
| 双向 | 同时配置上面两条独立通道 | 分别设置输入和输出 |

VB-CABLE 的 `Input` 是声音进入线缆的播放端，`Output` 是其他软件读取声音的录音端，名称容易看反。不要将同一条线缆同时用作双向传音的两条通道，也不要开启 Windows 录音设备的“侦听此设备”。

RMic 不会自动修改系统默认输入、输出设备；请在目标应用中明确选择。设备失效时也不会自动改用其他麦克风或扬声器。

## Linux 使用

需要正在运行的 PulseAudio，或 PipeWire 的 PulseAudio 兼容服务。在 Debian / Ubuntu 上可安装运行依赖：

```bash
sudo apt install libopus0 pulseaudio-utils
```

使用与系统兼容的包，建议 Debian 12 / Ubuntu 24.04 或更新版本。GUI 还需要可用的桌面图形环境及系统 OpenGL / X11 运行库。

解压后，在登录桌面的普通用户会话中运行 `./rmic-gui` 或 `./rmic`。RMic 使用两条独立虚拟音频通道：

| 用途 | 目标电脑应用的设备选择 |
| --- | --- |
| 使用手机麦克风 | 输入选择 `RMic_Virtual_Microphone` |
| 将电脑应用声音发到手机 | 输出选择 `RMic_Computer_Playback` |

电脑应用如果仍向原来的扬声器播放，手机就听不到这路声音。RMic 不会主动切换系统默认音频设备。默认创建的临时通道会在正常退出时清理。

随后在手机打开电脑显示的 HTTPS 地址，申请配对并在电脑端核对允许，再选择模式开始传音。

## 连接与证书

默认使用 **HTTPS TCP 8443** 和 **WebRTC UDP 50000**。如果电脑防火墙询问网络访问权限，应允许手机所在网络访问这两个端口。

- 手机和电脑使用同一 Wi-Fi 时，选择电脑的局域网地址。
- 使用 Tailscale 等覆盖网络时，选择对应网络地址，并确保两端访问规则允许通信。
- 多网卡电脑可能显示多个地址；终端版在二维码页按 `n` 切换。换网络或电脑 IP 变化后，重新启动 RMic 并打开新地址。
- 网页的 WebSocket / PCM 模式只使用 HTTPS 端口；WebRTC 还需要 UDP，只有 HTTPS 代理并不足以传输它的音频。

RMic 默认生成自签名证书。只对你确认属于自己电脑的地址和证书建立信任。浏览器跳过证书警告不一定能启用麦克风；如果仍提示连接不安全或不能录音，需要按设备系统的方式信任证书，或为电脑配置受设备信任且覆盖访问地址的证书。

遇到证书变化或网页无法恢复连接时，重新在浏览器打开电脑当前显示的完整 HTTPS 地址，处理证书提示，再返回传音页面。Android 若拒绝旧证书绑定，先停止传音，仅清除此服务器的本机记录，重新核对证书并配对。

## 常见问题

**已连接，但是没有声音？**

先确认两端没有静音、音量不是零，再检查电脑应用是否选择了正确设备。Windows 使用手机麦克风时，RMic 写入 `CABLE Input`，应用读取 `CABLE Output`。Linux 收听电脑声音时，应用必须输出到 `RMic_Computer_Playback`。若手机显示“启用播放”，点击后再试。

**配置和授权保存在哪里？**

Windows 默认保存在程序同级的 `rmic-data/`。目录不可写时会提示并尝试使用用户配置目录；显式指定的 `--config-dir` 不可写则直接报错。其他平台默认使用系统用户配置目录中的 `rmic` 子目录。实际路径可在终端帮助中查看。

升级时保留配置目录；迁移到自己的另一台电脑时可一并复制。该目录包含私钥和历史授权，不要随公开压缩包分享。GUI 和终端版可共用配置，但同一目录同时只能运行一个实例。

## 驱动与许可

RMic 项目自身的代码与文档采用 [MIT License](LICENSE)。

第三方驱动、依赖和资源仍遵循各自原始许可。


基础 VB-CABLE 由 Vincent Burel / VB-Audio 提供，属于 Donationware，欢迎通过 [官方网站](https://vb-audio.com/Cable/) 支持作者。完整版权、许可、安装说明和免责声明保留在原版 ZIP 的 `readme.txt` 中。


# 链接

- [LINUX DO](https://linux.do)
- [VB-CABLE Virtual Audio Device](https://vb-audio.com/Cable/)

# 其他

<details>
<summary>macOS 使用说明（未测试）</summary>

macOS 使用 CoreAudio，目标系统为 macOS 12 或以上，需要在 Mac 上原生构建。

通过 [BlackHole 官方项目](https://github.com/ExistentialAudio/BlackHole) 或 Homebrew 准备两条独立通道：

```bash
brew install --cask blackhole-2ch blackhole-16ch
```

手机麦克风送入 `BlackHole 2ch`，会议软件输入也选择它；电脑声音来源选择 `BlackHole 16ch`，播放软件输出也选择它。不要将两个方向配置为同一个设备。默认先使用双声道。

双击包中的 `Start-RMic.command` 或执行 `./rmic`。首次采集时，在系统“隐私与安全性 → 麦克风”中允许启动 RMic 的终端访问。当前包未签名、未公证，应核对来源后按系统提示操作，无需关闭系统安全机制。

</details>

<details>
<summary>从源码构建</summary>

普通用户可直接使用发布包。开发者需要 Go 1.26 或以上、C 编译器、pkg-config 和 Opus 开发库。Linux 示例：

```bash
sudo apt install build-essential pkg-config libopus-dev pulseaudio-utils
make build
./bin/rmic
```

GUI 另需图形开发包及中文字体：

```bash
sudo apt install libgl1-mesa-dev xorg-dev fonts-noto-cjk
make build-gui
```

Android 固定签名在 GitHub 仓库 Settings → Secrets and variables → Actions 配置 `RMIC_ANDROID_KEYSTORE_BASE64`、`RMIC_ANDROID_STORE_PASSWORD`、`RMIC_ANDROID_KEY_ALIAS`、`RMIC_ANDROID_KEY_PASSWORD`，分别为密钥库 Base64、密钥库密码、签名别名及密钥密码。未配置时提供可安装 Debug 和明确命名的未签名 Release；PR 不读取签名凭据。使用原有密钥才能覆盖安装已有正式客户端。

</details>
