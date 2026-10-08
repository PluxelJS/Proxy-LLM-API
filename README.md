# Dev Runtime

Linux / WSL 上的开发服务管理器。一个 Go 二进制内嵌网页，同时提供面向自动化和 agent 的 Kong CLI。两种入口调用同一套应用逻辑，直接共享本地 SQLite 工作区；CLI 无需先连接管理 daemon。

这是全新实现，不读取旧部署，也没有迁移或旧命令兼容层。

## 启动

运行机器只需要本项目的 Linux 二进制，以及已启动、当前用户可访问的 Docker 或 Podman API。无需 Python、Node、Compose、psql 或 Go；这些只涉及源码开发。

```bash
# Podman：启用用户 API socket；Docker 用户确保 docker.service 已启动且有访问权限。
systemctl --user enable --now podman.socket

# 把二进制放进 PATH，例如 ~/.local/bin/dev-runtime。
dev-runtime init --engine podman   # 或 --engine docker；省略时自动探测
# 首次直接运行也会初始化；启动网页与已启用的服务。
dev-runtime
```

打开 **http://127.0.0.1:8318**。默认启用 PostgreSQL 和 Dragonfly；其他服务在页面启用。后台常驻：

```bash
dev-runtime service install
systemctl --user status dev-runtime.service
```

daemon 由 systemd 保活；应用容器由引擎重启策略保活。用户服务默认登录后启动，若需要退出登录后继续运行，可配置系统的 user lingering。未启用 systemd 的 WSL 可以前台运行 `dev-runtime serve --autostart`；Podman API 也可用 `podman system service --time=0` 启动。详见 [Podman 官方说明](https://docs.podman.io/en/latest/markdown/podman-system-service.1.html)。

独立工作区和端口：

```bash
dev-runtime --state-dir /absolute/path/runtime init --engine podman
dev-runtime --state-dir /absolute/path/runtime serve --listen 127.0.0.1:18318
```

`serve` 不带 `--autostart` 时只启动网页。多个工作区需要为服务分别设置不同宿主端口。已包含旧数据的默认目录会被拒绝，请选择空目录。安装用户服务前先把二进制放到固定位置。

## 管理范围

| 服务 | 功能 | 默认宿主端口 |
| --- | --- | --- |
| PostgreSQL | 数据库、登录角色、密码、只读/读写授权、连接串、备份恢复 | 5432 |
| DragonflyDB | 常用参数表单、完整 argv、持久化与 PING 诊断 | 6379 |
| VictoriaMetrics / VictoriaLogs | 生命周期、参数、持久化、HTTP 检查 | 8428 / 9428 |
| New API | 生命周期、参数、独立 SQLite 数据 | 23000 |
| CLIProxyAPI | 生命周期、节点出站切换、实际请求诊断、原管理页入口 | 8317 |
| sing-box | 节点保存/切换、配置校验 | 仅容器网络 |
| Cloudflared / API 入口 | 可选 tunnel；入口只代理 `/v1/` 与 `/v1beta/` | 仅容器网络 |

所有服务可修改镜像、完整 argv、环境变量、端口、挂载、容器用户、资源限制和重启策略。网页使用 Dashboard 与服务侧栏：侧栏展示状态、待应用配置，并提供启动、停止、重启和自启动操作；选择服务后进入其专属管理页。PostgreSQL 集中管理数据库、用户和备份，sing-box 管理节点，CLIProxyAPI 管理出站诊断与原管理页入口。

### 配置与执行

配置保存在 SQLite；保存仅更新期望配置，应用后生效。每份服务配置有修订号，旧页面或 CLI 的过期写入会报冲突。服务导出可往返编辑，导入在一个事务中完成：

```bash
umask 077
dev-runtime config export --reveal > services.json
# 编辑 JSON，保留 revision。
dev-runtime config validate services.json
dev-runtime config save services.json
dev-runtime services plan dragonfly
dev-runtime config apply dragonfly --revision 2
```

`--reveal` 导出包含私密环境变量。默认查看隐藏环境变量和 argv；脱敏导出不可回写。导出是当前工作区的**服务配置快照**，不是包含账号、节点和数据库数据的整机备份，也不是跨机器模板。

argv 是 JSON 字符串数组，直接交给引擎，不经 shell。镜像的 ENTRYPOINT 保留，argv 替换 CMD，例如 Dragonfly：

```json
[
  "--logtostderr",
  "--dir=/data",
  "--dbfilename=dump",
  "--snapshot_cron=*/5 * * * *",
  "--maxmemory=1gb",
  "--proactor_threads=2",
  "--default_lua_flags=allow-undeclared-keys"
]
```

也可以 `services args set dragonfly --file args.json --revision 1`。常用表单与 argv 编辑的是相同数据。`memlock` 可通过完整服务 JSON 设置，默认沿用引擎限制以兼容 rootless 模式。

`services enable` 启动并纳入下次 daemon 的 autostart；`disable` 停止并取消 autostart。`start/stop` 是本次运行操作。`pull` 拉取镜像并更新待应用修订号，再 `config apply` 替换容器；镜像拉取失败不停止旧容器。替换时保留数据卷，但变更数据挂载目标或数据库大版本仍需要自行评估。

### PostgreSQL

```bash
dev-runtime services start postgres
dev-runtime diagnose postgres
dev-runtime pg database create demo --generate-password
dev-runtime pg connection demo --reveal

dev-runtime pg user create reader --generate-password
dev-runtime pg user grant reader --database demo --permission readonly
# 密码走 stdin，不放进命令行参数。
dev-runtime pg user set-password demo_owner --stdin

dev-runtime pg backup demo --name demo-first
dev-runtime pg restore demo-first --target demo_restored
```

新数据库默认有独立 owner；创建后验证连接。可显式选择已存在的角色。权限管理覆盖目标数据库的 public schema 现有表/序列，以及该数据库登记 owner 后续创建的对象；其他 schema 和其他 owner 不在此简化授权范围。`grant` 是增加授权，不是撤销旧权限。

密码从本工具生成或重置后才可取回。修改容器 `POSTGRES_PASSWORD` 不会改变已有库密码，因此初始化字段禁止当作密码编辑入口。系统角色受到保护。删除数据库/用户需要 CLI `--confirm` 或页面明确确认，不执行 CASCADE，也不强制断开连接。

备份采用 `pg_dump -Fc`，存放在工作区 `backups/`。恢复只允许不存在的新数据库，使用 `--no-owner --no-privileges`，恢复后的对象归管理角色；原角色密码/授权不会包含在库备份中。失败的新目标保留以便检查，不会覆盖旧库。备份是本地文件，异地保存需要另行安排。

### 代理节点

```bash
# 链接通过 stdin 输入。
dev-runtime nodes add --name office --stdin
dev-runtime nodes list
dev-runtime nodes apply NODE_ID
dev-runtime diagnose cliproxy
# 恢复直连
dev-runtime nodes apply direct
```

支持 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、AnyTLS、HTTP(S)、SOCKS5 分享链接；不支持的传输类型拒绝导入。活动节点编辑前须先切走。节点解析基于社区 sub2sing-box，生成配置交给 sing-box 校验。

出站诊断通过 CLIProxyAPI 的管理 API 发起实际请求，并核对测试前后的全局代理设置。它验证**全局出站链路**，不代表账号授权、账号级代理覆盖、模型推理或额度可用。账号授权和 CLIProxyAPI 特有配置继续在其原管理页处理。

Cloudflared 需要设置 `TUNNEL_TOKEN`，Cloudflare 端把路由指向 `http://api-entry:8080`。本项目的网页和 CLIProxyAPI 管理端点不会经默认 API 入口发布。

### 面向 agent 的 CLI

```bash
dev-runtime --json status
dev-runtime --json services inspect dragonfly --reveal
dev-runtime --json jobs list
dev-runtime --json jobs inspect OPERATION_ID
dev-runtime logs postgres
```

普通命令输出 `{ "schemaVersion": 1, "data": ... }`；失败通过非零退出码和 stderr 错误报告，`--json` 给出结构化错误。配置导出是可直接导入的原始 JSON，日志是原始文本，帮助/补全是 shell 文本。CLI 操作同步完成；网页长任务返回 job ID，关闭页面不会中止已提交任务。

```bash
# 放到对应 shell 配置文件中；补全直接来自 Kong 命令定义。
eval "$(dev-runtime completion bash --code)"
eval "$(dev-runtime completion zsh --code)"
dev-runtime completion fish --code | source
```

服务名静态补全，节点 ID 从工作区只读查询。补全能力使用 [kong-completion](https://github.com/jotaen/kong-completion)。

## 开发与验证

源码构建需要 Go 1.26、Node 22+、npm 和 make。前端构建产物已经内嵌并提交，普通 Go 构建无需 Node。

```bash
make build
make test
make web                  # 修改前端后重建嵌入资源
make ui-test              # 需要 Playwright Chromium；本机可指定 CHROMIUM_PATH
make integration ENGINE=podman  # 或 docker：需要对应引擎 CLI/API，用于隔离测试与清理
```

真实集成测试使用独立临时工作区、随机端口和带随机前缀的容器/卷，不接管现有服务。覆盖数据库权限/密码、备份恢复、重启持久化、各应用检查、参数应用及 CLIProxyAPI 经 sing-box 的出站选择。首次运行需要拉取镜像。默认测试不启动容器。

Nix 可用 `nix build`；Home Manager 导入 `homeManagerModules.default`，配置 `services.devRuntime.enable = true` 与 `services.devRuntime.engine = "podman"`。秘密保存在可写工作区，不放入 Nix 配置。Linux amd64/arm64 构建工作流会生成单二进制制品。

具体分层、故障语义与开发约束见 [架构说明](docs/architecture.md)。

### CLIProxyAPI 源码镜像

`.github/workflows/cliproxy-ghcr.yaml` 每天 UTC 19:17（北京时间次日 03:17）解析上游最新稳定版，并从对应提交源码仅构建 linux/amd64 镜像。也支持手动触发。发布到 `ghcr.io/pluxeljs/cliproxyapi`，提供 `latest`、`vX.Y.Z`、`upstream-<commit>` 标签。构建使用该版本上游原始 Dockerfile，并执行官方的模型目录刷新脚本；保留官方 CGO 编译参数、基础镜像和入口。无需 QEMU 或多架构 manifest 合并。启用 Go 缓存及独立的 `cliproxyapi-amd64` 镜像层缓存；管理页面由 CLIProxyAPI 的官方更新机制获取。

本地构建同样使用上游源码目录（需要 Go 1.26、Git 和 Podman）：

```bash
git clone --depth 1 --branch v8.0.20 https://github.com/router-for-me/CLIProxyAPI.git
cd CLIProxyAPI
bash .github/scripts/refresh-model-catalogs.sh
podman build --platform linux/amd64 --build-arg VERSION=v8.0.20 \
  -t localhost/pluxeljs/cliproxyapi:v8.0.20 .
```

已有工作区的服务配置不会被仓库默认值覆盖。将 CLIProxyAPI 服务的镜像地址保存为所需标签后，使用 `dev-runtime services pull cliproxy` 拉取，再用 `dev-runtime config apply cliproxy` 应用；同一标签的镜像更新需要 `dev-runtime services restart cliproxy` 重建容器。升级前备份配置与账号目录。管理页面要求 v8 时，应升级后端到 v8 或更高版本。
