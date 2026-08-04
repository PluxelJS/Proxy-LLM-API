# Docker 数据持久化目录

这里保存 Compose 内置服务的数据，目录内容不会进入 Git：

- `postgres/pgdata/`：PostgreSQL 18 数据目录。
- `dragonfly/`：Dragonfly 快照目录。
- `redis/`：迁移前 Redis 7 的保留数据，当前 Compose 不再写入。
- `sing-box/`：可选 sing-box 运行数据；只有设置代理节点时使用。

PostgreSQL 将宿主机 `data/postgres/` 挂载到容器 `/data`，并使用 `PGDATA=/data/pgdata`。Dragonfly 将 `data/dragonfly/` 挂载到 `/data`，每 5 分钟生成一次快照。

不要直接删除这些目录。迁移、恢复或调整权限前，应先停止对应容器并创建备份。
