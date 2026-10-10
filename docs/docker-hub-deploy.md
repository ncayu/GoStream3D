# Docker Hub 在线部署 / 升级 GoStream3D

版本：20261010-110721。平台 Linux amd64。两个仓库分别为管理网关与 CPU / NVIDIA / Intel 通用渲染器。

| 用途 | 固定版本 |
| --- | --- |
| 网关 | `ncayu8847/gostream3d-gateway:20261010-110721` |
| CPU / GPU | `ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721` |

网关提供 `latest`，Worker 提供 `3.1.5` 与 `latest`；生产与回滚建议固定配套版本。

## 已有安装包在线升级

先备份整个 `data/`、`.env` 与原 Compose，再拉取两个镜像，并设置 CPU、GPU **全部三个本地兼容标签**：

```bash
sudo docker pull ncayu8847/gostream3d-gateway:20261010-110721
sudo docker pull ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721
sudo docker tag ncayu8847/gostream3d-gateway:20261010-110721 gostream3d/gateway:latest
sudo docker tag ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721 gostream3d/neko/chromium:3.1.5
sudo docker tag ncayu8847/gostream3d-neko-chromium:3.1.5-20261010-110721 gostream3d/neko/nvidia-chromium:3.1.5
sudo docker compose -f docker-compose.yml config --quiet
sudo docker compose -f docker-compose.yml up -d --wait --wait-timeout 90 --pull never
sudo docker compose -f docker-compose.yml ps
```

Compose 2.x 独立程序可改用 `docker-compose`。在线升级不要运行旧包 `start.sh`：它会重新导入旧 `images.tar` 并覆盖刚设置的标签。

如果还需准备新的 CPU 控制组或 NVIDIA 监控，先更新为本次包的 Compose 和脚本，备份旧归档并将其移出启动目录，再运行当前 `sudo bash start.sh --public-ip 服务器IP`，使用已经拉取的本地镜像。保留原 `.env` 和数据。

## 新部署与 GPU

新部署可直接使用 [网关 Overview](https://hub.docker.com/r/ncayu8847/gostream3d-gateway) 的完整 Compose 示例，直接引用 Hub 固定镜像，CPU/GPU 两个变量均指向通用 Worker。

离线方式：解压本次 ZIP 后，在包目录执行 `sudo bash start.sh --public-ip 服务器IP`。脚本校验、导入 `images.tar`，准备 Compose、共享 CPU 控制组和 NVIDIA 监控。省略 IP 只适用于能自动获取可达 LAN 地址的环境；公网 / NAT 需显式填写 IP。

- NVIDIA Linux 需要宿主驱动、Container Toolkit、Docker `nvidia` runtime 与 H.264 NVENC 支持。
- WSL2 使用 Windows NVIDIA 驱动，确认 DXG 和 WSL 图形库；不要在 WSL 安装 Linux NVIDIA 内核驱动。
- Intel 需要实际 render 节点与 VA-API H.264 编码；GPU 利用率趋势目前采集 NVIDIA。
- GPU 自适应最高按比例限制在 3840×2160，NVIDIA 按尺寸和帧率更新编码预算。4K 是尺寸支持范围，不承诺所有场景稳定 4K30 / 60；H.264 为有损压缩。
- Linux GPU 通用 WebGL2 深度优化覆盖其他平台 HTTP(S) 链接、跨域与嵌套 iframe，保留多重采样和深度纹理兼容性；详见随包 linux-gpu-depth-stability.md。
- Linux CPU/GPU 逐路 WebRTC 性能由真实观看端 3 秒报告，无连接与过期数据不会作为有效测量。
- CPU 共享硬上限需要可写 cgroup v2 与相同 parent，启动脚本在 root / systemd / systemd cgroup driver 环境自动配置。

后台 `http://服务器IP:8888/admin`，当前全新初始化的账号为 `admin` / `Admin@2026.com`，初始配置在 `data/config.env`；首次登录后修改账号密码。两个 Worker 内部密码分别随机生成，保留本机生成值。已有数据保留原账号和项目；后台修改后的账号以 `data/admin-account.json` 为准。升级后后台与观看页 Ctrl+F5。

`GOSTREAM3D_ICE_PUBLIC_IP` 必须由部署者改为观看端可达的服务器 IP（局域网地址或公网地址，仅填写 IP）。Compose 的容器环境优先于 `data/config.env`：请同步核对 `.env` 和 `data/config.env`，修改 IP 后执行 `sudo docker compose up -d --force-recreate --wait --wait-timeout 90 --pull never gateway`，不能仅使用 `restart` 更新容器环境。随后重新预加载并刷新观看页。完整字段示例和生效顺序见网关 Overview。

默认开放 TCP 8888 与 UDP 52000–52023。管理接口只绑定回环；只开放后台端口不代表媒体可达。每个 Docker daemon 一个网关，网关通过 Docker socket 控制 Worker。

仓库：[网关](https://hub.docker.com/r/ncayu8847/gostream3d-gateway)、[通用渲染器](https://hub.docker.com/r/ncayu8847/gostream3d-neko-chromium)。`gateway-overview.md`、`worker-overview.md` 与 Hub 同步；完整部署、画质、GPU、资源和逐路性能见随包 README 及 Linux 专题文档。
