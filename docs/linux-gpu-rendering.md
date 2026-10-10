# Linux GPU 渲染与实测记录

版本：20261009-154343；验证日期：2026-10-09。

Linux Docker 已在 192.168.1.100 的 Intel Iris Xe 核显上完成实际园区场景的 GPU 绘制、H.264 硬件编码及远端观看验证。保留 1920×1080，没有通过缩小到 720p 提升帧率。30 FPS 设置平均播放 28.2 FPS；60 FPS 设置平均播放 39.7 FPS，尚未稳定达到 60 FPS。

## 实现

- “本机 GPU”入口自动识别宿主机 Intel render 节点；也可用 `GOSTREAM3D_GPU_BACKEND=intel|nvidia|auto` 指定后端。兼容原有 API 的 `render_mode=nvidia` 值。
- Intel worker 映射 `/dev/dri/renderD128`，服务用户加入对应设备组。Chromium 通过 ANGLE Vulkan 使用核显；预加载完成后检查 WebGL renderer，遇到 SwiftShader、llvmpipe 等软件渲染器会报错，不会悄悄退回 CPU。
- Intel 使用 `vah264lpenc`、CQP 22、无 B 帧进行 H.264 硬编。目标机器支持低功耗编码，但 CBR 在该设备上失败，CQP 实测通过，因此采用此配置。NVIDIA 使用 `nvh264enc` 和 Docker GPU device request。
- 编码前的单帧队列丢弃积压旧帧。分辨率跟随项目设置和窗口；管线没有 `videoscale`。CPU 模式继续保留，CPU 专用 1280×720 上限已撤回。

## 在目标机器上的证据

服务器为 i5-1135G7、8 个逻辑核心、Intel Iris Xe Tiger Lake，宿主机驱动 i915。浏览器实际 WebGL renderer：

```text
ANGLE (Intel, Vulkan 1.4.305 (Intel(R) Iris(R) Xe Graphics (TGL GT2) (0x00009A49)), Intel open-source Mesa driver)
```

这与 worker 的 render 节点映射、硬编管线一起记录在 [硬件配置证据](../output/gpu-render-benchmark/20261009-154343/gostream3d-gpu-proof.json)。

观看端为 Windows Edge，窗口 1920×1080，访问实际园区页面。预热后进行 5 次拖拽，每次约 6.2 秒，使用视频播放计数及 WebRTC 接收统计记录结果。FPS 表示观看端播放帧率，不等于场景内部绘制帧率。

| 项目设置 | 视频实际尺寸 | 实际协商编码 | 平均播放 FPS | 5 段播放 FPS | 视频接收码率 |
| --- | --- | --- | --- | --- | --- |
| 30 FPS | 1920×1080 | H.264 | 28.20 | 29.29 / 29.42 / 29.39 / 27.65 / 25.24 | 22.49 Mbps |
| 60 FPS | 1920×1080 | H.264 | 39.69 | 27.19 / 39.09 / 38.67 / 47.02 / 46.48 | 24.52 Mbps |

原始数据：[30 FPS](../output/gpu-render-benchmark/20261009-154343/cpu-benchmark-intelGPU1080p30.json)、[60 FPS](../output/gpu-render-benchmark/20261009-154343/cpu-benchmark-intelGPU1080p60.json)。截图：[30 FPS 画面](../output/gpu-render-benchmark/20261009-154343/cpu-benchmark-intelGPU1080p30.png)、[60 FPS 画面](../output/gpu-render-benchmark/20261009-154343/cpu-benchmark-intelGPU1080p60.png)。

60 FPS 测试期间，worker 的三次 Docker CPU 观测值为 292.54%、287.85%、290.23%。Docker 的 100% 对应一个逻辑核心，这些观测值约相当于占用 2.9 个逻辑核心；不是整段测试的 CPU 平均值。

同一时间读取该 worker 内 Chromium GPU 进程与 Neko 进程的宿主机 `/proc/PID/fdinfo`，按 DRM client ID 去重。15.229 秒内，Chromium 的 i915 render 引擎累计执行时间增加 10.093 秒，Neko 的 video 引擎累计执行时间增加 8.371 秒。这证明绘制和视频编码实际使用了 GPU。该累计值不能直接当作整机 GPU 利用率。详见 [DRM 原始计数](../output/gpu-render-benchmark/20261009-154343/gostream3d-gpu-usage-60.json)。

本轮 WebRTC `framesDropped` 增量为 0，但两个测试的 `packetsLost` 计数分别增加 1523、4523，因此不能声称网络无丢包。CQP 码率随内容变化，拖拽时约需 22–25 Mbps；画面读回和颜色转换仍占用 CPU。当前小核显和网络条件下，不承诺复杂场景稳定 60 FPS，也未以降低分辨率换取测试结果。

此前同一园区的纯 CPU 1080p 短时测试平均约 17.0 FPS；优化 VP8 管线后约 22.5 FPS。这是历史参考，未与本轮 GPU 做严格同步的受控对照。

## 交付及验证范围

前端测试与构建、Go 测试和 vet、Linux 交叉编译测试均通过。Docker Desktop 验证覆盖 CPU 1080p 预加载、健康检查、账号更新、配置初始化、停止与重启持久化。Intel GPU 额外在上述 Linux 实机验证了预加载、硬件 renderer、H.264 接收、1080p 尺寸和拖拽交互。

NVIDIA 路径和编码插件已包含，但当前服务器没有 NVIDIA 显卡，没有完成 NVIDIA 实机验证；宿主机需安装驱动与 NVIDIA Container Toolkit。Linux 原生 GPU 路径已编译验证，完整硬件运行验收此次覆盖的是 Docker 版本。旧代 Intel 和 AMD 未纳入本次验证。

平台监控中的 GPU 百分比仍使用 NVIDIA `nvidia-smi`，暂未接入 Intel 百分比采样；监控显示“其他厂商暂未接入”不代表 Intel 绘制没有生效，以上 renderer、DRM 和视频数据是本次依据。

服务器新版网关已部署并处于 healthy。临时 GPU 测试项目和容器已删除；原“智慧园区3D”的配置及停止状态保留，`data/config.env` 和 `.env` 与备份一致；磁盘监控仍读取 4 个分区且没有采集错误。备份位于 `/root/GoStrem3D/backups/gpu-20261009-154343/`。

使用原项目时，在编辑窗口选择“本机 GPU”并保存，再启动预加载。建议先用 30 FPS；60 FPS 可以使用，但本轮尚未稳定达标。新 Docker 包同时包含网关、CPU/GPU worker 标签、启动配置、SHA-256 和实测证据。
