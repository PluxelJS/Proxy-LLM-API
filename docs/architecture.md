# 实现结构

```text
cmd/dev-runtime
  ├─ internal/cli       Kong 参数/补全、JSON 输出、用户服务安装
  └─ internal/web       chi HTTP、会话边界、异步任务、嵌入前端
            │
       internal/app    应用用例、校验、修订号、生命周期、PG/代理操作
         │       │
 internal/state  internal/engine
 SQLite + goose  Moby client / 原生 Podman bindings
         │
   私有工作区文件
```

核心用例不导入 Kong 或 HTTP handler。前端以 React、TanStack Query、Radix Tabs、react-hook-form 实现，按编辑器/功能拆文件。JSON 服务模型是配置的唯一语义模型；没有同时维护 YAML、Compose 与数据库三种部署真相。

## 工作区与并发

- `state.db` 存服务配置、修订号、秘密、节点、数据库登记、任务、备份目录索引。Goose 只负责此新数据库未来的 schema 版本，不承担旧部署迁移。
- 配置实体使用类型化 Go struct，以 JSON 文档存入简单 SQLite resources 表。当前查询仅按 kind/id，不引入 ORM 或手写复杂 SQL 生成层。
- workspace mutation 文件锁同时约束 CLI 进程和 HTTP 工作线程；读操作不持有该锁。第一版串行执行变更，使容器与数据库操作的顺序清楚。
- 写配置要求 revision 匹配；bundle 全部预检再一次事务提交，任一冲突不落入部分更新。
- `server.lock` 限制一个网页 daemon。任务开始/结束写 SQLite；异常退出后标记 interrupted，不假装远端副作用能回滚，也不自动重放删除等操作。
- 网页提交返回 queued job；工作线程脱离请求 context，在 daemon context 下运行，有超时且正常退出时等待收尾。启动持两把锁恢复遗留任务，不把正在运行的 CLI 判为中断。
- 文件写入用同目录临时文件与 rename。工作区目录 0700、秘密/数据库文件 0600，主进程 umask 0077。秘密是本机明文，依赖本机用户权限边界。

## 容器与数据

- 引擎使用原生 Go SDK，常规管理路径不执行 docker/podman shell。安装 systemd 服务和集成测试清理使用外部工具。
- 默认支持本地 Unix socket。Docker 与 Podman 的连接行为封装在各自 adapter，应用只处理 Engine/Spec。
- 容器/网络/卷带独立 workspace 身份前缀；容器变更还校验标签所有权。没有全局 prune 或按模糊名称删除。
- 创建前先保证镜像可用，再停旧容器并替换。容器成功启动表示引擎接受启动，不等价于应用 ready；应用连接性由独立 diagnosis 报告。
- 保活交给引擎 restart policy 与宿主 systemd。daemon 不在循环中与用户 stop 操作互相对抗；重新启动 daemon 的 autostart 会启动已启用服务。
- 自动启动尊重依赖：CLIProxyAPI 选中节点时先启动 sing-box；Cloudflared 先启动 CLIProxyAPI 与 API 入口。
- PostgreSQL 初始化秘密与运行中的角色密码分开处理。数据库 DDL 用 PostgreSQL 自身 format 的 `%I`/`%L` 引用名字和字面值，错误输出不包含密码 SQL。
- 密码变更、容器替换和文件生成涉及外部副作用，不能承诺 SQLite 一笔事务实现跨系统回滚。错误任务保留、查询真实状态后再操作。
- 服务配置快照仅描述此工作区的服务；数据库备份另走 pg_dump。暂未提供全工作区灾难恢复、New API 在线备份、跨机器模板或跨 PostgreSQL 大版本自动升级。

## 本地 HTTP 边界

仅监听 127.0.0.1。校验 Host、浏览器 Origin，API 请求需本次 daemon 随机 token；不允许跨域。静态页 CSP 限制脚本来源。页面与所有有本机用户权限的程序拥有相同管理权，因此这是本地工具而非多用户授权系统。

API 入口 nginx 与 tunnel 网络分开，仅代理模型 API 路径。真实账号认证及应用协议由 CLIProxyAPI/New API 上游承担。

## 维护约束

新增服务先在默认值和 typed model 声明，再实现需要的应用特定诊断/管理动作；保持 CLI 与 HTTP 只做参数与结果映射。不要为每个管理页面复制执行命令或配置文件读写逻辑。

修复引擎行为时扩展同一真实集成用例，使用隔离工作区；没有运行对应引擎就不要把单元测试称作 Docker/Podman 兼容性验证。前端修改后 `make web` 更新嵌入产物，CI 校验可重建。

第一版采用一个工作区内每种内置服务一个实例。添加泛用多实例、任务取消或高级 PG schema 权限前，先扩展明确的数据模型和行为测试，避免插件框架或反射驱动编排。
