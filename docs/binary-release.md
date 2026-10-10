# GoStream3D 二进制安装包（Windows / Linux x86_64）

两个平台分别使用自己的可执行文件，管理网页已内嵌，无需安装 Go、Node.js 或编译源码。ARM 设备不适用本次 amd64 包。

运行 `scripts/package-release.ps1` 每次使用北京时间生成新的打包时间戳，文件名为 `GoStream3D-windows-amd64-YYYYMMDD-HHMMSS.zip` 和 `GoStream3D-linux-amd64-YYYYMMDD-HHMMSS.zip`，对应校验文件为 `SHA256SUMS-YYYYMMDD-HHMMSS.txt`。旧包保留；即使指定相同的 `GOSTREAM3D_RELEASE_VERSION`，不同时间打包的文件名仍然不同。

## Windows

将 Windows ZIP 完整解压到可写目录，双击 `start.bat` 即可启动后台服务并自动打开管理页面。需要本机已安装 Edge、Chrome 或 Chromium；默认使用本机浏览器渲染。关闭启动窗口不停止后台服务，再次双击会复用同目录的现有服务。可在任务管理器中结束对应的 `gostream3d.exe` 来停止。

脚本优先使用 `config.env` 中的端口（默认 `8888`）；端口被占用或被 Windows 保留时，会依次尝试 `10088`、`18888`、`28888`，显示实际地址，保留原配置。也可停止服务后编辑 `GOSTREAM3D_HTTP_PORT` 固定为可用端口。日志在同目录 `gostream3d.log`，启动失败时启动窗口会保留原因。

手动运行 `gostream3d.exe` 仍然可用，使用配置文件中的端口。只有 `start.bat` 提供端口回退和浏览器自动打开功能，需保留同目录的 `start-windows.ps1`。

## Linux

二进制为 Linux x86_64 静态链接网关，可用于 Ubuntu 22.04 和 openEuler 22.03。页面渲染依赖需另行准备：原生模式需要 Neko、Chromium、Xorg、PulseAudio 和相关插件；Docker 模式需要 Docker Engine 与 Neko Worker 镜像。安装包不包含这些渲染依赖或镜像。

依赖齐全的原生安装可直接启动：

```bash
chmod 755 gostream3d start.sh
chmod 600 config.env
./gostream3d --doctor
bash start.sh
```

新服务器建议让网关二进制管理 Docker Worker，Docker API 需为 1.44 或更新版本。先从已经发布的仓库拉取 Worker：

```bash
docker pull ncayu8847/gostream3d-neko-chromium:3.1.5
```

编辑同目录 `config.env`，将 `GOSTREAM3D_RUNTIME=native` 改为 `GOSTREAM3D_RUNTIME=docker`，增加：

```dotenv
GOSTREAM3D_NEKO_IMAGE=ncayu8847/gostream3d-neko-chromium:3.1.5
GOSTREAM3D_DOCKER_HTTP_BIND_IP=127.0.0.1
```

把已有的 `GOSTREAM3D_ICE_PUBLIC_IP=127.0.0.1` 改成观看者能够访问的服务器 IP，然后执行上面的检查和启动命令。运行用户需能访问 Docker Socket。浏览器打开 `http://服务器IP:8888/admin`；允许网关 TCP 端口及配置中的 Worker UDP 端口段（默认 `52000–52023`）。后台进程可交给现有 systemd 服务管理；前台输出即启动日志。

原生依赖的参考说明见 `NATIVE-LINUX.md`；其中的系统包命令以 Debian 13 为例，Ubuntu/openEuler 需按各自发行版调整。

## 登录、数据与升级

首次账号为 `config.env` 中的 `GOSTREAM3D_ADMIN_USERNAME`（默认 `admin`），首次密码为同文件的 `GOSTREAM3D_ADMIN_PASSWORD`。发布配置默认账号为 `admin`、默认密码为 `Admin@2026.com`；本机已修改的账号和项目数据不会加入安装包。登录后可在右上角用户菜单选择“修改账号密码”，保存后使用新凭据重新登录。

升级现有安装时，先停止旧程序并备份整个安装目录，仅替换对应平台的可执行文件。保留原有 `config.env` 和 `data` 目录，不使用安装包的新配置覆盖现有配置。网页修改过的账号保存在 `data/admin-account.json`，重启后仍然生效，详细恢复方法见 `ADMIN-ACCOUNT.md`。

可通过 `gostream3d.exe --version` 或 `./gostream3d --version` 查看版本，通过 `/healthz` 检查网关健康。网关健康不等于真实 3D 场景已经渲染成功，部署后仍需打开一个实际项目验收画面和交互。

“平台监控 → GPU 与显存”提供近 1 小时 GPU 使用率折线图，默认每 30 秒采样；多块 NVIDIA 显卡分别显示曲线。趋势从服务启动后开始累计，采样不可用时保留空缺；重启后历史重新积累。
