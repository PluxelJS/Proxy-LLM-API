#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
compose="$repo_root/scripts/compose"

usage() {
  cat <<'EOF'
用法: ./manage.sh <命令> [参数]

  up             生成所需配置并启动完整服务，清理旧 orphan
  down           停止当前配置选择的服务
  restart        强制重建并重启当前配置选择的服务
  update         拉取镜像并重新应用当前配置
  status         查看服务状态
  logs [服务]    跟踪全部或指定服务日志
  pull           拉取当前配置选择的镜像
  config         输出最终 Compose 配置
  proxy-test     验证 CLIProxyAPI 是否实际通过 sing-box 出站
  login ...      调用 CLIProxyAPI 登录助手
  build          从上游源码构建 CLIProxyAPI 镜像
EOF
}

command="${1:-status}"
shift || true

case "$command" in
  up)
    exec "$compose" up -d --remove-orphans "$@"
    ;;
  restart)
    exec "$compose" up -d --force-recreate --remove-orphans "$@"
    ;;
  update)
    "$compose" pull "$@"
    exec "$compose" up -d --remove-orphans "$@"
    ;;
  down)
    exec "$compose" down "$@"
    ;;
  status|ps)
    exec "$compose" ps "$@"
    ;;
  logs)
    exec "$compose" logs -f --tail=200 "$@"
    ;;
  pull)
    exec "$compose" pull "$@"
    ;;
  config)
    exec "$compose" config "$@"
    ;;
  proxy-test)
    exec "$repo_root/scripts/verify-proxy" "$@"
    ;;
  login)
    exec "$repo_root/scripts/cliproxy-login" "$@"
    ;;
  build)
    exec "$repo_root/scripts/build-cliproxy" "$@"
    ;;
  help|-h|--help)
    usage
    ;;
  *)
    usage >&2
    exit 2
    ;;
esac
