# Linux 服务器黑屏排查与修复记录

服务器：192.168.1.100，部署目录 `/root/GoStream3D-docker`。

实际系统为 openEuler 24.03 LTS，Docker 25.0.0，Compose 2.32.4。

## 原因和修复

原 Worker 日志反复出现 `token not found - make sure you are not using Cookie auth on the server`。Neko 3.1.5 的默认 Cookie 登录模式不返回 legacy `/ws` 适配器所需的 token，导致观看连接无法完成登录。

在服务器上构建兼容镜像，设置 `NEKO_SESSION_COOKIE_ENABLED=false`，保留原有账号密码认证；随后重启 gateway，让它重建 Worker。项目数据和管理员密码未修改。

服务器的 `images.tar` 已替换为包含修复镜像的离线包，`images.tar.sha256` 已更新并校验通过，防止下次执行 `start.sh` 时重新载入旧镜像。

## 验证结果

- 新 Worker `50c18ca5fc5e` 运行正常，健康检查通过，环境变量已生效。
- 实际观看连接的 ICE 和 WebRTC 状态均为 connected。
- 实际观看连接累计发送约 26.7 MB 数据，接收端 RTCP 反馈丢包数为 0。
- Chromium 截图正常，目标为 `http://192.168.1.100:5666/login`，显示 示例 NAS 主机 飞牛登录页面。
- 重新执行 `bash start.sh` 成功，离线包校验、镜像加载和 Compose 健康检查均通过，修复镜像保留。
- 当前项目为单路会话；已有观看连接占用时，额外测试连接收到 HTTP 429，这是并发限制。
- 客户端最终显示效果仍应以观看页面为准；服务器指标不能直接证明客户端解码后的 FPS。

## 日常启动

```bash
cd /root/GoStream3D-docker
bash start.sh
```

查看容器状态：

```bash
docker ps
docker-compose logs --tail=100 gateway
```

## 备份

- 原 Worker 镜像：`gostream3d/neko/chromium:3.1.5-before-cookie-fix`
- 原离线包：`.fix-cookie-auth/images.before.tar`
- 原校验文件：`.fix-cookie-auth/images.before.tar.sha256`
- 修复 Dockerfile、浏览器截图及本记录：`.fix-cookie-auth/`

Docker Hub 上的镜像和本地原始发行安装包尚未随本次服务器修复更新。
