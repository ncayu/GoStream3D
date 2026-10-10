# Linux CPU 观看黑屏：Neko Cookie 登录兼容修复

## 已确认的原因

本次对 `192.168.1.100:8888` 的检查中，网关健康接口正常，项目显示渲染实例已经就绪。但实际连接观看 WebSocket 时，Neko 返回：

```text
event: system/disconnect
title: couldn't create a new session
message: token not found - make sure you are not using Cookie auth on the server
```

Neko 3.1.5 默认启用 Cookie 登录，GoStream3D 使用的 Neko 旧版 WebSocket 适配器需要登录响应中的 token。Cookie 模式下响应没有该 token，适配器在视频协商前即断开连接，观看页因此可能一直黑屏、显示 0 FPS。

修复为在 Worker 中设置 `NEKO_SESSION_COOKIE_ENABLED=false`，让内部登录使用 token。管理员后台的登录不由该配置管理。健康检查、页面就绪和 Chromium 截图不能代替观看信令检查。

## 在已有 Linux 安装上修复

截图中的 Worker 使用 `gostream3d/neko/chromium:3.1.5`。以下方法复用已安装的镜像，不重新下载浏览器依赖。先保留旧镜像名称，再构建同名修复镜像：

```bash
sudo docker tag gostream3d/neko/chromium:3.1.5 gostream3d/neko/chromium:3.1.5-before-cookie-fix

sudo docker build --pull=false -t gostream3d/neko/chromium:3.1.5 - <<'EOF'
FROM gostream3d/neko/chromium:3.1.5
ENV NEKO_SESSION_COOKIE_ENABLED=false
EOF
```

构建成功后，在 GoStream3D 后台对受影响项目执行 **停止渲染 → 启动渲染**，让网关重新创建 Worker，再刷新观看页。`docker restart` 旧 Worker 仍会使用旧镜像配置，不能替代重新创建。

这不会删除项目配置与管理员密码。更新会暂时中断该项目的观看连接。

在已有安装包中再次执行 `start.sh` 会重新导入包内旧 `images.tar`，可能覆盖修复标签。修复后使用 `docker-compose up -d` 启动，或者在制作新的离线包时包含修复后的镜像。

检查新创建的 Worker 是否包含修复：

```bash
sudo docker ps --filter label=gostream3d.managed=true --format '{{.ID}} {{.Names}}'
# 将下面的容器ID替换成上一步输出中的对应实例。
sudo docker exec 容器ID printenv NEKO_SESSION_COOKIE_ENABLED
```

应输出 `false`。若要回退镜像标签：

```bash
sudo docker tag gostream3d/neko/chromium:3.1.5-before-cookie-fix gostream3d/neko/chromium:3.1.5
```

回退后同样需要在后台停止并启动渲染实例。

## 查看 Worker 的真实浏览器页面

这个镜像的 HTTP 根路径只有占位页面，没有完整 Neko 网页客户端。截图中的 `127.0.0.1:30000 → 8080` 是 Neko 内部 HTTP 接口，不能直接用它观看 Chromium 页面。

真实浏览器通过 CDP 调试。截图显示 CDP 映射为 Linux 宿主机 `127.0.0.1:31000 → 9222`，可以使用 SSH 转发访问：

1. 在自己电脑的终端或 PowerShell 执行，使用你已有的 SSH 账号：

   ```bash
   ssh -N -L 9222:127.0.0.1:31000 root@192.168.1.100
   ```

2. 保持终端连接，在本机 Chrome 打开 `chrome://inspect/#devices`。
3. 启用 **Discover network targets**，点击 **Configure**，添加 `127.0.0.1:9222`。
4. 在发现的页面旁点击 **inspect**。可查看 Console、Network；打开 DevTools 的 Screencast 功能，可查看容器 Chromium 的实际画面。

也可在自己的电脑检查目标页面列表：

```bash
curl http://127.0.0.1:9222/json/list
```

多实例时，每个 Worker 的 CDP 宿主端口可能不同，以当前 `docker ps` / `docker port 容器ID 9222/tcp` 输出为准。CDP 能控制浏览器，保持它绑定回环地址，通过 SSH 转发使用。

官方说明：[Chrome 的远程调试入口](https://developer.chrome.com/docs/automation-and-testing/debug-headless)。

## 修复后仍黑屏时

本次已确认的是登录阶段故障。若修复后观看仍无帧，需要继续区分页面渲染和媒体传输：先用上述 CDP 查看目标网页是否加载和渲染正常，再检查 WebRTC 状态、实际服务器 IP、UDP 端口段及防火墙。默认需要 TCP 8888 和 UDP 52000–52023；当前首个 Worker 实际使用 52000–52007。

Worker 应用日志在容器内：

```bash
sudo docker exec 容器ID tail -n 100 /var/log/neko/neko.log
sudo docker exec 容器ID tail -n 100 /var/log/neko/chromium.log
```

分享日志前去除密码、登录 token 和敏感业务 URL。

## 本地验证

修复前，真实 Docker Worker 经网关同款 WebSocket 代理连接，复现相同的 Cookie/token 拒绝错误。修复后，同一检查收到包含视频的 `signal/provide`，Chromium WebGL 截图仍通过。测试只操作独立测试实例；尚未替换上述 Linux 服务器的镜像或验证其修复后的跨设备视频效果。
