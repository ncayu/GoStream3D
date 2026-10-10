# GoStream3D Gateway — 交互式 3D 云渲染网关

GoStream3D 在服务器 Chromium 中加载和渲染 WebGL 网页，通过 WebRTC 将画面发送到观看端并回传键鼠操作。适合数字孪生、园区、工厂、BIM 与大屏展示：观看端使用浏览器，原始模型和纹理由服务器浏览器加载。

**本镜像提供管理后台和渲染调度；配套 [Chromium Worker](https://hub.docker.com/r/ncayu8847/gostream3d-neko-chromium) 已同时支持 Linux CPU、NVIDIA GPU 与 Intel GPU。两个仓库分别是网关和渲染器，部署时需要成套使用。**

*Interactive WebGL cloud rendering over WebRTC. Admin gateway, project management and isolated browser orchestration; pair with the CPU/GPU Chromium worker.*

## 版本与要求

| 组件 | 固定版本，推荐部署使用 | 滚动标签 |
| --- | --- | --- |
| 网关 | `ncayu8847/gostream3d-gateway:20261010-110721` | `latest` |
| CPU / GPU 渲染器 | `ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721` | `3.1.5`、`latest` |

平台为 **Linux amd64 / x86_64**。需要 Docker Engine 25+、Compose 2.20+，建议具备 systemd 的 Linux 宿主机。WSL2 NVIDIA 有专门适配。当前没有 ARM 镜像；Windows 原生程序属于另一种发行方式。

## 功能亮点

- **项目与观看链接**：创建、编辑、启停项目；每路独立浏览器和键鼠操作。
- **预加载与并发管理**：提前加载页面、模型和纹理；项目配额、服务器总通道数、空闲预加载回收与优先保留。
- **Linux 硬件渲染**：NVIDIA WebGL + NVENC H.264、Intel WebGL + VA-API H.264；保留纯 CPU 模式。GPU 预加载检查实际 WebGL renderer，拒绝 llvmpipe / SwiftShader 软件回退。
- **大屏画质**：GPU 自适应按物理像素同步观看尺寸，设备像素比最高 2，尺寸按比例限制在 3840×2160；NVIDIA 编码预算随实际分辨率和目标帧率调整，保留固定分辨率。
- **通用 WebGL2 深度优化**：Linux GPU 模式为兼容的 16 位深度渲染缓冲提升到 24 位，减少远景遮挡错误和拖动时的深度闪烁；适用于 HTTP(S) 平台链接、跨域及嵌套 iframe，保留兼容的多重采样。使用 16 位深度纹理的上下文保留原格式，避免深度复制失败。
- **交互优化**：减少旧帧积压、合并拥堵时的鼠标移动、修正滚轮方向和步长、隐藏视频中的远端鼠标。
- **资源监控**：整机 CPU、内存、磁盘分区、网络；NVIDIA 型号、显存、GPU 和视频编码器使用率（取决于驱动字段）。
- **逐路画面性能**：Linux CPU/GPU WebRTC 连接的接收 / 显示 FPS、真实尺寸、编码、码率、解码、缓冲、RTT、区间丢包 / 丢帧。
- **资源控制与维护**：Linux cgroup v2 共享 CPU 上限、管理员账号修改、项目备份与恢复、配置和数据持久化。

深度优化由配套网关安装，覆盖 WebGL2 页面上下文；WebGL1、WebGPU 和 Web Worker 内的渲染上下文保持原行为。场景自身的时域抗锯齿残影、重叠模型或材质问题仍需按场景排查。

逐路性能来自观看浏览器，每 3 秒报告；没有观看连接时不产生这类数据，过期数据会清除。解码 / 缓冲 / RTT 不等于完整操作到画面的延迟。GPU 百分比目前采集 NVIDIA；Intel 硬件渲染与编码支持不代表已接入 Intel 利用率趋势。

## 在线部署

先拉取**两种镜像**。网关动态创建 Worker，Compose 只定义网关；`docker compose pull` 不会拉取没有定义为服务的 Worker。

```bash
mkdir -p gostream3d/data
cd gostream3d
sudo docker pull ncayu8847/gostream3d-gateway:20261010-110721
sudo docker pull ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721
```

保存以下内容为 `docker-compose.yml`：

```yaml
services:
  gateway:
    image: ncayu8847/gostream3d-gateway:20261010-110721
    network_mode: host
    init: true
    restart: unless-stopped
    stop_grace_period: 45s
    runtime: ${GOSTREAM3D_GATEWAY_RUNTIME:-runc}
    cgroup_parent: ${GOSTREAM3D_CGROUP_PARENT:-}
    environment:
      DOCKER_HOST: unix:///var/run/docker.sock
      GOSTREAM3D_RUNTIME: docker
      GOSTREAM3D_HTTP_PORT: ${GOSTREAM3D_HTTP_PORT:-8888}
      GOSTREAM3D_ICE_PUBLIC_IP: ${GOSTREAM3D_ICE_PUBLIC_IP:?Set viewer-reachable server IP in .env}
      GOSTREAM3D_NEKO_IMAGE: ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721
      GOSTREAM3D_NEKO_NVIDIA_IMAGE: ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721
      GOSTREAM3D_GPU_BACKEND: ${GOSTREAM3D_GPU_BACKEND:-auto}
      GOSTREAM3D_GPU_RENDER_DEVICE: ${GOSTREAM3D_GPU_RENDER_DEVICE:-}
      GOSTREAM3D_HOST_ROOT: /host/root
      GOSTREAM3D_HOST_MOUNTINFO: /host/mountinfo
      GOSTREAM3D_CGROUP_PARENT: ${GOSTREAM3D_CGROUP_PARENT:-}
      GOSTREAM3D_CPU_CGROUP_PATH: ${GOSTREAM3D_CPU_CGROUP_PATH:-}
      NVIDIA_VISIBLE_DEVICES: all
      NVIDIA_DRIVER_CAPABILITIES: utility
      LD_LIBRARY_PATH: /usr/lib/wsl/lib
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
      - ./data:/data
      - ${GOSTREAM3D_CPU_CGROUP_DIR:-./cpu-control}:/host/cpu
      - ${GOSTREAM3D_GPU_LIBRARY_DIR:-./gpu-libs}:/usr/lib/wsl:ro
      - type: bind
        source: /
        target: /host/root
        read_only: true
        bind:
          propagation: rslave
          create_host_path: false
      - /proc/1/mountinfo:/host/mountinfo:ro
```

首次部署创建 `.env`，将示例 IP 换成**观看设备可访问的服务器 IP**；已有配置时编辑原文件：

```dotenv
# 必须修改：填写观看端可达的服务器 IP，不能照抄示例
GOSTREAM3D_ICE_PUBLIC_IP=192.168.1.100
GOSTREAM3D_HTTP_PORT=8888
GOSTREAM3D_GPU_BACKEND=auto
```

```bash
chmod 600 .env
sudo docker compose config --quiet
sudo docker compose up -d --wait --wait-timeout 90 --pull never
sudo docker compose ps
sudo docker compose logs -f gateway
```

有独立 Compose 2.x 时可将 `docker compose` 换成 `docker-compose`。缺少 IP 时 Compose 会提示填写；随离线包的 `sudo bash start.sh --public-ip 服务器IP` 可设置 IP、准备共享 CPU 控制组并检测 NVIDIA 监控环境。

打开 `http://服务器IP:8888/admin`。当前版本全新初始化的管理员为 `admin` / `Admin@2026.com`，初始配置保存在宿主机 `data/config.env`；已有安装保留原账号。首次登录后在右上角“修改账号密码”设置自己的账号密码，再创建项目、填写服务器可访问的 3D 网页 URL，选择 CPU 或本机 GPU，启动预加载并打开生成的观看链接。

## 配置文件：必须修改观看端可达 IP

`GOSTREAM3D_ICE_PUBLIC_IP` 用于 WebRTC 视频连接，**用户必须按实际部署网络填写或核对**。它不是后台监听地址，也不是场景网址；只填 IP，不带 `http://`、路径或端口。示例地址 `192.168.1.100` 仅作格式参考，不是所有用户都能使用的默认地址。

| 观看环境 | 应填写的 IP |
| --- | --- |
| 同一局域网 | 观看设备能访问的服务器局域网 IP |
| VPN / WSL2 | 观看设备实际可达、已配置媒体转发的地址；不要直接照抄容器或 WSL 虚拟网卡地址 |
| 公网 / NAT | 观看端能访问的公网 IP，并配置 TCP 8888 和 UDP 52000–52023 的放行与转发 |

| 配置位置 | 用途与生效顺序 |
| --- | --- |
| Compose 同目录 `.env` | 提供 YAML 中 `${...}` 的值；仅写入 `.env` 的变量不会自动进入容器，必须被 YAML 引用 |
| `gateway.environment` | 进入网关容器，**优先于 `config.env`**；本页示例显式传入 IP、端口、CPU/GPU Worker 镜像和 GPU 后端 |
| 宿主机 `./data/config.env` | 映射为容器 `/data/config.env`；首次启动自动创建，后续启动保留；补充没有由容器环境指定的应用配置 |

以下展示自动创建文件的字段结构。**不要整段覆盖现有文件**：IP 必须改成自己的地址，两个 `<...>` 是说明占位符，应保留本机已生成的独立随机值。

```dotenv
# GoStream3D Linux configuration. Existing values are never overwritten.
# Restart after editing. For Internet viewers set the reachable public IP.
GOSTREAM3D_RUNTIME=docker
GOSTREAM3D_HTTP_HOST=0.0.0.0
GOSTREAM3D_HTTP_PORT=8888
GOSTREAM3D_ADMIN_USERNAME=admin
GOSTREAM3D_ADMIN_PASSWORD=Admin@2026.com
GOSTREAM3D_NEKO_USER_PASSWORD=<保留本机自动生成的随机值>
GOSTREAM3D_NEKO_ADMIN_PASSWORD=<保留本机自动生成的另一个随机值>
# 必须修改 / 核对；同时修改 Compose 同目录 .env 中的相同字段
GOSTREAM3D_ICE_PUBLIC_IP=192.168.1.100
GOSTREAM3D_KEEP_PRELOADED=true
GOSTREAM3D_READY_TIMEOUT=300s
GOSTREAM3D_DOCKER_HTTP_BIND_IP=127.0.0.1
GOSTREAM3D_NEKO_IMAGE=gostream3d/neko/chromium:3.1.5
```

管理员初始密码和 Worker 内部密码用途不同。两个 `NEKO_*_PASSWORD` 是网关与渲染器通信的内部凭据，由每次全新安装分别生成；不要复制其他服务器的值。本页 Compose 指定 Hub 固定版 Worker 镜像，因此优先于文件末尾自动生成的本地镜像名；离线包则提供这个本地兼容标签。

### 修改 IP 后如何生效

在部署目录备份并编辑 `.env` 和 `data/config.env`，将两处 `GOSTREAM3D_ICE_PUBLIC_IP` 设置为同一正确地址；自定义 `data` 挂载目录时编辑对应文件。使用本页 Compose 时，**实际生效值来自 `.env` 经 YAML 传入的容器环境**。

```bash
sudo chmod 600 .env data/config.env
sudo docker compose config --quiet
sudo docker compose up -d --force-recreate --wait --wait-timeout 90 --pull never gateway
sudo docker compose exec -T gateway printenv GOSTREAM3D_ICE_PUBLIC_IP
```

最后一条仅显示生效的 IP，核对它是否正确。`.env` 修改后仅执行 `docker compose restart` 不会更新已有容器环境，需按上面命令重建网关。重建会中断当前观看连接；完成后重新启动预加载并刷新观看页。后台可以打开但视频不连接时，优先检查 IP、媒体端口和 NAT 转发。

后台修改过账号后，账号保存在 `data/admin-account.json`，重启优先使用该文件；只改 `config.env` 的初始密码不会覆盖已保存的账号。正常修改请使用后台“修改账号密码”。

## NVIDIA / Intel / WSL2

| 环境 | 宿主机要求 | 渲染与编码 |
| --- | --- | --- |
| 原生 Linux NVIDIA | NVIDIA 驱动、Container Toolkit、Docker 已注册 `nvidia` runtime，显卡支持 H.264 NVENC | 硬件 WebGL + NVENC H.264 |
| Linux Intel | 实际 Intel `/dev/dri/renderD*` 节点与兼容硬编能力 | ANGLE Vulkan + VA-API H.264 |
| WSL2 NVIDIA | Windows NVIDIA 驱动、`/dev/dxg` 与 `/usr/lib/wsl` 可用、Linux Container Toolkit | Mesa D3D12 / ANGLE GL + NVENC H.264 |
| CPU | 不要求 GPU | SwiftShader WebGL + VP8 软件编码 |

NVIDIA 主机启动前向 `.env` 添加：

```dotenv
GOSTREAM3D_GPU_BACKEND=nvidia
GOSTREAM3D_GATEWAY_RUNTIME=nvidia
```

WSL2 还需添加 `GOSTREAM3D_GPU_LIBRARY_DIR=/usr/lib/wsl`。Intel 可设 `GOSTREAM3D_GPU_BACKEND=intel`；多个节点时可设 `GOSTREAM3D_GPU_RENDER_DEVICE=/dev/dri/renderD128`，填写实际节点。

网关配置 Worker 的 GPU 资源，不必另建长期运行的 GPU 服务。宿主驱动和 Toolkit 不包含在镜像中。WSL2 使用 Windows 驱动，**不要在 WSL 内安装 Linux NVIDIA 内核驱动**。`nvidia-smi` 可用还不足以证明 WebGL 和硬编均已就绪。没有 NVIDIA 的机器不要设置网关 `runtime=nvidia`。

Linux CPU 硬上限需要可写 cgroup v2 CPU 控制器、网关与 Worker 使用同一 parent。随包 `start.sh` 在 root、systemd 与 systemd cgroup driver 环境自动准备；直接 Compose 部署需先配置控制组。未配置时后台明确提示不可用。

## 画质与容量

NVIDIA 使用低延迟 H.264 VBR、质量参数和空间自适应量化；1080p30 / 2K30 / 4K30 目标码率预算分别约 18 / 32 / 72 Mbps，实际流量随内容变化。高分辨率与多路观看建议使用有充足带宽的有线网络。

自适应 4K 是尺寸支持范围，**不是稳定 4K30 / 4K60 的性能承诺**。帧率取决于 GPU、显存、场景、并发与网络；H.264 / NV12 为有损编码，不保证逐像素无损。以最终大屏和场景验收后设置通道数；复杂 3D 推荐 GPU。

历史实机验证覆盖 Intel Iris Xe / Linux 的 1080p 园区，以及 RTX 3050 Ti Laptop / WSL2 的 NVIDIA WebGL、NVENC、园区与观看端统计。这些不代表本次包在所有 GPU 上均完成验收；本次构建和容器回归以随包 `VERIFICATION.json` 为准。

## 端口、数据与升级

| 项目 | 默认值 / 说明 |
| --- | --- |
| 网关入口 | TCP `8888`：后台、观看页、信令 |
| WebRTC 视频 | UDP `52000-52023`：防火墙、安全组和 NAT 需同步放行 |
| Worker 管理 | HTTP `30000-30002`、CDP `31000-31002`，默认只绑定宿主机回环 |
| 数据 | `./data`：配置、管理员凭据、项目；备份整个目录及 `.env` |

host 网络无需 `-p 8888:8888`。只开放 TCP 可能出现后台正常但视频不连接；公网 / NAT 应显式设置可达 IP 并转发媒体端口，脚本的 LAN IP 检测不替代公网配置。

每个 Docker daemon 只部署一个网关。网关挂载 Docker socket 管理 Worker，并只读挂载宿主根目录采集统计；它能控制宿主机 Docker，请部署在受信任的主机上。后台对外服务建议配置 HTTPS 与访问保护。

升级前备份 `data/` 与 `.env`；成套拉取网关和 Worker，修改 Compose 中网关及**两个 Worker 环境变量**的固定版本，再执行 `up -d --wait --pull never`。重启会重建实例，观看端需要重新连接；项目与凭据保留。后台与观看页升级后 Ctrl+F5。

已有离线包使用本地镜像名时，按随包 `docker-hub-deploy.md` 设置网关、CPU Worker、GPU Worker 三个兼容标签。旧 `start.sh` 会重新导入包内 `images.tar`，在线升级请避免重新导入旧归档。

相关文档：[Worker Overview](https://hub.docker.com/r/ncayu8847/gostream3d-neko-chromium)、[Docker Compose](https://docs.docker.com/compose/install/)、[NVIDIA Container Toolkit](https://docs.nvidia.com/datacenter/cloud-native/container-toolkit/latest/install-guide.html)、[NVIDIA WSL](https://docs.nvidia.com/cuda/wsl-user-guide/index.html)。
