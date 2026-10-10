# 外观配色持久化

外观设置选择配色后立即预览，点击“保存配色”才持久化。保存结果通过弹框反馈；恢复默认配色也需要点击保存。管理后台登录、重新打开或更换浏览器后从服务端读取已保存配色。未保存的预览不写入数据库，也不再读写旧的 localStorage 配色项。

配色为当前 GoStream3D 服务的管理员共享设置，仅影响管理页面。认证前的登录页使用默认蓝色；观看场景不受影响。

数据库为项目数据文件所在目录下的 `appearance.sqlite`，默认 `data/appearance.sqlite`；设置 `GOSTREAM3D_SESSION_STORE` 时跟随其目录。Windows 发布包中的默认位置是 EXE 旁的 `data/appearance.sqlite`。`admin_preferences` 表的 `theme` 记录保存主题 ID。首次创建默认蓝色，已有记录不覆盖。采用不依赖 CGO 的 SQLite 驱动，原有项目 JSON、平台 JSON 设置继续使用原格式。

认证接口 `GET /api/v1/admin/appearance` 读取，`PUT /api/v1/admin/appearance` 接收 `{"theme":"aurora"}` 保存。只允许现有十种主题 ID，SQL 使用参数绑定。数据库锁等待上限 1 秒、接口操作期限 3 秒；只有写入提交后才返回成功。读取失败会禁止保存并提供重新加载按钮，写入失败保留当前预览并允许重试。请求采用后台已有超时封装，退出登录会取消待处理请求。

运行 `go test ./internal/appearance ./internal/api ./cmd/server` 和 `npm --prefix web test` 验证数据库重开持久性、锁失败、请求校验、身份认证、草稿与保存结果。构建前端后可运行 `npm --prefix web run test:appearance:browser` 验证真实 Edge 的弹框焦点、重新打开、失败重试和移动端布局；需安装 Playwright 或通过 `PLAYWRIGHT_MODULE_PATH` 指定已有的 Playwright 包目录。
