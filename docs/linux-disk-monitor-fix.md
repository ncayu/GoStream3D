# Linux 磁盘监控修复（2026-10-09）

## 原因与修改

旧版使用 `disk.PartitionsWithContext(ctx, false)`。Docker 网关的根目录为 overlay，其他可读磁盘路径为绑定挂载，均被库的过滤规则排除，导致磁盘分区列表为空、总容量为 0。

Linux 采集改为解析选定挂载命名空间的 mountinfo，通过挂载路径获取文件系统容量。Compose 只读挂载宿主机根目录和 PID 1 的 mountinfo；API 展示宿主机路径。同一文件系统按设备号去重，保留 Btrfs 子卷及可读的绑定挂载，排除虚拟文件系统、Snap 和 Docker overlay 子挂载。原生 Linux 无需宿主机挂载配置，Windows 继续使用原有采集方式。

## 验证

- 新增 Linux 回归用例在旧实现下失败，修复后通过；覆盖宿主机路径映射、子卷、重复容量、路径转义、虚拟文件系统过滤、不可读挂载和缺失配置。
- Windows 完整后端测试 `go test ./cmd/... ./internal/...` 通过；Linux 监控测试二进制在容器中运行通过。
- Docker Desktop 使用挂载夹具验证宿主机路径映射；容器登录、CPU 预加载、项目保留及账号修改后重建均通过。Linux 根挂载传播在真实服务器验证。
- Ubuntu 服务器 `192.168.1.100` 已部署网关版本 `20261009-131013`。监控返回 `/`、`/boot`、`/boot/efi`、`/data`，总容量 `1005780172800` 字节，各分区总容量与宿主机 `statvfs` 一致，监控错误列表为空。
- `.env` 和 `data/config.env` 的 SHA256 在升级前后相同，原有项目身份保留。服务器健康接口与管理页均返回 HTTP 200。

服务器备份和验收记录位于 `/root/GoStrem3D/backups/disk-monitor-20261009-131013/`。新 Docker 安装包为 `dist/GoStream3D-docker-compose-linux-amd64-20261009-131544.zip`，配套 SHA256 校验通过。升级其他 Linux 安装时需同步 Compose 后执行 `docker-compose up -d --wait`，以添加只读宿主机挂载。
