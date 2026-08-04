#!/usr/bin/env bash
set -euo pipefail

deploy_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
env_file="$deploy_dir/.env"
compose_file="$deploy_dir/docker-compose.yaml"

die() {
  echo "错误: $*" >&2
  exit 1
}

compose() {
  docker compose --env-file "$env_file" -f "$compose_file" "$@"
}

random_hex() {
  local bytes="$1"
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex "$bytes"
  else
    od -An -N "$bytes" -tx1 /dev/urandom | tr -d ' \n'
  fi
}

set_env_value() {
  local key="$1"
  local value="$2"
  local temp_file
  temp_file="$(mktemp "${env_file}.XXXXXX")"
  awk -v key="$key" -v value="$value" '
    BEGIN { found = 0 }
    $0 ~ "^" key "=" { print key "=" value; found = 1; next }
    { print }
    END { if (!found) print key "=" value }
  ' "$env_file" > "$temp_file"
  chmod 600 "$temp_file"
  mv "$temp_file" "$env_file"
}

load_env() {
  set -a
  # The file is local, mode 0600, and intentionally follows shell-compatible
  # KEY=value syntax so quoted Cloudflare tokens are supported.
  # shellcheck disable=SC1090
  source "$env_file"
  set +a
}

normalize_path() {
  local value="$1"
  value="/${value#/}"
  value="${value%/}/"
  printf '%s' "$value"
}

[[ "$(uname -s)" == "Linux" ]] || die "host-network 模式仅支持 Linux VPS"
command -v docker >/dev/null 2>&1 || die "需要先安装 Docker Engine"
docker compose version >/dev/null 2>&1 || die "需要 Docker Compose v2 插件"
command -v python3 >/dev/null 2>&1 || die "自动初始化需要 python3"

if [[ ! -f "$env_file" ]]; then
  cp "$deploy_dir/.env.example" "$env_file"
  chmod 600 "$env_file"
  echo "已创建 $env_file；请填写 ANYTLS_DOMAIN 和 CF_API_TOKEN 后重新运行。"
  exit 2
fi
chmod 600 "$env_file"
load_env

if [[ -z "${SUI_ADMIN_PASSWORD:-}" ]]; then
  set_env_value SUI_ADMIN_PASSWORD "$(random_hex 18)"
fi
if [[ -z "${ANYTLS_PASSWORD:-}" ]]; then
  set_env_value ANYTLS_PASSWORD "$(random_hex 18)"
fi
if [[ -z "${SUI_PANEL_PATH:-}" ]]; then
  set_env_value SUI_PANEL_PATH "/$(random_hex 10)/"
fi
load_env

SUI_PANEL_PATH="$(normalize_path "$SUI_PANEL_PATH")"
SUI_SUB_PATH="$(normalize_path "${SUI_SUB_PATH:-/sub/}")"
set_env_value SUI_PANEL_PATH "$SUI_PANEL_PATH"
set_env_value SUI_SUB_PATH "$SUI_SUB_PATH"
load_env

[[ "${SUI_PANEL_PORT:-}" =~ ^[0-9]+$ ]] || die "SUI_PANEL_PORT 必须是端口数字"
[[ "${SUI_SUB_PORT:-}" =~ ^[0-9]+$ ]] || die "SUI_SUB_PORT 必须是端口数字"
[[ "${ANYTLS_PORT:-}" =~ ^[0-9]+$ ]] || die "ANYTLS_PORT 必须是端口数字"
for port in "$SUI_PANEL_PORT" "$SUI_SUB_PORT" "$ANYTLS_PORT"; do
  (( port >= 1 && port <= 65535 )) || die "端口必须介于 1 和 65535"
done
if [[ "$SUI_PANEL_PORT" == "$SUI_SUB_PORT" || "$SUI_PANEL_PORT" == "$ANYTLS_PORT" || "$SUI_SUB_PORT" == "$ANYTLS_PORT" ]]; then
  die "面板、订阅和 AnyTLS 端口不能重复"
fi
case "${SUI_PANEL_BIND:-127.0.0.1}" in
  127.0.0.1|0.0.0.0) ;;
  *) die "SUI_PANEL_BIND 目前只支持 127.0.0.1 或 0.0.0.0" ;;
esac

auto_configure_anytls="${AUTO_CONFIGURE_ANYTLS:-true}"
auto_configure_anytls="${auto_configure_anytls,,}"
case "$auto_configure_anytls" in
  true|false) ;;
  *) die "AUTO_CONFIGURE_ANYTLS 必须是 true 或 false" ;;
esac

if [[ "$auto_configure_anytls" == "true" ]]; then
  [[ -n "${ANYTLS_DOMAIN:-}" && "$ANYTLS_DOMAIN" != "jp.example.com" ]] || die "请在 .env 设置 ANYTLS_DOMAIN"
  [[ -n "${CF_API_TOKEN:-}" && "$CF_API_TOKEN" != replace-with-* ]] || die "请在 .env 设置 CF_API_TOKEN"
fi

mkdir -p "$deploy_dir/data/db" "$deploy_dir/data/acme" "$deploy_dir/data/cert"
chmod 700 "$deploy_dir/data" "$deploy_dir/data/db" "$deploy_dir/data/acme" "$deploy_dir/data/cert"

compose pull

version_output="$(compose run --rm --entrypoint ./sui s-ui -v)"
actual_sui_version="$(printf '%s\n' "$version_output" | awk '/^S-UI Panel/ {print $3}')"
actual_singbox_version="$(printf '%s\n' "$version_output" | awk '/^Sing-Box/ {print $2}')"
expected_sui_version="${SUI_EXPECTED_VERSION:-1.5.4}"
expected_singbox_version="${SUI_EXPECTED_SINGBOX_VERSION:-v1.13.14}"
[[ "$actual_sui_version" == "$expected_sui_version" ]] || \
  die "s-ui 版本不匹配：期望 $expected_sui_version，实际 ${actual_sui_version:-unknown}"
[[ "$actual_singbox_version" == "$expected_singbox_version" ]] || \
  die "sing-box 版本不匹配：期望 $expected_singbox_version，实际 ${actual_singbox_version:-unknown}"
printf '%s\n' "$version_output"

compose down --remove-orphans

# The image CLI initializes/migrates the SQLite database and hashes the admin
# password. It is run while the long-lived container is stopped.
compose run --rm --entrypoint ./sui s-ui admin \
  -username "$SUI_ADMIN_USERNAME" -password "$SUI_ADMIN_PASSWORD"
compose run --rm --entrypoint ./sui s-ui setting \
  -port "$SUI_PANEL_PORT" -path "$SUI_PANEL_PATH" \
  -subPort "$SUI_SUB_PORT" -subPath "$SUI_SUB_PATH"
# A standard rootful Docker daemon creates a root-owned bind-mounted DB. Hand
# it back only when needed; rootless Docker normally creates it writable.
if [[ ! -w "$deploy_dir/data/db/s-ui.db" ]]; then
  host_uid="$(id -u)"
  host_gid="$(id -g)"
  compose run --rm --entrypoint sh s-ui -c \
    "chown ${host_uid}:${host_gid} /app/db/s-ui.db"
fi
python3 "$deploy_dir/bootstrap.py" db "$deploy_dir/data/db/s-ui.db"

compose up -d

if [[ "$auto_configure_anytls" == "true" ]]; then
  anytls_url="$(python3 "$deploy_dir/bootstrap.py" anytls)"
  umask 077
  printf '%s\n' "$anytls_url" > "$deploy_dir/data/anytls-url"
  echo "AnyTLS 已配置，客户端链接保存在: $deploy_dir/data/anytls-url"
fi

echo
echo "s-ui 已启动。"
echo "管理员: $SUI_ADMIN_USERNAME"
echo "密码保存在: $env_file"
if [[ "${SUI_PANEL_BIND:-127.0.0.1}" == "127.0.0.1" ]]; then
  echo "从本机建立隧道: ssh -L ${SUI_PANEL_PORT}:127.0.0.1:${SUI_PANEL_PORT} <user>@<server>"
  echo "随后访问: http://127.0.0.1:${SUI_PANEL_PORT}${SUI_PANEL_PATH}"
else
  echo "面板监听: ${SUI_PANEL_BIND}:${SUI_PANEL_PORT}${SUI_PANEL_PATH}"
fi
echo "AnyTLS TCP 端口: ${ANYTLS_PORT}（请在云防火墙和主机防火墙放行）"
