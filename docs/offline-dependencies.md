# Linux 运行依赖本地缓存

项目的 `third_party/debian-trixie-amd64/` 保存 Debian 13 amd64 软件包缓存：`packages/*.deb`、Debian 软件包索引、运行依赖 `manifest.json` 和运行加编译依赖 `manifest-build.json`。清单逐项记录包名、版本、原始 Debian 镜像 URL、字节数和 SHA-256。下载器在保存时校验每个文件；重新运行时会复核已有文件并跳过一致的包。当前缓存有 664 个 `.deb`，共 479,781,208 字节；其中 433 个属于运行依赖。

这些 `.deb` 用于后续构建无 Docker 的 Linux 离线发布包，不能在 Windows 上直接运行。目标机必须是兼容的 Debian 13 amd64 系统。Neko 3.1.5 源码和自定义 Xorg 驱动源码位于 `neko-3.1.5/`；Neko 的 Go 模块源码缓存位于 `neko-3.1.5/server/vendor/`；Linux Go 1.26.8 工具链归档位于 `third_party/toolchains/`。此缓存没有编译好的 Neko 和驱动，也没有 NVIDIA 内核驱动。Linux 离线发布包尚需在目标系统上编译、装配和验证。

需要重新下载时，从 `tools/debianfetch` 执行：

```text
go run . -build
```

只查看预计包数与体积：`go run . -plan -build`。不加 `-build` 只缓存运行包。下载器解析 `Depends` 和 `Pre-Depends`，选择仓库中的可用候选包；最终安装兼容性仍需在目标 Debian 13 系统上用 `apt` 检查。下载文件量较大，缓存目录和 `vendor/` 被 `.gitignore` 排除，备份或交付源码时请显式包含它们。

离线复核下载文件完整性：`go run . -verify -build`。下载工具自己的 Go 依赖也已放在 `tools/debianfetch/vendor/`。
