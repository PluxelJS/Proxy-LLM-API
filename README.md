# Proxy-LLM-API

本仓库组合 Claude Code Hub 与 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)。CLIProxyAPI 不使用来源不明的第三方镜像：GitHub Actions 每天从官方 `main` 源码构建并发布多架构镜像；本地也可按需从同一上游构建：

```text
ghcr.io/pluxeljs/proxy-llm-api:latest
```

OAuth 凭证只保存在宿主机 `cliproxyapi/oa/`，以读写卷挂载到容器 `/data/auth`。CLIProxyAPI 会以正确映射的宿主用户身份写入这些目录，不需要 `sudo`、`chown` 或 `chmod 777`。凭证、配置密钥、日志、数据库和构建出的二进制均已排除在 Git 之外。

## 快速开始

宿主机只需要 Git，以及 Docker Compose v2 或 Podman Compose。设置分享链接代理时还需要 Python 3，执行出口验证时需要 curl。默认直接拉取本仓库发布的 GHCR 镜像，不需要在部署机器编译 CLIProxyAPI。

```bash
git clone https://github.com/PluxelJS/Proxy-LLM-API.git
cd Proxy-LLM-API
./manage.sh init
./manage.sh up
./manage.sh login codex-device
```

`init` 会自动创建 `.env` 与 `cliproxyapi/config.yaml`，生成 Hub 管理 Token、数据库密码和 CLIProxyAPI API key；重复执行不会覆盖已有配置。它会直接打印首次需要的两项凭据，之后也可执行：

```bash
./manage.sh secrets
```

需要 AnyTLS、VLESS 等出站代理时，只需在 `up` 前编辑 `.env` 中这一项：

```dotenv
SINGBOX_NODE_URL='anytls://password@example.com:443?security=tls&sni=example.com#node'
```

启动成功后打开 `http://127.0.0.1:23000`；远程部署则使用服务器 IP 和 `APP_PORT`。在 Hub 添加 CLIProxyAPI 时使用 Compose 内部地址 `http://cli-proxy-api:8317` 以及 `init` 生成的 API key。

## 目录结构

```text
cliproxyapi/
├── Dockerfile             # 拉取官方 main 并编译
├── config.example.yaml    # 跟随上游当前 main 的可提交配置基线
├── config.yaml            # 本机运行配置，包含 API key，不进 Git
├── oa/                    # OAuth JSON 凭证，不进 Git
├── logs/                  # 运行日志，不进 Git
└── plugins/               # 本机安装的插件，不进 Git
compose.internal-postgres.yaml   # 可选内置 PostgreSQL 服务与健康依赖
compose.internal-dragonfly.yaml  # 可选内置 Dragonfly 服务与健康依赖
compose.singbox.yaml             # 设置代理节点后追加官方 sing-box sidecar
compose.podman.yaml              # Podman 健康调度兼容层，由包装脚本自动选择
compose.build.yaml               # 仅本地源码构建时追加，不参与默认启动
sing-box/
└── config.json           # 根据节点链接生成的运行配置，含密钥，不进 Git
scripts/
├── build-cliproxy         # 解析 main 为提交 SHA，再构建默认镜像
├── cliproxy-login         # 在运行容器中登录，或复用同一镜像启动登录容器
├── generate-singbox-config # 将常见节点分享链接转换为 sing-box 配置
├── healthcheck            # 串行检查实际服务，不依赖容器运行时状态缓存
├── init                   # 创建本机配置并生成强随机凭据
├── service-exec           # 绕过 Compose API，直接选择 Docker/Podman exec
└── compose                # 自动选择 Docker Compose / Podman Compose
manage.sh                  # 日常唯一入口：启动、状态、日志、登录与代理验证
deploy/s-ui-server/        # 独立的远端 s-ui + AnyTLS + CF DNS-01 部署
deploy/ssh-hardening/       # Debian VPS 一次性 SSH 密钥与非标端口引导
```

## PostgreSQL 与 Redis 协议存储

默认不填写外部连接字段时，`./manage.sh up` 会启动 Compose 内置的 PostgreSQL 18 和 Dragonfly v1.40.0，并等待两者健康后再启动应用。它们和可选 sing-box 的运行状态默认保存在 Compose 命名卷，不会在仓库里生成 `root`、`nobody` 所有的 `data/` 文件。Dragonfly 每 5 分钟生成快照；小型部署可通过 `DRAGONFLY_THREADS`、`DRAGONFLY_MAXMEMORY` 调整资源。Compose 同时启用了 Dragonfly 的 `allow-undeclared-keys` Lua 兼容标志，以支持应用在脚本内动态生成 Session 键。

要使用外部服务，在 `.env` 填写对应字段：

```dotenv
EXTERNAL_POSTGRES_DSN=postgresql://user:password@db.example.com:5432/database
EXTERNAL_REDIS_URL=rediss://user:password@redis.example.com:6379
```

两项独立判断：只填写 PostgreSQL 就仍会启动内置 Dragonfly；只填写 Redis URL 就仍会启动内置 PostgreSQL；两项都填写则只启动应用和 CLIProxyAPI。日常只使用根目录的 `./manage.sh`；它会在内部选择正确的 Compose overlays，避免直接运行底层 Compose 留下旧服务。

从旧版本升级时，如果 `data/postgres`、`data/dragonfly` 或 `data/sing-box` 中已有文件，包装脚本会自动继续挂载原目录，不会静默换成空卷。确认数据迁移完成后才应自行删除旧目录。命名卷可用 `docker volume ls` 或 `podman volume ls` 查看；`./manage.sh down` 不会删除它们。

`./manage.sh up` 会等待当前模式下的每个服务真正可用后才成功返回；`./manage.sh status` 同时显示容器状态并重新执行串行健康检查。Podman 模式会自动启用专用兼容层，避开部分 Podman/conmon 组合通过 systemd timer 执行健康检查时产生的误报；Docker 模式仍使用 Compose 原生健康检查。

## 可选 sing-box 出站代理

不设置代理字段时不会启动 sing-box，CLIProxyAPI 继续直连。要使用节点分享链接，在 `.env` 填写：

```dotenv
SINGBOX_NODE_URL='vless://uuid@example.com:443?security=reality&type=tcp&sni=example.com&pbk=...#node'
```

`./manage.sh up` 会生成不进 Git 的 `sing-box/config.json`，启用官方 `ghcr.io/sagernet/sing-box:v1.13.16`，并让 CLIProxyAPI 通过 Compose 内网的 `socks5h://sing-box:1080` 出站。自动解析常见的 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、AnyTLS、HTTP 和 SOCKS 分享链接。链接通常包含 UUID、密码等秘密，务必放在已忽略的 `.env` 中；包含 `#` 时必须用引号包住整个值。

非标准分享格式或更复杂的 WireGuard、SSH、链式出站等配置，请编写完整的官方 sing-box JSON，然后设置：

```dotenv
SINGBOX_CONFIG_PATH=./sing-box/custom.json
```

`SINGBOX_NODE_URL` 与 `SINGBOX_CONFIG_PATH` 只能设置一个。自定义配置必须提供监听 `0.0.0.0:1080` 的 SOCKS 或 mixed inbound，供 CLIProxyAPI 容器访问。

填写或更换节点后执行 `./manage.sh up`。入口脚本会按配置内容计算哈希，节点变化时自动重建 sing-box；OAuth 登录容器也使用同一代理链路。执行 `./manage.sh proxy-test` 可以比较宿主机直连出口、sing-box SOCKS 出口和 CLIProxyAPI 容器自动出口，验证代理确实生效。

要恢复直连，清空两个 sing-box 字段后执行 `./manage.sh up`；这会移除不再属于当前模式的 sing-box 容器，但不会删除其配置或运行数据。

## 可选：从最新上游源码构建

正常部署无需执行本节。默认 `./manage.sh up` 使用 GHCR 镜像；只有需要自行审计构建或立即跟进 CLIProxyAPI 上游尚未发布的提交时，才执行：

```bash
./manage.sh build
./manage.sh up
```

`build-cliproxy` 会先把 `main` 解析成不可变提交 SHA，避免 Docker 把旧的 `main` clone 层当作缓存。若要复现指定版本：

```bash
CLIPROXY_REF=<commit-or-tag> ./manage.sh build
```

默认只把 API 暴露在宿主机 `127.0.0.1:8317`。其他 Compose 服务可使用 `http://cli-proxy-api:8317`。确需从其他机器访问时，在 `.env` 设置 `CLIPROXY_BIND_ADDRESS=0.0.0.0`，并确认 `api-keys` 足够强且防火墙规则正确。

## 在宿主机发起账号登录

推荐远程服务器优先使用无需回调端口的 Codex device-code：

```bash
./manage.sh login codex-device
```

其余当前上游支持的登录方式：

```bash
./manage.sh login codex
./manage.sh login claude
./manage.sh login antigravity
./manage.sh login kimi
./manage.sh login xai
```

脚本始终以 `-no-browser` 启动登录，并在终端显示授权 URL。服务已运行时，脚本在现有容器内执行登录；服务未运行时，它创建一个临时登录容器并开放需要的回调端口。两种方式都写入同一个宿主机 `cliproxyapi/oa/`，主服务会热加载新增或更新的凭证。

回调型登录端口如下：

| Provider | 端口 | 回调方式 |
| --- | ---: | --- |
| Codex OAuth | 1455 | `localhost` 浏览器回调 |
| Claude | 54545 | `localhost` 浏览器回调 |
| Antigravity | 51121 | `localhost` 浏览器回调 |
| Codex device / Kimi / xAI | 无 | device code |

若 CLIProxyAPI 部署在远端主机而浏览器在本机，按命令打印的 SSH tunnel 提示转发对应端口。

## 调用与运维

使用 `cliproxyapi/config.yaml` 里的 API key：

```bash
curl http://127.0.0.1:8317/v1/models \
  -H 'Authorization: Bearer <your-api-key>'
```

常用命令：

```bash
./manage.sh status
./manage.sh logs cli-proxy-api
./manage.sh proxy-test
./manage.sh restart
```

默认启动配置不包含 `build:`，因此只会使用 GHCR 镜像。本地执行 `build-cliproxy` 时才追加构建 overlay，并用源码构建覆盖同名本地标签：

```bash
./manage.sh update cli-proxy-api
```

配置基线来自上游当前 `main` 的 `config.example.yaml`。仓库内只维护与本部署有关的精简配置；新增 provider 或高级配置请对照[官方完整示例](https://github.com/router-for-me/CLIProxyAPI/blob/main/config.example.yaml)和[官方中文文档](https://help.router-for.me/cn/)。

## 独立的远端工具

`deploy/` 下的内容不会被根目录 `manage.sh` 或 Compose 自动加载：

- [`deploy/s-ui-server/README.md`](deploy/s-ui-server/README.md)：在独立 VPS 部署固定版本的 s-ui + AnyTLS，并用 Cloudflare DNS-01 自动维护证书。
- [`deploy/ssh-hardening/README.md`](deploy/ssh-hardening/README.md)：一次性创建部署用户、SSH 密钥、非标准端口和基础防护。

远端 s-ui 使用 host 网络，节点协议端口无需再同步维护 Docker `ports`；面板默认只监听远端 `127.0.0.1`，通过 SSH 隧道管理。
