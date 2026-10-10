# GoStream3D Chromium Worker — Linux CPU / NVIDIA / Intel 渲染器

这是 [GoStream3D Gateway](https://hub.docker.com/r/ncayu8847/gostream3d-gateway) 的浏览器渲染镜像，包含适配的 Neko 3.1.5、Chromium、Xorg、输入驱动及 GStreamer。它在服务器加载 WebGL 网页，提供 WebRTC 视频和键鼠交互。

**仓库名称保留 `gostream3d-neko-chromium`，当前镜像已经同时支持 CPU 与 GPU，不需要另找 NVIDIA 专用仓库。** 模式由网关按项目选择，设备、驱动库、编码管线和端口由网关配置。

*Chromium / Neko worker for interactive WebGL streaming. CPU rendering, NVIDIA NVENC and Intel VA-API on Linux amd64; managed by GoStream3D Gateway.*

## 标签与配套网关

| 镜像 | 推荐固定版本 | 滚动标签 |
| --- | --- | --- |
| 本渲染器 | `ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721` | `3.1.5`、`latest` |
| 网关 | `ncayu8847/gostream3d-gateway:20261010-110721` | `latest` |

平台 **Linux amd64 / x86_64**，配合网关需要 Docker Engine 25+。前缀 `3.1.5` 表示基于 Neko 的版本，后面的时间戳为定制构建；生产和回滚建议固定配套版本。

## 渲染与编码

| 模式 / 环境 | WebGL | 视频编码 | 宿主依赖 |
| --- | --- | --- | --- |
| CPU | Chromium SwiftShader | VP8 软件编码 | Docker |
| NVIDIA / Linux | ANGLE 硬件后端 | NVENC H.264 | NVIDIA 驱动与 Container Toolkit |
| NVIDIA / WSL2 | Mesa D3D12 / ANGLE GL | NVENC H.264 | Windows NVIDIA 驱动、DXG / WSL 图形库、Linux Toolkit |
| Intel / Linux | ANGLE Vulkan | VA-API H.264 | Intel render 节点与兼容硬编能力 |

GPU 模式检查实际 WebGL renderer，遇到 llvmpipe / SwiftShader 会报告错误，避免把 CPU 回退当成 GPU 成功。NVIDIA 显卡需支持 H.264 NVENC；工具可见显卡不等于绘制与硬编都已就绪。AMD 与 ARM 不在当前支持范围。

## 功能更新

- **大屏画质**：GPU 自适应按物理像素同步，最高按比例限制在 3840×2160，保留固定分辨率；不统一压缩到 720p。
- **NVIDIA 编码**：NVENC H.264 VBR、质量参数、空间自适应量化，预算随实际宽高和目标帧率更新；低延迟配置关闭 B 帧与前瞻。
- **通用 WebGL2 深度优化**：配套新网关在 Linux GPU 页面、跨域及嵌套 iframe 中提升兼容的深度渲染缓冲精度，并保留多重采样和深度纹理兼容性，减少远景遮挡错误与深度闪烁；不限于 ThingJS。单独启动 Worker 不会安装网关的页面优化。
- **交互**：单帧队列优先最新画面，拥堵时合并鼠标移动；修正滚轮方向与步长，隐藏视频中的远端鼠标。
- **逐路性能**：配合新网关和观看页，Linux CPU/GPU WebRTC 展示接收 / 显示 FPS、尺寸、编码、码率、解码、缓冲、RTT、丢包 / 丢帧。
- **独立会话与预加载**：每路独立 Chromium，提前加载网页，由网关调度。

深度优化覆盖 WebGL2 页面上下文；WebGL1、WebGPU 和 Web Worker 内的渲染保持原行为。场景自身的时域残影、重叠模型或材质问题需要单独排查。

H.264 / NV12 是有损压缩。4K 是尺寸支持范围，不保证复杂场景稳定 30 / 60 FPS；请用最终场景、显卡、显存、并发和网络验收。高分辨率多路观看增加 GPU、CPU、显存与带宽开销。

## 推荐：由网关管理

```bash
sudo docker pull ncayu8847/gostream3d-gateway:20261010-110721
sudo docker pull ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721
```

按 [网关 Overview](https://hub.docker.com/r/ncayu8847/gostream3d-gateway) 的完整 Compose 示例启动。网关中的**两个环境变量都指向本镜像**：

```dotenv
GOSTREAM3D_NEKO_IMAGE=ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721
GOSTREAM3D_NEKO_NVIDIA_IMAGE=ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721
GOSTREAM3D_GPU_BACKEND=auto
```

变量需进入**网关容器环境**，只写进 Compose 的 `.env` 而没有在 YAML 引用不会自动传入容器；网关完整示例已配置。正式部署无需再定义长期 Worker 服务，网关动态创建、停止实例。

打开 `http://服务器IP:8888/admin`，创建项目、填写服务器可访问的 3D 网页 URL，选择 CPU / 本机 GPU，配置独立会话与预加载。

### 网关配置、IP 与内部密码

**部署者必须修改 / 核对 `GOSTREAM3D_ICE_PUBLIC_IP`，使其为观看设备实际可达的服务器 IP**。同局域网使用可达的服务器 LAN IP；公网观看使用公网 IP，并放行 / 转发媒体端口。不要照抄示例地址、容器 IP 或仅在 WSL 内可达的虚拟地址；填写纯 IP，不带协议或端口。

网关的宿主机配置为 `./data/config.env`，容器内为 `/data/config.env`。它由网关首次启动创建，升级和重启保留。**容器环境变量优先于该文件**：使用网关 Overview 的 Compose 时，应同步修改 Compose 同目录 `.env` 与 `data/config.env` 中的 IP，并重建网关；只修改文件或只执行 `restart` 不能更新 Compose 已传入的旧 IP。

```dotenv
# 在网关 Compose 同目录 .env 设置，必须改为自己的可达 IP
GOSTREAM3D_ICE_PUBLIC_IP=192.168.1.100
```

```bash
sudo docker compose config --quiet
sudo docker compose up -d --force-recreate --wait --wait-timeout 90 --pull never gateway
sudo docker compose exec -T gateway printenv GOSTREAM3D_ICE_PUBLIC_IP
```

重建会中断当前观看连接，完成后重新启动预加载并刷新观看页。完整 `config.env` 字段示例、配置优先级与 IP 选择见 [网关 Overview](https://hub.docker.com/r/ncayu8847/gostream3d-gateway)。

当前全新初始化的后台账号为 `admin` / `Admin@2026.com`，首次登录后请在后台修改；已有安装保留原账号。`GOSTREAM3D_NEKO_USER_PASSWORD` 与 `GOSTREAM3D_NEKO_ADMIN_PASSWORD` 是自动生成的两份独立 Worker 内部凭据，不能当作后台登录密码，也不要复制其他服务器的值。由网关管理的 Worker 无需单独创建 `config.env`，网关会传入对应配置。后台修改后的账号保存在网关 `data/admin-account.json`，优先于初始登录配置。

### GPU 环境

- **NVIDIA Linux**：宿主安装驱动与 Container Toolkit，Docker 注册 `nvidia` runtime。网关可选 `GOSTREAM3D_GPU_BACKEND=nvidia`；监控需网关 `runtime=nvidia` 与 `utility` 能力，Worker 绘制与编码能力由网关请求。
- **WSL2 NVIDIA**：使用 Windows 显卡驱动，确认 `/dev/dxg`、`/usr/lib/wsl` 可用，安装 Linux Toolkit。网关需只读看到宿主根目录与 WSL 图形库；不要在 WSL 安装 Linux NVIDIA 内核驱动。
- **Intel Linux**：确认实际 render 节点及 VA-API H.264 编码可用。网关映射选定节点并配置设备组；可用 `GOSTREAM3D_GPU_RENDER_DEVICE` 指定节点。
- GPU 百分比与显存由网关采集，目前接入 NVIDIA；Intel 利用率趋势不可用不代表 Intel 渲染未生效。

宿主无需安装 Go、Node.js 或 Chromium；宿主显卡驱动与 Toolkit 不属于镜像内置软件。

## 可选：单独检查 CPU Worker

独立启动用于检查健康状态与 Chromium CDP。HTTP 根路径不包含 GoStream3D 管理后台，也不能单独生成正式观看链接；正式观看与 GPU 部署请使用网关。

在新的调试目录中执行，替换网页和 IP；已有 `worker.env` 时编辑原文件：

```bash
umask 077
cat > worker.env <<EOF
GOSTREAM3D_TARGET_URL=https://example.com
GOSTREAM3D_RENDER_MODE=cpu
NEKO_MEMBER_PROVIDER=multiuser
NEKO_MEMBER_MULTIUSER_USER_PASSWORD=$(openssl rand -hex 24)
NEKO_MEMBER_MULTIUSER_ADMIN_PASSWORD=$(openssl rand -hex 24)
NEKO_DESKTOP_SCREEN=1920x1080@30
NEKO_WEBRTC_EPR=52100-52107
NEKO_WEBRTC_ICELITE=true
NEKO_WEBRTC_NAT1TO1=192.168.1.100
EOF

sudo docker run -d \
  --name gostream3d-worker-debug \
  --init --shm-size 2g --memory 4g \
  --env-file worker.env \
  -p 127.0.0.1:8081:8080 \
  -p 127.0.0.1:9222:9222 \
  -p 52100-52107:52100-52107/udp \
  ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721

curl -fsS http://127.0.0.1:8081/health
curl -fsS http://127.0.0.1:9222/json/version
sudo docker logs gostream3d-worker-debug
```

CDP 能控制浏览器，请保留 HTTP/CDP 回环绑定。调试结束后停止并删除该调试容器；它不会自动加入网关。正式默认媒体端口 UDP 52000–52023，调试示例使用不同端口段，仍需确认主机没有其他占用。

## 升级与验证

内部密码、页面 URL、屏幕和端口由网关管理，不要长期手工修改网关生成的容器。管理员和项目数据保存在网关挂载的 `data/`；升级需成套更新网关与 Worker，并备份数据。

离线包 `GoStream3D-docker-compose-linux-amd64-20261010-110721.zip` 包含网关、通用 Worker、CPU/GPU 本地兼容标签、启动脚本、部署说明、SHA-256 和构建回归记录。Hub 与离线包对应同一构建；已有安装包在线升级见随包 `docker-hub-deploy.md`。

历史实机验收覆盖 Intel Iris Xe / Linux 的 1080p 园区，NVIDIA RTX 3050 Ti Laptop / WSL2 的硬件 WebGL、NVENC、园区观看及不同视频尺寸。本次构建回归以 `VERIFICATION.json` 为准；其他 GPU 型号、原生 Linux NVIDIA 的完整观看验收、最终大屏与并发容量需目标环境测试。

相关文档：[网关与完整部署](https://hub.docker.com/r/ncayu8847/gostream3d-gateway)、[NVIDIA Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)、[NVIDIA WSL](https://docs.nvidia.com/cuda/wsl-user-guide/index.html)。
