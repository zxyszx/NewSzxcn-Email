#!/usr/bin/env bash
set -Eeuo pipefail

REPOSITORY="zxyszx/NewSzxcn-Email"
RAW_BASE="https://raw.githubusercontent.com/${REPOSITORY}/main"
INSTALL_DIR="${LANQIN_INSTALL_DIR:-/opt/newszxcn-email}"
COMMAND="${1:-install}"
ROLLBACK_FILE="${INSTALL_DIR}/.rollback-image"
NGINX_CONFIG="/etc/nginx/conf.d/newszxcn-email.conf"
ACME_WEBROOT="/var/www/newszxcn-acme"
CERT_DIR="${INSTALL_DIR}/certs"

log() { printf '\033[1;34m[NewSzxcn]\033[0m %s\n' "$*"; }
success() { printf '\033[1;32m[完成]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[提示]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[错误]\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
NewSzxcn Email 管理命令

用法：newszxcn-email <command>

  install     首次安装；检测到已有安装时显示操作菜单
  update      备份数据库并更新到最新版
  status      查看容器与健康状态
  logs        持续查看运行日志
  restart     重启服务并重载 Nginx
  certificate 申请或续期自动模式的 SSL 证书
  rollback    回滚到上次命令行更新前的镜像
  uninstall   停止并移除容器，保留邮件与配置
EOF
}

require_root() {
  if [[ "${EUID}" -ne 0 ]]; then
    fail "请使用 root 运行，例如：curl -fsSL ${RAW_BASE}/install.sh | sudo bash"
  fi
}

require_curl() {
  command -v curl >/dev/null 2>&1 || fail "系统缺少 curl，请先安装 curl。"
}

install_packages() {
  if command -v apt-get >/dev/null 2>&1; then
    DEBIAN_FRONTEND=noninteractive apt-get update -y
    DEBIAN_FRONTEND=noninteractive apt-get install -y "$@"
  elif command -v dnf >/dev/null 2>&1; then
    dnf install -y "$@"
  elif command -v yum >/dev/null 2>&1; then
    yum install -y "$@"
  else
    fail "暂不支持当前系统的软件包管理器，请使用 Ubuntu、Debian、CentOS、Rocky Linux 或 AlmaLinux。"
  fi
}

ensure_docker() {
  if ! command -v docker >/dev/null 2>&1; then
    log "未检测到 Docker，正在安装 Docker Engine..."
    curl -fsSL https://get.docker.com | sh
  fi
  if command -v systemctl >/dev/null 2>&1; then
    systemctl enable --now docker >/dev/null 2>&1 || true
  fi
  docker compose version >/dev/null 2>&1 || fail "需要 Docker Compose v2。"
}

compose() {
  docker compose --project-directory "${INSTALL_DIR}" -f "${INSTALL_DIR}/docker-compose.yml" "$@"
}

script_dir() {
  cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd
}

refresh_assets() {
  local source_dir local_source="false"
  source_dir="$(script_dir || true)"
  if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" && "${BASH_SOURCE[0]}" != /dev/fd/* ]]; then
    local_source="true"
  fi
  install -d -m 0755 "${INSTALL_DIR}"
  if [[ "${local_source}" == "true" && -f "${source_dir}/deploy/docker-compose.yml" && -f "${source_dir}/deploy/.env.example" ]]; then
    install -m 0644 "${source_dir}/deploy/docker-compose.yml" "${INSTALL_DIR}/docker-compose.yml"
    install -m 0644 "${source_dir}/deploy/.env.example" "${INSTALL_DIR}/.env.example"
    install -m 0755 "${source_dir}/install.sh" /usr/local/bin/newszxcn-email
  else
    curl -fsSL "${RAW_BASE}/deploy/docker-compose.yml" -o "${INSTALL_DIR}/docker-compose.yml"
    curl -fsSL "${RAW_BASE}/deploy/.env.example" -o "${INSTALL_DIR}/.env.example"
    curl -fsSL "${RAW_BASE}/install.sh" -o /usr/local/bin/newszxcn-email.new
    chmod 0755 /usr/local/bin/newszxcn-email.new
    mv /usr/local/bin/newszxcn-email.new /usr/local/bin/newszxcn-email
  fi
}

random_secret() {
  if command -v openssl >/dev/null 2>&1; then
    openssl rand -hex 24
  else
    od -An -N24 -tx1 /dev/urandom | tr -d ' \n'
  fi
}

random_admin_password() {
  local value
  if command -v openssl >/dev/null 2>&1; then
    value="$(openssl rand -base64 24 | tr -dc 'A-Za-z0-9')"
  else
    value="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
  fi
  printf '%.12s' "${value}"
}

set_env() {
  local key="$1" value="$2" file="${INSTALL_DIR}/.env" tmp
  tmp="$(mktemp)"
  awk -v key="${key}" -v value="${value}" '
    BEGIN { found=0 }
    $0 ~ "^" key "=" { print key "=" value; found=1; next }
    { print }
    END { if (!found) print key "=" value }
  ' "${file}" > "${tmp}"
  cat "${tmp}" > "${file}"
  rm -f "${tmp}"
}

env_value() {
  local key="$1"
  sed -n "s/^${key}=//p" "${INSTALL_DIR}/.env" | tail -n 1
}

prompt_value() {
  local variable="$1" prompt="$2" default_value="$3" secret="${4:-false}"
  local value="${!variable:-}"
  if [[ -z "${value}" ]] && has_tty; then
    if [[ "${secret}" == "true" ]]; then
      read -r -s -p "${prompt}${default_value:+ [${default_value}]}: " value </dev/tty
      printf '\n' >/dev/tty
    else
      read -r -p "${prompt}${default_value:+ [${default_value}]}: " value </dev/tty
    fi
  fi
  value="${value:-${default_value}}"
  printf '%s' "${value}"
}

prompt_choice() {
  local variable="$1" prompt="$2" default_value="$3" value
  value="${!variable:-}"
  while true; do
    if [[ -z "${value}" ]] && has_tty; then
      read -r -p "${prompt}" value </dev/tty
    fi
    value="${value:-${default_value}}"
    if [[ "${value}" =~ ^[123]$ ]]; then
      printf '%s' "${value}"
      return
    fi
    prompt_text "[提示] 请输入 1、2 或 3。\n"
    value=""
    has_tty || fail "${variable} 必须设置为 1、2 或 3。"
  done
}

prompt_existing_install_action() {
  local public_url action
  public_url="$(env_value LANQIN_PUBLIC_BASE_URL || true)"
  prompt_text "\n[发现] 检测到已有 NewSzxcn Email 安装：${INSTALL_DIR}\n"
  if [[ -n "${public_url}" ]]; then
    prompt_text "[发现] 当前访问地址：${public_url}\n"
  fi
  prompt_text '请选择操作 [1]：\n1. 更新现有邮局（推荐，自动备份并支持回滚）\n2. 修复现有安装（保留配置和数据）\n3. 退出，不做任何修改\n'
  if [[ -z "${LANQIN_EXISTING_ACTION:-}" ]] && ! has_tty; then
    fail "非交互环境不能选择已有安装操作；更新请执行 newszxcn-email update。"
  fi
  action="$(prompt_choice LANQIN_EXISTING_ACTION "请选择 [1]: " "1")"
  printf '%s' "${action}"
}

has_tty() {
  [[ -e /dev/tty ]] && (: </dev/tty) 2>/dev/null
}

prompt_text() {
  if has_tty; then
    printf '%b' "$1" >/dev/tty
  else
    printf '%b' "$1" >&2
  fi
}

valid_hostname() {
  local hostname="$1" label tld
  local -a labels
  [[ ${#hostname} -le 253 && "${hostname}" == *.* ]] || return 1
  IFS='.' read -r -a labels <<<"${hostname}"
  for label in "${labels[@]}"; do
    [[ ${#label} -ge 1 && ${#label} -le 63 ]] || return 1
    [[ "${label}" =~ ^[A-Za-z0-9]([A-Za-z0-9-]*[A-Za-z0-9])?$ ]] || return 1
  done
  tld="${labels[${#labels[@]}-1]}"
  [[ "${tld}" =~ ^[A-Za-z]{2,63}$ ]]
}

prompt_admin_password() {
  local password="${LANQIN_ADMIN_PASSWORD:-}" confirm=""
  local safe_password_re='^[A-Za-z0-9][A-Za-z0-9._!@#%+,=:;?*/()^-]*$'
  if [[ -n "${password}" ]]; then
    [[ ${#password} -ge 6 ]] || fail "管理员密码至少需要 6 个字符。"
    [[ "${password}" =~ ${safe_password_re} ]] || fail "管理员密码包含安装配置不支持的字符。"
    printf '%s' "${password}"
    return
  fi
  if ! has_tty; then
    password="$(random_admin_password)"
    prompt_text "[提示] 已自动生成管理员密码：${password}\n"
    printf '%s' "${password}"
    return
  fi
  while true; do
    read -r -s -p "管理员密码（回车自动生成 12 位，或输入至少 6 位）: " password </dev/tty
    printf '\n' >/dev/tty
    if [[ -z "${password}" ]]; then
      password="$(random_admin_password)"
      prompt_text "[提示] 已自动生成管理员密码：${password}\n"
      printf '%s' "${password}"
      return
    fi
    if [[ ${#password} -lt 6 ]]; then
      prompt_text "[提示] 管理员密码至少需要 6 个字符。\n"
      continue
    fi
    if [[ ! "${password}" =~ ${safe_password_re} ]]; then
      prompt_text "[提示] 密码必须以字母或数字开头，只能使用字母、数字和常用符号。\n"
      continue
    fi
    read -r -s -p "再次输入管理员密码: " confirm </dev/tty
    printf '\n' >/dev/tty
    if [[ "${password}" != "${confirm}" ]]; then
      prompt_text "[提示] 两次输入的密码不一致，请重新输入。\n"
      continue
    fi
    printf '%s' "${password}"
    return
  done
}

configure_first_install() {
  if [[ -f "${INSTALL_DIR}/.env" ]]; then
    return
  fi

  local firewall_mode hostname admin_username admin_password web_mode public_url update_token
  prompt_text '\n防火墙配置 [1]：\n1. 仅开放邮局必要端口（推荐）\n2. 保留现有防火墙，由用户自行配置\n3. 开放全部端口（不推荐）\n'
  firewall_mode="$(prompt_choice LANQIN_INSTALL_FIREWALL_MODE "请选择 [1]: " "1")"

  hostname="$(prompt_value LANQIN_PUBLIC_HOSTNAME "邮件服务器域名，例如 mail.example.com" "")"
  valid_hostname "${hostname}" || fail "邮件服务器域名格式不正确。"

  admin_username="$(prompt_value LANQIN_ADMIN_USERNAME "管理员用户名" "admin")"
  [[ "${admin_username}" =~ ^[A-Za-z0-9][A-Za-z0-9._%+-]{1,79}$ ]] || fail "管理员用户名需为 2-80 位且不能包含 @。"
  admin_password="$(prompt_admin_password)"

  prompt_text '\nWeb 部署方式 [1]：\n1. 自动配置 Nginx + SSL\n2. 宝塔/已有 Nginx 反代\n3. 仅 HTTP 测试\n'
  web_mode="$(prompt_choice LANQIN_INSTALL_WEB_MODE "请选择 [1]: " "1")"
  if [[ "${web_mode}" == "3" ]]; then
    public_url="http://${hostname}"
  else
    public_url="https://${hostname}"
  fi

  update_token="$(random_secret)"
  install -m 0600 "${INSTALL_DIR}/.env.example" "${INSTALL_DIR}/.env"
  set_env LANQIN_INSTALL_FIREWALL_MODE "${firewall_mode}"
  set_env LANQIN_PUBLIC_HOSTNAME "${hostname}"
  set_env LANQIN_PUBLIC_BASE_URL "${public_url}"
  set_env LANQIN_ADMIN_USERNAME "${admin_username}"
  set_env LANQIN_ADMIN_PASSWORD "${admin_password}"
  set_env LANQIN_INSTALL_WEB_MODE "${web_mode}"
  set_env LANQIN_UPDATE_TOKEN "${update_token}"
  chmod 0600 "${INSTALL_DIR}/.env"
}

ensure_update_token() {
  local token
  token="$(env_value LANQIN_UPDATE_TOKEN || true)"
  if [[ -z "${token}" ]]; then
    set_env LANQIN_UPDATE_TOKEN "$(random_secret)"
    chmod 0600 "${INSTALL_DIR}/.env"
  fi
}

prepare_directories() {
  install -d -m 0755 "${INSTALL_DIR}/data" "${INSTALL_DIR}/mail" "${INSTALL_DIR}/dkim" "${CERT_DIR}"
  install -d -m 0700 "${INSTALL_DIR}/data/backups"
}

configure_runtime_bindings() {
  local web_mode
  web_mode="$(env_value LANQIN_INSTALL_WEB_MODE || true)"
  case "${web_mode}" in
    1|2)
      set_env LANQIN_HTTP_BIND "127.0.0.1:8088"
      set_env LANQIN_ALLOW_INSECURE_HTTP "false"
      ;;
    3)
      set_env LANQIN_HTTP_BIND "80"
      set_env LANQIN_ALLOW_INSECURE_HTTP "true"
      ;;
    "")
      warn "这是旧版安装配置，保留现有 Web 端口和反向代理设置。"
      ;;
  esac
}

detect_ssh_ports() {
  local ports=""
  if command -v sshd >/dev/null 2>&1; then
    ports="$(sshd -T 2>/dev/null | awk '$1 == "port" {print $2}' | sort -nu || true)"
  fi
  if [[ -z "${ports}" ]] && command -v ss >/dev/null 2>&1; then
    ports="$(ss -lntp 2>/dev/null | awk '/sshd/ {sub(/.*:/, "", $4); print $4}' | sort -nu || true)"
  fi
  printf '%s\n' "${ports:-22}"
}

configure_restricted_firewall() {
  local ports=(25 80 443 465 587 993 995) ssh_port
  while IFS= read -r ssh_port; do
    [[ "${ssh_port}" =~ ^[0-9]+$ ]] && ports+=("${ssh_port}")
  done < <(detect_ssh_ports)

  if command -v firewall-cmd >/dev/null 2>&1; then
    systemctl enable --now firewalld >/dev/null 2>&1 || fail "firewalld 启动失败。"
    for ssh_port in "${ports[@]}"; do
      firewall-cmd --permanent --add-port="${ssh_port}/tcp" >/dev/null
    done
    firewall-cmd --reload >/dev/null
    success "firewalld 已仅开放 SSH 和邮局必要端口。"
    return
  fi

  if ! command -v ufw >/dev/null 2>&1; then
    install_packages ufw
  fi
  if command -v ufw >/dev/null 2>&1; then
    for ssh_port in "${ports[@]}"; do
      ufw allow "${ssh_port}/tcp" >/dev/null
    done
    ufw --force enable >/dev/null
    success "UFW 已开放 SSH 和邮局必要端口。"
    return
  fi
  fail "没有找到可管理的 UFW 或 firewalld。"
}

configure_open_firewall() {
  warn "正在按选择开放全部端口，请同时检查云厂商安全组。"
  if command -v ufw >/dev/null 2>&1; then
    ufw --force disable >/dev/null 2>&1 || true
  fi
  if command -v systemctl >/dev/null 2>&1; then
    systemctl disable --now firewalld >/dev/null 2>&1 || true
  fi
  if command -v iptables >/dev/null 2>&1; then
    iptables -P INPUT ACCEPT
    iptables -F INPUT
  fi
  if command -v ip6tables >/dev/null 2>&1; then
    ip6tables -P INPUT ACCEPT
    ip6tables -F INPUT
  fi
  success "主机防火墙已调整为开放入站；云厂商安全组仍需单独配置。"
}

configure_firewall() {
  case "$(env_value LANQIN_INSTALL_FIREWALL_MODE || true)" in
    1) configure_restricted_firewall ;;
    2) warn "已保留现有防火墙，请自行开放 SSH、25、80、443、465、587、993、995/TCP。" ;;
    3) configure_open_firewall ;;
    "") warn "旧版安装未记录防火墙模式，本次不修改防火墙。" ;;
    *) fail "防火墙模式配置无效。" ;;
  esac
}

wait_for_health() {
  local attempts="${1:-60}" bind port
  bind="$(env_value LANQIN_HTTP_BIND || true)"
  bind="${bind:-80}"
  port="${bind##*:}"
  for ((i=1; i<=attempts; i++)); do
    if curl -fsS --max-time 3 "http://127.0.0.1:${port}/healthz" >/dev/null 2>&1; then
      return 0
    fi
    sleep 2
  done
  return 1
}

ensure_nginx() {
  if ! command -v nginx >/dev/null 2>&1; then
    log "正在安装宿主机 Nginx..."
    install_packages nginx
  fi
  install -d -m 0755 "$(dirname "${NGINX_CONFIG}")" "${ACME_WEBROOT}/.well-known/acme-challenge"
  if command -v getenforce >/dev/null 2>&1 && [[ "$(getenforce)" == "Enforcing" ]] && command -v setsebool >/dev/null 2>&1; then
    setsebool -P httpd_can_network_connect 1
  fi
}

write_nginx_http_config() {
  local hostname tmp
  hostname="$(env_value LANQIN_PUBLIC_HOSTNAME)"
  tmp="$(mktemp)"
  cat >"${tmp}" <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name ${hostname};

    location ^~ /.well-known/acme-challenge/ {
        root ${ACME_WEBROOT};
        default_type text/plain;
    }

    location / {
        proxy_pass http://127.0.0.1:8088;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto \$scheme;
        client_max_body_size 50m;
    }
}
EOF
  install -m 0644 "${tmp}" "${NGINX_CONFIG}"
  rm -f "${tmp}"
  nginx -t || fail "Nginx 配置检查失败，请检查 ${NGINX_CONFIG}。"
  if command -v systemctl >/dev/null 2>&1; then
    systemctl enable --now nginx
    systemctl reload nginx
  else
    nginx -s reload 2>/dev/null || nginx
  fi
}

write_nginx_https_config() {
  local hostname tmp
  hostname="$(env_value LANQIN_PUBLIC_HOSTNAME)"
  tmp="$(mktemp)"
  cat >"${tmp}" <<EOF
server {
    listen 80;
    listen [::]:80;
    server_name ${hostname};

    location ^~ /.well-known/acme-challenge/ {
        root ${ACME_WEBROOT};
        default_type text/plain;
    }

    location / {
        return 301 https://\$host\$request_uri;
    }
}

server {
    listen 443 ssl http2;
    listen [::]:443 ssl http2;
    server_name ${hostname};

    ssl_certificate ${CERT_DIR}/fullchain.pem;
    ssl_certificate_key ${CERT_DIR}/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_session_cache shared:NewSzxcnSSL:10m;
    ssl_session_timeout 1d;

    location / {
        proxy_pass http://127.0.0.1:8088;
        proxy_http_version 1.1;
        proxy_set_header Host \$host;
        proxy_set_header X-Real-IP \$remote_addr;
        proxy_set_header X-Forwarded-For \$proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto https;
        client_max_body_size 50m;
    }
}
EOF
  install -m 0644 "${tmp}" "${NGINX_CONFIG}"
  rm -f "${tmp}"
  nginx -t || fail "HTTPS 配置检查失败，请检查 ${NGINX_CONFIG}。"
  if command -v systemctl >/dev/null 2>&1; then
    systemctl reload nginx
  else
    nginx -s reload
  fi
}

ensure_acme() {
  if [[ ! -x /root/.acme.sh/acme.sh ]]; then
    local hostname
    hostname="$(env_value LANQIN_PUBLIC_HOSTNAME)"
    log "正在安装官方 acme.sh..."
    curl -fsSL https://get.acme.sh | sh -s email="hostmaster@${hostname}"
  fi
  [[ -x /root/.acme.sh/acme.sh ]] || fail "acme.sh 安装失败。"
}

install_certificate() {
  local hostname
  hostname="$(env_value LANQIN_PUBLIC_HOSTNAME)"
  ensure_acme
  log "正在为 ${hostname} 申请或检查 Let's Encrypt 证书..."
  if ! /root/.acme.sh/acme.sh --issue \
    --server letsencrypt \
    --keylength ec-256 \
    --domain "${hostname}" \
    --webroot "${ACME_WEBROOT}"; then
    warn "证书签发命令未创建新证书，将尝试安装已有的有效证书。"
  fi
  /root/.acme.sh/acme.sh --install-cert \
    --ecc \
    --domain "${hostname}" \
    --fullchain-file "${CERT_DIR}/fullchain.pem" \
    --key-file "${CERT_DIR}/privkey.pem" \
    --reloadcmd "/usr/local/bin/newszxcn-email reload" || fail "证书安装失败。请确认域名已解析到本机、80 端口可从公网访问，然后执行 newszxcn-email certificate 重试。"
  chmod 0644 "${CERT_DIR}/fullchain.pem"
  chmod 0600 "${CERT_DIR}/privkey.pem"
  set_env LANQIN_TLS_CERT_FILE "/certs/fullchain.pem"
  set_env LANQIN_TLS_KEY_FILE "/certs/privkey.pem"
  set_env LANQIN_SUBMISSION_ADDR ":587"
  set_env LANQIN_SUBMISSION_TLS_ADDR ":465"
}

configure_web_mode() {
  local web_mode
  web_mode="$(env_value LANQIN_INSTALL_WEB_MODE || true)"
  case "${web_mode}" in
    1)
      ensure_nginx
      write_nginx_http_config
      install_certificate
      write_nginx_https_config
      compose up -d --remove-orphans --force-recreate lanqin-email
      wait_for_health 90 || fail "启用证书后服务未通过健康检查，请执行 newszxcn-email logs。"
      ;;
    2)
      warn "请在宝塔或现有 Nginx 中把域名反代到 http://127.0.0.1:8088。"
      warn "邮件客户端证书仍需放入 ${CERT_DIR} 并配置 LANQIN_TLS_CERT_FILE/LANQIN_TLS_KEY_FILE。"
      ;;
    3)
      warn "当前为 HTTP 测试模式，不适合正式公网运行。"
      ;;
    "") ;;
    *) fail "Web 部署模式配置无效。" ;;
  esac
}

backup_database() {
  local timestamp
  timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
  if [[ -n "$(compose ps -q lanqin-email 2>/dev/null || true)" ]]; then
    compose exec -T lanqin-email sh -c "mkdir -p /data/backups && sqlite3 /data/lanqin.db \".backup '/data/backups/cli-update-${timestamp}.db'\"" >/dev/null
    log "数据库已备份到 data/backups/cli-update-${timestamp}.db"
  fi
}

remember_current_image() {
  local container_id image_id rollback_tag
  container_id="$(compose ps -q lanqin-email 2>/dev/null || true)"
  [[ -n "${container_id}" ]] || return 0
  image_id="$(docker inspect --format '{{.Image}}' "${container_id}")"
  rollback_tag="newszxcn-email:rollback-$(date -u +%Y%m%d%H%M%S)"
  docker image tag "${image_id}" "${rollback_tag}"
  printf '%s\n' "${rollback_tag}" > "${ROLLBACK_FILE}"
}

do_repair_install() {
  refresh_assets
  ensure_update_token
  configure_runtime_bindings
  ensure_docker
  backup_database
  remember_current_image
  configure_firewall
  prepare_directories
  log "正在拉取并修复 NewSzxcn Email 服务..."
  compose pull
  log "正在启动服务..."
  if ! compose up -d --remove-orphans; then
    warn "修复后容器启动失败，正在自动回滚。"
    do_rollback
    fail "修复失败，已回滚到原镜像。"
  fi
  if ! wait_for_health 90; then
    warn "修复后健康检查失败，正在自动回滚。"
    do_rollback
    fail "修复失败，已回滚到原镜像。"
  fi
  configure_web_mode
  success "安装完成：$(env_value LANQIN_PUBLIC_BASE_URL)"
  warn "下一步请配置 MX、SPF、DKIM、DMARC，并确认 25/465/587/993/995 端口可访问。"
}

do_install() {
  local action
  if [[ -f "${INSTALL_DIR}/.env" ]]; then
    action="$(prompt_existing_install_action)"
    case "${action}" in
      1) do_update ;;
      2) do_repair_install ;;
      3) success "已退出，现有邮局未作修改。" ;;
    esac
    return
  fi

  refresh_assets
  configure_first_install
  ensure_update_token
  configure_runtime_bindings
  ensure_docker
  configure_firewall
  prepare_directories
  log "正在拉取 NewSzxcn Email 镜像..."
  compose pull
  log "正在启动服务..."
  compose up -d --remove-orphans
  wait_for_health 90 || fail "服务未能通过健康检查，请执行 newszxcn-email logs 查看日志。"
  configure_web_mode
  success "安装完成：$(env_value LANQIN_PUBLIC_BASE_URL)"
  warn "下一步请配置 MX、SPF、DKIM、DMARC，并确认 25/465/587/993/995 端口可访问。"
}

do_update() {
  [[ -f "${INSTALL_DIR}/.env" ]] || fail "尚未安装，请先执行 install。"
  ensure_docker
  refresh_assets
  ensure_update_token
  backup_database
  remember_current_image
  log "正在拉取最新版..."
  compose pull
  if ! compose up -d --remove-orphans; then
    warn "新版本容器启动失败，正在自动回滚。"
    do_rollback
    fail "更新失败，已回滚到原镜像。"
  fi
  if ! wait_for_health 90; then
    warn "新版本健康检查失败，正在自动回滚。"
    do_rollback
    fail "更新失败，已回滚到原镜像。"
  fi
  success "系统已更新，配置、邮件、证书和数据库均已保留。"
}

do_rollback() {
  [[ -f "${ROLLBACK_FILE}" ]] || fail "没有可用的回滚镜像。"
  local image
  image="$(tr -d '\r\n' < "${ROLLBACK_FILE}")"
  docker image inspect "${image}" >/dev/null 2>&1 || fail "回滚镜像已不存在：${image}"
  log "正在回滚到 ${image}..."
  LANQIN_IMAGE="${image}" compose up -d --no-deps --force-recreate lanqin-email
  wait_for_health 90 || fail "回滚后服务仍未通过健康检查，请查看日志。"
  success "已回滚到 ${image}。"
}

reload_services() {
  [[ -f "${INSTALL_DIR}/docker-compose.yml" ]] || return 0
  ensure_docker
  compose restart lanqin-email >/dev/null
  if command -v nginx >/dev/null 2>&1 && [[ -f "${NGINX_CONFIG}" ]]; then
    nginx -t >/dev/null
    if command -v systemctl >/dev/null 2>&1; then
      systemctl reload nginx
    else
      nginx -s reload
    fi
  fi
}

do_restart() {
  reload_services
  wait_for_health 90 || fail "重启后服务未通过健康检查。"
  success "邮局服务已重启。"
}

do_certificate() {
  [[ -f "${INSTALL_DIR}/.env" ]] || fail "尚未安装。"
  [[ "$(env_value LANQIN_INSTALL_WEB_MODE || true)" == "1" ]] || fail "只有自动 Nginx + SSL 模式可使用此命令。"
  ensure_nginx
  write_nginx_http_config
  install_certificate
  write_nginx_https_config
  reload_services
  success "SSL 证书已安装并应用。"
}

do_status() {
  [[ -f "${INSTALL_DIR}/docker-compose.yml" ]] || fail "尚未安装。"
  compose ps
  if wait_for_health 1; then
    success "Web 与 API 健康检查正常。"
  else
    fail "健康检查失败。"
  fi
}

do_uninstall() {
  [[ -f "${INSTALL_DIR}/docker-compose.yml" ]] || fail "尚未安装。"
  compose down --remove-orphans
  if [[ -f "${NGINX_CONFIG}" ]]; then
    rm -f "${NGINX_CONFIG}"
    if command -v nginx >/dev/null 2>&1 && nginx -t >/dev/null 2>&1; then
      if command -v systemctl >/dev/null 2>&1; then
        systemctl reload nginx
      else
        nginx -s reload
      fi
    fi
  fi
  success "容器和自动生成的 Nginx 配置已移除，${INSTALL_DIR} 中的邮件、证书、配置和数据库仍然保留。"
}

if [[ "${LANQIN_SOURCE_ONLY:-false}" == "true" ]]; then
  if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
    return 0
  fi
  exit 0
fi

case "${COMMAND}" in
  help|-h|--help) usage ;;
  install) require_root; require_curl; do_install ;;
  update) require_root; require_curl; do_update ;;
  status) require_root; require_curl; ensure_docker; do_status ;;
  logs) require_root; require_curl; ensure_docker; compose logs -f --tail=200 lanqin-email updater ;;
  restart) require_root; require_curl; do_restart ;;
  reload) require_root; require_curl; reload_services ;;
  certificate) require_root; require_curl; do_certificate ;;
  rollback) require_root; require_curl; ensure_docker; do_rollback ;;
  uninstall) require_root; require_curl; ensure_docker; do_uninstall ;;
  *) usage; fail "未知命令：${COMMAND}" ;;
esac
