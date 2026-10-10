# Linux 一键部署验证说明

验证日期：2026-10-06。目标部署系统为 x86_64 Ubuntu 22.04 / openEuler 22.03，使用 Docker CPU 渲染镜像。

## 已完成的检查

- 网关 Go 包测试：`go test ./cmd/... ./internal/...`。
- 静态检查：`go vet ./cmd/... ./internal/...`。
- Bash 安装、启动脚本语法检查。
- 在真实 Linux 环境运行配置与路径测试：首次生成三个独立密码、配置权限 `0600`、重复初始化保留配置、显式缺失配置报错、相对路径以配置目录为基准。
- Linux amd64 发布程序实际运行：版本输出、初始化 Docker 配置、重复初始化保留原配置和权限。
- ZIP 使用 Linux 兼容的正斜杠路径，安装与启动脚本使用 LF 换行；不打包开发机账号、配置或项目数据。
- 实际构建 CPU 镜像：修改版 Neko、RandR 补丁后的 Xorg dummy 驱动、自定义输入驱动，以及 Chromium 154、D-Bus、PulseAudio 和 GStreamer。
- 使用网关真实 Docker 管理器启动 CPU 容器，检查 Neko HTTP、浏览器 CDP、目标页面就绪，再截取并验证 WebGL 画面；通过后清理该测试容器。测试命令为 `GOSTREAM3D_TEST_DOCKER_DEPLOY=1 go test -v ./internal/deploycheck -run TestDockerDeploymentWorker -count=1 -timeout 130s`。

最终镜像标签：`gostream3d/neko/chromium:3.1.5`；构建 ID：`sha256:8605c87ae0cc7042a67b8c13a426e0e213e65f093f906e810a211146e75cd98e`。随包 `worker.tar` 由该镜像导出，安装器自动导入，服务器无需重新编译渲染依赖。

实测修复了 GUI Chromium 只监听回环调试地址的问题：浏览器在容器内监听 9223，由受 supervisor 管理的代理提供容器端口 9222；宿主机默认仅绑定 `127.0.0.1`。同时补齐了 Debian 精简镜像缺失的 D-Bus 系统配置。

## 目标机仍需验收

开发机的 Linux 测试使用 WSL；Docker 镜像在 Docker Desktop 中构建。尚未在 Ubuntu 22.04 / openEuler 22.03 物理机或虚拟机上运行完整安装器。

目标机安装后，应确认 `gostream3d.service` 和 Docker 服务能开机启动，在另一台设备验证 WebRTC 视频、音频、键鼠，以及防火墙和 NAT 的端口规则。部署自检 `--doctor` 只检查配置、Docker 连接及镜像存在，不代替这些验收。

这份部署包支持 CPU 渲染；ARM 与 NVIDIA GPU 不在本次验证范围内。
