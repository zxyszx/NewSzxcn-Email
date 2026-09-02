# NewSzxcn-Email

NewSzxcn-Email 是一个可自建、可管理、带完整 Webmail 与管理后台的开源邮箱系统。

[![Release](https://img.shields.io/github/v/release/zxyszx/NewSzxcn-Email?display_name=tag&sort=semver)](https://github.com/zxyszx/NewSzxcn-Email/releases)
[![Docker Release](https://github.com/zxyszx/NewSzxcn-Email/actions/workflows/docker.yml/badge.svg)](https://github.com/zxyszx/NewSzxcn-Email/actions/workflows/docker.yml)
[![CI](https://github.com/zxyszx/NewSzxcn-Email/actions/workflows/ci.yml/badge.svg)](https://github.com/zxyszx/NewSzxcn-Email/actions/workflows/ci.yml)
[![License](https://img.shields.io/github/license/zxyszx/NewSzxcn-Email)](LICENSE)

[邮箱后台配置指南](docs/GUIDE.md) · [版本发布](https://github.com/zxyszx/NewSzxcn-Email/releases) · [部署文档](deploy/README.md) · [English](README.en.md)

## 主要功能

| 模块 | 能力 |
| --- | --- |
| Webmail | 收发邮件、草稿、附件、搜索、星标、标签、自定义文件夹、稍后提醒、导入与导出 |
| 邮箱管理 | 多邮箱切换、邮箱申请、暂停收信、账号级与邮箱级转发、外部 IMAP |
| 收信规则 | 多条件匹配、移动、标记、删除、转发、规则排序与应用到已有邮件 |
| 管理后台 | 账号、权限配置、域名、邮箱、转发、全部邮件、发送队列、系统设置 |
| 邮件服务 | Postfix、Dovecot、Rspamd、DKIM、IMAP、POP3、SMTP Submission |
| 安全 | 2FA、Turnstile、权限组、API Token、转发邮箱验证、SSRF 防护 |
| 运维 | Docker 单镜像部署、在线检查更新、页面一键更新、自动备份、命令行回滚 |

## 一键安装

支持 Debian / Ubuntu 的 `amd64` 与 `arm64` 服务器。建议至少 2 核、2 GB 内存，并准备一个已解析到服务器的邮件主机名，例如 `mail.example.com`。

```bash
curl -fsSL https://raw.githubusercontent.com/zxyszx/NewSzxcn-Email/main/install.sh | sudo bash
```

已使用 `root` 登录时，也可以使用：

```bash
bash <(curl -fsSL https://raw.githubusercontent.com/zxyszx/NewSzxcn-Email/main/install.sh)
```

### 管理面板

脚本会根据服务器当前状态显示不同菜单。空白服务器只显示安装和退出，避免误选尚不可用的更新、回滚或重启功能：

```text
==================================================
          NewSzxcn Email 管理面板
==================================================
状态：尚未安装
--------------------------------------------------
1. 一键安装 NewSzxcn Email
0. 退出
==================================================
请选择 [1]：
```

检测到已有安装后，会动态读取服务状态、实际镜像版本和访问地址，并默认选择安全更新：

```text
==================================================
          NewSzxcn Email 管理面板
==================================================
状态：运行中
版本：v1.2.19（示例，以实际安装版本为准）
地址：https://mail.example.com
--------------------------------------------------
安装与维护
1. 重新安装（完整备份，失败自动恢复）
2. 更新系统（自动备份，失败自动回滚）
3. 检查并修复现有安装

服务管理
4. 查看运行状态
5. 重启服务
6. 查看实时日志

证书与恢复
7. 管理 SSL 证书
8. 回滚到上次更新前版本

账号与帮助
9. 邮箱后台配置指南
10. 查看管理员登录信息
11. 重置管理员登录密码

危险操作
12. 卸载服务（保留数据）

0. 退出
==================================================
请选择 [2]：
```

容器停止后菜单会显示“已停止”；配置存在但运行文件残缺时会显示“安装不完整”并默认选择修复。空白服务器进入安装后，会依次引导配置防火墙、邮件服务器域名、邮箱地址域名、管理员邮箱和 Web 部署方式。选择重新安装会先将
`/opt/newszxcn-email` 完整改名备份，失败时自动恢复原目录、Nginx 和旧容器。更新前会
校验数据库备份并保存镜像、Compose、环境、安装脚本和 Nginx，失败时执行完整恢复。

脚本会自动完成：

- 安装或检查 Docker Engine 与 Docker Compose v2
- 选择自动添加邮局必要端口规则，或保留现有防火墙由用户自行配置
- 自动检测并确认邮箱地址域名；创建管理员邮箱时可选择默认 `admin` 前缀或自行输入前缀，例如服务器域名 `mail.example.com`、前缀 `admin` 会创建 `admin@example.com`；回车自动生成 12 位密码，自定义密码最少 6 位
- 选择自动 Nginx + SSL、宝塔/已有 Nginx 反代或 HTTP 测试模式
- 自动模式使用官方 `acme.sh` 签发和续期证书，不会强制停止占用 80 端口的进程
- 创建 `/opt/newszxcn-email` 持久化目录
- 拉取 GHCR 镜像并启动邮件服务
- 生成后台在线更新所需的内部鉴权令牌
- 等待 Web 与 API 健康检查通过

安装完成后访问配置的 `LANQIN_PUBLIC_BASE_URL`。首次登录后，在后台添加邮件域名并按照 DNS 检测页配置记录。

> 一键安装不会替你修改 DNS，也不能绕过云厂商对 25 端口的限制。公网收信前必须确认 25 端口可入站，公网发信前需确认 25 端口可出站。

## 更新与回滚

### 后台页面更新

超级管理员可点击后台侧栏中的版本号，查看当前版本、最新版本与更新日志。点击“立即更新”后，系统会先在线备份 SQLite 数据库，把当前正常镜像标记为 `rollback-previous`，再拉取新镜像并重启；页面会等待服务恢复后自动刷新。只有新容器通过健康检查后，更新器才会删除属于 NewSzxcn Email 的多余无标签旧镜像。

更新期间容器会短暂重启。接口会先向页面确认更新已受理，再异步替换容器；页面遇到临时 `502/503/504` 或网络中断时会继续检查服务状态，不会立即误报更新失败。

更新服务只在 Docker 内部网络开放，不映射公网端口。普通用户和普通后台权限组无法执行系统更新。更新器使用跨 CLI 与网页更新的文件锁，重复触发会被拒绝。项目没有启用 Watchtower `--cleanup`，避免它在健康检查和回滚确认前删除旧镜像。

### 命令行更新

```bash
sudo newszxcn-email update
```

命令行更新会创建完整回滚快照、校验数据库备份并执行健康检查。旧版本安装首次执行此命令时，会自动用受控更新器替换原 Watchtower 服务并写入安装目录配置。需要恢复上次更新前的镜像、数据库和配置时运行：

```bash
sudo newszxcn-email rollback
```

手动回滚会先要求确认，并额外备份当前数据库，再恢复更新前版本。回滚后的镜像会保持锁定，直到下一次明确执行更新。

正常情况下只保留当前运行镜像、`ghcr.io/zxyszx/newszxcn-email:rollback-previous` 和更新器镜像。清理只匹配带有 NewSzxcn Email 项目标签且没有任何标签的旧镜像，不运行全局 prune，不删除容器、卷、邮件、数据库、证书、配置或备份。每次清理会在更新器日志中记录清理前后的 `docker system df`、删除的镜像 ID 和估算释放空间。

手动检查：

```bash
docker system df
docker image ls --filter 'label=org.opencontainers.image.title=NewSzxcn Email all-in-one'
docker image ls --filter 'label=org.opencontainers.image.title=NewSzxcn Email updater'
docker image inspect ghcr.io/zxyszx/newszxcn-email:rollback-previous --format '{{.Id}}'
cd /opt/newszxcn-email && docker compose ps
cd /opt/newszxcn-email && docker compose logs --tail=200 updater
```

常用运维命令：

```bash
sudo ns
sudo newszxcn-email guide
sudo newszxcn-email credentials
sudo newszxcn-email reset-password
sudo newszxcn-email repair
sudo newszxcn-email status
sudo newszxcn-email logs
sudo newszxcn-email restart
sudo newszxcn-email certificate
sudo newszxcn-email uninstall
```

`credentials` 显示安装或最近一次命令行重置时记录的管理员登录信息。数据库中的密码采用 bcrypt 哈希，无法反向查看；如果管理员后来在网页修改过密码，记录值可能已经失效。`reset-password` 只重置配置管理员的统一登录密码，并同步该管理员名下邮箱的 SMTP/IMAP 密码，不会修改普通用户或其邮箱。重置前会先备份并校验数据库。

`uninstall` 会移除容器和自动生成的 Nginx 配置，但不删除 `/opt/newszxcn-email` 中的配置、证书、数据库与邮件。卸载时可以单独停止当前域名的 acme.sh 自动续期，不会影响 acme.sh 管理的其他域名。

## DNS 与端口

至少需要以下 DNS 记录：

| 类型 | 示例 | 用途 |
| --- | --- | --- |
| A / AAAA | `mail.example.com -> 服务器 IP` | 邮件主机与 Webmail |
| MX | `example.com -> mail.example.com` | 接收邮件 |
| SPF TXT | 后台生成 | 声明允许发信的服务器 |
| DKIM TXT | 后台按域名生成 | 邮件签名验证 |
| DMARC TXT | 后台生成建议值 | 发信策略与报告 |

服务器防火墙和云安全组应按需开放：

| 端口 | 协议 | 用途 |
| --- | --- | --- |
| 25 | TCP | SMTP 服务器间收发信 |
| 80 / 443 | TCP | Webmail 与证书签发 |
| 465 / 587 | TCP | 邮件客户端 SMTP 发信 |
| 993 | TCP | IMAP SSL |
| 995 | TCP | POP3 SSL |

## 数据目录

默认部署目录为 `/opt/newszxcn-email`：

```text
/opt/newszxcn-email/
|-- .env                 # 环境配置与内部更新令牌
|-- docker-compose.yml   # 邮箱主服务与内部更新服务
|-- data/                # SQLite、附件和更新前备份
|-- mail/                # Maildir 邮件原文
|-- dkim/                # DKIM 私钥
`-- certs/               # Web、SMTP、IMAP、POP3 共用的 TLS 证书
```

升级和重建容器不会删除这些目录。备份时应同时保存 `data`、`mail`、`dkim`、`certs` 与 `.env`。

## 手动部署

需要自行控制 Compose 配置时：

```bash
git clone https://github.com/zxyszx/NewSzxcn-Email.git
cd NewSzxcn-Email/deploy
cp .env.example .env
# 编辑 .env
docker compose pull
docker compose up -d
```

本地源码构建：

```bash
cd deploy
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

更完整的证书、外部 SMTP、Webhook 和排错说明见 [deploy/README.md](deploy/README.md)。

## 技术栈

- 后端：Go、Chi、SQLite
- 前端：React、TypeScript、TanStack Query、shadcn/ui、Tailwind CSS
- 邮件：Postfix、Dovecot、Rspamd
- 部署：Docker、Docker Compose、GitHub Actions、GHCR

## 本地开发

```bash
cd apps/api
go run ./cmd/server
```

```bash
cd apps/web
pnpm install
pnpm run dev
```

后端不在默认的 `http://localhost:8080` 时，可通过 `VITE_API_TARGET=http://localhost:18080 pnpm run dev` 指定本地代理目标。

提交前建议运行：

```bash
cd apps/api && go test ./...
cd apps/web && pnpm run check
```

## 开源协议

[MIT](LICENSE)
