# NewSzxcn Email

NewSzxcn Email is a self-hosted mail server with a complete Webmail client and administration console. It bundles Go, React, Postfix, Dovecot, Rspamd, and SQLite into an all-in-one Docker deployment.

[Releases](https://github.com/zxyszx/NewSzxcn-Email/releases) · [Chinese README](README.md)

## Features

- Webmail with compose, drafts, attachments, search, labels, folders, reminders, import, and export
- Multiple domains and mailboxes, DKIM, DNS checks, verified forwarding, and external IMAP
- Incoming mail rules with conditions, ordering, forwarding, moving, and bulk application
- Administration for users, permission quotas, domains, mailboxes, messages, and send queues
- SMTP, IMAP, POP3, Postfix, Dovecot, Rspamd, and SMTP Submission
- Release checks, admin-only web updates, pre-update database backups, and CLI rollback

## One-command install

Debian and Ubuntu on `amd64` or `arm64` are supported.

```bash
curl -fsSL https://raw.githubusercontent.com/zxyszx/NewSzxcn-Email/main/install.sh | sudo bash
```

The installer configures `/opt/newszxcn-email`, starts the Docker services, and waits for the health check. DNS records and provider port restrictions must still be configured by the operator.

During first installation it prompts for the firewall policy, mail hostname, administrator username/password, and Web mode. Automatic mode configures host Nginx and obtains a Let's Encrypt certificate with the official `acme.sh` client. The default username is `admin`; an empty password generates 12 characters, while a custom password requires at least 12 characters. After installation or disaster recovery, one summary prints the mail URL, admin URL, mail domain, administrator address, recorded password, and the `ns` management command.

Running the installer on an existing instance rebuilds only runtime files and containers while preserving the database, messages, certificates, and configuration. The installed menu defaults to the read-only status action.

## Update

System administrators can click the version badge in the admin sidebar to review and install a GitHub release. The updater is only reachable on the internal Docker network.

CLI update and rollback:

```bash
sudo newszxcn-email update
sudo newszxcn-email rollback
```

Useful commands:

```bash
sudo newszxcn-email status
sudo newszxcn-email logs
sudo newszxcn-email restart
sudo newszxcn-email certificate
sudo newszxcn-email uninstall
```

The uninstall command removes the containers and generated Nginx configuration while preserving certificates, configuration, messages, and the database under `/opt/newszxcn-email`.

## Required ports

Open TCP ports `25`, `80`, `443`, `465`, `587`, `993`, and `995` as needed. Public delivery also requires correct MX, SPF, DKIM, and DMARC records.

## Manual source deployment

```bash
git clone https://github.com/zxyszx/NewSzxcn-Email.git
cd NewSzxcn-Email/deploy
cp .env.example .env
docker compose -f docker-compose.yml -f docker-compose.build.yml up -d --build
```

## License

[MIT](LICENSE)
