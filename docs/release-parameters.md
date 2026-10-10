# 发布包使用与参数

## 平台与启动

发布包分别包含 Windows amd64 和 Linux amd64 网关可执行文件，前端管理页和观看页已嵌入。它们是两个不同的平台构建，不存在一个同时被 Windows 和 Linux 直接执行的文件。

先完整解压 ZIP，发布包自带可直接读取的 `config.env`。Windows 双击 `gostream3d.exe` 后访问管理页，直接使用文件中的账号密码登录；不再提供首次配置表单。Linux 启动前需安装渲染运行依赖，ICE 地址按部署网络调整。两个平台的登录账号和密码均在配置文件中设置，发布时默认账号为 `admin`、密码为 `Admin@2026.com`。命令行启动方式：

Linux 从 zip 解压后先执行 `chmod +x gostream3d`。

```text
Windows: .\gostream3d.exe --config config.env
Linux:   ./gostream3d --config config.env
```

`--help` 显示命令行参数；`--version` 显示构建版本；`--config PATH` 指定配置文件。Linux 默认读取当前目录的 `config.env`；Windows 默认读取可执行文件旁的 `config.env`。Windows 只有单独 EXE 且缺少默认文件时，自动写入默认端口、账号与默认密码 `Admin@2026.com`后继续启动，无需填写初始化表单；已有配置绝不覆盖。已有进程环境变量优先于文件；环境变量部署方式仍可使用，显式指定的文件不存在会报错。配置格式为每行 `GOSTREAM3D_KEY=value`，支持空行、整行 `#` 注释和单/双引号包围的值。Windows 启动日志写入程序旁的 `gostream3d.log`（目录不可写时写入用户缓存目录），启动失败时会弹出错误提示。请勿直接在 ZIP 压缩包预览中运行 EXE；应先完整解压到可写目录。

Linux 原生模式不使用 Docker，但必须单独安装 `neko` 3.1.5 服务端、带 Neko 输入驱动的 Xorg、Chromium、PulseAudio、GStreamer；GPU 模式还需要 NVIDIA 驱动与 VirtualGL。详见 `NATIVE-LINUX.md`。已下载的 Debian 13 运行包与编译包在源码树 `third_party/debian-trixie-amd64/`，尚未嵌入网关二进制，详见 `OFFLINE-DEPENDENCIES.md`。

Windows `native` 模式在程序所在电脑启动 Edge、Chrome 或 Chromium 加载原始 3D 页面，画面通过 JPEG/WebSocket 推送给观看者，鼠标事件回传到本机浏览器。CPU 模式可选 SwiftShader 或 WARP 软件渲染；GPU 模式尝试使用本机浏览器的硬件加速，不能保证选中 NVIDIA 或特定显卡。Windows 目前没有 Neko/WebRTC 视频链路、音频以及经过验证的高帧率性能。浏览器仍是独立安装的运行依赖，EXE 并未把它嵌入单文件。Linux `native` 使用本机 Neko/Xorg/WebRTC，不需要远端网关。

Windows 本机后台初始账号读取程序旁 `config.env` 中的 `GOSTREAM3D_ADMIN_USERNAME` 与 `GOSTREAM3D_ADMIN_PASSWORD`。Linux 本机后台初始账号也读取自身配置。登录后可通过右上角用户菜单“修改账号密码”保存新账号；保存值位于会话数据目录的 `admin-account.json`，立即生效并在重启后优先于环境变量及初始配置。旧版 `GOSTREAM3D_ADMIN_API_KEY` 可作为兼容项；网页修改账号后旧 API Key 失效。恢复方法见 `docs/admin-account.md`。

## 登录与启动参数：本机模式

| 参数 | 填写位置 | 如何填写 |
| --- | --- | --- |
| `GOSTREAM3D_RUNTIME=native` | 当前设备的 `config.env` | 原始页面在当前设备渲染。无需 `GOSTREAM3D_REMOTE_BASE_URL`。 |
| `GOSTREAM3D_HTTP_PORT=8888` | 当前设备的 `config.env` | 后台和观看链接共用的监听端口；修改后重启生效。 |
| `GOSTREAM3D_HTTP_HOST=0.0.0.0` | 当前设备的 `config.env` | 默认允许局域网访问；仅本机使用时可改为 `127.0.0.1`。 |
| `GOSTREAM3D_PUBLIC_BASE_URL` | 当前设备的 `config.env` | 可选的源地址，无路径；用于缺少请求 Host 时的 URL 回退。后台观看链接始终跟随当前管理页的访问地址，无需为局域网访问另行配置。 |
| `GOSTREAM3D_ADMIN_USERNAME` | 当前设备的 `config.env` | 本机后台管理员账号，例如 `admin`。 |
| `GOSTREAM3D_ADMIN_PASSWORD` | 当前设备的 `config.env` | 发布配置默认填入 `Admin@2026.com`，可改为自定义的至少 12 位密码；登录页面填写相同的值。 |

原始 3D 页面链接不写在上述配置中。打开本机 `/admin`，在“原始 3D 页面 URL”输入框中填写。

## 配置项

| 参数 | 默认值 | 用途 |
| --- | --- | --- |
| `GOSTREAM3D_RUNTIME` | `native` | Windows 和 Linux 均在本机运行渲染。旧版 `remote` 可转发；`docker` 需要 Docker Engine 与 Neko 镜像。 |
| `GOSTREAM3D_REMOTE_BASE_URL` | 无 | 仅旧版 `remote` 模式使用；本机模式无需填写。 |
| `GOSTREAM3D_HTTP_PORT` | `8888` | 管理后台及观看链接的监听端口；设置后覆盖旧版 `GOSTREAM3D_HTTP_ADDR` 中的端口。 |
| `GOSTREAM3D_HTTP_HOST` | 空（监听所有网卡） | 网关监听 IP；设置后覆盖旧版 `GOSTREAM3D_HTTP_ADDR` 中的主机部分。 |
| `GOSTREAM3D_HTTP_ADDR` | `:8888` | 兼容旧配置的完整监听地址；新配置建议使用上面的 HOST 和 PORT。 |
| `GOSTREAM3D_PUBLIC_BASE_URL` | 根据监听地址和端口生成 | 可选的 HTTP(S) 源地址，无路径。API 默认按请求 Host/TLS 返回观看链接，仅在缺少 Host 时回退到此值；后台按浏览器当前访问地址生成链接，也适用于反向代理。 |
| `GOSTREAM3D_ADMIN_USERNAME` | 无 | 当前设备的管理后台账号；与管理员密码一起配置。 |
| `GOSTREAM3D_ADMIN_PASSWORD` | 无 | 当前设备的管理后台密码，至少 12 个字符；与账号一起配置。建议限制 `config.env` 的文件读取权限。 |
| `GOSTREAM3D_ADMIN_API_KEY` | 无 | 可选的旧版 Bearer API Key。旧配置只有此项时，网页可用账号 `admin`、密码为该 Key 登录。迁移后可删除。 |
| `GOSTREAM3D_NEKO_USER_PASSWORD` | 无 | Neko 内部用户密码；与 WebSocket 代理一致。 |
| `GOSTREAM3D_NEKO_ADMIN_PASSWORD` | 无 | Neko 内部管理员密码。 |
| `GOSTREAM3D_ICE_PUBLIC_IP` | 无 | WebRTC 候选地址；应填写观看者能访问的服务端 IP。 |
| `GOSTREAM3D_SESSION_STORE` | `data/sessions.json` | 持久化链接配置的 JSON 文件。 |
| `GOSTREAM3D_WEB_DIST` | `web/dist` | 开发时可覆盖嵌入的前端资源；发布包无需设置。 |
| `GOSTREAM3D_HTTP_PORTS` | `30000-30002` | Worker 初始容量预留端口池；Windows 本机 JPEG 模式不监听这些端口。后台保存的服务器总通道数按此起始端口调整末端。 |
| `GOSTREAM3D_CDP_PORTS` | `31000-31002` | Chromium DevTools 初始本地端口池，每个 Worker 用一个；后台通道设置保留起始端口并调整末端。 |
| `GOSTREAM3D_UDP_PORTS` | `52000-52023` | Linux Neko/WebRTC 初始 UDP 端口池；Windows 本机 JPEG 模式不监听。后台设置按起始端口与每 Worker 的 UDP 数量调整末端。 |
| `GOSTREAM3D_UDP_PER_WORKER` | `8` | 每个 Worker 保留的 UDP 端口数。 |
| `GOSTREAM3D_KEEP_PRELOADED` | `true` | 启动时恢复已保存项目的渲染进程，并持续检查和保持就绪。失败时保留链接并自动重试。 |
| `GOSTREAM3D_IDLE_TIMEOUT` | `180s` | 将 `KEEP_PRELOADED` 设为 `false` 后，无观看者时回收 Worker 的时间。 |
| `GOSTREAM3D_READY_TIMEOUT` | `300s` | 预热页面等待 3D 场景就绪的上限；复杂场景可按实际加载时间调大。 |
| `GOSTREAM3D_NEKO_BIN` | `neko` | Linux 原生 Neko 服务端命令或绝对路径。 |
| `GOSTREAM3D_CHROMIUM_BIN` | `chromium` | Chromium 命令或绝对路径；Windows 未找到时自动寻找 Edge 或 Chrome。 |
| `GOSTREAM3D_XORG_BIN` | `Xorg` | Linux 原生 Xorg 命令或绝对路径。 |
| `GOSTREAM3D_PULSE_BIN` | `pulseaudio` | Linux 原生 PulseAudio 命令或绝对路径。 |
| `GOSTREAM3D_VGLRUN_BIN` | `vglrun` | Linux NVIDIA 模式 VirtualGL 命令或绝对路径。 |
| `GOSTREAM3D_WORK_ROOT` | `data/workers` | 本机 Worker 工作目录与日志。 |
| `GOSTREAM3D_NEKO_IMAGE` | 无 | Docker CPU 模式使用的镜像名。 |
| `GOSTREAM3D_NEKO_NVIDIA_IMAGE` | 无 | Docker NVIDIA 模式使用的镜像名。 |
| `GOSTREAM3D_DOCKER_HTTP_BIND_IP` | 无 | Docker Worker HTTP 端口绑定的宿主机 IP。 |
| `GOSTREAM3D_SCREEN` | `1920x1080@30` | Docker Neko 默认屏幕模式。 |
| `GOSTREAM3D_WORKER_MEMORY_BYTES` | `4294967296` | Docker Worker 内存上限字节数。 |
| `GOSTREAM3D_WORKER_NANO_CPUS` | `0` | Docker Worker CPU 配额；0 表示不限制。 |
| `GOSTREAM3D_CPU_MAX_PERCENT` | Windows 本机默认 `50`；其他模式默认 `0` | Windows `native` 整个 GoStream3D 进程树的 CPU 硬上限，占整机总处理能力的百分比。1–100 有效，0 关闭。没有平台保存值时作为启动值；管理员在“后台设置 → CPU 资源设置”保存 1–100% 后立即生效，平台值保存为会话数据目录下 `cpu-settings.json`，后续重启优先恢复该值。设置失败或保存文件损坏时拒绝启动。Linux 原生模式请用 systemd `CPUQuota` 或 cgroup v2；Docker 请设置 Worker 配额。 |
| `GOSTREAM3D_CPU_RENDERER` | `swiftshader` | Windows `native` CPU 软件渲染后端：`swiftshader` 或 `warp`。WARP 使用 Microsoft 软件光栅化，不使用硬件显卡渲染；根据真实场景比较帧率。重启生效，GPU 模式不使用此项；Linux 使用原有 Mesa/Neko 后端。 |
| `GOSTREAM3D_CPU_EFFECTS` | `original` | Windows `native` CPU 效果配置：`original` 保留原效果；`simplified` 在原分辨率、原像素比下禁用 WebGL 抗锯齿，并简化 ThingJS 园区的后期特效、阴影和描边。需要明确接受效果变化；模型、纹理、拾取与控件保留。重启生效，可切回 `original`；GPU 不受影响。Linux/Docker 暂不支持 `simplified`，不静默忽略。 |

## 独立会话与服务器总通道数

1 路对应一位用户独占的浏览器、画面流和输入通道。`single` 的项目配额固定为 1；`multi` 的 `max_viewers` 是单链接的独立会话上限，可自定义 2–100，允许高于当前服务器总配额。每位同时观看者各占一个 Worker，操作互不干扰。`preload_count` 为提前准备的独立实例数（1–`max_viewers`），旧项目未设置时为 1；新建多路项目的页面默认 3。已就绪实例直接分配给观看者，超过预加载数量时才按需启动。预加载实例与观看实例共用总通道配额；设置项目配额 10 不等于预加载 10。多路预加载池中的观看者退出后，该实例在后台重新加载原始场景，并复用其独立磁盘缓存。

默认 3 路来自管理员的端口池配置：HTTP 可分配端口数、CDP 可分配端口数及完整 UDP 分组数的最小值。默认 HTTP/CDP 各 3 个端口，UDP 24 个端口、每 Worker 8 个，故得到 3 路。这个值不是 GPU 硬件检测结果，系统不会自动测定设备最大可流畅渲染路数，也不会在 Windows JPEG/WebSocket 模式下根据 NVENC 编码通道推算路数。

管理员在“后台设置 → 渲染通道设置”输入服务器总通道数（1–100）并保存，立即生效。保存值位于会话数据目录的 `channel-settings.json`，重启后优先于配置文件端口池的初始容量。系统保留原 HTTP/CDP/UDP 起始端口，并按总通道数及 `GOSTREAM3D_UDP_PER_WORKER` 自动扩展或收缩末端。例如使用默认起始端口改为 10 路，HTTP/CDP 末端分别为 `30009`、`31009`，UDP 为 `52079`。Linux/Neko 部署须允许实际使用的 UDP 端口段。缩小设置若影响正在使用的实例或已分配端口，则拒绝保存、保留原配额，不强停现有会话。

CPU/GPU 渲染项目、正在启动的实例和预加载 Worker 共用服务器总配额。“已分配通道”包含预加载实例，不等同于观看人数。项目配额已满或服务器总配额不足时，新访问明确提示繁忙；不排队，也不会接入其他用户的画面。提高项目配额只允许该项目申请更多实例，提高服务器总配额才扩大全局调度范围。扩大配置值不保证硬件有相应性能；可流畅承载的实际路数需按场景、分辨率、目标帧率、GPU/CPU、显存、内存和网络实测。

管理员接口为 `GET/PUT /api/v1/admin/channels/settings`。保存使用整数 `max_channels`，返回当前总配额及 `active_channels`（已分配通道）；接口需要管理员登录。访问管理页 `/admin`，观看短链为 `/Stream3D/:token`。

Windows 本机 JPEG 模式可设置 90/120 FPS 目标上限（仅 1280×720、1920×1080）；能否达到取决于页面渲染、Chromium 截帧、网络和整机 CPU 配额。CPU 模式保持原分辨率，不自动降低渲染像素比例；接受算力限制下较低的实际帧率，播放页显示实际显示帧率。Linux/Neko 和 Docker 仍只支持 30/60 FPS；2560×1440 仍仅支持 30 FPS。

管理后台“平台监控”使用 gopsutil/v4 在渲染节点采集 Windows/Linux 主机指标，默认每 30 秒更新缓存。“后台设置”可在 10–300 秒范围内调整采样间隔，立即生效并保存在会话数据目录的 `monitor-settings.json` 中；未配置时使用 30 秒默认值。监控页面按相同间隔读取缓存；`GET /api/v1/admin/monitor` 读取快照，`POST /api/v1/admin/monitor/refresh` 立即采样，`GET/PUT /api/v1/admin/monitor/settings` 读取和保存间隔，均需要管理员登录。网络趋势在进程内最多保留最近 1 小时，服务重启后重新累计；速率由相邻采样的网络字节计数计算。磁盘显示操作系统返回的可用分区，部分容器、虚拟接口或受权限限制的分区可能不提供指标，页面会提示缺失项。

后台使用浅色内容区、深色侧栏与蓝色按钮。登录后点击“外观设置”可选择 10 种强调色，选择在当前浏览器中保存，无需修改服务端配置；窄屏设备先打开顶部导航。观看短链页面不受后台配色影响。

程序启动后会立即开放后台，并在后台逐个预加载已保存的项目。项目尚未预热完成时，后台显示等待状态且不提供可复制观看链接。若预热失败，链接仍保留，后台会显示错误并定时重试。需要空闲回收时，将 `GOSTREAM3D_KEEP_PRELOADED=false`。

预渲染的目标是让服务端完成 3D 场景加载和 WebGL 渲染，观看端只接收画面和发送输入。Windows 本机模式不会把 HTML 的 `readyState=complete` 或第一张占位帧单独当作场景就绪；ThingJS 园区页面还需等待其场景加载器清空、场景实例初始化，并在此后取得新画面。其他 3D 页面若有专门的加载完成元素，可在“页面就绪标记”中填写 CSS 选择器。复杂场景可将 `GOSTREAM3D_READY_TIMEOUT` 调大；该等待发生在服务端预加载阶段。

“已创建的链接”菜单可以单独停止或启动每个项目的渲染。停止状态会持久保存，自动预热不会重新启动该项目。Windows 本机 GPU 模式已预加载页面在无人观看时保留场景、停止 JPEG 抓帧并将动画约束到 2 FPS；观看和操作会自动恢复画面与交互。CPU 硬上限仍然生效。高负载 WebGL 页面仍需按实际设备能力确定可流畅并发路数；详见 [GPU 多路性能验证](gpu-multisession-performance.md)。

## 多路预加载实例池（2026-10-02）

第二、第三路首次访问慢的原因为独立浏览器冷启动及 3D 场景加载。项目的预加载路数可提前保持多个独立实例；“就绪 3 / 预加载 3 路”表示三位用户可各取得一个已经加载的实例。部分就绪时不承诺所有用户快速进入。预加载持续消耗 CPU/GPU、内存及全局通道配额，服务器总通道数必须覆盖需要保持的实例数。后台加载不持有项目锁，已就绪实例可以继续接入；停止、删除、修改渲染设置和退出程序会取消并等待在途加载清理。等待显示使用缓存的会话状态，不增加系统监控采样。
