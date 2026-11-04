# Android 第三方组件

Android 客户端通过 Gradle 获取以下主要组件，版本固定在 `android/app/build.gradle.kts`：

| 组件 | 版本 | 来源与许可 |
| --- | --- | --- |
| AndroidX / Jetpack Compose | 见 Gradle 配置与 Compose BOM | https://github.com/androidx/androidx ，Apache-2.0 |
| Kotlin / kotlinx.coroutines | 2.1.21 / 1.10.2 | https://github.com/JetBrains/kotlin 、https://github.com/Kotlin/kotlinx.coroutines ，Apache-2.0 |
| OkHttp / Okio | 4.12.0 / 依赖解析版本 | https://github.com/square/okhttp 、https://github.com/square/okio ，Apache-2.0 |
| WebRTC SDK Android | 144.7559.15 | https://github.com/webrtc-sdk/android/tree/v144.7559.15 ，SDK 构建项目为 MIT |
| libwebrtc | 随 SDK 提供 | https://webrtc.googlesource.com/src/ ，BSD-3-Clause 及其第三方组件许可 |

原生 libwebrtc 包含 Opus、BoringSSL 等第三方组件，完整清单与许可证由对应 WebRTC 源码版本的 `third_party` 目录及许可证生成工具维护。SDK 的 MIT 许可不替代 libwebrtc 及其第三方组件许可。

Gradle Wrapper 来自 Gradle 8.13，许可为 Apache-2.0。Android SDK 为构建工具，不打包进 APK，使用时由构建者接受 Google Android SDK 许可。

以下保留 WebRTC SDK 构建项目的原始许可：

```text
MIT License

Copyright (c) 2023 WebRTC SDKs

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```
