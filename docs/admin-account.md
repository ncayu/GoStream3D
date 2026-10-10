# 管理员账号修改与恢复

登录管理后台，点击右上角用户名或头像，选择“修改账号密码”。填写账号名称、当前密码、新密码及确认新密码，然后点击“保存并重新登录”。账号名称可以保持原值；新密码至少 12 个字符，最多 72 字节，中文字符占多个字节。账号名称不含空白，最多 64 个字符。

保存成功后返回登录页，使用新账号和新密码登录。其他已登录的后台会话和旧版管理员 API Key 同时失效；渲染实例、项目和观看链接保留。密码错误、保存失败会显示原因，失败时原账号仍有效。当前密码连续验证失败 5 次后，请等待一分钟再试。

首次登录读取环境变量或 `config.env` 中的初始账号。网页修改后，账号保存在 `GOSTREAM3D_SESSION_STORE` 对应目录的 `admin-account.json`（默认 `data/admin-account.json`），密码保存为带盐 bcrypt 哈希。保存采用同目录临时文件与替换，Linux 文件权限为 `0600`；重启后优先使用此文件中的账号。Docker 部署的 `data` 挂载目录需要持久保存并允许网关写入。文件损坏时启动报错，不会自动恢复初始密码。

忘记密码时，在服务器上停止程序，备份并移走当前数据目录的 `admin-account.json`，在 `config.env` 设置新的 `GOSTREAM3D_ADMIN_USERNAME` 与 `GOSTREAM3D_ADMIN_PASSWORD` 后重新启动。如果使用环境变量指定初始凭据，应同步修改环境变量。该操作恢复初始配置中的账号，不会删除项目文件。再次登录后可继续在网页修改账号密码。

管理员接口：

- `GET /api/v1/auth/me`：读取当前账号名称和 `account_update_supported`，需要登录。
- `PUT /api/v1/auth/account`：需要有效登录及同源请求，JSON 字段为 `username`、`current_password`、`new_password`。成功返回 `200` 并清除当前 Cookie、注销全部旧会话；当前密码错误返回 `403`，验证过多返回 `429`，参数错误返回 `400`，持久保存失败返回 `500`。无账号存储的嵌入式路由返回 `503`。

验证命令：`go test ./internal/api ./cmd/server ./internal/config`；网页端到端验证使用 `npm run test:account:browser`，运行前设置 `ADMIN_ACCOUNT_TEST_BINARY` 为最新 Windows 构建路径，并通过 `PLAYWRIGHT_MODULE_PATH` 指定 Playwright 安装位置。测试独立启动临时实例，覆盖新旧密码、退出登录、保存失败、重启恢复、重复修改和手机表单布局，不修改当前安装的凭据或项目。
