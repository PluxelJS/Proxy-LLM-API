# New API Runtime

这个仓库现在只负责 **New API + SQLite** 的部署。默认入口为
`http://127.0.0.1:23000`，提供模型转发、Web 管理、持久 token 用量和消费日志。
镜像固定为 `docker.io/calciumion/new-api:v0.13.2`，不自动跟随 `latest`。

原来的 Claude Code Hub、CLIProxyAPI、PostgreSQL、Dragonfly、sing-box、OAuth
登录助手和每日镜像构建工作流已移除。仓库 URL 暂时保留以免现有链接失效；这不是
New API 源码分叉，不再发布原来的 `ghcr.io/pluxeljs/proxy-llm-api` 镜像。
上游项目：[QuantumNous/new-api](https://github.com/QuantumNous/new-api)。

## 快速开始

Linux 上需要 Python 3、Podman 和 podman-compose；也支持 Docker Compose v2。

```bash
./manage.sh init
./manage.sh up
./manage.sh check
```

打开 `http://127.0.0.1:23000` 完成首次设置，创建自己的管理员密码并选择自用模式。
添加 OpenAI 类型渠道时，填写上游 origin（如 `https://api.example.com`，不带 `/v1`），
配置实际支持的模型，再创建客户端令牌。客户端 API 地址为
`http://127.0.0.1:23000/v1`。

本项目不会生成通用管理员密码，也不会把任何真实凭证放进 Git 或 Nix store。
自用模式不是无限余额；令牌的无限额度也不等于用户无限余额。按实际需要设置
用户额度、模型价格和日志选项。

```bash
./manage.sh status
./manage.sh logs
./manage.sh restart
./manage.sh down
./manage.sh config  # 只列出服务名，不输出秘密
```

Docker Compose v2 用户首次运行：

```bash
NEW_API_ENGINE=docker ./manage.sh init
./manage.sh up
```

容器引擎和项目名会写入状态目录的 `.env`，后续命令无需重复指定。
Docker 容器使用当前用户 UID/GID 写入 SQLite，普通用户可以直接备份；rootless
Podman 使用容器内 root（映射到当前宿主用户）。请用有 Docker daemon 访问权限的
同一个用户执行全部命令，不要混用 `sudo` 或在同一状态目录交替运行两个引擎。
默认使用独立 Compose 项目
`new-api-runtime`，可通过 `NEW_API_PROJECT` 覆盖。Podman 不创建共享 pod。

## 状态与配置

默认状态目录为 `~/.local/state/new-api-runtime`，可用 `NEW_API_STATE_DIR` 覆盖；
支持 `XDG_STATE_HOME`。目录权限为 0700，配置和备份文件为 0600。

| 文件 | 用途 |
| --- | --- |
| `.env` | 端口、固定镜像、持久会话密钥、数据路径 |
| `data/new-api.db` | 账号、渠道、客户端令牌、配置、消费记录 |
| `data/new-api.db-wal` / `-shm` | SQLite 运行时可能存在的事务文件 |

运行 `init` 不会覆盖已经配置的端口或会话密钥。编辑状态目录中的 `.env` 后执行
`restart` 生效；模板 `.env.example` 仅供参考。值采用 `KEY=value` 原样格式，
不支持 shell 表达式、引号或换行。

主要变量和本机 dev-runtime 保持一致：`NEW_API_IMAGE`、`NEW_API_BIND_ADDRESS`、
`NEW_API_PORT`、`NEW_API_DATA_DIR`、`NEW_API_SESSION_SECRET`、`TZ`。
`NEW_API_DATA_DIR` 必须为绝对路径。容器诊断日志限制为 16 MB；消费记录保存在
SQLite，除非管理员主动清理。管理界面的统计图表可能存在聚合刷新延迟。

默认只监听 loopback。如需跨机器访问，应由已有的 HTTPS 反向代理负责入口和访问
控制；此仓库不再捆绑隧道、出站代理或额外网络服务。

## 用量与价格

在管理界面确认启用消费日志和数据统计。实际 token 取决于上游返回的 usage；
缺少 usage 的上游无法保证精确计数。对于仅支持 Responses 的上游，客户端直接使用
`/v1/responses`，不要依赖协议转换来补齐上游能力。

本地开发机建议在性能设置中将 CPU 阈值设为 `0`，禁用 CPU 过载拒绝请求，
避免编译测试触发 503；该设置在线生效并保存到 SQLite。

如需先按本地额度观察消耗，可将模型倍率和分组倍率设为 `1`；输入基准为
$2/百万 token，输出采用 New API 生效的模型规则。管理员可在用户管理中增加
本地额度。这不是对上游账户充值，也不等同于上游真实账单。

模型价格必须按你的上游收费填写。模型倍率设置为零时，仍可记录 token，但费用为零，
不能用作上游账单。运行时不会捏造或自动覆盖模型价格。

## 备份、恢复和升级

```bash
./manage.sh backup "$HOME/new-api-backup-$(date +%Y%m%d-%H%M%S)"
```

备份通过 SQLite backup API 获取一致快照，包括已提交的 WAL 事务，不需要停止服务。
备份包含数据库、会话密钥、镜像版本及时间；目标目录必须不存在。妥善保护整个备份。

恢复只允许写入空状态目录，不覆盖当前数据库：

```bash
NEW_API_STATE_DIR="$HOME/.local/state/new-api-restored" \
  ./manage.sh restore "$HOME/new-api-backup-YYYYMMDD-HHMMSS"
```

恢复后检查 `.env` 的端口与镜像。先停止旧实例，再启动恢复实例；不要让两个实例
同时打开同一个 SQLite 数据目录。恢复保留备份时的镜像版本，不自动升级数据库。

升级流程：先备份，在 `.env` 修改 `NEW_API_IMAGE`，执行 `pull`，再 `restart`，
检查转发及消费记录。需要回滚时，使用旧镜像和升级前的数据库快照，不要直接让旧版
程序打开新版迁移过的数据库。

## Nix / Home Manager

```bash
nix run . -- init
nix run . -- up
nix flake check
```

导出 `packages.<system>.new-api-runtime`（也是 default）、默认 app 和
`homeManagerModules.default`。支持 x86_64-linux、aarch64-linux。
独立管理模式示例：

```nix
{
  imports = [ inputs.proxy-llm.homeManagerModules.default ];
  services.newApiRuntime = {
    enable = true;
    # stateDir = "${config.xdg.stateHome}/new-api-runtime";
  };
}
```

`proxy-llm` 只是示例中的 flake input 名称，旧 `services.proxyLlm` 和 `proxy-llm`
命令已删除。系统需要提供 Podman 和 systemd 用户会话。Home Manager 单元名为
`new-api-runtime.service`，`autoStart = false` 可关闭登录时启动。

**已有 dev-runtime 的机器继续由 dev-runtime 管理，不启用上述独立服务。**
使用 `dev-runtime enable new-api`、`dev-runtime logs new-api` 等命令。它的状态目录
是 `~/.local/state/dev-runtime/new-api/`，会话密钥在 dev-runtime 的 `.env` 中。
此仓库可作为单服务 Compose 定义和其他机器的部署入口，不抢占本机 23000 端口。

## 从旧设计迁移

这是一次明确的架构变更，不会在首次启动时自动停止或删除旧容器。迁移流程见
[迁移说明](docs/migration.md)。旧数据库日志不伪装成新系统的历史统计。

旧节点部署及 SSH 加固工具也已移除；如需查阅，可从 Git 历史恢复。
