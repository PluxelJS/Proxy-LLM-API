#!/usr/bin/env bash
set -euo pipefail

admin_user="${SSH_ADMIN_USER:-deploy}"
new_port="${SSH_PORT:-23472}"
config_file="/etc/ssh/sshd_config.d/00-proxy-llm-hardening.conf"
finalize_script="/usr/local/sbin/proxy-llm-ssh-finalize"

die() {
  echo "错误: $*" >&2
  exit 1
}

reload_ssh() {
  if systemctl list-unit-files ssh.service >/dev/null 2>&1; then
    systemctl reload ssh
  elif systemctl list-unit-files sshd.service >/dev/null 2>&1; then
    systemctl reload sshd
  else
    die "找不到 ssh.service 或 sshd.service"
  fi
}

[[ "$(id -u)" -eq 0 ]] || die "请以 root 运行（curl ... | sudo env ... bash）"
[[ "$admin_user" =~ ^[a-z_][a-z0-9_-]{0,30}$ ]] || die "SSH_ADMIN_USER 格式无效"
[[ "$admin_user" != "root" ]] || die "SSH_ADMIN_USER 不能是 root"
[[ "$new_port" =~ ^[0-9]+$ ]] || die "SSH_PORT 必须是数字"
(( new_port >= 1024 && new_port <= 65535 )) || die "SSH_PORT 必须介于 1024 和 65535"
case "$new_port" in
  2095|2096) die "SSH_PORT 与 s-ui 默认端口冲突" ;;
esac

export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y --no-install-recommends \
  ca-certificates curl iproute2 openssh-server openssh-client sudo

command -v sshd >/dev/null 2>&1 || die "未找到 sshd"
grep -Eq '^[[:space:]]*Include[[:space:]]+/etc/ssh/sshd_config\.d/\*\.conf' \
  /etc/ssh/sshd_config || die "sshd_config 未包含 /etc/ssh/sshd_config.d/*.conf"

current_port=""
if [[ -n "${SSH_CONNECTION:-}" ]]; then
  current_port="$(awk '{print $4}' <<<"$SSH_CONNECTION")"
fi
if [[ ! "$current_port" =~ ^[0-9]+$ ]]; then
  current_port="$(sshd -T | awk '$1 == "port" { print $2; exit }')"
fi
[[ "$current_port" =~ ^[0-9]+$ ]] || current_port=22

if [[ "$new_port" != "$current_port" ]] && \
   ss -H -ltn | awk '{print $4}' | grep -Eq "(^|:)$new_port$"; then
  die "TCP $new_port 已被其他服务占用"
fi

if ! id "$admin_user" >/dev/null 2>&1; then
  useradd --create-home --shell /bin/bash "$admin_user"
fi
usermod --append --groups sudo --shell /bin/bash "$admin_user"
passwd --lock "$admin_user" >/dev/null 2>&1 || true

admin_home="$(getent passwd "$admin_user" | cut -d: -f6)"
[[ -n "$admin_home" && -d "$admin_home" ]] || die "无法确定 $admin_user 的主目录"
admin_group="$(id -gn "$admin_user")"

# /run is normally tmpfs on Debian, so the generated private key is never
# written to persistent storage.
temp_dir="$(mktemp -d /run/proxy-llm-ssh.XXXXXX)"
cleanup() {
  rm -rf -- "$temp_dir"
}
trap cleanup EXIT
chmod 700 "$temp_dir"

key_comment="$admin_user@$(hostname)-$(date -u +%Y%m%dT%H%M%SZ)"
ssh-keygen -q -t ed25519 -a 64 -N '' -C "$key_comment" -f "$temp_dir/id_ed25519"
public_key="$(<"$temp_dir/id_ed25519.pub")"

install -d -m 700 -o "$admin_user" -g "$admin_group" "$admin_home/.ssh"
touch "$admin_home/.ssh/authorized_keys"
chmod 600 "$admin_home/.ssh/authorized_keys"
chown "$admin_user:$admin_group" "$admin_home/.ssh/authorized_keys"
if ! grep -qxF "$public_key" "$admin_home/.ssh/authorized_keys"; then
  printf '%s\n' "$public_key" >> "$admin_home/.ssh/authorized_keys"
fi

sudoers_temp="$temp_dir/sudoers"
printf '%s ALL=(ALL:ALL) NOPASSWD: ALL\n' "$admin_user" > "$sudoers_temp"
chmod 440 "$sudoers_temp"
visudo -cf "$sudoers_temp" >/dev/null
install -o root -g root -m 440 "$sudoers_temp" "/etc/sudoers.d/90-$admin_user"

write_sshd_config() {
  local include_old_port="$1"
  local output="$2"
  {
    echo "# Managed by Proxy-LLM-API deploy/ssh-hardening/bootstrap.sh"
    if [[ "$include_old_port" == "true" && "$current_port" != "$new_port" ]]; then
      echo "Port $current_port"
    fi
    echo "Port $new_port"
    echo "PubkeyAuthentication yes"
    echo "AuthenticationMethods publickey"
    echo "PasswordAuthentication no"
    echo "KbdInteractiveAuthentication no"
    echo "PermitRootLogin no"
    echo "PermitEmptyPasswords no"
    echo "MaxAuthTries 3"
    echo "X11Forwarding no"
    echo "AllowUsers $admin_user"
  } > "$output"
}

config_temp="$temp_dir/sshd.conf"
write_sshd_config true "$config_temp"
install -o root -g root -m 600 "$config_temp" "$config_file"
sshd -t || die "新的 sshd 配置校验失败"

if command -v ufw >/dev/null 2>&1 && ufw status | grep -q '^Status: active'; then
  ufw allow "$new_port/tcp" >/dev/null
fi
if command -v firewall-cmd >/dev/null 2>&1 && firewall-cmd --state >/dev/null 2>&1; then
  firewall-cmd --permanent --add-port="$new_port/tcp" >/dev/null
  firewall-cmd --reload >/dev/null
fi

finalize_temp="$temp_dir/finalize"
cat > "$finalize_temp" <<FINALIZE
#!/usr/bin/env bash
set -euo pipefail
[[ "\$(id -u)" -eq 0 ]] || { echo "请通过 sudo 运行" >&2; exit 1; }
temp_file="\$(mktemp)"
trap 'rm -f -- "\$temp_file"' EXIT
cat > "\$temp_file" <<'CONFIG'
# Managed by Proxy-LLM-API deploy/ssh-hardening/bootstrap.sh
Port $new_port
PubkeyAuthentication yes
AuthenticationMethods publickey
PasswordAuthentication no
KbdInteractiveAuthentication no
PermitRootLogin no
PermitEmptyPasswords no
MaxAuthTries 3
X11Forwarding no
AllowUsers $admin_user
CONFIG
install -o root -g root -m 600 "\$temp_file" "$config_file"
sshd -t
if systemctl list-unit-files ssh.service >/dev/null 2>&1; then
  systemctl reload ssh
else
  systemctl reload sshd
fi
sleep 1
if [[ "$current_port" != "$new_port" ]] && \
   ss -H -ltn | awk '{print \$4}' | grep -Eq '(^|:)$current_port\$'; then
  echo "警告: 旧端口 $current_port 仍被另一份 sshd 配置声明。" >&2
  echo "请执行 sshd -T | grep ^port 并检查 /etc/ssh/sshd_config。" >&2
  exit 1
fi
rm -f -- "$finalize_script"
echo "SSH 加固已确认；旧端口 $current_port 已停止监听。"
FINALIZE
install -o root -g root -m 700 "$finalize_temp" "$finalize_script"

reload_ssh
sleep 1
ss -H -ltn | awk '{print $4}' | grep -Eq "(^|:)$new_port$" || \
  die "sshd 没有监听新端口 $new_port"

public_ip="$(curl -4 -fsSL --max-time 10 https://api.ipify.org 2>/dev/null || true)"
[[ -n "$public_ip" ]] || public_ip="<服务器IP>"

echo
echo "SSH 密钥入口已建立。当前会话确认完成前不要关闭。"
if [[ "$current_port" != "$new_port" ]]; then
  echo "旧端口 $current_port 暂时保留，但只允许 $admin_user 使用密钥登录。"
fi
echo "请先确认云防火墙已放行 TCP $new_port。"
echo
echo "========== 私钥开始（只显示这一次） =========="
cat "$temp_dir/id_ed25519"
echo "========== 私钥结束 =========="
echo
echo "保存到本机 ~/.ssh/jp_proxy 后执行："
echo "  chmod 600 ~/.ssh/jp_proxy"
echo "  ssh -p $new_port -i ~/.ssh/jp_proxy $admin_user@$public_ip"
echo
echo "新连接成功后，在新连接中关闭旧 SSH 端口："
echo "  sudo $finalize_script"
echo
echo "对应的 ~/.ssh/config："
echo "Host jp-proxy"
echo "    HostName $public_ip"
echo "    User $admin_user"
echo "    Port $new_port"
echo "    IdentityFile ~/.ssh/jp_proxy"
echo "    IdentitiesOnly yes"
