# GoStream3D Linux 安装与启动教程

> 更新：Ubuntu 22.04 / openEuler 22.03 可使用新的 [Docker 一键部署说明](linux-quick-deploy.md)。以下保留 Debian 原生安装步骤；新版本 Linux 可自动准备配置，并将相对数据路径固定到配置所在目录。

整理日期：2026-10-06；发布文件名更新：2026-10-08。适用：`GoStream3D-linux-amd64-YYYYMMDD-HHMMSS.zip`，Debian 13（trixie）、x86_64/amd64，原生运行，无需 Docker。

本教程根据本项目源码、发布 ZIP 和 Debian 官方资料整理。当前开发环境为 Windows，尚未在真实 Linux 服务器上完成 Xorg → Neko → Chromium → WebRTC 的端到端验证；以下是部署步骤，不代表已经在你的服务器上实测通过。Ubuntu、CentOS、ARM 设备需要另外适配，不能直接照搬软件包和二进制。

## 1. Windows 和 Linux 配置不一样，合理吗？

合理，因为目前两个平台使用不同的渲染和传输实现。共同参数的名称和含义一致，平台专用参数按实际需要保留。

| 项目 | Windows 原生模式 | Linux 原生模式 |
| --- | --- | --- |
| 运行模式 | `GOSTREAM3D_RUNTIME=native` | `GOSTREAM3D_RUNTIME=native` |
| 网关端口与管理员账号 | `HTTP_HOST`、`HTTP_PORT`、管理员账号密码 | 使用相同参数 |
| 渲染环境 | 本机 Edge、Chrome 或 Chromium | Chromium、Xorg、自定义显示/输入驱动 |
| 画面传输 | JPEG / WebSocket | Neko / WebRTC |
| Linux 专用配置 | 不需要 | Neko 路径、两个内部密码、PulseAudio、ICE 地址、UDP 端口段 |
| CPU 总限额 | `GOSTREAM3D_CPU_MAX_PERCENT`，默认 50 | 此参数只能省略或为 `0`；需要限额时由 Linux systemd/cgroup 配置 |
| CPU 渲染选项 | SwiftShader / WARP，支持页面效果简化 | 当前使用 SwiftShader；Windows 的 WARP 和效果简化能力不能照搬 |
| 启动与配置位置 | 默认读取 EXE 旁的配置，缺失时可自动创建 | 本教程显式指定配置文件，并固定工作目录 |

不要把 Windows 的整份 `config.env` 覆盖到 Linux。尤其 `GOSTREAM3D_CPU_MAX_PERCENT=50` 和 `GOSTREAM3D_CPU_EFFECTS=simplified` 会使当前 Linux 程序拒绝启动。

**当前 Linux ZIP 包含网关程序、配置和说明文档，但没有包含 Neko、Chromium、Xorg、PulseAudio、GStreamer 和编译好的自定义 Xorg 驱动。第一次部署必须先安装这些依赖。只拿 ZIP、解压后直接启动，依赖不齐时会失败。**

## 2. 准备服务器和文件

在 Linux 上使用一个可以执行 `sudo` 的普通用户登录。下面把安装位置统一为 `/opt/gostream3d`。

```bash
cat /etc/os-release
uname -m
dpkg --print-architecture
mkdir -p "$HOME/gostream3d-src" "$HOME/gostream3d-upload"
```

确认系统为 Debian 13，架构分别显示 `x86_64` 和 `amd64`。本教程先用 CPU、单路、720p/30 FPS 验证，再增加路数或启用 NVIDIA。

需要上传这些文件：

- `dist/GoStream3D-linux-amd64-YYYYMMDD-HHMMSS.zip`：发布包。
- `dist/SHA256SUMS-YYYYMMDD-HHMMSS.txt`：相同时间戳的校验文件。
- `neko-3.1.5/server/`：本项目随附的 Neko 源码，尽量包含 `vendor/`。
- `neko-3.1.5/utils/xorg-deps/`：自定义输入驱动和带 RandR 补丁的 dummy 显示驱动源码。

可以用 SFTP 上传。若使用 Windows 自带 OpenSSH，在项目根目录的 PowerShell 中执行下面命令；先把 `your-user`、`server-ip` 换成你的 Linux 用户名和服务器 IP：

```powershell
$stamp = 'YYYYMMDD-HHMMSS' # 替换成该次压缩包文件名的实际时间戳
scp ".\dist\GoStream3D-linux-amd64-$stamp.zip" your-user@server-ip:gostream3d-upload/
scp ".\dist\SHA256SUMS-$stamp.txt" your-user@server-ip:gostream3d-upload/
scp -r .\neko-3.1.5\server your-user@server-ip:gostream3d-src/
scp -r .\neko-3.1.5\utils\xorg-deps your-user@server-ip:gostream3d-src/
```

上传后应得到 `~/gostream3d-src/server/` 和 `~/gostream3d-src/xorg-deps/`。这些源码不在 Linux 发布 ZIP 里，必须另行上传。不要随便用另一个版本的 Neko 替换，本项目随附版本为 3.1.5，并包含配套修改。

后续代码块均在 Linux 的 Bash 中执行。每一步成功后再进入下一步；编译报错时不要继续执行安装命令。

## 3. 安装系统依赖

```bash
sudo apt-get update
sudo apt-get install -y \
  ca-certificates curl unzip git golang-go \
  chromium xserver-xorg-core pulseaudio dbus-x11 \
  xdotool xclip libgtk-3-0t64 fonts-wqy-microhei \
  gstreamer1.0-tools gstreamer1.0-plugins-base \
  gstreamer1.0-plugins-good gstreamer1.0-plugins-bad \
  gstreamer1.0-plugins-ugly gstreamer1.0-pulseaudio \
  libx11-dev libxrandr-dev libxtst-dev libgtk-3-dev libxcvt-dev \
  libgstreamer1.0-dev libgstreamer-plugins-base1.0-dev \
  gcc pkgconf autoconf automake libtool make patch xorg-dev xutils-dev
```

Neko 的 `go.mod` 要求 Go 1.25.0 或更高。Debian 13 主仓库的 `golang-go` 默认提供 Go 1.24；本教程构建命令使用 `GOTOOLCHAIN=auto`，让 Go 根据模块要求下载所需工具链，因此这一步需要能访问 Go 工具链下载服务。其机制见 [Go 官方工具链说明](https://go.dev/doc/toolchain)，Debian 默认版本见 [Debian golang-go 软件包](https://packages.debian.org/trixie/golang-go)。

如果工具链下载失败，请按 [Go 官方安装说明](https://go.dev/doc/install) 安装 Go 1.25+，再继续。项目也有 `third_party/toolchains/` 缓存；需单独上传、校验并解压到一个全新的目录，不能将归档叠加到已有 Go 安装目录。运行发布包本身不需要 Go，Go 只用于这次编译 Neko。

## 4. 编译并安装 Neko

```bash
cd "$HOME/gostream3d-src/server"
GOTOOLCHAIN=auto go version
```

在这个模块目录里，确认输出工具链至少为 Go 1.25。如果上传了完整的 `vendor/`，执行：

```bash
CGO_ENABLED=1 GOTOOLCHAIN=auto go build -mod=vendor -buildvcs=false \
  -o bin/neko ./cmd/neko
sudo install -m 0755 bin/neko /usr/local/bin/neko
/usr/local/bin/neko --help
ldd /usr/local/bin/neko
```

`ldd` 输出不应包含 `not found`。Neko 需要 CGO 和前一步的系统开发库，不能用 Windows 网关打包时的 `CGO_ENABLED=0` 来编译。

如果没有 `vendor/`，将构建命令中的 `-mod=vendor` 改为 `-mod=readonly`，其余相同；这会联网下载 Go 模块。教程采用直接构建，避免随附 `build` 脚本中的 `go get` 改动依赖版本。

## 5. 编译并安装 Xorg 驱动

需要两个组件：支持 RandR 调整分辨率的 `dummy_drv.so`，以及用于远程鼠标、键盘输入的 `neko_drv.so`。只有系统普通 dummy 驱动，不能替代这里的配套安装。

从 Windows 上传的源码可能使用 CRLF 换行。先对下面这些文本源码统一换行，避免执行脚本或应用补丁时失败：

```bash
GOSTREAM3D_SRC="$HOME/gostream3d-src"
find "$GOSTREAM3D_SRC/xorg-deps" -type f \
  \( -name '*.c' -o -name '*.h' -o -name '*.ac' -o -name '*.am' \
     -o -name '*.sh' -o -name '*.patch' \) \
  -exec sed -i 's/\r$//' {} +
```

编译显示驱动。在全新上传的源码上执行一次补丁命令；如果此前已成功应用过相同补丁，跳过 `patch` 这一行。补丁失败且不能确认已应用时，停止并检查源码：

```bash
cd "$HOME/gostream3d-src/xorg-deps/xf86-video-dummy/v0.3.8"
patch --batch --forward -p1 < ../01_v0.3.8_xdummy-randr.patch
autoreconf -v --install
./configure --prefix=/usr --with-xorg-module-dir=/usr/lib/xorg/modules
make -j"$(nproc)"
sudo make install
```

编译输入驱动：

```bash
cd "$HOME/gostream3d-src/xorg-deps/xf86-input-neko"
sh ./autogen.sh
./configure --prefix=/usr --with-xorg-module-dir=/usr/lib/xorg/modules
make -j"$(nproc)"
sudo make install
```

核对安装结果：

```bash
ls -l /usr/lib/xorg/modules/drivers/dummy_drv.so
ls -l /usr/lib/xorg/modules/input/neko_drv.so
ldd /usr/lib/xorg/modules/drivers/dummy_drv.so
ldd /usr/lib/xorg/modules/input/neko_drv.so
command -v neko chromium pulseaudio
test -x /usr/lib/xorg/Xorg && echo 'Xorg executable OK'
gst-inspect-1.0 ximagesrc
gst-inspect-1.0 vp8enc
gst-inspect-1.0 pulsesrc
gst-inspect-1.0 opusenc
```

驱动要在目标系统上编译，以匹配 Xorg ABI；以后系统升级 Xorg 后，如提示驱动 ABI 不匹配，需要重新编译。`/usr/lib/xorg/Xorg` 是 Debian 13 的实际服务器程序路径，见 [Debian 文件清单](https://packages.debian.org/trixie/amd64/xserver-xorg-core/filelist)。

## 6. 解压程序并修改配置

```bash
cd "$HOME/gostream3d-upload"
stamp='YYYYMMDD-HHMMSS' # 替换成上传文件名的实际时间戳
archive="GoStream3D-linux-amd64-$stamp.zip"
checksums="SHA256SUMS-$stamp.txt"
unzip -l "$archive"
```

当前 ZIP 内文件直接位于根层，没有额外的安装目录。若上传了相同时间戳的校验文件，可以只校验 Linux ZIP：

```bash
awk -v archive="$archive" '$2 == archive {print}' "$checksums" \
  | sha256sum -c -
```

应显示该 ZIP 校验为 `OK`。文件缺失或校验失败时不要继续。

首次安装执行：

```bash
sudo install -d -m 0750 -o "$(id -un)" -g "$(id -gn)" /opt/gostream3d
unzip "$archive" -d /opt/gostream3d
cd /opt/gostream3d
chmod 0755 gostream3d
chmod 0600 config.env
mkdir -p data/workers "$HOME/.local/share/xorg" "$HOME/.config/pulse"
./gostream3d --version
nano config.env
```

没有 `nano` 时，可用其他文本编辑器，或先 `sudo apt-get install -y nano`。上述解压步骤用于全新安装；升级时保留已有 `config.env` 和整个 `data/`，不要覆盖。

发布包的 `config.env` 已填入默认管理员密码 `Admin@2026.com`，不要再用示例覆盖它。按你的环境检查或修改：

```dotenv
GOSTREAM3D_RUNTIME=native
GOSTREAM3D_HTTP_HOST=0.0.0.0
GOSTREAM3D_HTTP_PORT=8888

GOSTREAM3D_ADMIN_USERNAME=admin
# 默认管理员密码为 Admin@2026.com；可在网页修改，或设置自己的初始密码（至少 12 个字符）。
# GOSTREAM3D_ADMIN_PASSWORD=...

# 分别保留发布包提供的两个内部随机密码；两者必须不同。
# GOSTREAM3D_NEKO_USER_PASSWORD=...
# GOSTREAM3D_NEKO_ADMIN_PASSWORD=...

# 示例：局域网部署时，换成这台服务器真正的局域网 IP。
GOSTREAM3D_ICE_PUBLIC_IP=192.168.1.100

GOSTREAM3D_NEKO_BIN=/usr/local/bin/neko
GOSTREAM3D_CHROMIUM_BIN=/usr/bin/chromium
GOSTREAM3D_XORG_BIN=/usr/lib/xorg/Xorg
GOSTREAM3D_PULSE_BIN=/usr/bin/pulseaudio

GOSTREAM3D_WORK_ROOT=/opt/gostream3d/data/workers
GOSTREAM3D_SESSION_STORE=/opt/gostream3d/data/sessions.json
GOSTREAM3D_HTTP_PORTS=30000-30002
GOSTREAM3D_CDP_PORTS=31000-31002
GOSTREAM3D_UDP_PORTS=52000-52023
GOSTREAM3D_UDP_PER_WORKER=8
GOSTREAM3D_KEEP_PRELOADED=true
GOSTREAM3D_READY_TIMEOUT=300s
```

上面是需要核对的参数片段，不是一份完整密码配置。编辑包内原文件对应的行，每个参数只留一行。不要把注释里的省略号作为密码。

`GOSTREAM3D_ICE_PUBLIC_IP` 是观看端连接 WebRTC 使用的 IP，必须是 IP 地址，不能填写域名、URL 或端口：

- 只在服务器本机观看：可以使用 `127.0.0.1`。
- 局域网设备观看：改成服务器的局域网 IP，例如 `192.168.1.100`。
- 外网观看：改成观看者可达的公网 IP，并完成下面的 UDP 放行和 NAT 转发。

网关监听地址 `0.0.0.0` 不可拿来替代 ICE 地址。不同网络之间的 NAT 环境可能需要额外网络方案；当前模板不会自动解决所有内外网混合观看需求。

## 7. 放行网络端口

| 端口 | 用途 | 是否对观看设备开放 |
| --- | --- | --- |
| TCP 8888 | 管理后台、观看页面、信令 | 是；如果改了 HTTP 端口，放行新端口 |
| UDP 52000–52023 | 默认 3 路的 WebRTC 媒体 | 是 |
| TCP 30000–30002 | Neko 内部 HTTP 服务 | 否，当前程序绑定本机回环地址 |
| TCP 31000–31002 | Chromium DevTools | 否，当前程序绑定本机回环地址 |

在服务器现有防火墙中放行上述 TCP/UDP。云服务器还要在安全组中放行；服务器在路由器后面时，外网访问需要相应端口转发，仅转发 8888 会出现后台可用但视频连不上的情况。

如果已经使用 UFW，可以执行：

```bash
sudo ufw allow 8888/tcp
sudo ufw allow 52000:52023/udp
sudo ufw status
```

这些命令只适用于已安装并使用 UFW 的系统；其他防火墙按相同规则配置。不要在远程安装过程中未经检查直接启用新防火墙，以免影响现有 SSH 连接。

后台增加服务器总通道数时，UDP 端口段也会扩大。例如从 3 路增加到 6 路，每路 8 个 UDP 端口、起始端口仍为 52000，则要放行 UDP `52000–52047`。通道数是调度配额，实际流畅并发还要测算 CPU/GPU、显存、内存和网络。

## 8. 第一次启动和登录

使用普通用户启动，不要用 `sudo ./gostream3d`：

```bash
cd /opt/gostream3d
./gostream3d --config /opt/gostream3d/config.env
```

终端会显示日志。此时不要关闭终端；按 `Ctrl+C` 可停止。另开一个 SSH 终端检查：

```bash
curl -fsS http://127.0.0.1:8888/healthz
```

预期返回 `{"status":"ok"}`。这只说明网关 HTTP 正常，接下来还要验证实际画面和交互。

在你的电脑浏览器打开 `http://服务器IP:8888/admin`，使用 `/opt/gostream3d/config.env` 中的管理员账号密码登录，无需首次网页初始化。

先创建一个能从服务器访问的测试页面，选择 CPU、单路、1280×720、30 FPS、预加载 1 路。等待后台显示“已预加载”，复制观看链接，在另一台设备浏览器打开，确认：

1. 能显示画面，网页或 3D 场景实际加载完成。
2. 鼠标点击、拖动、滚轮等操作正常。
3. 从服务器之外的观看设备能连接，以验证 ICE 地址和 UDP 网络。
4. 验证通过后再调整自适应分辨率、60 FPS、多路预加载或 GPU。

Linux 当前支持 30/60 FPS。默认总配额为 3 路，所有 CPU/GPU 项目和预加载实例共用；多路项目需要足够的总通道数和预加载数量。Linux 当前页面就绪检查不能识别所有业务的异步 3D 加载；如果页面提供专门的场景完成标记，可填写相应 CSS 选择器。

配置文件修改后需要重启。已有 `GOSTREAM3D_` 环境变量会优先于 `config.env`，如修改配置后行为不符，检查终端或服务环境是否还设置了旧值。

## 9. 配置后台运行和开机启动

先完成第 8 节的画面与操作验证，再按 `Ctrl+C` 停止手动运行的程序，避免两个进程争抢端口。下面以当前普通用户作为服务运行用户，并固定工作目录。

在该普通用户的 Bash 中执行：

```bash
GOSTREAM3D_RUN_USER="$(id -un)"
GOSTREAM3D_RUN_GROUP="$(id -gn)"
sudo tee /etc/systemd/system/gostream3d.service >/dev/null <<EOF
[Unit]
Description=GoStream3D rendering gateway
Wants=network-online.target
After=network-online.target

[Service]
Type=simple
User=$GOSTREAM3D_RUN_USER
Group=$GOSTREAM3D_RUN_GROUP
WorkingDirectory=/opt/gostream3d
ExecStart=/opt/gostream3d/gostream3d --config /opt/gostream3d/config.env
Environment=PATH=/usr/local/bin:/usr/bin:/bin
Restart=on-failure
RestartSec=5
KillMode=control-group
TimeoutStopSec=30
UMask=0077

[Install]
WantedBy=multi-user.target
EOF

sudo systemd-analyze verify /etc/systemd/system/gostream3d.service
sudo systemctl daemon-reload
sudo systemctl enable --now gostream3d
sudo systemctl status gostream3d --no-pager
```

`WorkingDirectory` 保证配置中的相对路径不会因服务工作目录不同而改变，`KillMode` 让服务退出时清理所属子进程。对应语义见 [systemd 执行环境说明](https://manpages.debian.org/trixie/systemd/systemd.exec.5.en.html) 和 [服务说明](https://manpages.debian.org/trixie/systemd/systemd.service.5.en.html)。

常用操作：

```bash
sudo journalctl -u gostream3d -n 100 --no-pager
sudo journalctl -u gostream3d -f
sudo systemctl restart gostream3d
sudo systemctl stop gostream3d
sudo systemctl start gostream3d
```

`journalctl -f` 按 `Ctrl+C` 只退出日志查看。启用服务后退出 SSH，程序仍会运行，系统重启后也会启动。重启服务后再次创建或打开测试链接，确认 systemd 环境下渲染也正常。

`config.env` 修改后用 `systemctl restart`；服务文件修改后先 `daemon-reload` 再重启。保留 `config.env` 和整个 `data/`，里面有项目、设置及数据备份。

## 10. NVIDIA GPU 模式（可选）

CPU 模式验证通过后，再安装与目标显卡及系统匹配的 NVIDIA 驱动、VirtualGL、Vulkan/EGL 运行库，并配置普通运行用户访问 GPU 的权限。

```bash
nvidia-smi
command -v vglrun
```

两个命令均可用时，在 `config.env` 中填写实际 `vglrun` 路径，例如 `GOSTREAM3D_VGLRUN_BIN=/opt/VirtualGL/bin/vglrun`，重启服务，然后创建 GPU/NVIDIA 项目。本项目 GPU 路径会设置 `VGL_DISPLAY=egl`。

`nvidia-smi` 正常只证明驱动可用，还要验证 Chromium 的实际 WebGL 渲染、无显示器启动和 GStreamer 视频输出；当前没有真实 Linux/NVIDIA 端到端验收结果。选择 GPU 渲染不等于已经启用 NVIDIA 硬件视频编码，默认视频编码仍要以 Neko 实际管线为准。

## 11. 常见问题

| 现象或错误 | 检查方式 |
| --- | --- |
| `Permission denied`，执行不了程序 | `chmod 0755 /opt/gostream3d/gostream3d`；确认所在分区允许执行 |
| `Exec format error` | 确认服务器是 `x86_64/amd64`，不是 ARM；确认上传的是 Linux 二进制 |
| `find neko`、`find chromium` 等 | 依赖未安装，或配置中的程序路径错误；用 `command -v` 和绝对路径核对 |
| Neko 编译提示 Go 版本不足/下载失败 | 使用 Go 1.25+，或让 `GOTOOLCHAIN=auto` 成功下载；不能只凭 Debian 默认 Go 版本继续 |
| `distinct Neko passwords are required` | 两个内部密码都要填写且不同；不要用管理员登录密码替代这两个参数 |
| `CPU_MAX_PERCENT ... requires Windows native mode` | 删除该参数或设为 `0`；Linux 限额由系统管理 |
| `CPU_EFFECTS=simplified ... requires Windows` | 改为 `original` 或删除该参数 |
| Xorg 启动失败、模块 ABI 不匹配 | 检查 driver 路径，在当前服务器重编两个模块；优先用 `/usr/lib/xorg/Xorg` 核对是否避开包装器限制 |
| `Only console users are allowed` | 核对配置是否仍指向 Xorg 包装器；不要直接把整套服务改成 root 运行 |
| Xorg 提示无法访问控制台、VT 或配置文件 | 检查服务器的无显示器 Xorg 运行权限、实际二进制和生成配置；这里仍需目标机验证，不能仅靠修改包装器路径保证解决 |
| 后台正常、观看页面一直连接中或无画面 | 检查 ICE IP、UDP 端口、防火墙、安全组和 NAT；HTTP 正常不证明 WebRTC 正常 |
| `no element ...` 等 GStreamer 错误 | 用 `gst-inspect-1.0 元素名` 检查对应插件是否已安装 |
| 画面正常但鼠标/键盘无效 | 确认 `neko_drv.so` 已被 Xorg 加载，检查 Worker 的输入 socket 和日志 |
| `address already in use` | 手动程序未停止，或另有服务占用端口；确认只运行一份网关 |
| 修改配置没有变化 | 重启程序；检查外部同名环境变量；总通道数已在后台保存时优先恢复 `data/channel-settings.json` |
| 修改配置后登录失败 | 确认使用的是实际加载的配置；密码至少 12 字符；不要误用 `config.example.env` |
| 单路正常、多路失败或卡顿 | 检查总通道配额、扩大后的 UDP 段，以及 CPU、内存、显存和网络；配额不等于硬件性能 |

手动运行时网关日志输出到终端，systemd 运行时查看 journal。已运行 Worker 的日志位于 `/opt/gostream3d/data/workers/worker-*/`，包括 `xorg.log`、`pulseaudio.log`、`neko.log`、`chromium.log`：

```bash
find /opt/gostream3d/data/workers -maxdepth 2 -type f -name '*.log'
```

用 `tail -n 100 实际日志路径` 查看。**当前代码在 Worker 启动失败或停止后会删除其临时目录，相关子进程日志可能随之消失。** 所以失败时先保存后台错误和网关日志，不能保证事后总能找到 Worker 日志。

正式对外提供管理后台时，通过 HTTPS 访问；观看链接无需登录，只分享给需要访问的人。安装验收以跨设备画面、操作和实际 3D 场景为准，不能只看健康检查通过。
