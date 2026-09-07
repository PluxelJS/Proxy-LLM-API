# 从 Hub / CLIProxyAPI 迁移

目标链路是「客户端 → New API → 上游」，一个 New API 容器和一个本地 SQLite
数据库，不需要 PostgreSQL、Dragonfly、CLIProxyAPI 或 sing-box。

1. 在旧版本仍可运行时，保存 `.env`、CLIProxyAPI 配置、OAuth 数据以及 Hub 数据库
   的一致备份。记录旧镜像版本和 Compose 项目名。备份必须留在私有目录。
2. 导出渠道 origin、上游密钥、模型列表和需要保留的客户端令牌。OAuth 订阅凭据不能
   直接当作 New API 的普通 API 渠道密钥；这类用户必须先确认可用的 API 上游。
3. 初始化新状态，在 `.env` 将 `NEW_API_PORT` 暂设为 23002，启动 New API 并完成
   Web 设置。添加实际渠道、模型、价格，开启消费日志和统计。
4. 用最小模型请求验证鉴权、流式完成、输入/输出 token、失败日志和重启后的记录。
   New API v0.13.2 的常规管理 API 生成随机客户端令牌，不支持直接指定旧令牌。
   通用迁移应更新客户端令牌；需要保留旧令牌时，必须另做针对实际数据库结构的
   离线迁移与鉴权验证，不应直接执行未经验证的 SQL 模板。
5. 停止旧入口及其自动启动服务，确认 23000 已释放，将新端口改为 23000 并重启。
   检查实际客户端请求和 Web 消费记录。
6. 确认新入口稳定后，删除旧网关容器和旧服务定义。PostgreSQL / Dragonfly 如仍被
   其他开发项目使用，应保留；否则停止并移除相关容器。旧数据先归档，不使用
   `down -v`、全局 `prune` 或不加区分的 `--remove-orphans`。

在 Home Manager 配置中移除旧 `services.proxyLlm`，改用
`services.newApiRuntime`；已有 dev-runtime 则只启用它的 `new-api` 目标，不再创建
第二套 systemd 网关服务。旧版本曾手工创建的
`default.target.wants/proxy-llm.service` 链接也应在切换时停用。

Hub 的 PostgreSQL 历史统计不会自动导入 SQLite。新统计从迁移后的请求开始；
如果需要审计旧统计，应单独保存原数据库或导出报表。

回退时先停止新入口，使用旧版本配置与旧数据库重新启动旧栈；不要将两个入口同时
绑定到 23000。此版本不再构建 CLIProxyAPI 镜像，也不把旧镜像的 `latest` 标签
替换为不兼容的 New API 镜像。
