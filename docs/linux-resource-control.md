# Linux CPU 配额与 NVIDIA 监控

## 本次修复

交付版本：`20261009-225730`。已部署到 `192.168.1.100`。

此前 CPU 上限只实现了 Windows Job Object，Linux Docker 的后台不会提供调整入口；`GOSTREAM3D_WORKER_NANO_CPUS` 仅控制单个 worker，不能代替全部实例和网关的共享上限。

新版将网关与全部 CPU/GPU worker 放入同一个 `gostream3d.slice`，使用 cgroup v2 的 `cpu.max` 实施共享 CPU 上限。保存后直接更新控制组，不需要重启渲染实例；设置写入与会话数据同目录的 `cpu-settings.json`，服务启动时在预加载之前恢复。当前目标服务器对应 `/root/GoStrem3D/data/data/cpu-settings.json`。新增实例自动加入同一组。其他软件的 CPU 使用不计入此配额，平台监控中的整机 CPU 使用率仍包括其他软件。

百分比按 Linux 可见逻辑核心总数计算。例如 16 核设置 25%，对应每 100 毫秒最多共使用 400 毫秒 CPU 时间，即约 4 个核心的总能力。多个实例共用这 4 核预算，而非每个实例各分配 4 核。CPU 上限不能控制 GPU 百分比；降低 CPU 配额也可能降低 GPU 场景的帧率。

NVIDIA 采集此前失败，是因为 `nvidia-smi` 安装在宿主机，后台网关容器没有 NVIDIA utility 驱动及命令。新版启动脚本为具备 NVIDIA 驱动与 runtime 的服务器启用网关的 NVIDIA runtime；WSL2 映射 `/usr/lib/wsl`，采集工具会自动查找 `/usr/lib/wsl/lib/nvidia-smi`。原生 Linux 由 NVIDIA Container Toolkit 注入工具和驱动库。

采集显卡名称、UUID、GPU 使用率、编码器使用率、显存已用量及总量。GPU 趋势沿用监控采样间隔。整卡数据包含其他软件，不将它归属到单个项目；驱动返回 N/A 时保留为空并显示原因。WSL2 指标支持因 Windows 驱动版本而异。

## 部署与升级

在服务器软件目录使用新版镜像包及配置：

```bash
sudo bash start.sh
```

脚本保留现有账号和项目，备份 `.env`，在 systemd / cgroup v2 Docker 主机上创建并启用持久的 `gostream3d.slice`，将下面的部署值写入 `.env`：

```ini
GOSTREAM3D_CGROUP_PARENT=gostream3d.slice
GOSTREAM3D_CPU_CGROUP_DIR=/sys/fs/cgroup/gostream3d.slice
GOSTREAM3D_CPU_CGROUP_PATH=/host/cpu/cpu.max
```

检测到可用 NVIDIA 工具及 runtime 时还写入：

```ini
GOSTREAM3D_GATEWAY_RUNTIME=nvidia
# 仅 WSL2：
GOSTREAM3D_GPU_LIBRARY_DIR=/usr/lib/wsl
```

之后可照常 `docker-compose -f docker-compose.yml up -d`。必须同时更新 Compose 和镜像；单纯 `restart` 旧网关不会添加控制组及驱动挂载。观看页、后台请 Ctrl+F5 刷新。

Docker 需使用 systemd cgroup driver、cgroup v2 及 CPU 控制器。新版自动配置在此环境完成；其他环境可人工提供同一可写控制组和 parent。原生 Linux 可将服务置于具备 CPU 控制器的 systemd 单元内，并将 `GOSTREAM3D_CPU_CGROUP_PATH` 指向该单元的 `cpu.max`；子进程必须继承同一组。未配置时后台仍明确提示未启用，不能把“保存成功”当成已限额。

网关仅以读写方式挂载本软件控制组，宿主机统计目录仍只读。不需要 privileged，不挂载整个 cgroup 层级为读写。NVIDIA 驱动及 Toolkit 仍由宿主机提供；WSL2 不安装 Linux NVIDIA 内核驱动。

## 核对方法

```bash
docker-compose ps
cat /sys/fs/cgroup/gostream3d.slice/cpu.max
docker inspect $(docker-compose ps -q gateway) --format '{{.HostConfig.CgroupParent}}'
docker ps --filter label=gostream3d.managed=true -q | xargs -r docker inspect --format '{{.Name}} {{.HostConfig.CgroupParent}}'
```

设置 25% 后，16 核应读到 `400000 100000`；设置 100% 时为 `1600000 100000`。尚未设置、没有保存文件时为 `max 100000`。检查 quota 配合负载下的 `cpu.stat` 增量，比仅查看一瞬间的 CPU 百分比更可靠。

NVIDIA 检查：

```bash
docker exec $(docker-compose ps -q gateway) sh -c 'p=$(command -v nvidia-smi || true); [ -n "$p" ] || p=/usr/lib/wsl/lib/nvidia-smi; "$p" --query-gpu=uuid,name,utilization.gpu,utilization.encoder,memory.used,memory.total --format=csv,noheader,nounits'
```

后台“平台监控”应出现实际显卡卡片和 GPU 使用率趋势。等待下一次采样或点击刷新，不需要创建新的渲染项目。

## 服务器实测

2026-10-09 在当前 WSL2 Ubuntu / NVIDIA RTX 3050 Ti Laptop 服务器验证：

| 检查 | 结果 |
| --- | --- |
| Linux 可见逻辑核心 | 16 |
| 25% 上限对应 `cpu.max` | `400000 100000` |
| 饱和负载下，连续 5 秒的软件整体 CPU 使用率 | 25.01% |
| 测量期间被限额调度周期 | 50 |
| 网关与已有渲染 worker 的 parent | 均为 `gostream3d.slice` |
| 采集到的显卡 | NVIDIA GeForce RTX 3050 Ti Laptop GPU |
| 后台 GPU 页面实际截图值 | GPU 37%，编码器 18%，显存 2.74 / 4.00 GB |
| GPU 历史趋势 | 正常显示实际采样点 |
| 实际后台保存操作 | 99% 与 100% 均成功，最后恢复为 100% |
| 网关、原项目 | 网关 healthy，原项目预加载就绪 |

使用临时 CPU 负载容器验证共享组的总消耗，而非分别给实例设置限制。测试容器已删除。为保持原来的 CPU 性能空间，目前上限为 100%；管理员可自行保存 25%、40%、50% 等需要的比例。GPU 数值随负载变化，表内数值只对应截图当时。

设置文件保存和启动恢复由 Go 测试覆盖；systemd slice 已启用开机创建，但本轮未重启物理主机验收。前端单元测试、前端构建、Go 完整测试、GPU 图表浏览器回归及 Docker 容器回归均通过。实际服务器浏览器还验证了 CPU 输入可操作、保存成功、NVIDIA 卡片及趋势显示，无页面脚本错误。

备份目录：`/root/gostream3d-backup-resources-20261009-225835/`。新版包附带 `LINUX_RESOURCE_VERIFICATION.json` 和 `resource-proof/` 原始数据与后台截图。旧 CPU 设置未启用时没有保存的 CPU 文件；若回退到不支持 Linux CPU 控制的旧版本，应同时恢复旧 Compose 和备份配置。

参考：[Linux cgroup v2 CPU 控制器](https://www.kernel.org/doc/html/latest/admin-guide/cgroup-v2.html)、[Docker cgroup parent](https://docs.docker.com/reference/cli/dockerd/)、[NVIDIA WSL2 支持说明](https://docs.nvidia.com/cuda/wsl-user-guide/index.html)。
