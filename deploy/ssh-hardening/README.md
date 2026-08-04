# SSH 一键加固

脚本面向新安装的 Debian VPS。它会创建非 root 管理用户、生成一次性
Ed25519 密钥、配置免密 sudo、禁用 SSH 密码与 root 登录，并增加非标准
SSH 端口。私钥在 `/run` 的内存文件系统中生成，只打印到当前终端，脚本
退出时会删除临时文件。

先在云服务商防火墙放行准备使用的新端口，然后在当前 SSH 会话执行：

```bash
curl -fsSL \
  https://raw.githubusercontent.com/PluxelJS/Proxy-LLM-API/main/deploy/ssh-hardening/bootstrap.sh |
  sudo env SSH_PORT=23472 SSH_ADMIN_USER=deploy bash
```

不要立即关闭当前连接。把输出的私钥保存到本机，按脚本打印的命令建立
第二条 SSH 连接。确认成功后，在新连接运行：

```bash
sudo /usr/local/sbin/proxy-llm-ssh-finalize
```

在确认前，脚本会同时保留原 SSH 端口与新端口作为密钥入口，但两个端口
都已禁用密码及 root 登录。确认命令会移除旧端口，只留下新端口。

重复运行脚本会生成并追加一把新密钥，可用于旧私钥丢失但当前控制台或
SSH 会话仍然可用的情况。需要撤销某把密钥时，从
`/home/deploy/.ssh/authorized_keys` 删除对应行。
