#!/usr/bin/env bash
set -euo pipefail

deploy_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
env_file="$deploy_dir/.env"
compose_file="$deploy_dir/docker-compose.yaml"

[[ -f "$env_file" ]] || {
  echo "缺少 $env_file，请先运行 ./install.sh" >&2
  exit 2
}

compose() {
  docker compose --env-file "$env_file" -f "$compose_file" "$@"
}

command="${1:-status}"
shift || true

case "$command" in
  up)
    compose up -d "$@"
    ;;
  down)
    compose down "$@"
    ;;
  restart)
    compose restart s-ui
    ;;
  logs)
    compose logs -f --tail=200 s-ui "$@"
    ;;
  status)
    compose ps
    ;;
  update)
    # Upgrades must pass the version/API guard and idempotent bootstrap.
    exec "$deploy_dir/install.sh"
    ;;
  backup)
    backup_dir="$deploy_dir/backups"
    mkdir -p "$backup_dir"
    chmod 700 "$backup_dir"
    output="$backup_dir/s-ui-$(date -u +%Y%m%dT%H%M%SZ).db"
    compose exec -T s-ui ./sui backup -output - > "$output"
    if [[ "$(head -c 16 "$output")" != "SQLite format 3"* ]]; then
      rm -f "$output"
      echo "s-ui 备份失败，输出不是有效的 SQLite 数据库。" >&2
      exit 1
    fi
    chmod 600 "$output"
    echo "$output"
    ;;
  link)
    [[ -f "$deploy_dir/data/anytls-url" ]] || {
      echo "尚未生成 AnyTLS 链接，请运行 ./install.sh 完成自动配置。" >&2
      exit 2
    }
    cat "$deploy_dir/data/anytls-url"
    ;;
  reconfigure)
    exec "$deploy_dir/install.sh"
    ;;
  *)
    echo "用法: $0 {up|down|restart|logs|status|update|backup|link|reconfigure}" >&2
    exit 2
    ;;
esac
