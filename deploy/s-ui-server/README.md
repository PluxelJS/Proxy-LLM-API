# s-ui 远端节点

这套目录用于在单台 Linux VPS 上运行 s-ui，并自动创建一个使用 Cloudflare DNS-01 ACME 的 AnyTLS 入站。它与仓库根目录的 CLIProxyAPI Compose 完全独立；远端节点只需要 Docker Compose 和 Python 3.9 以上版本。

## 部署

在远端克隆仓库后执行：

```bash
cd deploy/s-ui-server
cp .env.example .env
chmod 600 .env
```

至少填写：

```dotenv
ANYTLS_DOMAIN=jp.example.com
CF_API_TOKEN=...
```

域名应提前建立指向 VPS 公网地址的 A/AAAA 记录，并保持 Cloudflare **DNS Only（灰云）**。Token 最小权限为该 Zone 的 `Zone:DNS:Edit` 和 `Zone:Zone:Read`；也可以把只读权限拆成单独的 `CF_ZONE_TOKEN`。

然后运行：

```bash
./install.sh
```

首次执行会自动生成管理员密码、随机面板路径和 AnyTLS 密码。安装结果和 Token 保存在权限为 `0600` 的 `.env`，客户端链接保存在 `data/anytls-url`。再次执行是幂等的，会更新同名的 TLS、客户端和入站配置。

自动配置通过 s-ui 自己前端使用的会话 API 完成。当前载荷已经针对 s-ui `1.5.4` 的真实发布二进制验证，但该 API 不视为跨大版本稳定接口。因此镜像同时固定了版本标签和多架构内容摘要，安装时还会核对 s-ui `1.5.4` 与内置 sing-box `v1.13.14`；上游重推标签不会静默改变行为。升级时必须一起修改 `SUI_IMAGE`、`SUI_EXPECTED_VERSION` 和 `SUI_EXPECTED_SINGBOX_VERSION`，并重新完成契约测试。

自动配置由多个独立 API 事务组成，并不是一个跨对象数据库事务。如果 Cloudflare Token 或 DNS 配置错误，TLS/客户端对象可能已经创建而 AnyTLS 入站尚未成功；修正 `.env` 后重新执行 `./install.sh` 即可幂等续跑，不需要手工清理。

如果只想启动空面板，把 `AUTO_CONFIGURE_ANYTLS=false`，之后在 s-ui 中手动配置。

## 非标准端口

ACME 使用 DNS-01，因此 AnyTLS 可以监听任意 TCP 端口，不依赖 80/443：

```dotenv
ANYTLS_PORT=28443
```

客户端链接会自动包含 `:28443`。非标端口需要在云防火墙和 VPS 防火墙放行 TCP；部分公司、校园或移动网络只允许常见端口，所以长期稳定性仍以 TCP 443 最好。

Compose 使用 `network_mode: host`，s-ui 后续在面板中新增任何入站端口都不需要修改 Docker 端口映射。相应地，端口隔离完全由主机防火墙负责。

面板默认只监听 `127.0.0.1`，通过 SSH 隧道访问：

```bash
ssh -L 2095:127.0.0.1:2095 user@jp-server
```

如果修改了 `SUI_PANEL_PORT`，隧道两侧使用修改后的端口。只有在已经配置好来源 IP 白名单、Tailscale 或其他私网访问控制后，才应把 `SUI_PANEL_BIND` 改成 `0.0.0.0`。

## 运维

```bash
./manage.sh status
./manage.sh logs
./manage.sh link
./manage.sh backup
./manage.sh update
./manage.sh reconfigure
```

`update` 会重新进入安装器，执行镜像版本核对和幂等 API 配置。要升级 s-ui，必须同时明确修改镜像、期望版本并重新验证 API；不要在无人值守环境使用浮动的 `latest`。

持久数据位于：

```text
data/db/       s-ui SQLite 数据库（含 CF Token 与节点配置）
data/acme/     ACME 账号、证书及续期状态
data/cert/     手工证书预留目录
```

请备份 `data/`，且不要提交 `.env`、数据库、证书或生成的分享链接。
