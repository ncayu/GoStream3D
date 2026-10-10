# Ubuntu 22.04 / openEuler 22.03 一键部署

使用新的 `GoStream3D-linux-deploy-amd64.zip`，适用于 **x86_64/amd64、使用 systemd 的 Ubuntu 22.04 或 openEuler 22.03（含 SP 版本）**。它和旧的 `GoStream3D-linux-amd64.zip` 是不同的包。ARM 机器不能运行这份 amd64 程序。

本次交付的包已包含通过启动与 WebGL 截图测试的 CPU 镜像 `worker.tar`，安装器会自动导入，无需在服务器编译镜像。详细验证范围见随包 `VERIFICATION.md`。

这条部署路径使用 Docker：网关安装在宿主机，Neko、Chromium、Xorg、自定义驱动、PulseAudio 和 GStreamer 在统一的 Debian 运行镜像内。Ubuntu/openEuler 无需分别安装这些渲染依赖，也不需要在服务器安装 Go 或 Node.js。

## 首次安装

上传 ZIP 和配套 `.sha256` 文件到服务器，解压到一个新目录。系统缺少 `unzip` 时，Ubuntu 用 `sudo apt-get install -y unzip`，openEuler 用 `sudo dnf install -y unzip`。

```bash
sha256sum -c GoStream3D-linux-deploy-amd64.sha256
mkdir gostream3d-deploy
unzip GoStream3D-linux-deploy-amd64.zip -d gostream3d-deploy
cd gostream3d-deploy
sudo bash install-linux.sh --public-ip 192.168.1.100
```

把 `192.168.1.100` 换成**观看设备可达的服务器 IP**：局域网部署用局域网 IP，外网部署用公网 IP。不要填 URL、域名或 `0.0.0.0`。省略 `--public-ip` 时，新配置会选择一个本机私有 IPv4，找不到则回退 `127.0.0.1`；多网卡、NAT、云服务器请明确指定。

安装器会：

1. 检查系统、架构与 Docker API。已有 Docker 保留；如果没有 Docker，下载安装 Docker 官方静态版 29.8.2，创建本软件专用 `gostream3d-docker.service`。
2. 准备 CPU 渲染镜像。若包内包含 `worker.tar`，直接导入；否则用包内配套源码及 Neko vendor 构建，首次在线构建会下载基础镜像及 Debian 软件包，需要联网和足够磁盘空间，可能持续数分钟。
3. 安装程序到 `/opt/gostream3d`，首次设置管理员默认密码 `Admin@2026.com`，Neko 内部密码仍独立随机生成，已有 `config.env` 和项目数据保留。
4. 检查 Docker、镜像和配置，安装 `gostream3d.service`，启动并设置开机自启。

随后打开 `http://服务器IP:8888/admin`。管理员账号默认 `admin`，密码查看：

```bash
sudo cat /opt/gostream3d/config.env
```

该文件包含内部密码，请保管好。不要用 `config.example.env` 覆盖它。默认开放规则是 **TCP 8888、UDP 52000–52023**；云安全组、服务器防火墙以及 NAT 都要按网络环境配置。安装脚本不会自行改防火墙或安全组。只开放 8888 会导致后台可用而 WebRTC 画面连接失败。

## 后续启动、停止和日志

```bash
sudo systemctl start gostream3d
sudo systemctl stop gostream3d
sudo systemctl restart gostream3d
sudo systemctl status gostream3d --no-pager
sudo journalctl -u gostream3d -f
```

关闭 SSH 不影响服务运行。修改 `/opt/gostream3d/config.env` 后执行 `restart`。项目与平台设置保存在 `/opt/gostream3d/data/`，应连同配置备份。

部署检查（使用本软件专用 Docker 时）：

```bash
sudo env DOCKER_HOST=unix:///run/gostream3d-docker.sock \
  /opt/gostream3d/gostream3d --config /opt/gostream3d/config.env --doctor
```

复用了系统 Docker 时，改用 `DOCKER_HOST=unix:///var/run/docker.sock`。`--doctor` 检查配置、Docker 连接及 CPU 镜像，不创建项目，不代表视频与键鼠已经验收。

可先创建 CPU、单路、720p/30 FPS、预加载 1 路的测试项目。等待“已预加载”后，在另一台设备打开观看链接，验证画面、鼠标和键盘，再扩大分辨率、FPS 和路数。增加总通道数时，需要放行扩展后的 UDP 段。

## 常见安装情况

| 情况 | 处理 |
| --- | --- |
| Docker Hub 拉取失败 | 重试安装时增加 `--mirror-ecr`，从 Docker Official Images 的 ECR 公共镜像获取基础镜像；仍需访问 Debian 软件源 |
| 已有 Docker 太旧 | 当前 Go SDK 最低 API 为 1.44，即 Docker 25+；先升级已有 Docker。安装器不会直接替换现有 Docker 和容器 |
| 内网已经有 CPU 镜像 | 脚本会复用 `gostream3d/neko/chromium:3.1.5`；更新同名镜像时需主动重新构建或加载新镜像 |
| 不能在线构建 | 先在可联网机器制作镜像归档，部署时传 `--image-archive /路径/worker.tar` |
| 希望安装后暂不启动 | 增加 `--no-start`；之后用 `systemctl start gostream3d` |
| 旧的原生配置需要迁移 | 先备份配置和 data，再把 `GOSTREAM3D_RUNTIME` 改为 `docker`；保留账号及两个不同的 Neko 内部密码。安装器不会默默覆盖旧配置 |
| 迁移后报 Windows CPU 参数错误 | 删除 `GOSTREAM3D_CPU_MAX_PERCENT` 或设为 `0`；`CPU_EFFECTS` 使用 `original` |
| 开机后 Docker 未就绪 | 查看 `journalctl -u gostream3d-docker -n 100`；复用系统 Docker 则查看 `journalctl -u docker -n 100` |

例：基础镜像走 ECR 的一条安装命令：

```bash
sudo bash install-linux.sh --public-ip 192.168.1.100 --mirror-ecr
```

## 在联网机器准备镜像，供其他服务器复用

在解压后的部署包目录、具有 Docker 25+ 的 x86_64 机器执行：

```bash
docker build -f deploy/Dockerfile.worker -t gostream3d/neko/chromium:3.1.5 .
docker save -o worker.tar gostream3d/neko/chromium:3.1.5
sha256sum worker.tar > worker.tar.sha256
```

复制归档及校验文件到目标机，校验后安装：

```bash
sha256sum -c worker.tar.sha256
sudo bash install-linux.sh --public-ip 192.168.1.100 --image-archive /绝对路径/worker.tar
```

镜像归档可以免去每台服务器重复构建。要完全离线部署，目标机还须事先装好兼容 Docker；没有 Docker 时，安装器仍需要联网安装必要系统工具和下载引擎。

## 保留原生方式

原生依赖已经齐全的 Linux 安装，也可以直接运行 `./gostream3d`。新版本优先使用当前目录已有 `config.env`，否则读取程序旁的配置；缺失时自动生成。相对数据路径以配置文件所在目录为基准，无需 `source config.env`。

部署包里的 `start-linux.sh` 固定到程序目录后启动。首次解压通常需要：

```bash
chmod +x gostream3d
bash start-linux.sh
```

直接启动默认使用原生模式，不能代替上面的 Docker 安装命令；原生的 Neko/Xorg/浏览器仍需另行安装。Docker 一键路径不安装这些宿主机依赖。

## 范围与维护

这份 CPU 镜像尚不包含 NVIDIA 驱动、VirtualGL 和 GPU 镜像。GPU 部署仍需 NVIDIA Container Toolkit、匹配的 GPU 镜像以及目标显卡验收，不能因为 CPU 安装成功就默认 GPU 可用。

网关负责创建和清理渲染容器，需要 Docker socket 权限；本部署的网关服务以 root 运行。原生渲染仍建议普通用户运行。专用 Docker 使用 `/run/gostream3d-docker.sock` 和 `/var/lib/gostream3d-docker`，由本软件单独维护；已有系统 Docker 不会被替换。静态引擎不会由 apt/dnf 自动升级，后续需按 Docker 官方版本更新它；需要纳入系统补丁管理时，可以预装发行版支持的新版 Docker，让安装器复用。

升级软件时解压新部署包并重跑安装命令。已有配置和数据保留，`--public-ip` 只用于首次初始化；更新现有 IP 请编辑配置并重启。首次镜像构建或预加载失败不会被描述为部署成功。

系统安装与实际视频链路的验收结果应以随包验证说明为准。Docker Desktop 的构建/测试结果不能替代 Ubuntu 22.04、openEuler 22.03 的宿主机 systemd、网络和防火墙验收。

官方依据：[Ubuntu Chromium 的 Snap 过渡包](https://packages.ubuntu.com/jammy/chromium-browser)、[openEuler Docker 指南](https://www.openeuler.org/en/wiki/install/image/)、[Docker API 版本](https://docs.docker.com/reference/api/engine/)、[Docker 官方静态分发](https://download.docker.com/linux/static/stable/x86_64/)。

开发机打包：`powershell -File scripts/package-linux-deploy.ps1`。本地已构建 CPU 镜像时，增加 `-IncludeWorkerImage` 可把镜像归档直接放进 ZIP，目标机首次安装无需编译镜像。
