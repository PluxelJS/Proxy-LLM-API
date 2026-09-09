# 迁移到网页管理

CLIProxyAPI 使用官方 Management Center 网页维护账号和配置；`manage.sh` 只负责
初始化、镜像和服务生命周期。New API 保持独立，原入口改为 `new-api.sh`；
`new-api-runtime` 包及 `services.newApiRuntime` 不变。

## 从上一版运行时迁移

1. 备份整个私有状态目录。
2. 重建镜像（`build`），使镜像包含固定版本的官方管理页面，再执行 `restart`。
3. 脚本将原 `config.yaml` 迁移到 `settings/config.yaml`，保存原文件为
   `config.yaml.before-webui`；原 API key 和 `auth/` 保留。已有新路径时不会覆盖。
4. 自动生成独立的 `management-key`，通过 `ui` 查看本机页面地址和密码。
5. 使用 cloudflared 的部署，将 Cloudflare origin 从 `http://cli-proxy-api:8317`
   改为 **`http://api-entry:8080`**。新入口只开放模型 API，管理页面通过本机或 SSH
   转发访问。原直连 origin 在新的网络隔离下不可达。

移除了 `login <provider>` 和 OAuth 宿主回调端口配置。使用网页中的 device-code
流程、回调 URL 提交或凭据导入；旧 `CLIPROXY_*_CALLBACK_PORT` 字段自动清理。
旧 `generated/config.yaml` 不再使用，配置以 `settings/config.yaml` 为唯一来源。

## 从更早的 Hub / CLIProxyAPI 栈迁移

先备份旧 `.env`、CLIProxyAPI 配置、OAuth `oa/` 及镜像版本，然后在独立新状态目录
执行 `init`。将需要保留的配置复制到 `settings/config.yaml`，OAuth 文件复制到
`auth/`，按需在 `.env` 填入节点链接或隧道 token。不要把旧 `.env` 整体覆盖进来。

停止旧 CLIProxyAPI 及启动单元、释放 8317 后再启动新版。脚本不扫描或删除
`~/.local/state/proxy-llm` 下的归档数据，也不自动删除旧容器或卷。
旧 `services.proxyLlm` 改为独立的 `services.cliProxyRuntime`。
不恢复 Hub、PostgreSQL 或 Dragonfly，它们不是 CLIProxyAPI 的依赖。

## 已有 New API

继续使用原 `dev-runtime`、`new-api-runtime`，或把自定义脚本中的 New API
`manage.sh` 调用改成 `new-api.sh`。不需要迁移 SQLite 数据。
New API 默认 23000，CLIProxyAPI 默认 8317，可各自独立运行。

如需将 CLIProxyAPI 接为 New API 渠道，另行配置容器间可达地址、CLIProxyAPI API key
及模型列表；OAuth 凭据和管理密码不能当作渠道 API key。两套服务不自动互接或迁移用量。

最新版默认允许直连；sing-box 改为可选，其转发探测失败仅警告，不阻止启动。
