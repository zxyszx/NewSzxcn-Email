# NewSzxcn Email Docker 部署说明

## 一键安装与更新

推荐直接使用仓库根目录的管理脚本：

```bash
curl -fsSL https://raw.githubusercontent.com/zxyszx/NewSzxcn-Email/main/install.sh | sudo bash
```

后续操作：

```bash
sudo newszxcn-email update
sudo newszxcn-email status
sudo newszxcn-email logs
sudo newszxcn-email rollback
sudo newszxcn-email guide
sudo newszxcn-email credentials
sudo newszxcn-email reset-password
sudo newszxcn-email reset-2fa
```

一键安装会把配置和数据放在 `/opt/newszxcn-email`，并部署项目自带的受控更新服务。该服务不映射公网端口，仅接受带随机令牌的容器内请求；后台“立即更新”也只允许超级管理员执行。旧安装执行一次 `sudo newszxcn-email update` 后会自动重建原 Watchtower 服务并切换到新更新器。

首次安装会依次询问防火墙模式和邮件服务器域名，自动检测邮箱地址域名，再选择默认 `admin` 前缀或自行创建管理员邮箱前缀，最后输入密码并选择 Web 部署方式。防火墙可以选择自动添加邮局必要端口规则或保留现有规则，不会清空服务器已有防火墙。自动 Web 模式会把容器绑定到 `127.0.0.1:8088`，配置宿主机 Nginx，并使用官方 `acme.sh` 申请和续期证书。例如服务器域名 `mail.newszxcn.com`、选择默认前缀会创建 `admin@newszxcn.com`；自定义管理员密码最少 6 位，留空则生成 12 位密码。

安装后输入 `ns` 可以打开统一管理菜单。更新前会创建包含数据库、镜像、Compose、环境、安装脚本和 Nginx 的回滚快照，并把当前正常镜像标记为 `ghcr.io/zxyszx/newszxcn-email:rollback-previous`；更新或健康检查失败时会自动恢复。只有健康检查成功后才会删除带 NewSzxcn Email 项目标签的无标签旧镜像。手动完整回滚前还会单独备份当前数据库，回滚镜像会保持锁定到下一次更新。

更新器不会使用 Watchtower `--cleanup`，也不会运行 `docker system prune`。清理不会涉及容器、卷、`data`、`mail`、`certs`、`.env` 或 `backups`。CLI 与网页更新使用同一个 `${LANQIN_INSTALL_DIR}/.update.lock` 文件锁，避免并发任务移动回滚标签。清理日志包含操作前后的 `docker system df`、删除的镜像 ID 和估算释放字节数。

```bash
docker system df
docker image ls --filter 'label=org.opencontainers.image.title=NewSzxcn Email all-in-one'
docker image ls --filter 'label=org.opencontainers.image.title=NewSzxcn Email updater'
docker image inspect ghcr.io/zxyszx/newszxcn-email:rollback-previous --format '{{.Id}}'
cd /opt/newszxcn-email && docker compose ps
cd /opt/newszxcn-email && docker compose logs --tail=200 updater
```

菜单可查看安装或最近一次命令行重置时记录的管理员登录信息，也可单独重置唯一管理员的统一登录密码。密码采用 bcrypt 哈希，无法从数据库反向解密；网页修改密码后，脚本中的记录可能已经失效。命令行重置前会备份并校验数据库，同时同步该管理员名下邮箱的 SMTP/IMAP 密码，不会修改普通用户或其邮箱。唯一管理员 2FA 锁死时可使用 `sudo newszxcn-email reset-2fa` 应急关闭。

## 最简单部署：单容器镜像版

服务器上不需要源码构建，只要 `docker-compose.yml` 和 `.env` 即可。

```bash
cd deploy
cp .env.example .env
# 修改 LANQIN_PUBLIC_HOSTNAME / LANQIN_PUBLIC_BASE_URL / LANQIN_MAIL_DOMAIN / LANQIN_ADMIN_EMAIL / LANQIN_ADMIN_PASSWORD
docker compose pull
docker compose up -d
```

也可以使用脚本：

```bash
cd deploy
bash install.sh
```

第一次执行会生成 `.env` 并提示你修改配置；修改完成后再次执行 `bash install.sh`。

默认只启动一个业务容器：

```text
lanqin-email
```

容器内部包含：

- Go API
- Web 静态站点
- Nginx
- Postfix
- Dovecot
- Rspamd

常用命令：

```bash
# 查看日志
docker compose logs -f lanqin-email

# 更新镜像并重启
docker compose pull
docker compose up -d

# 停止
docker compose down
```

## GHCR 镜像权限

默认镜像：

```text
ghcr.io/zxyszx/newszxcn-email:latest
ghcr.io/zxyszx/newszxcn-email-api:latest
ghcr.io/zxyszx/newszxcn-email-web:latest
ghcr.io/zxyszx/newszxcn-email-postfix:latest
ghcr.io/zxyszx/newszxcn-email-dovecot:latest
ghcr.io/zxyszx/newszxcn-email-rspamd:latest
```

如果拉取时报：

```text
unauthorized
```

说明 GHCR Package 还是私有，二选一：

1. 到 GitHub Packages 把镜像改成 Public。
2. 在服务器登录 GHCR：

```bash
echo "<github_token>" | docker login ghcr.io -u <github_user> --password-stdin
```

## 本地源码构建

如果你是在完整源码仓库里本机构建，使用 build override：

```bash
cd deploy
cp .env.example .env
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

这样会使用 `deploy/all-in-one/Dockerfile` 构建单容器镜像。

## 可选：多容器调试部署

如果需要分别查看 Postfix / Dovecot / Rspamd 日志，可以使用 stack 编排。

拉取镜像版：

```bash
cd deploy
docker compose -f docker-compose.stack.yml up -d
```

源码构建版：

```bash
cd deploy
docker compose -f docker-compose.stack.yml -f docker-compose.stack.build.yml up -d --build
```

## DNS

进入 Web 管理后台后，在域名管理中查看每个域名需要配置的：

- MX
- SPF TXT
- DKIM TXT
- DMARC TXT

配置完成后点击“检测”。

## Telegram 通知

### 私聊新邮件通知

每台邮局可以在“管理后台 -> 系统设置 -> 通知”中独立配置 Telegram 私聊邮件通知：

1. 使用 `@BotFather` 创建机器人并填写 Bot Token。
2. 在 Telegram 中打开该机器人并发送 `/start`。
3. 回到后台点击“自动获取”，系统会填写最近一个私聊 Chat ID。
4. 选择“正文摘要”或“尽量显示完整正文”，点击“测试通知”。
5. 测试成功后开启“私聊新邮件通知”并保存。

Bot Token 不会通过设置查询接口返回。新邮件通知会先持久化到 SQLite 队列，Telegram 暂时不可用时按退避策略重试；通知失败不会阻塞收件。通知包含发件人、收件邮箱、主题、收件时间、正文和附件名称，不会把附件文件上传到 Telegram。

手动部署也可以在 `.env` 中设置 `LANQIN_TELEGRAM_MAIL_ENABLED`、`LANQIN_TELEGRAM_BOT_TOKEN`、`LANQIN_TELEGRAM_PRIVATE_CHAT_ID` 和 `LANQIN_TELEGRAM_BODY_MODE`。后台保存的值会持久化到数据库，并在后续启动时优先使用。

### GitHub Release 版本频道通知

版本频道通知由 GitHub Release 工作流统一发送，与各台已部署邮局是否更新无关。仓库需要配置以下 GitHub Actions Secrets：

```text
TELEGRAM_RELEASE_BOT_TOKEN
TELEGRAM_RELEASE_CHAT_ID
```

`TELEGRAM_RELEASE_CHAT_ID` 可以填写频道用户名（例如 `@YourChannel`）或频道数字 ID。机器人必须先添加为频道管理员，并具有发布消息权限。工作流只在检查、全部 Docker 镜像和 GitHub Release 成功后发送一次；未配置密钥时自动跳过，Telegram 发送失败也不会把版本发布标记为失败。

## 邮件服务边界

- Postfix 读取 `/data/lanqin.db` 中的 `domains`、`mailboxes`、`aliases`。
- Dovecot 读取同一个 SQLite 数据库进行邮箱认证，并使用 `/var/mail/vhosts` 作为 Maildir 根目录。
- 第三方客户端可使用 IMAP SSL `993`、POP3 SSL `995`、SMTP SSL `465` 或 Submission `587`。
- Rspamd 通过 milter 接入 Postfix，负责 DKIM 签名和垃圾邮件标记。
- Rspamd 会周期性从 SQLite 导出域名 DKIM 私钥到容器内 `/var/lib/rspamd/dkim`；仅当密钥内容变化时重新载入签名配置，避免继续使用内存中的旧密钥。
- Go API 是 Webmail 和管理后台入口；浏览器不直接连接 SMTP/IMAP/POP3。
- Go API 会读取 `LANQIN_MAILDIR_ROOT=/var/mail/vhosts`，周期扫描 Maildir，把 Postfix/Dovecot 入站邮件同步成 Webmail 索引。
- 第三方客户端可通过 LanQin API 提供的 SMTP `465/587` 发信；Webmail/API 和第三方客户端的“已发送”都由 API 写入，外发投递进入发送队列并由 API worker relay/retry，客户端后续 IMAP APPEND 到 Sent 会按 `Message-ID` 去重。
- 用户可在个人邮箱管理中接入外部 IMAP 账号；默认关闭，可在后台“系统设置 > 外部 IMAP”开启并配置密钥/OAuth。本地存储模式会同步到 LanQin，远端直连模式每次从远端读取。启用前必须配置外部 IMAP 密码加密密钥，默认不允许连接 localhost / 内网 / link-local IMAP 主机。Gmail / Microsoft 365 / Outlook OAuth2 需要在对应控制台配置回调地址：`/api/external-imap-oauth/gmail/callback` 或 `/api/external-imap-oauth/outlook/callback`。
- send-as v1 支持本人邮箱、启用的别名转发 source 指向本人邮箱，或数据库表 `send_as_grants` 中显式授权的地址。

## 邮件客户端 TLS 证书

Web 站点可以由宿主机 Nginx / 宝塔反代到容器 `80`，但 SMTP/IMAP/POP3 端口不会使用 Web 反代的证书。
此时可在 `.env` 调整 Web 端口绑定，避免与宿主机 Nginx 的 `80/443` 冲突：

```dotenv
LANQIN_HTTP_BIND=127.0.0.1:8088
```

宿主机 Nginx 再反向代理到 `http://127.0.0.1:8088`。容器内 Web 服务只监听 HTTP，公网 HTTPS 由宿主机 Nginx 或宝塔终止。
如果第三方客户端连接 `993/995` 时提示证书是 `localhost`，说明 Dovecot 仍在使用容器自带的测试证书。LanQin API 的 SMTP `465/587` submission 不会使用自签测试证书；启用前必须配置可读的真实证书。

生产环境请把域名证书挂载进容器，并在 `.env` 指向证书文件：

```env
LANQIN_TLS_CERT_FILE=/certs/fullchain.pem
LANQIN_TLS_KEY_FILE=/certs/privkey.pem
LANQIN_SUBMISSION_ADDR=:587
LANQIN_SUBMISSION_TLS_ADDR=:465
```

单容器示例：

```yaml
services:
  lanqin-email:
    volumes:
      - ./data:/data
      - ./mail:/var/mail/vhosts
      - ./dkim:/var/lib/rspamd/dkim
      - ./certs:/certs:ro
```

证书域名必须覆盖 `LANQIN_PUBLIC_HOSTNAME`。更新后执行：

```bash
docker compose up -d --force-recreate
```

## SMTP 发信排查

单容器部署时，Webmail 发信默认提交给同容器内的 Postfix：

```env
LANQIN_SMTP_HOST=127.0.0.1
LANQIN_SMTP_PORT=25
LANQIN_SMTP_REQUIRE_TLS=false
```

如需把上游服务商或 DSN 处理器的最终送达、退信、投诉、拒收事件写回开放 API，请设置：

```env
LANQIN_DELIVERY_WEBHOOK_SECRET=replace-with-a-long-random-secret
```

回调地址、签名算法和事件格式见仓库中的 `docs/API.md` 与 `docs/openapi.json`。该接口未配置密钥时返回 `503`。

如需把状态变化主动推送到集成方，可额外设置：

```env
LANQIN_STATUS_WEBHOOK_URL=https://integration.example.com/hooks/lanqin
LANQIN_STATUS_WEBHOOK_SECRET=replace-with-another-long-random-secret
LANQIN_STATUS_WEBHOOK_ALLOW_PRIVATE_HOSTS=false
```

事件先写入 SQLite outbox，再由后台 worker 投递；非 2xx 响应会按退避策略重试，最多 10 次。默认只允许公网 HTTPS，禁止重定向、URL 用户信息和私网/本机目标。只有可信内网或本地测试才应开启 `LANQIN_STATUS_WEBHOOK_ALLOW_PRIVATE_HOSTS`。

Split stack 使用 `docker-compose.stack.yml` 时，API 容器默认会把 `LANQIN_SMTP_HOST` 覆盖为 `postfix`，让 Webmail 和 SMTP 提交都 relay 到 Postfix service。只有改用外部 SMTP 时才需要在 `.env` 明确填写 `LANQIN_STACK_SMTP_HOST` / `LANQIN_STACK_SMTP_PORT`。

如果发送队列里出现 relay 失败，通常是 Postfix 会话被中断或外部 SMTP 配置错误。优先检查：

```bash
docker compose exec lanqin-email supervisorctl status
docker compose exec lanqin-email postconf -M smtp/inet
# SMTP 提交 465/587 由 LanQin API 提供，不再由 Postfix 监听。
docker compose exec lanqin-email sqlite3 /data/lanqin.db "select key,value from system_settings where key like 'smtp%' order by key;"
docker compose exec lanqin-email sqlite3 /data/lanqin.db "select status,attempt_count,last_error from send_queue order by created_at desc limit 10;"
docker compose logs --tail=200 lanqin-email
```

确认后台“系统设置”里没有把本机 Postfix 的 `SMTP Require TLS` 打开；本机 `127.0.0.1:25` 必须保持 TLS=false。

## 生产注意

- 建议在服务器或边缘网关配置 HTTPS。
- 云厂商通常默认封禁 25 端口，需要单独申请解封。
- SQLite 适合 V1 单机部署；多节点部署前迁移到 PostgreSQL，并把 Postfix/Dovecot maps 改为 PostgreSQL。
