# Windows 首次启动预加载修复（2026-10-09）

## 问题与改动

原日志 `local browser exited before DevTools became available: <nil>` 表示受监控的启动进程以成功状态退出，代码却立即将它当成浏览器启动失败。此时尚未得到 DevTools 地址，错误发生在打开 3D 页面之前。

Windows 浏览器的启动进程可能把工作交给另一个进程。原实现已经为 Edge 添加兼容启动参数，但仍假设初始 PID 的生命周期就是浏览器生命周期。这一假设也影响清理：原 PID 退出后，对它执行 `taskkill /T` 无法可靠清理接力进程。

- 成功退出后继续等待 DevTools，使用原来的启动超时与取消信号；退出通知只消费一次，避免关闭的通道造成忙循环。
- 非零退出仍立即报告真实错误，保留错误链；超时会说明启动器已成功退出但 DevTools 尚未就绪。
- 每个 Worker 用独立 Windows Job Object 管理浏览器及其子进程，先挂起启动、加入 Job，再恢复执行，消除快速接力时的管理空隙。
- 停止、取消及启动失败时结束整个 Job，并等待已观测进程真正退出后释放端口。整体 CPU 配额仍由网关原有 Job 管理。Windows 支持此类嵌套与子进程继承，参见 [Microsoft 嵌套 Job 文档](https://learn.microsoft.com/en-us/windows/win32/procthread/nested-jobs)。
- 将 Worker 根目录转换为绝对路径；启动诊断包含浏览器路径、CDP 端口与 `chromium.log` 路径。

## 验证

新增回归用例在修改前复现了同样的 `<nil>` 错误，修改后通过。覆盖启动器成功退出后 DevTools 延迟出现、真实失败、无 DevTools 的超时和取消等待。

真实 Windows Edge 验证使用全新配置目录，另用辅助启动器启动 Edge 后立即成功退出，确认接力浏览器可连接、停止后进程退出且 DevTools 端口释放。该用例在外层 50% CPU Job 下运行，验证嵌套行为。

普通浏览器测试继续验证场景就绪后首帧、CPU SwiftShader/WARP、GPU、观看输入、多路独立实例与重启恢复。全量测试首轮出现一次多路 WebGL 页面就绪超时，该用例单独复测通过，随后再次验证整个渲染模块。最终测试结果记录于同次发布目录的 `VALIDATION.md`。

没有取得用户故障现场的 `chromium.log` 和浏览器版本，因此本次确认并修复的是启动器正常退出的误判与进程清理缺陷。目标安装仍需升级后验证具体 ThingJS 项目；若再次失败，可按新增错误中的日志路径取得进一步证据。

## 升级

停止旧软件、备份安装目录，从修复包中仅替换 `gostream3d.exe`，保留已有 `config.env` 与 `data`。重新启动后，在项目列表点击“重新预加载”。
