# GoStream3D

### Linux Docker 镜像部署

最新 `GoStream3D-docker-compose-linux-amd64-时间戳.zip` 提供 `docker-compose.yml`、网关和 CPU / NVIDIA / Intel 通用渲染镜像。Docker 25+ 与 Compose 2.20+ 就绪后，解压执行 `sudo bash start.sh --public-ip 服务器IP`，自动选择 `docker-compose` 或 `docker compose`；配置和项目数据保存在镜像外。GPU 另需宿主驱动，NVIDIA 还需 Container Toolkit。制作命令为 `powershell -File scripts/package-docker-release.ps1 -MirrorECR -Validate`。启动、升级及验证范围见 [Docker 部署教程](../docs/docker-linux-deploy.md)。

Docker Hub 仓库为 `ncayu8847/gostream3d-gateway` 和 `ncayu8847/gostream3d-neko-chromium`。固定版本、拉取命令及现有 Compose 安装包的在线部署方法见 [Docker Hub 部署说明](../docs/docker-hub-deploy.md)。

### Ubuntu 22.04 / openEuler 22.03 简化部署

使用独立的 `GoStream3D-linux-deploy-amd64.zip`，解压后运行 `sudo bash install-linux.sh --public-ip 服务器IP`。安装器准备 Docker 和配套 CPU 渲染镜像，生成配置并注册开机服务；后续用 `sudo systemctl start gostream3d` 启动。打包命令为 `powershell -File scripts/package-linux-deploy.ps1`。适配范围、离线镜像、旧 Docker 检查及验收限制见 [Linux 一键部署说明](../docs/linux-quick-deploy.md)。

### 管理诊断与恢复

Linux GPU / CPU 的 WebRTC 观看连接已接入逐路画面性能，展示实收/显示 FPS、真实分辨率、视频码率、解码、缓冲、RTT 与区间丢包/丢帧；连接独立统计，过期样本清除。测量口径见 [Linux 逐路性能说明](../docs/linux-stream-performance.md)。

平台监控增加 NVIDIA GPU / 显存、逐路页面回调与发送 FPS，以及输入至后续画面的参考耗时。后台设置提供实际场景容量测试，逐级测试独立副本并生成服务器建议值；项目 JSON 增加自动 / 手动备份、坏记录隔离、损坏原文保留与缺失项目恢复。使用方式和测量限制见 [管理诊断与恢复说明](../docs/management-diagnostics-recovery.md)。

### 多路性能与空闲通道

Windows GPU 项目可选择“交互优先”或“持续动画”；后台设置可调整所有 GPU 路共享的帧率调度预算，以及是否为实际观看请求回收其他项目的空闲预加载实例。重点项目可配置优先保留的预加载路数。平台监控新增逐路 JPEG 链路性能，展示采集/显示 FPS、发送等待、解码/绘制和显示反馈耗时。测量口径、回收规则与视频传输评估方法见 [多路优化说明](../docs/multistream-optimization.md)。

### 自适应观看窗口

创建或编辑观看链接时，分辨率可选择 **自适应观看窗口（推荐）**。
服务端按每位观看者的窗口尺寸调整其独立渲染实例，窗口大小变化及通过浏览器手动进入/退出全屏时自动同步。观看页不会自动进入全屏，可使用浏览器的 F11 控制。
已加载的页面和场景保留，调整尺寸不会重新导航页面；各路尺寸与操作互不影响。
预加载时先使用 1920 × 1080，连接后调整为观看窗口尺寸。
Linux GPU 观看端按物理像素同步尺寸（设备像素比最高 2），CPU 与 Windows 继续采用 CSS 像素。超大窗口按比例限制在 3840 × 2160 内，并对尺寸微调以适配编码器。NVIDIA 编码随实际分辨率调整画质预算，详见 [Linux GPU 大屏画质优化](../docs/linux-gpu-quality.md)。
窗口变化防抖 300 毫秒；固定分辨率仍按原比例显示。
Windows 使用 CDP 调整视口并更新 JPEG 采集尺寸；Linux/Neko 通过服务端私有管理连接调整 Xorg 屏幕与视频采集，观看者不获得管理员权限。

Windows 观看页仅转发普通字符、方向键、Enter、Backspace、Delete、Home/End、PageUp/PageDown 和 Shift；F11、F5、其他功能键、Tab、Escape 以及 Ctrl/Alt/Meta 组合交给本地浏览器处理。切换窗口、隐藏标签页、画面失焦或离开观看页时补发已按下按键和鼠标的释放事件。尺寸调整独立于输入处理，连续变化仅保留最新待处理尺寸。

后台 API 读取请求超时为 10 秒，提交操作为 30 秒，包含响应正文读取。刷新期间可点击“取消刷新”；退出登录或离开页面会取消相关读取请求。超时会提示重试，后续自动刷新仍可继续。提交操作超时后请先刷新状态确认结果，再决定是否重试。

GoStream3D 在服务端浏览器提前加载 3D 数字孪生页面、模型与纹理并执行 WebGL 渲染，然后生成免密观看链接。低配置客户端只接收画面流、回传操作，不运行原始页面的 3D 场景。管理员在 `/admin` 配置 URL、CPU 或 GPU、分辨率、帧率及观看并发；Windows 本机模式目前传输 JPEG 画面，Linux Neko 模式传输 WebRTC 视频。

管理控制台采用深色侧栏、白色内容区和简洁表格，提供三步创建表单、可筛选与分页的项目列表、资源设置摘要和十种界面配色，详见 [控制台界面规范](../docs/ui-console.md)。

## 当前功能

- 创建链接时启动独立 Worker，并等待 Chromium 页面就绪；可用 `ready_selector` 等待应用自己的场景就绪标记。
- 不同链接各自使用独立浏览器进程。Linux Neko 模式另使用显示号、音频与输入 socket、TCP/UDP 端口。默认端口池配置为 3 路，这个数值来自配置，系统不自动测定设备或 GPU 的最大流畅渲染路数。“后台设置 → 渲染通道设置”可修改服务器总通道数（1–100），保存后立即生效并在重启后保留。
- `single` 为 1 路：同一时刻仅一位用户独占一个图形实例与输入。`multi` 可自定义 2–100 路项目会话配额，也可高于当前服务器总配额；每位并发用户分配单独的浏览器 Worker、画面流和输入通道，操作互不影响。项目配额与预加载路数分开设置。例如项目上限 6 路、预加载 3 路时，前 3 位使用已就绪实例，第 4–6 位各自按需启动新实例；服务器总通道也须至少为 6 路，且其他项目占用的通道会减少可用数量。所有项目的 CPU/GPU 会话和预加载 Worker 共用服务器总配额。达到项目配额或服务器配额时，新访问显示“路数超出限额，请查看配置”，不共享已有用户画面。
- 创建链接时先保存短链，再在服务端后台预加载；只有场景就绪后才显示“已预加载”并允许分享。链接配置写入 `data/sessions.json`，程序重启时自动恢复未停止项目；后台显示预热失败原因并允许手动重试。资源有限时可设置 `GOSTREAM3D_KEEP_PRELOADED=false`，空闲 180 秒后回收 Worker，但这种按需运行方式不满足“打开即见已预渲染场景”的要求。
- Go 网关支持 `native`、`remote` 和 `docker` 三种运行时。Windows 与 Linux 默认 `native`，都在部署设备本机运行。`remote` 是保留的旧版转发模式。
- 管理后台“平台监控”默认每 30 秒采样运行节点的 CPU、内存、磁盘、网络和 NVIDIA GPU 状态，并用按需加载的 ECharts 图表分别展示近 1 小时整机 CPU 使用率、内存使用率、GPU 使用率和网络趋势，同时展示磁盘分区、网络接口及系统信息。GPU 趋势位于“GPU 与显存”区域，多卡按 UUID 分别绘制。趋势复用同一份服务器采样，CPU/内存/GPU 纵轴固定为 0–100%，悬停可查看时间与数值；采集失败显示数据缺口，不记成 0%。历史保存在服务器内存，重启后重新积累。“后台设置”可将间隔调整为 10–300 秒，立即生效并保存到服务器。系统资源采集基于 `github.com/shirou/gopsutil/v4`，GPU 使用 `nvidia-smi`；Windows 与 Linux 使用相同监控接口，监控 API 仅向管理员开放。

## Linux 原生运行

原生运行需要一台 Linux 服务器，并先安装 Neko 依赖的 Chromium、Xorg 及自定义 `neko` 输入驱动、PulseAudio、GStreamer 和 Neko 3.1.5 服务端二进制。NVIDIA 模式另外需要驱动与 VirtualGL。安装细节见 [原生部署指南](../docs/native-linux.md)。这些是系统运行资源；GoStream3D 启动和调度由一个 `gostream3d` 程序负责，不需要 Docker 守护进程。

Debian 13 amd64 的运行依赖下载缓存与来源清单见 [离线依赖说明](../docs/offline-dependencies.md)。缓存供后续制作单文件离线包使用；当前发布包尚未嵌入这些系统组件。

配置：

```bash
cp .env.example .env
# 修改管理员账号密码、Neko 内部密码、服务端公网 IP 等参数
```

构建包含后台和观看页资源的 Go 网关：

```bash
bash scripts/build-release.sh
```

在 Windows 开发机上可运行 `powershell -File scripts/build-release.ps1`，交叉编译出 `dist/gostream3d-linux-amd64`。

若要同时生成 Windows 和 Linux amd64 发布包，运行 `powershell -NoProfile -ExecutionPolicy Bypass -File scripts/package-release.ps1`。压缩包位于 `dist/GoStream3D-windows-amd64-YYYYMMDD-HHMMSS.zip` 与 `dist/GoStream3D-linux-amd64-YYYYMMDD-HHMMSS.zip`，每次打包按北京时间添加时间戳并保留旧包；对应校验文件为 `dist/SHA256SUMS-YYYYMMDD-HHMMSS.txt`。包内含可执行文件、可直接使用的 `config.env`、`config.example.env`、启动和参数说明。发布配置使用账号 `admin` 和默认密码 `Admin@2026.com`，登录后可在网页修改；打包不会复制开发机的实际凭据或项目数据。两个平台的默认模式都在本机渲染。详见 [二进制启动说明](../docs/binary-release.md)及[发布参数](../docs/release-parameters.md)。

从仓库根目录导入 `.env` 后执行 `./dist/gostream3d`。发布时前端资源已嵌入这个文件，可把它单独复制到服务器；Go 进程仍需要能访问系统的 `neko`、`chromium`、`Xorg` 和 `pulseaudio` 可执行文件。配置项 `GOSTREAM3D_WORK_ROOT` 下会生成每个 Worker 的临时目录和日志。

Windows `native` 模式使用本机 Edge、Chrome 或 Chromium 加载 3D 页面，并通过本机浏览器的 DevTools 协议采集画面与发送鼠标输入。它不依赖 Linux 节点，但浏览器需要单独安装。此模式的 JPEG/WebSocket 传输无音频，尚未达到 Neko/WebRTC 的视频性能；GPU 模式尝试调用本机浏览器硬件加速，是否实际使用特定显卡需要在目标机器验证。

Windows 应先完整解压 ZIP 到可写目录，双击 `gostream3d.exe` 即启动服务，访问 `/admin` 直接登录，不再出现首次配置表单。初始账号、密码、监听 IP 和端口读取 exe 旁的 `config.env`；默认监听 `0.0.0.0:8888`。如果只有单独的 EXE、没有配置文件，程序自动创建 `config.env`（账号 `admin`，默认密码 `Admin@2026.com`）后继续启动，无需网页初始化。已有配置始终保留。网页修改账号密码后，已保存账号优先生效；监听 IP 和端口修改后重启生效。旧配置若包含 `GOSTREAM3D_RUNTIME=remote`，请改成 `native` 并删除远端地址；已有配置若启动失败，会弹出错误原因并记录到 `gostream3d.log`。

## 管理与观看

访问 `http://<server>:8888/admin`（若修改了 `GOSTREAM3D_HTTP_PORT`，使用配置的新端口），首次使用当前设备 `config.env` 中的 `GOSTREAM3D_ADMIN_USERNAME` 和 `GOSTREAM3D_ADMIN_PASSWORD` 登录，在后台创建、查看和删除链接。登录后使用 12 小时有效的 HttpOnly 会话 Cookie；密码不会保存在浏览器存储中。右上角用户菜单的“修改账号密码”可修改管理员账号名称和密码，需验证当前密码并确认新密码；保存后注销全部后台会话，使用新账号重新登录，观看和渲染不受影响。新密码为 12–72 字节，账号为 1–64 个字符且不含空白。网页保存值位于会话数据目录的 `admin-account.json`，采用带盐 bcrypt 哈希，保存成功后立即生效并在重启后优先于初始配置恢复。旧版仅配置了 `GOSTREAM3D_ADMIN_API_KEY` 的节点仍可使用账号 `admin`、密码为该 API Key 登录；网页修改账号后，旧 API Key 同时失效。正式部署应通过 HTTPS 提供后台。详见 [管理员账号修改与恢复](../docs/admin-account.md)。

管理后台采用浅色内容区与深色导航，默认蓝色强调色。登录后在左侧选择“外观设置”（窄屏时先打开顶部导航），可以在 10 种强调色之间即时切换，选择会保存在当前浏览器；“恢复默认配色”可重置。配色只改变后台和登录页的外观，不改变 `/Stream3D/:token` 观看页，也不需要修改 `config.env`。

创建接口 `POST /api/v1/sessions` 使用网页登录会话；仍配置了旧版 `GOSTREAM3D_ADMIN_API_KEY` 时，也接受 Bearer 密钥。请求体例如：

```json
{
  "name": "智慧园区",
  "target_url": "https://example.com/digital-twin",
  "render_mode": "cpu",
  "concurrency_mode": "multi",
  "max_viewers": 3,
  "resolution": "1920x1080",
  "fps": 30,
  "ready_selector": "#scene-ready"
}
```

`render_mode` 可选 `cpu` 或 `nvidia`，默认 `cpu`。`concurrency_mode` 可选 `single` 或 `multi`，默认 `single`；`max_viewers` 表示同一链接的独立会话配额，单路固定为 1，多路允许输入 2–100，允许超过当前服务器总配额。`preload_count` 可设为 1–`max_viewers`，控制提前准备的独立实例数，默认 1；多路表单默认 3。等待就绪数达到目标后，后续观看者直接取得已预加载实例。超过预加载数量时按需启动；所有实例仍受服务器总配额限制。`ready_selector` 可省略；未指定时，只能确认文档加载完成，不能推断任意 3D 应用内部已经完成异步渲染。管理后台使用 `POST /api/v1/sessions?async=1`：先保存项目并返回 HTTP 202，再由服务端独立预加载；后台显示“已预加载”后才提供复制观看链接。普通 `POST /api/v1/sessions` 保持等待预加载成功再返回 HTTP 201。后台列表和删除接口分别是 `GET /api/v1/admin/sessions` 与 `DELETE /api/v1/admin/sessions/:token`。

服务器总通道数可在“后台设置 → 渲染通道设置”中设置为 1–100。保存值位于会话数据目录的 `channel-settings.json`，启动时优先于端口池的默认容量；系统按现有 HTTP/CDP/UDP 起始端口和每 Worker 的 UDP 数量调整端口池末端。缩小时，若会占用正在使用的实例或端口，保存失败并保留原配额，不强停用户会话。管理员 API 为 `GET/PUT /api/v1/admin/channels/settings`，使用 `max_channels` 保存总配额，返回 `active_channels` 表示已分配的通道（包括预加载）。扩大配额只改变调度限制，实际流畅并发需结合场景、分辨率、帧率、GPU/CPU、显存、内存和网络实测；Windows JPEG/WebSocket 模式不会根据 NVENC 编码通道推算路数。

Windows 本机模式可选 30、60、90、120 FPS；90/120 FPS 适用于 1280×720 和 1920×1080，2560×1440 仍限 30 FPS。该值是截帧发送的目标上限，实际帧率会受页面渲染速度、Chromium 截帧、网络和 CPU 配额影响，不保证达到所选数值。Linux/Neko 与 Docker 模式仍只接受 30/60 FPS。

程序启动后会立即开放后台，并在后台逐个预热保存的项目。预热完成前，观看链接暂不可分享；若某个页面预热失败，链接仍保留，后台显示失败原因并定时重试。没有后台保存的通道设置时，默认端口池可同时保持 3 个 Worker；要预加载更多项目，可提高服务器总通道数，并确认机器资源足够。

观看链接统一为 `http(s)://管理平台访问地址:端口/Stream3D/:token`，显示、复制和打开均使用当前管理页的协议、IP/域名和端口。通过局域网 HTTP 访问也支持复制，结果在页面顶部居中提示。旧 `/v/:token` 地址自动跳转到新地址，既有 token 保持不变。

观看链接无需登录。后台还可调用 `POST /api/v1/admin/sessions/:token/warm` 重新预热已休眠链接。Linux Neko 模式使用 WebRTC，需要允许配置的 UDP 端口段；Windows 本机模式通过同一个 HTTP(S) 端口建立 WebSocket 连接，传输 JPEG 帧和鼠标输入。

“已创建的链接”菜单提供“停止渲染”和“启动渲染”。停止后链接配置仍在，但新观众无法连接，现有观众会断开；停止状态写入 `sessions.json`，服务重启和自动预热不会自行启动该项目。管理员接口分别是 `POST /api/v1/admin/sessions/:token/stop` 和 `POST /api/v1/admin/sessions/:token/start?async=1`。

Windows 本机 CPU 模式始终保持项目配置的原分辨率和像素比例，允许实际帧率低于目标上限。连续鼠标移动按实际画面更新节奏保留最新位置，点击、松开及键盘输入及时冲刷待发坐标，最多 4 条命令等待浏览器回复、最多 64 条待发；滚轮、按钮及键盘事件保持顺序。无观看者时停止 JPEG 编码，保留场景运行及最新画面缓存。采集持续推送画面，停滞时执行单个补充采集请求，避免反复重启采集和操作积压。播放页按 2 秒窗口显示含小数的实际帧率。Windows 可通过 `GOSTREAM3D_CPU_RENDERER=swiftshader|warp` 选择软件渲染后端，默认 SwiftShader，WARP 使用 Windows 软件光栅化；两者均需要在实际场景中比较。

Windows 本机 GPU 多路模式在无人观看的已预加载实例中停止 JPEG 截帧，并将页面动画降至约 2 FPS；接入观看者后立即恢复截帧，操作时解除动画节流，停止操作后被动观看约 6 FPS。观看页所在浏览器标签隐藏后，该路暂停截图和发送，切回时立即恢复；其他可见观看者不受影响。捕获管线只有连续 3 秒无新帧才重新启动，避免鼠标拖动时反复重启。每路仍是独立浏览器、原分辨率画面和输入；2/6 FPS 是空闲资源调度，不是用户设置的最大目标帧率。Windows JPEG/WebSocket 模式仍未使用 NVENC，复杂 3D 页面能达到的三路交互帧率须在目标 GPU 上实测，详见 [GPU 多路性能验证](../docs/gpu-multisession-performance.md)。

经用户明确许可，可设置 `GOSTREAM3D_CPU_EFFECTS=simplified` 并重启 Windows 本机服务：CPU WebGL 禁用多重抗锯齿；ThingJS 园区关闭后期特效、阴影和描边，保留模型、纹理、点击拾取、标签及缩放所需的接口。原分辨率和像素比保持不变，边缘和色彩效果会不同。GPU 模式不使用此配置；`original` 恢复原效果，默认值为 `original`。Linux/Docker 暂不支持此简化配置，设置后明确报错。实际复杂园区帧率仍受算力和场景影响，详见 [CPU 性能测试](../docs/cpu-rendering-performance.md)。


浏览器静态资源设置每项目约 256 MiB 的磁盘缓存目标，Windows 整机 CPU 硬上限始终生效。内存和磁盘缓存能减少重复加载，但不能代替 CPU 执行 WebGL 软件渲染。复杂场景在原分辨率及 CPU 配额下可能只有很低的帧率；性能测量及其限制见 [CPU 优化验证](../docs/cpu-rendering-performance.md)。

Windows 本机模式默认将 GoStream3D 与它启动的渲染浏览器放进同一个 Windows Job Object，CPU 硬上限为整机总处理能力的 50%。管理员在“后台设置 → CPU 资源设置”输入 1–100% 并保存，新的硬上限立即作用于服务及所有现有/新建渲染进程，无需重启。平台值保存在会话数据目录的 `cpu-settings.json`，重启后优先恢复此值；没有平台保存值时采用 `GOSTREAM3D_CPU_MAX_PERCENT`（Windows 本机默认 50%，配置文件的 `0` 仍表示未启用 CPU 限额）。多个链接共享此总上限，GPU 模式仍会使用 CPU 并计入上限。此值限制本软件的进程组，不限制其他应用，不能保证服务器整体使用率低于该值。系统无法施加上限或保存的 CPU 设置损坏时服务拒绝启动。请手动在浏览器中打开管理地址，避免管理员浏览器进入渲染进程组。内存和硬盘读写继续用于浏览器运行及缓存；CPU 上限不等于内存或磁盘配额。Linux 原生模式需在 systemd/cgroup v2 层配置 CPU 配额，Docker 模式可通过 `GOSTREAM3D_WORKER_NANO_CPUS` 配置每个 Worker 的配额；平台对此类节点明确显示暂不支持，禁止返回设置成功。

## 验证范围

网关检查使用 `go test ./cmd/... ./internal/...`、`go vet ./cmd/... ./internal/...`，前端使用 `npm test` 与 `npm run build`。Windows 本机 Edge 的 JPEG 画面采集与鼠标输入已用测试页面验证；Linux 配置和 Docker CPU 渲染、网关容器的登录/预加载/持久化已验证。Ubuntu/openEuler 宿主机、跨设备 WebRTC/音频/键鼠及 GPU 仍需目标机器验收，见 [Docker 部署教程](../docs/docker-linux-deploy.md)。
