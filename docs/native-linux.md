# Linux 原生部署（无 Docker）

建议在 Debian 13（与 Neko 3.1.5 原始运行镜像一致）上构建和运行。服务由 `gostream3d` 进程管理；Neko、Chromium、Xorg 和 PulseAudio 是宿主机上的子进程。首次安装系统依赖需要管理员权限，日常运行应使用普通用户。

## 1. 系统依赖

按 `neko-3.1.5/runtime/Dockerfile` 和 `neko-3.1.5/server/Dockerfile` 安装运行、编译依赖：

```bash
sudo apt-get update
sudo apt-get install -y \
  ca-certificates chromium xserver-xorg-core pulseaudio dbus-x11 \
  xdotool xclip libgtk-3-0t64 \
  gstreamer1.0-plugins-base gstreamer1.0-plugins-good \
  gstreamer1.0-plugins-bad gstreamer1.0-plugins-ugly \
  gstreamer1.0-pulseaudio \
  libx11-dev libxrandr-dev libxtst-dev libgtk-3-dev libxcvt-dev \
  libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
  gcc pkgconf autoconf automake libtool make patch xorg-dev xutils-dev
```

编译 Neko 服务端需要 Go 1.25+；编译 GoStream3D 网关需要 Go 1.24+。在目标机用 `go version` 确认工具链。

## 2. 编译 Neko 服务端和 Xorg 驱动

在仓库根目录执行：

```bash
cd neko-3.1.5/server
./build core
sudo install -m 0755 bin/neko /usr/local/bin/neko
cd ../..

cd neko-3.1.5/utils/xorg-deps/xf86-video-dummy/v0.3.8
patch -p1 < ../01_v0.3.8_xdummy-randr.patch
autoreconf -v --install
./configure --prefix=/usr
make -j"$(nproc)"
sudo make install
cd ../../../../..

cd neko-3.1.5/utils/xorg-deps/xf86-input-neko
./autogen.sh
./configure --prefix=/usr
make -j"$(nproc)"
sudo make install
cd ../../../..
```

确认 Xorg 模块目录中存在 `dummy_drv.so` 和 `neko_drv.so`。Neko 的输入驱动是鼠标操作必需的；GoStream3D 会为每个 Worker 从内置 Xorg 配置生成独立的 `SocketName`。

## 3. 编译并运行网关

```bash
bash scripts/build-release.sh
cp .env.example .env
# 编辑 .env：替换所有密钥，设置 GOSTREAM3D_ICE_PUBLIC_IP 为观看者可达的宿主机 IP
set -a
. ./.env
set +a
./dist/gostream3d
```

发布包自带 `config.env`，使用文件中的管理员账号密码直接登录；运行 `./gostream3d --config config.env` 无需网页初始化或使用 shell 导入环境变量。仅使用示例模板时，将 `config.example.env` 复制为 `config.env` 并替换密码占位值。

`GOSTREAM3D_RUNTIME=native` 为默认值。`GOSTREAM3D_HTTP_PORTS`、`GOSTREAM3D_CDP_PORTS` 和 `GOSTREAM3D_UDP_PORTS` 共同决定同时运行的 Worker 上限；每个 Worker 占一个 HTTP 端口、一个仅供本机使用的 Chromium DevTools 端口和一段 WebRTC UDP 端口。只对外开放网关 HTTP(S) 端口与 WebRTC UDP 端口，CDP 端口绑定 `127.0.0.1`。

每个 Worker 的日志存放在 `GOSTREAM3D_WORK_ROOT/worker-*/`，包括 `xorg.log`、`pulseaudio.log`、`neko.log` 和 `chromium.log`。页面创建失败时优先查看这些日志。

## NVIDIA GPU

可选 `render_mode=nvidia` 需要宿主机 NVIDIA 驱动、`nvidia-smi`、VirtualGL 的 `vglrun` 和浏览器可用的 Vulkan/EGL 运行库。先在宿主机确认 `nvidia-smi` 与 `vglrun` 可执行，再创建 GPU 链接。GPU 模式启动 Chromium 时设置 `VGL_DISPLAY=egl`；GPU 资源调度与隔离仍取决于宿主机驱动和系统权限。

目前未在真实 Linux/NVIDIA 环境中跑通端到端验收。尤其要验证目标发行版的 Xorg 模块 ABI、无显示器启动权限、Chromium WebGL、GStreamer 编码、ICE 网络和多路并发资源占用。
