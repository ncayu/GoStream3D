# Linux docker-compose 一键启动安装包

本次交付为 `GoStream3D-docker-compose-linux-amd64-YYYYMMDD-HHMMSS.zip`，文件名末尾为北京时间构建时间戳，包含 `docker-compose.yml`、一键启动脚本 `start.sh`、配置示例和离线镜像归档 `images.tar`。适用于 x86_64 的 Ubuntu 22.04 / openEuler 22.03；服务器无需安装 Go、Node.js、Chromium 或 Xorg。

网关镜像为 `gostream3d/gateway:latest`，另附构建时间版本标签。网关负责管理项目、提供后台及观看链接；每个渲染实例使用 `gostream3d/neko/chromium:3.1.5`。两种镜像在同一个归档中，不必分别下载。

## 一条命令启动

前提：本机 Docker Engine 25+、Compose 2.20+ 已安装且 Docker 服务正在运行。脚本优先使用 `docker-compose`，没有兼容版本时自动尝试 `docker compose`。带连字符的命令也可以是新版独立程序，见 [Docker 官方独立版说明](https://docs.docker.com/compose/install/standalone/)；旧的 Python Compose 1.x 请先升级。需要插件时，参见 [Compose Linux 安装说明](https://docs.docker.com/compose/install/linux/)。之前提供的专用静态 Docker 不含 Compose，需先补装。

上传 ZIP 与对应的 `.zip.sha256` 文件，将下面时间戳替换为下载文件的实际时间戳，校验并解压：

```bash
archive=GoStream3D-docker-compose-linux-amd64-YYYYMMDD-HHMMSS.zip
sha256sum -c "$archive.sha256"
mkdir gostream3d-docker
unzip "$archive" -d gostream3d-docker
cd gostream3d-docker
sudo bash start.sh --public-ip 192.168.1.100
```

把 IP 换成观看设备可达的服务器 IP。局域网用局域网 IP，外网用公网 IP；不要填 URL。也可直接执行 `sudo bash start.sh`，脚本在缺少 IP 时读取宿主机默认路由的 IPv4 地址用于局域网观看；这不会自动识别 NAT 公网地址，外网访问仍需明确填写 `--public-ip`。

脚本校验并导入镜像、补齐 `.env` 中缺失或空的 IP、HTTP 端口和 Docker socket，启动网关并等待健康检查通过。已有有效值、密码和项目数据保留；显式传入 `--public-ip` 或 `--http-port` 时仅更新对应配置项，修改已有 `.env` 前生成 `.env.before-时间戳.随机后缀` 备份。最终显示实际管理地址。

如果直接执行 `docker-compose -f docker-compose.yml up -d` 提示缺少 `GOSTREAM3D_ICE_PUBLIC_IP`，先执行启动脚本初始化或补齐 `.env`。例如服务器为 `192.168.1.100`，且 8888 被其他服务占用时：

```bash
sudo bash start.sh --public-ip 192.168.1.100 --http-port 10088
# 管理地址：http://192.168.1.100:10088/admin
```

脚本会在端口被占用且本项目尚未运行时给出可操作的错误提示。选择空闲端口即可，无需停止其他服务。之后在同目录运行 `docker-compose -f docker-compose.yml up -d` 会读取已保存的 `.env`。

打开脚本显示的管理地址。初始管理员账号默认 `admin`，初始密码与账号配置保存在：

```bash
sudo cat data/config.env
```

`data/` 整个目录持久化到容器 `/data`，内含配置和项目数据子目录。备份时保留整个 `data/` 及 `.env`。镜像与 ZIP 均不包含开发机密码或项目数据。

登录后点击右上角用户名或头像 →“修改账号密码”，验证当前密码并填写新账号、新密码及确认新密码。保存后重新登录，新账号密码立即生效，重启或重建网关容器后继续保留。新账号以带盐密码哈希保存在会话数据目录的 `admin-account.json`，优先于初始配置；不能删除持久化的 `data/` 目录。详细规则和忘记密码的恢复步骤见随包 `admin-account.md`。

服务器防火墙、云安全组和 NAT 默认需放行 **TCP 8888、UDP 52000–52023**。只开 8888 会导致后台可用而视频连接失败；增加总通道数后须同步扩展 UDP 段。

## 日常操作

Linux CPU 共享配额及 NVIDIA 后台监控需要新版 `sudo bash start.sh` 准备控制组、驱动 runtime 并重建网关。配置完成后，后台可以即时保存 CPU 上限，GPU 卡片与趋势按采样间隔更新。详见 [Linux 资源控制说明](linux-resource-control.md)。

```bash
sudo docker-compose ps
sudo docker-compose logs -f gateway
sudo docker-compose stop
sudo docker-compose start
sudo docker-compose restart gateway
```

服务设置了 `restart: unless-stopped`，Docker 开机启动后会恢复此前未手动停止的网关。网关正常停止时会释放自己创建的渲染容器；项目数据保留。修改 `data/config.env` 后执行 `restart`；修改 `.env` 的 IP、HTTP 端口、镜像或挂载路径后执行 `sudo docker-compose up -d --wait`。直接启动已有安装也可使用 `sudo docker-compose up -d`。插件方式将上述命令中的 `docker-compose` 换成 `docker compose`。

采用专用 Docker socket 时，后续管理命令也需指向同一引擎，例如：

```bash
sudo env DOCKER_HOST=unix:///run/gostream3d-docker.sock \
  docker-compose ps
```

`start-docker.sh` 会优先使用系统 socket；系统 socket 不存在时尝试之前安装的 `/run/gostream3d-docker.sock`。也可显式传 `GOSTREAM3D_DOCKER_SOCKET`。复用引擎、网关和数据时，应停止旧的原生/systemd 网关再启动新版。同一 Docker 引擎只运行一个 GoStream3D 网关，避免两个网关清理或抢占同一组渲染容器。

## 手动导入与启动

也可以不用启动脚本，直接执行：

```bash
sha256sum -c images.tar.sha256
sudo docker load -i images.tar
cp docker.env.example .env
# 编辑 .env，填写服务器实际 IP（示例中的 IP 留空，不能直接启动）。
sudo docker-compose up -d --wait --wait-timeout 90 --pull never
```

Compose 使用 Linux `host` 网络，以便网关访问渲染容器映射到宿主机回环地址的 HTTP/CDP 端口；不要随意改成 bridge 并只映射 8888。Docker 的平台支持说明见 [host 网络官方文档](https://docs.docker.com/engine/network/drivers/host/)。网关需要本地 Docker socket 来管理渲染实例，该权限能控制 Docker 宿主机；Compose 未设置 `privileged`。

HTTP 端口、观看 IP、可选的镜像和宿主机挂载路径放在 `.env`；账号、Neko 内部密码、通道与渲染参数放在 `data/config.env`。Compose 的网络环境值优先于文件配置。

平台监控通过只读挂载 `/` 到 `/host/root`、`/proc/1/mountinfo` 到 `/host/mountinfo` 获取宿主机磁盘分区和容量。页面仍显示宿主机路径，如 `/`、`/boot`、`/data`；同一文件系统的绑定挂载或子卷只计一次，排除内存盘、Snap 镜像及 Docker overlay 子挂载。`rslave` 传播用于观察宿主机新增挂载，需要 Linux 根挂载为 shared/slave（可用 `findmnt -n -o PROPAGATION /` 检查）。升级旧版时需要更新 Compose 并执行 `docker-compose up -d --wait` 重建网关，仅替换镜像或 `restart` 不会添加挂载。Linux 原生二进制读取自身挂载信息，无需配置上述变量。

## 开发机重新制作

Windows 开发机有 Go、Node.js 和 Docker 时：

```powershell
powershell -File scripts/package-docker-release.ps1 -MirrorECR -Validate
```

脚本重建前端与 Linux 网关，再根据配套源码构建同时包含 CPU/GPU 依赖的 worker 镜像，导出网关、两类 worker 标签、启动配置和校验文件。`-MirrorECR` 用 Docker Official Images 的 ECR 公共镜像获取基础镜像；无需该镜像源时可以省略。镜像仅保存在本地，不会自动推送到公共仓库。

网关还提供完整源码的容器构建入口：

```bash
docker build -f deploy/Dockerfile.gateway -t gostream3d/gateway:latest .
```

## 验证范围

### Linux GPU 模式

Linux WebRTC 的 GPU / CPU 观看连接均支持后台逐路性能测量。升级后观看页和管理页按 Ctrl+F5，保持观看页可见约 6 秒后，后台可查看实收/显示帧率、实际分辨率、视频码率、解码/缓冲、RTT 和区间丢包/丢帧。多个连接独立统计，过期数据清除；详见 [Linux 逐路性能说明](linux-stream-performance.md)。

创建或编辑项目时选择“本机 GPU”。Linux 会自动选择 Intel render 设备或 NVIDIA Container Toolkit runtime；API 中 `render_mode=nvidia` 为兼容已有项目继续保留，现代表该 GPU 入口。可在 `.env` 设置 `GOSTREAM3D_GPU_BACKEND=intel|nvidia|auto`，修改后重建网关。

镜像包同时提供 `gostream3d/neko/nvidia-chromium:3.1.5` GPU 标签及 CPU 标签，两者使用包含 Vulkan/Mesa、Intel iHD、GStreamer 的相同运行环境。只有 GPU worker 映射显卡：Intel 映射一个 `/dev/dri/renderD*` 节点，并动态为服务用户添加设备所属组；NVIDIA 使用 Docker GPU device request，宿主机仍需 NVIDIA 驱动和 NVIDIA Container Toolkit。无需 `privileged`。

原生 Linux GPU 浏览器采用 ANGLE Vulkan。WSL2 + NVIDIA 自动改用 ANGLE OpenGL，通过 Mesa D3D12 使用 Windows NVIDIA 显卡；worker 同时映射 `/dev/dxg` 并只读挂载 `/usr/lib/wsl`，设置 NVIDIA 适配器选择。WSL2 需更新 Windows NVIDIA 驱动及 WSL，安装 Linux NVIDIA Container Toolkit；不要在 WSL 内安装 Linux NVIDIA 内核驱动。启动后验证 WebGL renderer 必须是 Intel/NVIDIA，拒绝 SwiftShader/llvmpipe 软件回退。Intel 使用 `vah264lpenc` 进行 H.264 硬编，CQP 22；NVIDIA 使用 `nvh264enc`。保留项目原分辨率和帧率，未设置 CPU 720p 上限。Intel CQP 优先兼容没有 HuC 码率控制固件的设备，实际码率随画面变化，需保证网络容量。

Intel 宿主机需提供 i915/xe 驱动和 render 节点；本次目标机器为 Tiger Lake Iris Xe。旧代 Intel 或其他厂商并不据此保证支持。GPU 绘制、读回、色彩转换仍可能使用 CPU，硬件支持不等于任意场景保证 60 FPS。NVIDIA 路径需有对应硬件实测，CPU 容器测试不能代替 GPU 验收。

版本 `20261009-154343` 已在目标 Linux 服务器的 Intel Iris Xe 上实测：保持 1920×1080，30 FPS 设置平均播放 28.2 FPS，60 FPS 设置平均播放 39.7 FPS；实际接收 H.264，Chromium render 与 Neko video 引擎的 DRM 计数均增长。详细数据及验证限制见 [Linux GPU 实测记录](linux-gpu-rendering.md)。2026-10-09 在另一台 WSL2 服务器的 NVIDIA RTX 3050 Ti Laptop 上，Chromium WebGL2 探针实测返回 `ANGLE (Microsoft Corporation, D3D12 (NVIDIA GeForce RTX 3050 Ti Laptop GPU), OpenGL 4.6)`。仅 `nvidia-smi` 正常不能证明浏览器采用硬件绘制；还需检查实际项目的 WebGL renderer 和观看端 H.264 视频。WSL 图形接口说明见 [Mesa D3D12](https://docs.mesa3d.org/drivers/d3d12.html) 和 [Microsoft WSL GPU 选择](https://github.com/microsoft/wslg/wiki/GPU-selection-in-WSLg)。

版本 `20261009-182507` 已在 192.168.1.100 的 WSL2 Ubuntu / RTX 3050 Ti Laptop 上实测：1920×1080，30 FPS 设置平均播放 29.55 FPS，实际接收 H.264，采样期间解码丢帧和 RTP 丢包均为 0；网关重启后原项目恢复 NVIDIA GPU 预加载。详细数据和限制见 [NVIDIA WSL2 实测记录](linux-nvidia-wsl-rendering.md)，该版本安装包包含 `NVIDIA_GPU_VERIFICATION.json` 及原始证据。

版本 `20261009-221512` 修复 legacy 视频管线重复捕获鼠标，以及滚轮像素值误作步数、方向相反的问题，并优化鼠标输入合并、观看端视频缓冲及 NVIDIA 低延迟编码。保持 1920×1080 / 30 FPS 的简单 WebGL 点击实验，平均反馈从 76.9 ms 降至 61.5 ms；真实园区拖动采样平均播放 27.22 FPS，无解码丢帧或 RTP 丢包。这是本轮短时结果，不能外推为所有场景的同等改善。升级后请对观看页按 Ctrl+F5；详见 [Linux GPU 交互优化记录](linux-gpu-interaction.md)。

版本 `20261009-232907` 优化 NVIDIA GPU 大屏画质：GPU 自适应模式按观看端物理像素渲染，最高 3840×2160；NVENC 使用 VBR、CQ 20 和空间自适应量化，编码预算随实际分辨率及帧率重新计算，同时保留无 B 帧、无 lookahead 的低延迟设置。须同时更新网关及配套 worker 镜像，升级后观看页按 Ctrl+F5。参数、实机对比及网络预算见 [Linux GPU 大屏画质优化](linux-gpu-quality.md)。

Linux CPU 渲染采用 SwiftShader 软件绘制。自适应分辨率跟随观看窗口，沿用 3840×2160 的通用上限；已撤回 CPU 模式专用的 1280×720 上限，预加载也不再强制缩小到 720p。固定分辨率按项目选择的尺寸渲染。

CPU 视频管线采用实时 VP8 编码：2 个线程、`cpu-used=8`、更宽的量化范围，并在色彩转换和编码前用单帧队列丢弃积压旧帧，减少延迟。编码帧率跟随项目设置。这些取舍可能降低画面细节；实际帧率仍取决于场景和 CPU，不保证所有场景达到目标帧率。

此前同一园区场景的 1080p 对比中，新编码管线平均播放帧率约 22.5 FPS，旧管线本轮约 17.0 FPS。这是短时测试结果，不保证其他场景达到相同帧率；CPU 仍有较高占用。

使用 `-Validate` 制作安装包时，先运行前端测试、构建及 Go 测试，再实测容器健康检查、自动初始化 `0600` 配置、最新管理页、管理员登录、CPU 渲染项目预加载、观看页、停止渲染、重启后的项目保留、网页接口修改账号密码、旧密码/会话失效，以及重建容器后新账号继续有效。实际结果写入随包 `VERIFICATION.json`；未使用该开关时不会声称本次镜像已通过运行验证。配套 CPU 镜像包含 Neko、Chromium、Xorg 驱动、音频和编码依赖。

打包脚本的容器验证在 Docker Desktop Linux 引擎中完成，覆盖 CPU、登录和持久化。Intel/NVIDIA GPU 需在目标硬件验证绘制、编码和跨设备观看；CPU 验证不代表两类 GPU 都已验证。目标机还需验收开机恢复、音频及网络规则。当前交付为 linux/amd64，ARM 未包含。构建版本、镜像 ID 和验证说明见随包 `VERIFICATION.json`。
