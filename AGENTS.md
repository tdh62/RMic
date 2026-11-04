# 仓库协作约定

## CI 与联合构建

- GitHub 公开仓库使用 `.github/workflows/build.yml`，监听 main push、pull_request、v* tag push 与手动触发。复用 `.ci/` 脚本和现成 Go 环境，不引入自定义镜像。
- 联合构建 Linux amd64、Linux arm64、Windows amd64 的 TUI/GUI 与 Android。Linux 使用原生对应架构 runner；Windows 使用 MinGW-w64 和静态 Opus 交叉编译。
- 普通提交和 PR 仅上传 Actions artifacts；v* 标签或手动启用 publish_release 时，在所有平台成功后验证 SHA-256 并发布 Release。手动发布填写 version，缺失的标签在构建完成后创建；已有标签必须对应本次构建提交，不覆盖已发布版本。带连字符的标签标记预发布。
- Android 签名仅从 GitHub Secrets 读取，PR 不使用签名凭据；无配置时明确输出未签名 Release。
- 如实区分本地验证、工作流触发成功和远端构建通过，不得混淆。
- 公开分支不跟踪 `docs/`，用户说明统一在 README.md，各平台包附同一说明。

## 参考材料与驱动

- `docs/ZeroMic/` 仅为本地参考材料，不提交参考项目源码、页面、图标或整个示例项目。
- 本分支用于公开分发，仅随 Windows 包原样提供基础 VB-CABLE Pack45 ZIP 与许可说明，不嵌入程序。默认引导用户自行解压安装。当前配置目录存在有效 `vbcable-installer.json` 且指定校验通过的本地原包时才开放 RMic 安装/卸载入口，保留 UAC。公开包不提供启用配置；移除 A/B 包与安装选项及 Steam 驱动包。完整自用版保留在 `feat/rmic-core` 分支。
- `docs/SteamLinkAudioDrivers/` 仅作本地参考，不提交整个参考仓库或驱动文件。原个人使用分支的历史含驱动包，公开源码请使用干净快照，不公开继承该历史的 Git 仓库。
- Windows 默认使用 exe 同级 `rmic-data/` 保存设置、证书及历史授权，该目录必须保持忽略；明确指定的配置目录不可写时不得静默改用其他授权库。
- 设备配对须由电脑本地 TUI / GUI 许可，网络 API 不提供批准入口；持久授权仅保存随机凭证的 SHA-256，支持撤销并停止对应活动会话。
- Linux 双向音频使用两条独立通道；设备故障不回退默认设备，不自动修改系统默认音频路由。
