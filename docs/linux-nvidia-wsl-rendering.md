# Linux / WSL2 NVIDIA GPU 渲染修复与验收

验证日期：2026-10-09。镜像版本：20261009-182507。安装包时间戳：20261009-182915。

服务器：192.168.1.100，WSL2 Ubuntu 24.04，NVIDIA GeForce RTX 3050 Ti Laptop GPU，4 GB 显存。软件目录：`/root/GoStrem3D`。

## 原因和修复

原日志中的 `llvmpipe` 是软件渲染器。NVIDIA Container Toolkit 可以提供 CUDA/NVENC，但不代表 Chromium 的 Vulkan 图形后端也能使用显卡。该服务器是 WSL2，图形设备是 `/dev/dxg`，使用 Windows 驱动提供的 D3D12 接口。原启动方式强制 ANGLE Vulkan，最终选中了 Mesa llvmpipe，软件正确拒绝了 CPU 回退。

新版从网关只读挂载的宿主机根目录识别 WSL。NVIDIA worker 自动映射 `/dev/dxg`、只读挂载 `/usr/lib/wsl`，通过 `GALLIUM_DRIVER=d3d12` 和 `MESA_D3D12_DEFAULT_ADAPTER_NAME=NVIDIA` 选择 NVIDIA，设置驱动库路径，改用 `--use-gl=angle --use-angle=gl`。WSL 的 GPU 请求使用 `all`。原生 Linux 和 Intel 路径保留原有方式。缺少 DXG 或 D3D12 驱动库会报告具体路径，不会偷偷使用 CPU。

项目分辨率和目标帧率保留。NVIDIA 编码管线使用 GStreamer `nvh264enc`（NVENC），NV12、H.264 constrained-baseline、无 B 帧及单帧队列。仍然验证 WebGL renderer，继续拒绝 llvmpipe/SwiftShader。

WSL 需要 Windows NVIDIA 驱动、WSL 图形支持和 Linux NVIDIA Container Toolkit；不要在 WSL 里安装 Linux NVIDIA 内核驱动。相关机制见 [Mesa D3D12 官方文档](https://docs.mesa3d.org/drivers/d3d12.html)、[Microsoft WSL GPU 选择说明](https://github.com/microsoft/wslg/wiki/GPU-selection-in-WSLg) 及 [ANGLE 后端选择文档](https://chromium.googlesource.com/angle/angle/+/main/doc/DebuggingTips.md)。

## 实机证据

在实际“智慧园区3D大屏”项目中，通过 Chromium DevTools 查询 WebGL2 得到：

```text
ANGLE (Microsoft Corporation, D3D12 (NVIDIA GeForce RTX 3050 Ti Laptop GPU), OpenGL 4.6)
```

服务器保留原项目配置：自适应分辨率、30 FPS、多路独立、最大 3 路、预加载 1 路。预加载完成后为运行状态，准备好的实例数为 1，无预加载错误。

Windows Edge 观看端实际视频为 **1920×1080，video/H264**。另建相同园区场景的临时项目，固定 1080p30，等待模型加载后进行 5 次拖动，记录约 30 秒播放结果：

| 指标 | 实测值 |
| --- | --- |
| 平均播放帧率 | 29.55 FPS |
| 5 次采样帧率 | 29.42 / 29.22 / 29.91 / 29.61 / 29.60 FPS |
| 解码丢帧累计值 | 0 |
| RTP 网络丢包累计值 | 0 |
| 编码 | H.264，通过配置的 NVENC 管线 |
| 画面 | 园区模型完整显示，拖动后视角发生变化 |

重启网关后，原项目自动恢复 GPU 预加载；再次查询得到相同 NVIDIA D3D12 renderer，网关健康状态为 `healthy`。配置文件及 `.env` 与升级前备份一致。临时项目和探针容器已删除，仅保留原项目运行。

此结果针对这台 RTX 3050 Ti / WSL2 服务器和该园区场景的短时 30 FPS 测试，不代表已验证 60 FPS、多用户并发上限、音频、交互延迟或整台服务器重启。原生 Linux NVIDIA 路径保留，但本次实机验证覆盖 WSL2 Docker。绘制与编码使用 GPU 后，截图读回和色彩转换仍可能占用 CPU。

## 验证与回滚

完整 Go 测试、Linux amd64 编译通过。新增测试覆盖 WSL 设备/只读挂载、NVIDIA 适配器选择、原生 Linux 行为以及缺少驱动库时失败。容器回归覆盖 CPU 预加载、登录、账号变更、项目持久化及容器重建。

随包 `NVIDIA_GPU_VERIFICATION.json` 和 `nvidia-proof/` 保存实际 renderer、设备映射、编码管线、重启检查、观看端统计、帧率原始数据及截图。镜像归档经 SHA256 校验后导入。

服务器升级前备份：`/root/gostream3d-backup-nvidia-20261009-182119/`。旧镜像保留为 `gostream3d/gateway:before-wsl-nvidia` 和 `gostream3d/neko/nvidia-chromium:before-wsl-nvidia`。回滚时将旧标签重新指向当前网关、CPU/GPU worker 标签，并执行 `docker-compose up -d --force-recreate --pull never`；备份目录包含配置和数据，应按需要恢复，不要删除现有数据。

后台：[http://192.168.1.100:8888/admin](http://192.168.1.100:8888/admin)。
