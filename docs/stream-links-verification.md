# 观看链接与复制验证

验证日期：2026-10-01。运行版本：`20261001-stream3d`，监听 `0.0.0.0:8888`。

- 所有观看地址采用当前管理页面的协议、IP/域名和端口，路径统一为 `/Stream3D/:token`。列表和卡片的显示、复制、打开一致，即使 API 返回旧 localhost 链接也使用当前访问地址。
- 在 `http://192.168.1.100:8888/admin#links` 的普通 Chrome 环境中验证五个项目；未授予额外剪贴板权限，也未启用浏览器安全设置例外。该环境无 Async Clipboard API，实际点击复制后使用 Ctrl+V 粘贴，完整内容与新观看地址一致。
- 本机安全环境的 Clipboard API、兼容选区复制、两种方式均失败的错误提示、提示居中与自动关闭、重复点击及异步响应顺序均通过验证。复制不移动项目列表，兼容复制结束后恢复焦点。
- API 创建、编辑、列表返回的 URL 跟随请求 Host/TLS；测试覆盖局域网、本机、HTTPS 域名与 IPv6。代理可以转发新的观看路径。
- 所有既有 token 保留。新地址返回播放器 HTML；旧 `/v/:token` 返回 307 跳转并保留查询参数；不存在的 token 返回 404。
- 实际打开 `/Stream3D/REDACTED_SESSION_TOKEN`，收到 9 个二进制画面帧，播放器画布为 1920×1080。验证后关闭测试观看会话。
- 最新源码构建并重启成功，本机和局域网 `/healthz` 均返回 200。重启前后配置文件、项目数据、CPU 设置与监控采样设置的 SHA256 一致。

自动验证脚本：`tools/stream-links-check.cjs`、`tools/copy-feedback-check.cjs`；接口与代理验证：`go test ./internal/api ./internal/proxy`。局域网复制截图位于 `docs/ui-preview/copy-success-lan.png`。
