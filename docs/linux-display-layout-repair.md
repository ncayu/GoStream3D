# Docker Worker 页面布局与黑边修复

2026-10-06 在 192.168.1.100 实机检查：项目为固定 1920×1080，Xorg 桌面为 1920×1080，但 Chromium 应用窗口只有 945×1060。采集器将小窗口和未使用的黑色桌面一起编码，目标网页也按竖屏宽度进行响应式布局。

容器没有窗口管理器，Chromium 的 `--start-maximized`、`--kiosk` 参数不能保证应用窗口铺满桌面。手动将窗口调整为桌面尺寸后，飞牛登录页面立即恢复横屏布局。

## 持久修复

- Chromium 启动时显式设置窗口尺寸及设备缩放比例 1。
- Supervisor 运行 `sync-chromium-window.sh`，将 Chromium 主窗口的位置和大小保持为当前 Xorg 桌面尺寸。
- 桌面因自适应观看窗口切换分辨率时，浏览器主窗口同步变化。
- 仅处理 Chromium 普通主窗口，跳过菜单、提示和文件选择对话框。窗口尺寸正确时不重复调整。
- 保留上次的 `NEKO_SESSION_COOKIE_ENABLED=false` 登录修复。

## 实机验证

服务器新 Worker 为 `3137001910be`，窗口与桌面均为 1920×1080，位置为 (0,0)。`chromium-window` 进程正常运行，Worker 与 gateway 健康，实际观看连接保持 connected，RTCP 反馈丢包为 0。

服务器 `images.tar` 与校验文件已更新，重新执行 `bash start.sh` 后修复保留。镜像备份标签为 `gostream3d/neko/chromium:3.1.5-before-display-fix`，上一版离线包在 `.fix-display-size/images.before.tar`。

## 回归验证

增加了实际 Chromium 页面内容尺寸和 Xorg 桌面尺寸一致的部署测试。旧镜像失败，实际页面为 945×1060；修复镜像通过初始 1280×720，以及 1600×800 → 560×960 → 1920×1080 → 1280×720 的实际自适应切换，CPU WebGL 绿色画面截图通过。

```powershell
$env:GOSTREAM3D_TEST_DOCKER_DEPLOY='1'
$env:GOSTREAM3D_TEST_WORKER_IMAGE='gostream3d/neko/chromium:3.1.5-display-fix'
go test ./internal/deploycheck -run TestDockerDeploymentWorker -count=1 -v
```

## 固定分辨率与自适应

固定 1920×1080 的视频比例为 16:9，观看窗口比例不同会保留对称黑边，这是保持图像比例的显示行为。需要让目标网页随观看窗口重新排版时，项目分辨率应选择“自适应观看窗口”。本次保留服务器当前的固定分辨率设置。

Windows 本地浏览器的密码管理器弹窗属于本地扩展界面，不会出现在服务器 Chromium 中。字体和视频压缩也可能带来细微视觉差异。

本次更新了源代码、服务器 Worker 镜像与服务器离线包；Docker Hub 和本地旧发行 zip 尚未更新。
