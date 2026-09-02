# NewSzxcn 邮箱后台配置指南

本指南介绍 NewSzxcn Email 的安装入口、首次配置、邮箱申请、无人收件、SSL 证书和日常更新。管理员密码等敏感信息不会保存在本文档中。

## 一键安装

建议使用 Debian 或 Ubuntu，并提前准备一个已经解析到服务器的邮件主机名，例如 `mail.example.com`。

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/zxyszx/NewSzxcn-Email/main/install.sh)
```

安装脚本会依次询问防火墙配置和邮件服务器域名，自动检测邮箱地址域名，再让你选择默认 `admin` 前缀或自定义管理员邮箱前缀，最后输入密码并选择 Web 部署方式。例如输入服务器域名 `mail.newszxcn.com`，确认检测结果 `@newszxcn.com`，选择 `1. 使用默认前缀 admin` 会创建 `admin@newszxcn.com`；选择 `2. 自定义管理员邮箱前缀` 后才需要输入邮箱账号前缀。选择“自动配置 Nginx + SSL”时，脚本会安装 Nginx，并使用官方 `acme.sh` 申请 Let's Encrypt 证书。

安装完成后，请记录终端汇总中显示的邮箱地址、后台地址、邮局域名、管理员邮箱和初始密码。初始密码仅在安装时显示；如果以后在后台修改密码，请以新密码为准。已安装系统再次运行安装命令只会重建运行环境并保留全部数据。

## 登录入口

假设安装时填写的邮件服务器域名为 `mail.example.com`：

| 入口 | 地址 | 用途 |
| --- | --- | --- |
| 邮箱前台 | `https://mail.example.com/` | 收发邮件、申请邮箱和账号设置 |
| 管理后台 | `https://mail.example.com/admin` | 管理域名、账号、邮箱、DNS 和系统设置 |

管理员账号是安装时创建的完整邮箱地址，默认为 `admin@邮箱地址域名`。前台和后台都只能使用完整主登录邮箱 + 密码登录，显示名称仅用于页面展示。

## 首次配置

### 1. 添加邮件域名

1. 登录 NewSzxcn Email 管理后台。
2. 进入“域名管理”，点击“添加域名”。
3. 填写需要收发邮件的域名并保存。
4. 点击该域名右侧的“DNS”，查看系统生成的记录。
5. 前往域名服务商的 DNS 管理页面，逐项添加 MX、SPF、DKIM 和 DMARC 记录。
6. 返回管理后台，点击“检测”。
7. 所有记录检测通过后，即可使用该域名创建邮箱。

DNS 生效通常需要几分钟到数小时。系统只能检测记录，不能代替你修改域名服务商的 DNS。

### 2. 开启账号自助申请邮箱

1. 进入“管理后台 -> 系统设置 -> 邮件”。
2. 开启“账号自助申请邮箱”。
3. 在“开放域名”中勾选允许用户申请邮箱的域名。
4. 保存设置。

开启后，用户登录邮箱前台，进入“设置 -> 邮箱管理”，即可在账号配额范围内自行申请邮箱，无需管理员逐个分配。

如果账号还没有邮箱，邮箱前台会显示“还没有可用邮箱”。此时应点击“前往邮箱管理”，进入个人中心申请邮箱。

### 3. 开启无人收件

1. 进入“管理后台 -> 系统设置 -> 邮件”。
2. 开启“无人收件”并保存。

开启后，对于系统中已经添加并启用的邮件域名，即使收件地址尚未注册，服务器仍会接收邮件。例如已经启用 `example.com` 后，发送到 `111@example.com` 的邮件也会被保留。

无人收件不会自动创建邮箱，也不会把邮件分配给普通用户。只有管理员可以在邮箱前台左侧的“未知收件”中查看这些邮件。

## Telegram 私聊邮件通知

管理员可以把每封新收邮件的概要发送到自己的 Telegram 私聊：

1. 使用 `@BotFather` 创建一个邮件通知机器人并取得 Bot Token。
2. 进入“管理后台 -> 系统设置 -> 通知”，填写 Bot Token。
3. 点击“安全绑定”生成一次性绑定码，再点击“打开机器人”。
4. 在机器人会话中发送页面生成的绑定码，然后点击“完成绑定”。
5. 勾选需要通知的邮箱；需要接收未注册地址邮件时，另行勾选“未知收件”。
6. 选择正文显示方式并点击“测试通知”。
7. 测试成功后开启“私聊新邮件通知”，保存设置。

一次性绑定码有效期为 10 分钟，只会匹配发送了该绑定码的私聊账号。通知会显示主题、发件人、收件邮箱、服务器收件时间、正文和附件摘要；识别到唯一高可信验证码时，会高亮显示并提供“复制验证码”按钮。外部 IMAP 第一次同步导入的历史邮件不会发送通知，后续新邮件才会通知。

Telegram 连接失败不会影响邮局收件。系统会保留通知任务并自动重试；关闭通知、更换机器人、更换私聊账号或修改通知邮箱范围时，尚未发送的旧任务会被清除。Telegram Bot API 不提供客户端幂等键，因此网络超时发生在 Telegram 已收到请求但服务器未收到响应时，极少数通知可能重复发送。Bot Token 不会在设置页面重新显示；以后修改其他设置时，Token 输入框留空即可保留原值。

邮件通知机器人只负责部署实例的私聊提醒。项目版本频道通知由 GitHub Release 工作流统一发送，不需要在每台服务器重复配置。

## SSL 证书与自动续期

选择“自动配置 Nginx + SSL”后，官方 `acme.sh` 会安装定时检查任务。证书接近到期时会自动续期，续期成功后自动重载 NewSzxcn Email 和 Nginx。

查看当前域名的证书和续期信息：

```bash
/root/.acme.sh/acme.sh --info --domain mail.example.com --ecc
```

查看证书实际到期时间：

```bash
openssl x509 -in /opt/newszxcn-email/certs/fullchain.pem -noout -enddate
```

手动申请、检查或重新安装证书：

```bash
sudo newszxcn-email certificate
```

证书续期计划由 `acme.sh` 和证书颁发机构动态决定，不应把预计续期日期写死在配置或文档中。

## 更新与运维

重新打开安装与运维菜单：

```bash
sudo ns
```

也可以执行 `sudo newszxcn-email menu`，或重新运行一键安装命令。

常用命令：

```bash
sudo newszxcn-email update
sudo newszxcn-email status
sudo newszxcn-email restart
sudo newszxcn-email logs
sudo newszxcn-email certificate
sudo newszxcn-email rollback
sudo newszxcn-email guide
sudo newszxcn-email credentials
sudo newszxcn-email reset-password
sudo newszxcn-email reset-2fa
```

命令行更新会创建完整回滚快照、校验 SQLite 数据库备份、将当前镜像保留为 `rollback-previous`、拉取最新镜像并执行健康检查。新版本健康后才会清理本项目无标签旧镜像；失败时立即恢复旧镜像且不会清理。`rollback` 命令会先备份当前数据库并要求确认，然后恢复上次更新前的镜像、数据库、Compose、环境、安装脚本和 Nginx 配置。CLI 与后台更新共用更新锁，回滚镜像会保持锁定，直到下一次确认正常的更新替换它。

检查镜像保留与清理日志：

```bash
docker system df
docker image ls --filter 'label=org.opencontainers.image.title=NewSzxcn Email all-in-one'
docker image ls --filter 'label=org.opencontainers.image.title=NewSzxcn Email updater'
docker image inspect ghcr.io/zxyszx/newszxcn-email:rollback-previous --format '{{.Id}}'
cd /opt/newszxcn-email && docker compose logs --tail=200 updater
```

`guide` 命令会读取当前安装地址、管理员邮箱、证书到期时间和 acme.sh 续期状态，重新生成仅 root 可读的 `/root/newszxcn-email-guide.txt`。

`credentials` 显示安装或最近一次命令行重置时记录的管理员登录信息。数据库只保存 bcrypt 密码哈希，无法反向查看真实密码；若管理员后来在网页修改过密码，记录值可能已经失效。忘记密码时执行 `reset-password`，脚本会先备份并校验数据库，然后按管理员邮箱重置唯一管理员的统一登录密码，同时同步该管理员名下邮箱的 SMTP/IMAP 密码。该操作不会修改普通用户或其邮箱。唯一管理员因双因素认证无法登录时，可执行 `reset-2fa` 应急关闭管理员 2FA，登录后应重新绑定并保存新的恢复码。

超级管理员也可以点击管理后台侧栏中的版本号，在版本更新页面检查并安装新版本。

## 必要端口

请同时检查服务器防火墙和云服务商安全组：

| 端口 | 用途 |
| --- | --- |
| `25/TCP` | 邮件服务器之间收发邮件 |
| `80/TCP` | HTTP 跳转和证书签发验证 |
| `443/TCP` | 邮箱前台和管理后台 |
| `465/TCP` | SMTP SSL 发信 |
| `587/TCP` | SMTP Submission 发信 |
| `993/TCP` | IMAP SSL 收信 |
| `995/TCP` | POP3 SSL 收信 |

部分云服务商默认封锁出站 `25/TCP`。网页可以正常打开并不代表公网邮件一定能够成功投递。

## 数据与备份

默认数据目录为 `/opt/newszxcn-email`。重要数据包括：

```text
/opt/newszxcn-email/
|-- .env
|-- data/
|-- mail/
|-- dkim/
`-- certs/
```

执行服务器快照或异地备份时，应同时保存这些目录。不要公开 `.env`、证书私钥、数据库备份或管理员登录信息。

## 更多文档

- [项目说明](../README.md)
- [Docker 部署说明](../deploy/README.md)
- [API 文档](API.md)
- [版本发布](https://github.com/zxyszx/NewSzxcn-Email/releases)
