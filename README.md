# New API 渠道上传平台

独立的在线渠道创建平台：管理员维护平台用户，普通用户登录后只能读取 New API 分组/模型并创建渠道，不具备查询、修改或删除渠道的接口。

## 启动

PowerShell：

```powershell
$env:NEWAPI_BASE_URL='http://127.0.0.1:3000'
$env:NEWAPI_ACCESS_TOKEN='New API 管理员个人密钥'
$env:NEWAPI_USER_ID='1'
$env:ADMIN_PASSWORD='至少8位的初始密码'
$env:DATABASE_URL='postgres://专属用户:密码@数据库:5432/postgres?sslmode=require&options=-csearch_path%3Dnewapi_pan'
$env:HTTP_PROXY='http://127.0.0.1:7890'
$env:HTTPS_PROXY='http://127.0.0.1:7890'
go run .
```

访问 `http://127.0.0.1:8080`。首次空库启动会创建 `admin`；未设置 `ADMIN_PASSWORD` 时，随机密码会打印到控制台。

Docker：复制 `.env.example` 为 `.env`、填写 New API 配置后执行 `docker compose up -d --build`。
Docker Desktop 使用 7890 代理时，将代理地址写为 `http://host.docker.internal:7890`。

## 行为约束

- 非 Anthropic 渠道的 `base_url` 在服务端强制置空。
- Anthropic（类型 14）的 `base_url` 固定为 `https://openrouter.ai/api`。
- New API 个人密钥只从服务端环境变量读取，不下发浏览器，不写入审计。
- `setting`、`settings`、映射/覆盖字段及“额外渠道 JSON”覆盖 New API 添加渠道的完整配置能力。
- 平台使用独立 PostgreSQL 模式和专属数据库用户，`database/sql` 连接池最大连接数与最大空闲连接数均固定为 5。
- 平台密码使用独立随机盐与 120,000 轮 SHA-256 派生存储。

## 验证

```powershell
go test ./...
go build -o newapi-upload.exe .
```
