#!/usr/bin/env bash
set -Eeuo pipefail

REPOSITORY="zxyszx/NewSzxcn-Email"
RAW_BASE="https://raw.githubusercontent.com/${REPOSITORY}/main"
INSTALL_DIR="${LANQIN_INSTALL_DIR:-/opt/newszxcn-email}"
COMMAND="${1:-menu}"
ROLLBACK_FILE="${INSTALL_DIR}/.rollback-image"
ROLLBACK_POINTER="${INSTALL_DIR}/.rollback-manifest"
RUNTIME_IMAGE_PIN="${INSTALL_DIR}/.rollback-runtime-image"
NGINX_CONFIG="/etc/nginx/conf.d/newszxcn-email.conf"
ACME_WEBROOT="/var/www/newszxcn-acme"
CERT_DIR="${INSTALL_DIR}/certs"
GUIDE_FILE="/root/newszxcn-email-guide.txt"
CLI_PATH="${LANQIN_CLI_PATH:-/usr/local/bin/newszxcn-email}"
CLI_ALIAS_PATH="${LANQIN_CLI_ALIAS_PATH:-/usr/local/bin/ns}"

log() { printf '\033[1;34m[NewSzxcn]\033[0m %s\n' "$*"; }
success() { printf '\033[1;32m[完成]\033[0m %s\n' "$*"; }
warn() { printf '\033[1;33m[提示]\033[0m %s\n' "$*"; }
fail() { printf '\033[1;31m[错误]\033[0m %s\n' "$*" >&2; exit 1; }

usage() {
  cat <<'EOF'
NewSzxcn Email 管理命令

用法：newszxcn-email <command>

  menu        显示安装与运维菜单
  install     首次安装；已有安装会先完整备份再重新安装
  update      备份数据库并更新到最新版
  status      查看容器与健康状态
  logs        持续查看运行日志
  restart     重启服务并重载 Nginx
  certificate 申请或续期自动模式的 SSL 证书
  rollback    回滚到上次更新前版本
  guide       显示并更新 NewSzxcn 邮箱指南
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
  local runtime_image=""
  if [[ -s "${RUNTIME_IMAGE_PIN}" ]]; then
    runtime_image="$(tr -d '\r\n' < "${RUNTIME_IMAGE_PIN}")"
  fi
  if [[ -n "${runtime_image}" ]]; then
    LANQIN_IMAGE="${runtime_image}" docker compose --project-directory "${INSTALL_DIR}" -f "${INSTALL_DIR}/docker-compose.yml" "$@"
  else
    docker compose --project-directory "${INSTALL_DIR}" -f "${INSTALL_DIR}/docker-compose.yml" "$@"
  fi
}

clear_runtime_image_pin() {
  rm -f "${RUNTIME_IMAGE_PIN}"
}

script_dir() {
  cd "$(dirname "${BASH_SOURCE[0]}")" 2>/dev/null && pwd
}

stage_assets() {
  local source_dir local_source="false"
  local compose_new env_example_new installer_new
  source_dir="$(script_dir || true)"
  if [[ -n "${BASH_SOURCE[0]:-}" && -f "${BASH_SOURCE[0]}" && "${BASH_SOURCE[0]}" != /dev/fd/* ]]; then
    local_source="true"
  fi
  install -d -m 0755 "${INSTALL_DIR}"
  compose_new="${INSTALL_DIR}/.docker-compose.yml.new"
  env_example_new="${INSTALL_DIR}/.env.example.new"
  installer_new="${INSTALL_DIR}/.install.sh.new"
  rm -f "${compose_new}" "${env_example_new}" "${installer_new}"
  if [[ "${local_source}" == "true" && -f "${source_dir}/deploy/docker-compose.yml" && -f "${source_dir}/deploy/.env.example" ]]; then
    install -m 0644 "${source_dir}/deploy/docker-compose.yml" "${compose_new}"
    install -m 0644 "${source_dir}/deploy/.env.example" "${env_example_new}"
    install -m 0755 "${source_dir}/install.sh" "${installer_new}"
  else
    curl -fsSL "${RAW_BASE}/deploy/docker-compose.yml" -o "${compose_new}"
    curl -fsSL "${RAW_BASE}/deploy/.env.example" -o "${env_example_new}"
    curl -fsSL "${RAW_BASE}/install.sh" -o "${installer_new}"
  fi
  chmod 0755 "${installer_new}"
  bash -n "${installer_new}" || fail "新版安装脚本语法检查失败，现有文件未修改。"
  if command -v shellcheck >/dev/null 2>&1; then
    shellcheck -x "${installer_new}" || fail "新版安装脚本 ShellCheck 未通过，现有文件未修改。"
  fi
  if [[ -f "${INSTALL_DIR}/.env" ]] && command -v docker >/dev/null 2>&1; then
    docker compose --project-directory "${INSTALL_DIR}" --env-file "${INSTALL_DIR}/.env" -f "${compose_new}" config >/dev/null \
      || fail "新版 Docker Compose 配置检查失败，现有文件未修改。"
  fi
}

apply_staged_assets() {
  install -m 0644 "${INSTALL_DIR}/.docker-compose.yml.new" "${INSTALL_DIR}/docker-compose.yml" || return 1
  install -m 0644 "${INSTALL_DIR}/.env.example.new" "${INSTALL_DIR}/.env.example" || return 1
  install -m 0755 "${INSTALL_DIR}/.install.sh.new" "${CLI_PATH}.new" || return 1
  mv "${CLI_PATH}.new" "${CLI_PATH}" || return 1
  rm -f "${INSTALL_DIR}/.docker-compose.yml.new" "${INSTALL_DIR}/.env.example.new" "${INSTALL_DIR}/.install.sh.new"
}

ensure_cli_alias() {
  local target="${CLI_PATH}" alias_path="${CLI_ALIAS_PATH}" existing
  [[ -x "${target}" ]] || return 0
  if [[ -L "${alias_path}" ]]; then
    existing="$(readlink "${alias_path}" 2>/dev/null || true)"
    if [[ "${existing}" == "${target}" ]]; then
      return 0
    fi
    warn "未修改 ${alias_path}：现有符号链接指向 ${existing:-未知目标}。"
    return 0
  fi
  if [[ -e "${alias_path}" ]]; then
    warn "未创建快捷命令 ns：${alias_path} 已被其他文件占用。"
    return 0
  fi
  existing="$(type -P ns 2>/dev/null || true)"
  if [[ -n "${existing}" && "${existing}" != "${alias_path}" ]]; then
    warn "未创建快捷命令 ns：系统中已存在 ${existing}。"
    return 0
  fi
  ln -s "${target}" "${alias_path}"
  success "快捷命令已创建：输入 ns 可打开管理菜单。"
}

refresh_assets() {
  stage_assets
  apply_staged_assets
  ensure_cli_alias
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
  local variable="$1" prompt="$2" default_value="$3" max_value="${4:-3}" value
  value="${!variable:-}"
  while true; do
    if [[ -z "${value}" ]] && has_tty; then
      read -r -p "${prompt}" value </dev/tty
    fi
    value="${value:-${default_value}}"
    if [[ "${value}" =~ ^[0-9]+$ ]] && (( value >= 1 && value <= max_value )); then
      printf '%s' "${value}"
      return
    fi
    prompt_text "[提示] 请输入 1 至 ${max_value}。\n"
    value=""
    has_tty || fail "${variable} 必须设置为 1 至 ${max_value}。"
  done
}

prompt_menu_choice() {
  local default_value="$1" max_value="${2:-10}" value="${LANQIN_MENU_ACTION:-}"
  if [[ -z "${value}" ]] && ! has_tty; then
    fail "非交互环境请直接使用 install、update、status 等子命令。"
  fi
  while true; do
    if [[ -z "${value}" ]] && has_tty; then
      read -r -p "请选择 [${default_value}]: " value </dev/tty
    fi
    value="${value:-${default_value}}"
    if [[ "${value}" =~ ^[0-9]+$ ]] && (( value >= 0 && value <= max_value )); then
      printf '%s' "${value}"
      return
    fi
    prompt_text "[提示] 请输入 0 至 ${max_value}。\n"
    value=""
    has_tty || fail "LANQIN_MENU_ACTION 必须设置为 0 至 ${max_value}。"
  done
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
  prompt_text '\n防火墙配置 [1]：\n1. 自动添加邮局必要端口规则（推荐）\n2. 保留现有防火墙，由用户自行配置\n'
  firewall_mode="$(prompt_choice LANQIN_INSTALL_FIREWALL_MODE "请选择 [1]: " "1" "2")"

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
    success "firewalld 已添加 SSH 和邮局必要端口规则。"
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
    success "UFW 已添加 SSH 和邮局必要端口规则。"
    return
  fi
  fail "没有找到可管理的 UFW 或 firewalld。"
}

configure_firewall() {
  case "$(env_value LANQIN_INSTALL_FIREWALL_MODE || true)" in
    1) configure_restricted_firewall ;;
    2) warn "已保留现有防火墙，请自行开放 SSH、25、80、443、465、587、993、995/TCP。" ;;
    3) warn "检测到旧版开放全部端口配置。为避免破坏现有安全规则，本次不再修改防火墙。" ;;
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

reload_nginx() {
  command -v nginx >/dev/null 2>&1 || return 0
  nginx -t >/dev/null 2>&1 || return 1
  if command -v systemctl >/dev/null 2>&1; then
    systemctl reload nginx
  else
    nginx -s reload
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
  ensure_cron_scheduler
  if [[ ! -x /root/.acme.sh/acme.sh ]]; then
    local hostname
    hostname="$(env_value LANQIN_PUBLIC_HOSTNAME)"
    log "正在安装官方 acme.sh..."
    curl -fsSL https://get.acme.sh | sh -s email="hostmaster@${hostname}"
  fi
  [[ -x /root/.acme.sh/acme.sh ]] || fail "acme.sh 安装失败。"
  if ! acme_cron_enabled; then
    /root/.acme.sh/acme.sh --install-cronjob >/dev/null || fail "acme.sh 自动续期任务安装失败。"
  fi
}

acme_cron_enabled() {
  command -v crontab >/dev/null 2>&1 \
    && crontab -l 2>/dev/null | grep -Eq 'acme\.sh"?/acme\.sh[[:space:]]+--cron'
}

ensure_cron_scheduler() {
  if ! command -v crontab >/dev/null 2>&1; then
    if command -v apt-get >/dev/null 2>&1; then
      install_packages cron
    else
      install_packages cronie
    fi
  fi
  command -v crontab >/dev/null 2>&1 || fail "系统缺少 Cron，无法配置证书自动续期。"
  if command -v systemctl >/dev/null 2>&1; then
    systemctl enable --now cron >/dev/null 2>&1 \
      || systemctl enable --now crond >/dev/null 2>&1 \
      || fail "Cron 服务启动失败，无法保证证书自动续期。"
  elif command -v service >/dev/null 2>&1; then
    service cron start >/dev/null 2>&1 || service crond start >/dev/null 2>&1 \
      || fail "Cron 服务启动失败，无法保证证书自动续期。"
  fi
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
    --reloadcmd "${CLI_PATH} reload" || fail "证书安装失败。请确认域名已解析到本机、80 端口可从公网访问，然后执行 newszxcn-email certificate 重试。"
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

current_image_id() {
  local container_id image_ref
  container_id="$(compose ps -aq lanqin-email 2>/dev/null | head -n 1 || true)"
  if [[ -n "${container_id}" ]]; then
    docker inspect --format '{{.Image}}' "${container_id}"
    return
  fi
  image_ref="$(env_value LANQIN_IMAGE || true)"
  image_ref="${image_ref:-ghcr.io/zxyszx/newszxcn-email:latest}"
  docker image inspect --format '{{.Id}}' "${image_ref}" 2>/dev/null
}

sqlite_integrity_check() {
  local database="$1" image="$2" relative result
  if command -v sqlite3 >/dev/null 2>&1; then
    result="$(sqlite3 "${database}" 'PRAGMA integrity_check;' 2>/dev/null || true)"
  else
    relative="${database#"${INSTALL_DIR}"/data/}"
    [[ "${relative}" != "${database}" ]] || return 1
    result="$(docker run --rm --entrypoint sqlite3 -v "${INSTALL_DIR}/data:/data" "${image}" "/data/${relative}" 'PRAGMA integrity_check;' 2>/dev/null || true)"
  fi
  [[ "${result}" == "ok" ]]
}

backup_database() {
  local destination="$1" image="$2" container_id running="false" relative container_destination
  [[ -s "${INSTALL_DIR}/data/lanqin.db" ]] || { warn "未找到可备份的数据库：${INSTALL_DIR}/data/lanqin.db"; return 1; }
  install -d -m 0700 "$(dirname "${destination}")"
  rm -f "${destination}"
  relative="${destination#"${INSTALL_DIR}"/data/}"
  [[ "${relative}" != "${destination}" ]] || { warn "数据库备份必须保存在 ${INSTALL_DIR}/data 内。"; return 1; }
  container_destination="/data/${relative}"
  container_id="$(compose ps -aq lanqin-email 2>/dev/null | head -n 1 || true)"
  if [[ -n "${container_id}" ]] && [[ "$(docker inspect --format '{{.State.Running}}' "${container_id}" 2>/dev/null || true)" == "true" ]]; then
    running="true"
  fi

  if [[ "${running}" == "true" ]]; then
    docker exec "${container_id}" sqlite3 /data/lanqin.db ".backup '${container_destination}'" >/dev/null \
      || { warn "运行中数据库备份失败。"; return 1; }
  elif command -v sqlite3 >/dev/null 2>&1; then
    sqlite3 "${INSTALL_DIR}/data/lanqin.db" ".backup '${destination}'" >/dev/null \
      || { warn "离线数据库备份失败。"; return 1; }
  else
    docker image inspect "${image}" >/dev/null 2>&1 || { warn "无法找到用于离线备份的旧镜像。"; return 1; }
    docker run --rm --entrypoint sqlite3 -v "${INSTALL_DIR}/data:/data" "${image}" /data/lanqin.db ".backup '${container_destination}'" >/dev/null \
      || { warn "离线数据库备份失败。"; return 1; }
  fi

  [[ -s "${destination}" ]] || { warn "数据库备份文件为空。"; return 1; }
  sqlite_integrity_check "${destination}" "${image}" || { warn "数据库备份完整性检查未通过。"; return 1; }
  log "数据库已备份并校验：${destination}"
}

create_update_snapshot() {
  local timestamp snapshot image version pointer_tmp
  timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
  snapshot="${INSTALL_DIR}/data/backups/cli-rollback-${timestamp}"
  image="$(current_image_id || true)"
  [[ -n "${image}" ]] || { warn "无法确定当前运行镜像。"; return 1; }
  docker image inspect "${image}" >/dev/null 2>&1 || { warn "当前镜像不存在：${image}"; return 1; }
  install -d -m 0700 "${snapshot}"

  cp -p "${INSTALL_DIR}/docker-compose.yml" "${snapshot}/docker-compose.yml" || return 1
  cp -p "${INSTALL_DIR}/.env" "${snapshot}/.env" || return 1
  if [[ -f "${INSTALL_DIR}/.env.example" ]]; then
    cp -p "${INSTALL_DIR}/.env.example" "${snapshot}/.env.example" || return 1
  else
    : > "${snapshot}/env-example.absent"
  fi
  if [[ -f "${CLI_PATH}" ]]; then
    cp -p "${CLI_PATH}" "${snapshot}/newszxcn-email" || return 1
  else
    : > "${snapshot}/installer.absent"
  fi
  if [[ -f "${NGINX_CONFIG}" ]]; then
    cp -p "${NGINX_CONFIG}" "${snapshot}/nginx.conf" || return 1
  else
    : > "${snapshot}/nginx.absent"
  fi
  if [[ -d "${CERT_DIR}" ]]; then
    cp -a "${CERT_DIR}" "${snapshot}/certs" || return 1
  else
    : > "${snapshot}/certs.absent"
  fi
  backup_database "${snapshot}/database.db" "${image}" || return 1

  version="$(docker image inspect --format '{{index .Config.Labels "org.opencontainers.image.version"}}' "${image}" 2>/dev/null || true)"
  printf '%s\n' "${image}" > "${snapshot}/image"
  printf '%s\n' "${version:-unknown}" > "${snapshot}/version"
  cat > "${snapshot}/rollback-manifest.json" <<EOF
{
  "image": "${image}",
  "database_backup": "${snapshot}/database.db",
  "compose_backup": "${snapshot}/docker-compose.yml",
  "installer_backup": "${snapshot}/newszxcn-email",
  "version": "${version:-unknown}",
  "created_at": "${timestamp}"
}
EOF
  find "${snapshot}" -maxdepth 1 -type f -exec chmod 0600 {} +
  pointer_tmp="${ROLLBACK_POINTER}.new"
  printf '%s\n' "${snapshot}" > "${pointer_tmp}"
  mv "${pointer_tmp}" "${ROLLBACK_POINTER}"
  printf '%s\n' "${image}" > "${ROLLBACK_FILE}"
  log "更新回滚快照已创建：${snapshot}"
}

restore_update_snapshot() {
  local snapshot="${1:-}" restore_database="${2:-true}" image
  if [[ -z "${snapshot}" && -f "${ROLLBACK_POINTER}" ]]; then
    snapshot="$(tr -d '\r\n' < "${ROLLBACK_POINTER}")"
  fi
  [[ -d "${snapshot}" && -s "${snapshot}/database.db" && -s "${snapshot}/image" && -s "${snapshot}/docker-compose.yml" && -s "${snapshot}/.env" ]] \
    || { warn "回滚快照不完整。"; return 1; }
  [[ -f "${snapshot}/.env.example" || -f "${snapshot}/env-example.absent" ]] \
    && [[ -f "${snapshot}/newszxcn-email" || -f "${snapshot}/installer.absent" ]] \
    && [[ -f "${snapshot}/nginx.conf" || -f "${snapshot}/nginx.absent" ]] \
    && [[ -d "${snapshot}/certs" || -f "${snapshot}/certs.absent" ]] \
    || { warn "回滚快照缺少文件状态标记。"; return 1; }
  image="$(tr -d '\r\n' < "${snapshot}/image")"
  docker image inspect "${image}" >/dev/null 2>&1 || { warn "回滚镜像已不存在：${image}"; return 1; }
  sqlite_integrity_check "${snapshot}/database.db" "${image}" || { warn "回滚数据库完整性检查未通过。"; return 1; }

  log "正在恢复更新前快照：${snapshot}"
  compose down --remove-orphans >/dev/null 2>&1 || true
  install -m 0644 "${snapshot}/docker-compose.yml" "${INSTALL_DIR}/docker-compose.yml" || return 1
  install -m 0600 "${snapshot}/.env" "${INSTALL_DIR}/.env" || return 1
  if [[ -f "${snapshot}/.env.example" ]]; then
    install -m 0644 "${snapshot}/.env.example" "${INSTALL_DIR}/.env.example" || return 1
  elif [[ -f "${snapshot}/env-example.absent" ]]; then
    rm -f "${INSTALL_DIR}/.env.example"
  fi
  if [[ -f "${snapshot}/newszxcn-email" ]]; then
    install -m 0755 "${snapshot}/newszxcn-email" "${CLI_PATH}" || return 1
  elif [[ -f "${snapshot}/installer.absent" ]]; then
    rm -f "${CLI_PATH}"
  fi
  if [[ "${restore_database}" == "true" ]]; then
    rm -f "${INSTALL_DIR}/data/lanqin.db-wal" "${INSTALL_DIR}/data/lanqin.db-shm"
    install -m 0600 "${snapshot}/database.db" "${INSTALL_DIR}/data/lanqin.db" || return 1
  fi
  if [[ -d "${snapshot}/certs" ]]; then
    rm -rf "${CERT_DIR}"
    cp -a "${snapshot}/certs" "${CERT_DIR}" || return 1
  elif [[ -f "${snapshot}/certs.absent" ]]; then
    rm -rf "${CERT_DIR}"
  fi
  if [[ -f "${snapshot}/nginx.conf" ]]; then
    install -m 0644 "${snapshot}/nginx.conf" "${NGINX_CONFIG}" || return 1
  elif [[ -f "${snapshot}/nginx.absent" ]]; then
    rm -f "${NGINX_CONFIG}"
  fi
  printf '%s\n' "${image}" > "${RUNTIME_IMAGE_PIN}"
  chmod 0600 "${RUNTIME_IMAGE_PIN}"
  compose up -d --remove-orphans --force-recreate || return 1
  reload_nginx || return 1
  wait_for_health 90 || return 1
  ensure_cli_alias
}

do_repair_install() {
  ensure_docker
  create_update_snapshot || fail "修复前备份失败，未修改现有安装。"
  stage_assets
  clear_runtime_image_pin
  if ! apply_staged_assets || ! ensure_update_token || ! configure_runtime_bindings; then
    restore_update_snapshot "" false || true
    fail "修复准备失败，已恢复原安装。"
  fi
  if ! (configure_firewall && prepare_directories); then
    restore_update_snapshot "" false || true
    fail "修复环境准备失败，已恢复原安装。"
  fi
  log "正在拉取并修复 NewSzxcn Email 服务..."
  if ! compose pull; then
    restore_update_snapshot "" false || true
    fail "修复镜像拉取失败，已恢复原安装。"
  fi
  log "正在启动服务..."
  if ! compose up -d --remove-orphans; then
    warn "修复后容器启动失败，正在自动回滚。"
    restore_update_snapshot || fail "修复失败，且自动恢复未完成，请使用回滚快照手动恢复。"
    fail "修复失败，已恢复到修复前版本。"
  fi
  if ! wait_for_health 90; then
    warn "修复后健康检查失败，正在自动回滚。"
    restore_update_snapshot || fail "修复失败，且自动恢复未完成，请使用回滚快照手动恢复。"
    fail "修复失败，已恢复到修复前版本。"
  fi
  if ! (configure_web_mode); then
    restore_update_snapshot || fail "Web 配置失败，且自动恢复未完成，请使用回滚快照手动恢复。"
    fail "Web 配置失败，已恢复到修复前版本。"
  fi
  generate_guide >/dev/null || warn "安装成功，但邮箱指南生成失败，可稍后执行 newszxcn-email guide 重试。"
  success "安装完成：$(env_value LANQIN_PUBLIC_BASE_URL)"
  warn "下一步请配置 MX、SPF、DKIM、DMARC，并确认 25/465/587/993/995 端口可访问。"
  warn "输入 ns 可打开管理菜单；输入 newszxcn-email guide 可查看邮箱指南。"
}

do_install() {
  if [[ -f "${INSTALL_DIR}/.env" ]]; then
    do_backup_reinstall
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
  generate_guide >/dev/null || warn "安装成功，但邮箱指南生成失败，可稍后执行 newszxcn-email guide 重试。"
  success "安装完成：$(env_value LANQIN_PUBLIC_BASE_URL)"
  warn "下一步请配置 MX、SPF、DKIM、DMARC，并确认 25/465/587/993/995 端口可访问。"
  warn "输入 ns 可打开管理菜单；输入 newszxcn-email guide 可查看邮箱指南。"
}

do_update() {
  [[ -f "${INSTALL_DIR}/.env" ]] || fail "尚未安装，请先执行 install。"
  ensure_docker
  create_update_snapshot || fail "更新前备份失败，未修改现有安装。"
  stage_assets
  clear_runtime_image_pin
  if ! apply_staged_assets || ! ensure_update_token; then
    restore_update_snapshot "" false || true
    fail "更新文件替换失败，已恢复原安装。"
  fi
  log "正在拉取最新版..."
  if ! compose pull; then
    restore_update_snapshot "" false || true
    fail "镜像拉取失败，已恢复到更新前版本。"
  fi
  if ! compose up -d --remove-orphans; then
    warn "新版本容器启动失败，正在自动回滚。"
    restore_update_snapshot || fail "更新失败，且自动恢复未完成，请使用回滚快照手动恢复。"
    fail "更新失败，已恢复到更新前版本。"
  fi
  if ! wait_for_health 90; then
    warn "新版本健康检查失败，正在自动回滚。"
    restore_update_snapshot || fail "更新失败，且自动恢复未完成，请使用回滚快照手动恢复。"
    fail "更新失败，已恢复到更新前版本。"
  fi
  ensure_cli_alias
  generate_guide >/dev/null || warn "更新成功，但邮箱指南生成失败，可稍后执行 newszxcn-email guide 重试。"
  success "系统已更新，配置、邮件、证书和数据库均已保留。"
}

do_rollback() {
  [[ -f "${ROLLBACK_POINTER}" ]] || fail "没有可用的完整回滚快照。"
  local confirm="${LANQIN_ROLLBACK_CONFIRM:-}" image timestamp emergency_backup
  if [[ -z "${confirm}" ]] && has_tty; then
    read -r -p "回滚会用更新前数据库覆盖当前数据库，确认继续吗？[y/N]: " confirm </dev/tty
  fi
  [[ "${confirm}" =~ ^([Yy]|[Yy][Ee][Ss])$ ]] || { success "已取消回滚。"; return 0; }
  image="$(current_image_id || true)"
  [[ -n "${image}" ]] || fail "无法确定当前镜像，已取消回滚。"
  timestamp="$(date -u +%Y%m%dT%H%M%SZ)"
  emergency_backup="${INSTALL_DIR}/data/backups/pre-rollback-${timestamp}.db"
  backup_database "${emergency_backup}" "${image}" || fail "当前数据库备份失败，已取消回滚。"
  restore_update_snapshot || fail "完整回滚失败，请检查回滚快照和服务日志。"
  log "回滚前数据库已保留：${emergency_backup}"
  success "已恢复镜像、数据库、Compose、环境配置、安装脚本和 Nginx 配置。"
}

reload_services() {
  [[ -f "${INSTALL_DIR}/docker-compose.yml" ]] || return 0
  ensure_docker
  compose restart lanqin-email >/dev/null
  if [[ -f "${NGINX_CONFIG}" ]]; then
    reload_nginx || fail "Nginx 配置检查或重载失败。"
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
  generate_guide >/dev/null || warn "证书已应用，但邮箱指南生成失败，可稍后执行 newszxcn-email guide 重试。"
  success "SSL 证书已安装并应用。"
}

generate_guide() {
  [[ -f "${INSTALL_DIR}/.env" ]] || return 1
  local public_url admin_url hostname admin_username certificate_expiry="未安装" renewal_status="未开启" next_renewal="等待 acme.sh 生成续期计划"
  local acme_info="" tmp
  public_url="$(env_value LANQIN_PUBLIC_BASE_URL || true)"
  hostname="$(env_value LANQIN_PUBLIC_HOSTNAME || true)"
  admin_username="$(env_value LANQIN_ADMIN_USERNAME || true)"
  public_url="${public_url:-http://${hostname}}"
  admin_url="${public_url%/}/admin"

  if [[ -s "${CERT_DIR}/fullchain.pem" ]] && command -v openssl >/dev/null 2>&1; then
    certificate_expiry="$(openssl x509 -in "${CERT_DIR}/fullchain.pem" -noout -enddate 2>/dev/null | sed 's/^notAfter=//' || true)"
    certificate_expiry="${certificate_expiry:-无法读取}"
  fi
  if [[ -x /root/.acme.sh/acme.sh ]]; then
    acme_info="$(/root/.acme.sh/acme.sh --info --domain "${hostname}" --ecc 2>/dev/null || true)"
    next_renewal="$(printf '%s\n' "${acme_info}" | sed -n "s/^Le_NextRenewTimeStr=['\"]*\([^'\"]*\).*/\1/p" | tail -n 1)"
    next_renewal="${next_renewal:-等待 acme.sh 生成续期计划}"
    if acme_cron_enabled; then
      renewal_status="已开启"
    else
      renewal_status="已安装 acme.sh，但未检测到定时任务"
    fi
  fi

  tmp="$(mktemp)"
  cat > "${tmp}" <<EOF
==================================================
 NewSzxcn 邮箱指南
==================================================

【安装信息】

邮箱前台：${public_url}
管理后台：${admin_url}
管理员账号：${admin_username:-admin}
管理员密码：仅在安装完成时显示；修改后请使用新密码
SSL 证书：有效期至 ${certificate_expiry}
自动续期：${renewal_status}
预计续期：${next_renewal}

--------------------------------------------------
一、添加邮件域名并配置 DNS
--------------------------------------------------

1. 登录管理后台，进入「域名管理」。
2. 添加邮件域名并保存，然后点击该域名右侧的「DNS」。
3. 在域名服务商处添加系统列出的 MX、SPF、DKIM、DMARC 记录。
4. 返回管理后台点击「检测」，确认所有记录均已通过。

--------------------------------------------------
二、开启账号自助申请邮箱
--------------------------------------------------

1. 进入「管理后台 -> 系统设置 -> 邮件」。
2. 开启「账号自助申请邮箱」，并勾选至少一个开放域名。
3. 用户登录邮箱前台后，可在「设置 -> 邮箱管理」中自行申请邮箱。

--------------------------------------------------
三、开启无人收件
--------------------------------------------------

1. 进入「管理后台 -> 系统设置 -> 邮件」。
2. 开启「无人收件」并保存。
3. 已启用域名下未注册地址收到的邮件，只能由管理员在「未知收件」中查看。

--------------------------------------------------
四、服务器管理与更新
--------------------------------------------------

打开菜单：ns
完整命令：newszxcn-email menu
更新系统：newszxcn-email update
查看状态：newszxcn-email status
查看日志：newszxcn-email logs
恢复版本：newszxcn-email rollback

公开教程：
https://github.com/zxyszx/NewSzxcn-Email/blob/main/docs/GUIDE.md
EOF
  install -m 0600 "${tmp}" "${GUIDE_FILE}"
  rm -f "${tmp}"
}

do_guide() {
  generate_guide || fail "尚未安装，无法生成邮箱指南。"
  cat "${GUIDE_FILE}"
  success "指南已更新并保存到 ${GUIDE_FILE}。"
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
  local confirm="${LANQIN_UNINSTALL_CONFIRM:-}" remove_renewal="${LANQIN_REMOVE_CERT_RENEWAL:-}" hostname
  if [[ -z "${confirm}" ]] && has_tty; then
    read -r -p "确认停止并卸载服务吗？邮件和配置将保留。[y/N]: " confirm </dev/tty
  fi
  [[ "${confirm}" =~ ^([Yy]|[Yy][Ee][Ss])$ ]] || { success "已取消卸载。"; return 0; }
  hostname="$(env_value LANQIN_PUBLIC_HOSTNAME || true)"
  compose down --remove-orphans
  if [[ -f "${NGINX_CONFIG}" ]]; then
    rm -f "${NGINX_CONFIG}"
    reload_nginx || warn "Nginx 未能重载，请检查并手动重载。"
  fi
  if [[ -x /root/.acme.sh/acme.sh && -n "${hostname}" ]]; then
    if [[ -z "${remove_renewal}" ]] && has_tty; then
      read -r -p "是否停止 ${hostname} 的证书自动续期？[Y/n]: " remove_renewal </dev/tty
    fi
    remove_renewal="${remove_renewal:-y}"
    if [[ "${remove_renewal}" =~ ^([Yy]|[Yy][Ee][Ss])$ ]]; then
      if /root/.acme.sh/acme.sh --remove --domain "${hostname}" --ecc >/dev/null 2>&1; then
        success "已停止 ${hostname} 的证书自动续期。"
      else
        warn "未能移除 ${hostname} 的续期记录，请使用 acme.sh 手动检查。"
      fi
    fi
  fi
  success "容器和自动生成的 Nginx 配置已移除，${INSTALL_DIR} 中的邮件、证书、配置和数据库仍然保留。"
}

do_backup_reinstall() {
  local backup_dir failed_dir old_image
  backup_dir="${INSTALL_DIR}.backup-$(date -u +%Y%m%dT%H%M%SZ)"
  failed_dir="${INSTALL_DIR}.failed-$(date -u +%Y%m%dT%H%M%SZ)"

  [[ -f "${INSTALL_DIR}/docker-compose.yml" ]] || fail "旧安装缺少 docker-compose.yml，请先选择修复现有安装。"
  ensure_docker
  old_image="$(current_image_id || true)"
  [[ -n "${old_image}" ]] || fail "无法确定旧安装镜像，已取消重新安装。"
  printf '%s\n' "${old_image}" > "${INSTALL_DIR}/.reinstall-image"
  if [[ -f "${CLI_PATH}" ]]; then
    cp -p "${CLI_PATH}" "${INSTALL_DIR}/.reinstall-installer"
  fi
  if [[ -f "${NGINX_CONFIG}" ]]; then
    cp -p "${NGINX_CONFIG}" "${INSTALL_DIR}/.reinstall-nginx.conf"
  else
    : > "${INSTALL_DIR}/.reinstall-nginx.absent"
  fi
  compose down --remove-orphans
  if [[ -f "${NGINX_CONFIG}" ]]; then
    rm -f "${NGINX_CONFIG}"
    if ! reload_nginx; then
      install -m 0644 "${INSTALL_DIR}/.reinstall-nginx.conf" "${NGINX_CONFIG}"
      reload_nginx || true
      LANQIN_IMAGE="${old_image}" compose up -d --remove-orphans --force-recreate \
        || fail "Nginx 配置已恢复，但旧容器启动失败，请检查 ${INSTALL_DIR}。"
      wait_for_health 90 || fail "Nginx 配置已恢复，但旧服务健康检查失败，请查看日志。"
      fail "Nginx 重载失败，已恢复旧配置并取消重新安装。"
    fi
  fi

  if ! mv "${INSTALL_DIR}" "${backup_dir}"; then
    if [[ -f "${INSTALL_DIR}/.reinstall-nginx.conf" ]]; then
      install -m 0644 "${INSTALL_DIR}/.reinstall-nginx.conf" "${NGINX_CONFIG}"
    fi
    LANQIN_IMAGE="${old_image}" compose up -d --remove-orphans --force-recreate || true
    reload_nginx || true
    fail "旧安装目录备份失败，已取消重新安装。"
  fi
  success "旧安装已完整备份到 ${backup_dir}。"
  log "现在开始全新安装。"
  if (do_install); then
    success "重新安装完成；旧安装备份保留在 ${backup_dir}。"
    return 0
  fi

  warn "重新安装失败，正在自动恢复旧安装。"
  if [[ -d "${INSTALL_DIR}" ]]; then
    if [[ -f "${INSTALL_DIR}/docker-compose.yml" ]]; then
      compose down --remove-orphans >/dev/null 2>&1 || true
    fi
    mv "${INSTALL_DIR}" "${failed_dir}"
  fi
  mv "${backup_dir}" "${INSTALL_DIR}" || fail "无法恢复旧安装目录，备份仍位于 ${backup_dir}。"
  old_image="$(tr -d '\r\n' < "${INSTALL_DIR}/.reinstall-image")"
  if [[ -f "${INSTALL_DIR}/.reinstall-installer" ]]; then
    install -m 0755 "${INSTALL_DIR}/.reinstall-installer" "${CLI_PATH}"
  fi
  if [[ -f "${INSTALL_DIR}/.reinstall-nginx.conf" ]]; then
    install -m 0644 "${INSTALL_DIR}/.reinstall-nginx.conf" "${NGINX_CONFIG}"
  elif [[ -f "${INSTALL_DIR}/.reinstall-nginx.absent" ]]; then
    rm -f "${NGINX_CONFIG}"
  fi
  LANQIN_IMAGE="${old_image}" compose up -d --remove-orphans --force-recreate \
    || fail "旧安装目录已恢复，但旧容器启动失败，请检查 ${INSTALL_DIR}。"
  reload_nginx || fail "旧安装目录和容器已恢复，但 Nginx 重载失败，请手动检查。"
  wait_for_health 90 || fail "旧安装已恢复，但健康检查失败，请查看日志。"
  ensure_cli_alias
  fail "重新安装失败，旧安装已自动恢复。失败的新安装保存在 ${failed_dir}。"
}

do_menu() {
  local installed="false" default_choice="1" public_url="" choice
  if [[ -f "${INSTALL_DIR}/.env" ]]; then
    installed="true"
    default_choice="2"
    public_url="$(env_value LANQIN_PUBLIC_BASE_URL || true)"
  fi

  prompt_text '\n==================================================\n'
  prompt_text ' NewSzxcn Email 一键安装与管理\n'
  prompt_text '==================================================\n'
  if [[ "${installed}" == "true" ]]; then
    prompt_text " 状态：已安装\n 路径：${INSTALL_DIR}\n"
    [[ -n "${public_url}" ]] && prompt_text " 地址：${public_url}\n"
  else
    prompt_text ' 状态：未安装\n'
  fi
  prompt_text '--------------------------------------------------\n'
  prompt_text ' 1. 安装 / 重新安装（完整备份，失败自动恢复）\n'
  prompt_text ' 2. 更新系统（数据库备份，失败自动回滚）\n'
  prompt_text ' 3. 检查并修复现有安装\n'
  prompt_text ' 4. 查看运行状态\n'
  prompt_text ' 5. 重启服务\n'
  prompt_text ' 6. 查看实时日志\n'
  prompt_text ' 7. 申请、检查或续期 SSL 证书\n'
  prompt_text ' 8. 回滚到上次更新前版本\n'
  prompt_text ' 9. NewSzxcn 邮箱指南\n'
  prompt_text ' 10. 卸载服务（保留数据）\n'
  prompt_text ' 0. 退出\n'
  prompt_text '==================================================\n'

  choice="$(prompt_menu_choice "${default_choice}" "10")"
  if [[ "${choice}" != "0" && "${choice}" != "1" && "${installed}" != "true" ]]; then
    fail "尚未安装，请先选择 1。"
  fi

  case "${choice}" in
    0) success "已退出，未作任何修改。" ;;
    1) do_install ;;
    2) do_update ;;
    3) do_repair_install ;;
    4) ensure_docker; do_status ;;
    5) do_restart ;;
    6) ensure_docker; compose logs -f --tail=200 lanqin-email updater ;;
    7) do_certificate ;;
    8) ensure_docker; do_rollback ;;
    9) do_guide ;;
    10) ensure_docker; do_uninstall ;;
  esac
}

if [[ "${LANQIN_SOURCE_ONLY:-false}" == "true" ]]; then
  if [[ "${BASH_SOURCE[0]}" != "$0" ]]; then
    return 0
  fi
  exit 0
fi

if [[ "${EUID}" -eq 0 ]]; then
  ensure_cli_alias
fi

case "${COMMAND}" in
  help|-h|--help) usage ;;
  menu) require_root; require_curl; do_menu ;;
  install) require_root; require_curl; do_install ;;
  update) require_root; require_curl; do_update ;;
  status) require_root; require_curl; ensure_docker; do_status ;;
  logs) require_root; require_curl; ensure_docker; compose logs -f --tail=200 lanqin-email updater ;;
  restart) require_root; require_curl; do_restart ;;
  reload) require_root; require_curl; reload_services ;;
  certificate) require_root; require_curl; do_certificate ;;
  rollback) require_root; require_curl; ensure_docker; do_rollback ;;
  guide) require_root; require_curl; do_guide ;;
  uninstall) require_root; require_curl; ensure_docker; do_uninstall ;;
  *) usage; fail "未知命令：${COMMAND}" ;;
esac
