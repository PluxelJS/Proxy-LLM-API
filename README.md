# CLIProxyAPI Runtime

从 [官方 Git 仓库](https://github.com/router-for-me/CLIProxyAPI) 自行编译 CLIProxyAPI，
默认通过官方 Management Center 网页维护账号、渠道、API key 和配置。
New API 保持独立，继续负责自己的渠道、用量与消费记录。

## 开始使用

需要 Python 3 + PyYAML、Podman + podman-compose，或 Docker Compose v2。

```bash
./manage.sh init
# 编辑 ~/.local/state/cliproxy-runtime/.env：
# 可选填写 SINGBOX_NODE_URL；留空即可直连
./manage.sh up
./manage.sh ui
```

`up` 在默认本地镜像不存在时自动从固定官方提交构建。`ui` 显示管理页面地址和管理密码，
打开 `http://127.0.0.1:8317/management.html` 即可登录。日常账号登录、凭据导入删除、
配额查看、渠道和 API key 编辑都在网页完成，不再提供 CLI 登录子命令。

管理密码独立保存在状态目录的 `management-key`（0600），初始化不会反复更换。
启动和服务日志仅显示文件位置，不打印密码。客户端调用模型使用页面中的 API key，
不是管理密码；本地模型 API 地址为 `http://127.0.0.1:8317/v1`。

官方页面固定版本和 SHA256，随镜像打包；首次访问不需要临时下载，默认不在后台更新。
面板升级和后端升级都通过重建镜像完成。

## 网络选择

| `.env` 配置 | 运行方式 | CLIProxyAPI 出站 |
| --- | --- | --- |
| 只有 `SINGBOX_NODE_URL` | CLIProxyAPI + sing-box | 默认经 sing-box |
| 只有 `CF_TUNNEL_TOKEN` | CLIProxyAPI + 公网 API 入口 + cloudflared | 默认直连 |
| 两者都有 | 两种网络功能同时启用 | 默认经 sing-box |
| 两者都没有 | 仅 CLIProxyAPI | 直连 |

默认允许直连。`SINGBOX_NODE_URL` 是可选的单个节点分享链接，支持 VLESS、VMess、
Trojan、Shadowsocks、Hysteria2、TUIC、AnyTLS、HTTP(S) 和 SOCKS。
非法节点链接会警告并按未配置 sing-box 启动，不阻止 CLIProxyAPI 使用。

配置有效节点后，将全局代理设为 sing-box；账号和渠道的单独代理设置仍由网页维护。
启动和 `check` 会在 CLIProxyAPI 容器内显式经过 SOCKS 代理发起 HTTP 测试请求，
禁用测试请求的 NO_PROXY 绕过；成功显示 `OK sing-box`，失败显示 `WARNING`，
但服务继续运行。测试地址由 `SINGBOX_CHECK_URL` 配置，默认 Google 的 204 端点。
这验证的是 sing-box 的实际转发能力，不代表每个账号都采用全局代理或模型登录有效。

CLIProxyAPI 保留直连网络。探测失败不会暗中修改代理或自动重试直连；可以在网页选择
direct，或清空节点链接后重启。没有节点时，网页设置的其他代理也会保留。

启用 cloudflared 后，在 Cloudflare Tunnel 的已发布应用中将 origin 设置为
**`http://api-entry:8080`**。自动创建的 API 入口只转发 `/v1/` 和 `/v1beta/`，保留
API 鉴权、流式传输和 WebSocket；管理页面、管理接口及其他路径返回 404。
cloudflared 不连接 CLIProxyAPI 的内部网络，不能直接访问其管理端口。

本地端口只允许 loopback 绑定。远程机器的管理员使用 SSH 转发 8317，再从本机打开
管理页面。OAuth 使用页面中的 device-code 流程或回调 URL 提交，不再发布宿主机的
1455、54545、51121 回调端口；浏览器无法打开 localhost 回调时，复制完整回调 URL
回页面提交即可。

## 持久配置

状态默认位于 `~/.local/state/cliproxy-runtime`，支持 `XDG_STATE_HOME` 和
`CLIPROXY_STATE_DIR`；目录权限为 0700。

| 文件 | 用途 |
| --- | --- |
| `.env` | 节点链接、隧道 token、镜像与端口 |
| `management-key` | 独立网页管理密码 |
| `settings/config.yaml` | 网页直接读写的唯一配置，重启不丢失 |
| `auth/` | OAuth 和导入的账号凭据 |
| `logs/` | 请求日志（启用时） |
| `generated/`、`compose.json` | 运行时自动管理，不手动修改 |

`.env` 使用原样 `KEY=value`，不加引号、不执行 shell 表达式。环境变量只提供初次
初始化默认值，之后以 `.env` 为准。Docker 首次使用：
`CLIPROXY_ENGINE=docker ./manage.sh init`。不要对同一个状态目录混用两个引擎。

网页配置即时保存，服务按上游热加载规则生效；网络、镜像、端口、管理密码变更后执行
`restart`。切换网络组合会先停止原组合，再创建新组合。已有配置和账号不会被清空。

## 少量维护命令

```bash
./manage.sh status
./manage.sh logs
./manage.sh restart
./manage.sh down
```

镜像升级：在 `.env` 修改 `CLIPROXY_REF` 为官方完整提交 SHA，执行 `build` 和
`restart`。构建固定使用 `router-for-me/CLIProxyAPI.git`。也可以指定自己的
`CLI_PROXY_IMAGE`；镜像需要包含官方二进制、curl、`/healthz` 及
`/CLIProxyAPI/static/management.html`。已发布镜像使用 `pull` 更新。
每日 GHCR 工作流继续从官方 main 解析 SHA，发布 `latest` 和 `upstream-<短 SHA>` 标签。

`config` 只列启用服务，不打印秘密；`check` 检查本地服务健康，实际账号和节点可用性
在页面或模型请求中确认。停止前后均可备份整个私有状态目录；`down` 不删除数据。

## Nix / Home Manager

```bash
nix run . -- init
nix run . -- up
nix run . -- ui
```

```nix
{
  imports = [ inputs.proxy-llm.homeManagerModules.cliproxy ];
  services.cliProxyRuntime.enable = true;
}
```

本机可由 `dev-runtime` 统一调用本包，使用 `enable cliproxy`、`check cliproxy`、
`ui cliproxy`，无需启用第二个 systemd 单元。

默认 package/app 是 `cliproxy-runtime`，创建独立 `cliproxy-runtime.service`，
首次启动缺少默认镜像时自动构建。凭证仍放私有状态目录，不写入 Nix store。
需要手动启停时设置 `services.cliProxyRuntime.autoStart = false`。

已有 New API 继续由 `dev-runtime`、`new-api-runtime` 或 `./new-api.sh` 管理。
两者独立启停，不自动注册渠道。设置 `CLIPROXY_EXTERNAL_NETWORK` 可加入已有
外部 Compose 网络；本机 dev-runtime 自动创建共享网络，New API 可使用
`http://cli-proxy-api:8317` 作为渠道 origin。见 [New API 文档](docs/new-api.md)
和 [迁移说明](docs/migration.md)。

## 验证

```bash
python3 -m unittest discover -s tests -v
python3 tests/smoke_cliproxy.py
nix flake check
```

容器测试使用临时项目、本地模拟节点和测试凭据，验证页面资源、管理鉴权、配置和账号
持久化、API 入口隔离、模型转发及 sing-box 停止后的警告行为，不调用真实模型账号。
可用 `CLIPROXY_TEST_IMAGE` 指定测试镜像，`CLIPROXY_TEST_ENGINE=docker` 测试 Docker。
