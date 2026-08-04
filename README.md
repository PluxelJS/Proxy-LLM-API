# Proxy-LLM-API

本仓库组合 Claude Code Hub 与 [CLIProxyAPI](https://github.com/router-for-me/CLIProxyAPI)。CLIProxyAPI 不再使用第三方现成镜像：本地和 GitHub Actions 都会拉取官方 `main` 源码并编译，GitHub Actions 会发布多架构镜像到：

```text
ghcr.io/pluxeljs/proxy-llm-api:latest
```

OAuth 凭证只保存在宿主机 `cliproxyapi/oa/`，以读写卷挂载到容器 `/data/auth`。凭证、配置密钥、日志、数据库和构建出的二进制均已排除在 Git 之外。

## 目录结构

```text
cliproxyapi/
├── Dockerfile             # 拉取官方 main 并编译
├── config.example.yaml    # 跟随当前 v7 的可提交配置基线
├── config.yaml            # 本机运行配置，包含 API key，不进 Git
├── oa/                    # OAuth JSON 凭证，不进 Git
├── logs/                  # 运行日志，不进 Git
└── plugins/               # 本机安装的插件，不进 Git
compose.internal-postgres.yaml   # 仅内部 PostgreSQL 模式追加健康依赖
compose.internal-dragonfly.yaml  # 仅内部 Dragonfly 模式追加健康依赖
compose.singbox.yaml             # 设置代理节点后追加官方 sing-box sidecar
sing-box/
└── config.json           # 根据节点链接生成的运行配置，含密钥，不进 Git
scripts/
├── build-cliproxy         # 解析 main 为提交 SHA，再构建默认镜像
├── cliproxy-login         # 在运行容器中登录，或复用同一镜像启动登录容器
├── generate-singbox-config # 将常见节点分享链接转换为 sing-box 配置
└── compose                # 自动选择 Docker Compose / Podman Compose
deploy/s-ui-server/        # 独立的远端 s-ui + AnyTLS + CF DNS-01 部署
deploy/ssh-hardening/       # Debian VPS 一次性 SSH 密钥与非标端口引导
```

## 可选远端 s-ui 节点

`deploy/s-ui-server/` 可以独立复制或克隆到 Linux VPS，通过固定版本的 s-ui 官方维护者镜像启动节点。首次部署只需填写 AnyTLS 域名和限权的 Cloudflare DNS Token；脚本会生成管理员凭据和节点密码，并通过 s-ui API 创建 AnyTLS、可信 ACME TLS 配置及客户端链接。

远端 Compose 使用 host 网络，因此 AnyTLS 和后续新增的协议可以使用任意端口，不必同步维护 Docker `ports`。DNS-01 与端口无关，非标准端口同样能自动签发和续期；但 TCP 443 的网络兼容性通常最好。面板默认只监听远端 `127.0.0.1`，通过 SSH 隧道管理。完整说明见 [`deploy/s-ui-server/README.md`](deploy/s-ui-server/README.md)。

## PostgreSQL 与 Redis 协议存储

默认不填写外部连接字段时，`./scripts/compose` 会启动 Compose 内置的 PostgreSQL 18 和 Dragonfly v1.39.0，并等待两者健康后再启动应用。Dragonfly 提供 Redis 协议，数据写入 `data/dragonfly/`，每 5 分钟生成快照。小型部署默认使用 2 个工作线程和 1 GiB 最大内存，可通过 `DRAGONFLY_THREADS`、`DRAGONFLY_MAXMEMORY` 调整。Compose 同时启用了 Dragonfly 的 `allow-undeclared-keys` Lua 兼容标志，以支持应用在脚本内动态生成 Session 键。

要使用外部服务，在 `.env` 填写对应字段：

```dotenv
EXTERNAL_POSTGRES_DSN=postgresql://user:password@db.example.com:5432/database
EXTERNAL_REDIS_URL=rediss://user:password@redis.example.com:6379
```

两项独立判断：只填写 PostgreSQL 就仍会启动内置 Dragonfly；只填写 Redis URL 就仍会启动内置 PostgreSQL；两项都填写则只启动应用和 CLIProxyAPI。切换模式时请始终使用 `./scripts/compose`，不要绕过包装脚本直接运行 `docker compose`。

## 可选 sing-box 出站代理

不设置代理字段时不会启动 sing-box，CLIProxyAPI 继续直连。要使用节点分享链接，在 `.env` 填写：

```dotenv
SINGBOX_NODE_URL='vless://uuid@example.com:443?security=reality&type=tcp&sni=example.com&pbk=...#node'
```

`./scripts/compose` 会生成不进 Git 的 `sing-box/config.json`，启用官方 `ghcr.io/sagernet/sing-box:v1.13.16`，并让 CLIProxyAPI 通过 Compose 内网的 `socks5h://sing-box:1080` 出站。自动解析常见的 VLESS、VMess、Trojan、Shadowsocks、Hysteria2、TUIC、AnyTLS、HTTP 和 SOCKS 分享链接。链接通常包含 UUID、密码等秘密，务必放在已忽略的 `.env` 中；包含 `#` 时必须用引号包住整个值。

非标准分享格式或更复杂的 WireGuard、SSH、链式出站等配置，请编写完整的官方 sing-box JSON，然后设置：

```dotenv
SINGBOX_CONFIG_PATH=./sing-box/custom.json
```

`SINGBOX_NODE_URL` 与 `SINGBOX_CONFIG_PATH` 只能设置一个。自定义配置必须提供监听 `0.0.0.0:1080` 的 SOCKS 或 mixed inbound，供 CLIProxyAPI 容器访问。

填写或更换节点后执行 `./scripts/compose up -d`。包装脚本会按配置内容计算哈希，节点变化时自动重建 sing-box；OAuth 登录容器也使用同一代理链路。

要恢复直连，清空两个 sing-box 字段后执行 `./scripts/compose up -d --remove-orphans`；这会移除不再属于当前模式的 sing-box 容器，但不会删除其配置或运行数据。

## 首次准备

现有部署可以直接保留当前 `.env`、`cliproxyapi/config.yaml` 和 `cliproxyapi/oa/`。新克隆的仓库执行：

```bash
cp .env.example .env
cp cliproxyapi/config.example.yaml cliproxyapi/config.yaml
```

随后至少修改 `cliproxyapi/config.yaml` 中的 `api-keys`。示例值会触发 CLIProxyAPI 的安全模式，代理端点不会工作。

从最新上游 `main` 构建并启动：

```bash
./scripts/build-cliproxy
./scripts/compose up -d
```

`build-cliproxy` 会先把 `main` 解析成不可变提交 SHA，避免 Docker 把旧的 `main` clone 层当作缓存。若要复现指定版本：

```bash
CLIPROXY_REF=<commit-or-tag> ./scripts/build-cliproxy
```

默认只把 API 暴露在宿主机 `127.0.0.1:8317`。其他 Compose 服务可使用 `http://cli-proxy-api:8317`。确需从其他机器访问时，在 `.env` 设置 `CLIPROXY_BIND_ADDRESS=0.0.0.0`，并确认 `api-keys` 足够强且防火墙规则正确。

## 在宿主机发起账号登录

推荐远程服务器优先使用无需回调端口的 Codex device-code：

```bash
./scripts/cliproxy-login codex-device
```

其余当前上游支持的登录方式：

```bash
./scripts/cliproxy-login codex
./scripts/cliproxy-login claude
./scripts/cliproxy-login antigravity
./scripts/cliproxy-login kimi
./scripts/cliproxy-login xai
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
./scripts/compose logs -f cli-proxy-api
./scripts/compose ps
./scripts/compose up -d --no-build
```

默认镜像名就是 GHCR 地址。本地执行 `build-cliproxy` 时会用源码构建并覆盖同名本地标签；未本地构建时则可直接拉取 GitHub Actions 发布的镜像：

```bash
./scripts/compose pull cli-proxy-api
./scripts/compose up -d --no-build cli-proxy-api
```

配置基线来自上游当前 `main` 的 `config.example.yaml`。仓库内只维护与本部署有关的精简配置；新增 provider 或高级配置请对照[官方完整示例](https://github.com/router-for-me/CLIProxyAPI/blob/main/config.example.yaml)和[官方中文文档](https://help.router-for.me/cn/)。
