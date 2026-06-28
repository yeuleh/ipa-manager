# Requirements — ipa-manager

<!-- mindtrek-bootstrap:start -->
## 项目概述

**ipa-manager** 是一个 macOS 上的 CLI 工具，用于管理多个 Apple 账号下的 iOS 应用（`.ipa`）全生命周期：登录/切换账号、按账号隔离下载与本地管理、推送到 iOS 设备安装/更新。

## 讨论摘要

### Q1 — 项目管理的是什么 IPA？

用户明确：iOS `.ipa` 应用包。这是一个 Mac CLI 工具，能力包括：
- 登录并切换多个 Apple 账号
- 分账号下载 / 隔离管理 app 文件
- 推送 / 安装 / 更新到 iOS 设备

### Q2 — 语言偏好

用户表述："这只是我个人的小工具，不限语言，即使用 bash 能实现也可以，关键是能用最小成本实现。"

→ 无语言硬约束，优化目标是 **最小实现成本**。

### Q3 — 是否接受依赖外部 CLI 工具

用户反问 `ipatool` 与 `ideviceinstaller` 是否活跃/稳定/安全，能否集成。

经研究验证（详见 `research.md`）：
- 两个 raw tool 都不直接支持**多账号切换 + 按账号隔离**——这正是本项目要填补的空白。
- 用户进一步希望"合并成一个交互体验良好的工具"，而非简单 shell 包装。

### Q4 — "抄过来合并" 的可行性

用户提议读 ideviceinstaller 代码用 Go 重写以避开 GPL。

研究结论：
1. `ipatool` 是 Go + MIT，**可直接 import 为依赖**（`pkg/appstore` 暴露干净的 `AppStore` 接口），无需"抄"。
2. **不需要自己重写** ideviceinstaller —— 存在纯 Go + MIT 的替代品 `go-ios`（`danielpaulus/go-ios`），它把 libimobiledevice/ideviceinstaller 的设备通信能力用 Go 重新实现，且可 import。
3. 纠正一个误解：即便没有 go-ios，subprocess 调用 GPL 程序也不会让本代码变 GPL（FSF 认可的聚合 vs 衍生边界）。

### Q5 — go-ios 的健康度复核

用户要求对 go-ios 做与 ipatool/ideviceinstaller 同等标准的尽职调查。结论：**go-ios 是三者中综合最强的**（详见 `research.md`）。

## 结论

| 维度     | 结论                                                                                              |
| -------- | ------------------------------------------------------------------------------------------------- |
| 项目名   | `ipa-manager`                                                                                     |
| 项目类型 | Mac CLI 工具                                                                                      |
| 语言     | **Go**（因两个核心底层库均为 Go 且可 import）                                                       |
| 核心功能 | ① 多 Apple 账号登录/切换 ② 按账号隔离下载管理 `.ipa` ③ 推送/安装/更新到 iOS 设备                      |
| 底层依赖 | `github.com/majd/ipatool/v2`（MIT，账号侧：登录/搜索/下载）+ `github.com/danielpaulus/go-ios`（MIT，设备侧：安装/列举/卸载） |
| 规模     | 个人小工具                                                                                        |
| 部署目标 | 本地 macOS，单二进制                                                                              |
| 交互     | TUI（账号选择 / 进度 / 彩色输出）                                                                  |
| 关键约束 | 全 MIT、零 subprocess、代码级 import；UI 交互良好                                                  |
| 已知风险 | ① ipatool 依赖 Apple 私有 API，服务端变更时需等项目跟进；② iOS 17+ 设备通信需 tunnel               |

## YAGNI 检查

范围聚焦明确：多账号编排 + 隔离 + 设备安装。不包含 App Store 搜索 UI（用 ipatool search 即可）、不包含签名/越狱相关能力（超出个人小工具范围）。
<!-- mindtrek-bootstrap:end -->
