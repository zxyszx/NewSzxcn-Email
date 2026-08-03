#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
export LANQIN_SOURCE_ONLY=true
# shellcheck source=install.sh
source "${ROOT_DIR}/install.sh"

fail_test() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

assert_eq() {
  local want="$1" got="$2" label="$3"
  [[ "${got}" == "${want}" ]] || fail_test "${label}: got '${got}', want '${want}'"
}

test_hostname_validation() {
  valid_hostname "mail.example.com" || fail_test "valid hostname rejected"
  valid_hostname "mx-1.example.co.uk" || fail_test "valid multi-label hostname rejected"
  ! valid_hostname "mail_example.com" || fail_test "hostname with underscore accepted"
  ! valid_hostname "localhost" || fail_test "single-label hostname accepted"
  ! valid_hostname "-mail.example.com" || fail_test "hostname with leading hyphen accepted"
}

test_password_validation() {
  LANQIN_ADMIN_PASSWORD="abc123"
  assert_eq "abc123" "$(prompt_admin_password)" "six-character password"
  if (LANQIN_ADMIN_PASSWORD="abc12" prompt_admin_password >/dev/null 2>&1); then
    fail_test "five-character password accepted"
  fi
  if (LANQIN_ADMIN_PASSWORD="abc\$123" prompt_admin_password >/dev/null 2>&1); then
    fail_test "unsafe env-file password accepted"
  fi
  if (LANQIN_ADMIN_PASSWORD="#abc123" prompt_admin_password >/dev/null 2>&1); then
    fail_test "password beginning with an env-file comment marker accepted"
  fi
  LANQIN_RESET_PASSWORD="reset1"
  assert_eq "reset1" "$(prompt_reset_password)" "six-character reset password"
  if (LANQIN_RESET_PASSWORD="reset" prompt_reset_password >/dev/null 2>&1); then
    fail_test "five-character reset password accepted"
  fi
}

test_install_configuration() {
  local firewall_mode="$1" web_mode="$2" want_bind="$3" want_url="$4" want_insecure="$5"
  local temp_dir
  temp_dir="$(mktemp -d)"
  cp "${ROOT_DIR}/deploy/.env.example" "${temp_dir}/.env.example"

  export INSTALL_DIR="${temp_dir}"
  export LANQIN_INSTALL_FIREWALL_MODE="${firewall_mode}"
  export LANQIN_PUBLIC_HOSTNAME="mail.example.com"
  export LANQIN_ADMIN_USERNAME="admin"
  export LANQIN_ADMIN_PASSWORD="abc123"
  export LANQIN_INSTALL_WEB_MODE="${web_mode}"
  configure_first_install
  configure_runtime_bindings

  assert_eq "${firewall_mode}" "$(env_value LANQIN_INSTALL_FIREWALL_MODE)" "firewall mode"
  assert_eq "${web_mode}" "$(env_value LANQIN_INSTALL_WEB_MODE)" "web mode"
  assert_eq "${want_bind}" "$(env_value LANQIN_HTTP_BIND)" "HTTP bind"
  assert_eq "${want_url}" "$(env_value LANQIN_PUBLIC_BASE_URL)" "public URL"
  assert_eq "${want_insecure}" "$(env_value LANQIN_ALLOW_INSECURE_HTTP)" "insecure HTTP flag"
  assert_eq "abc123" "$(env_value LANQIN_ADMIN_PASSWORD)" "administrator password"
}

test_nginx_configuration() {
  local temp_dir old_path
  temp_dir="$(mktemp -d)"
  old_path="${PATH}"
  mkdir -p "${temp_dir}/bin" "${temp_dir}/install" "${temp_dir}/certs" "${temp_dir}/acme"
  printf '#!/bin/sh\nexit 0\n' >"${temp_dir}/bin/nginx"
  printf '#!/bin/sh\nexit 0\n' >"${temp_dir}/bin/systemctl"
  chmod 0755 "${temp_dir}/bin/nginx" "${temp_dir}/bin/systemctl"
  cp "${ROOT_DIR}/deploy/.env.example" "${temp_dir}/install/.env"

  export PATH="${temp_dir}/bin:${PATH}"
  INSTALL_DIR="${temp_dir}/install"
  NGINX_CONFIG="${temp_dir}/newszxcn-email.conf"
  ACME_WEBROOT="${temp_dir}/acme"
  CERT_DIR="${temp_dir}/certs"
  set_env LANQIN_PUBLIC_HOSTNAME "mail.example.com"

  write_nginx_http_config
  grep -Fq 'proxy_pass http://127.0.0.1:8088;' "${NGINX_CONFIG}" || fail_test "HTTP proxy target missing"
  grep -Fq 'root '"${ACME_WEBROOT}"';' "${NGINX_CONFIG}" || fail_test "ACME webroot missing"

  write_nginx_https_config
  grep -Fq 'listen 443 ssl http2;' "${NGINX_CONFIG}" || fail_test "HTTPS listener missing"
  # shellcheck disable=SC2016
  grep -Fq 'return 301 https://$host$request_uri;' "${NGINX_CONFIG}" || fail_test "HTTPS redirect missing"
  grep -Fq "ssl_certificate ${CERT_DIR}/fullchain.pem;" "${NGINX_CONFIG}" || fail_test "certificate path missing"
  PATH="${old_path}"
}

test_compose_configuration() {
  # shellcheck disable=SC2016
  grep -Fq '${LANQIN_HTTP_BIND:-80}:80' "${ROOT_DIR}/deploy/docker-compose.yml" || fail_test "HTTP port mapping missing"
  ! grep -Fq 'LANQIN_HTTPS_BIND' "${ROOT_DIR}/deploy/docker-compose.yml" || fail_test "dead container HTTPS mapping remains"
  grep -Fq './certs:/certs:ro' "${ROOT_DIR}/deploy/docker-compose.yml" || fail_test "certificate mount missing"
}

test_legacy_configuration_is_preserved() {
  local temp_dir
  temp_dir="$(mktemp -d)"
  cp "${ROOT_DIR}/deploy/.env.example" "${temp_dir}/.env"
  export INSTALL_DIR="${temp_dir}"
  set_env LANQIN_INSTALL_WEB_MODE ""
  set_env LANQIN_HTTP_BIND "127.0.0.1:9090"
  configure_first_install
  configure_runtime_bindings
  assert_eq "127.0.0.1:9090" "$(env_value LANQIN_HTTP_BIND)" "legacy HTTP bind"
}

test_menu_choice() {
  export LANQIN_MENU_ACTION=0
  assert_eq "0" "$(prompt_menu_choice 1)" "menu exit action"
  export LANQIN_MENU_ACTION=1
  assert_eq "1" "$(prompt_menu_choice 2)" "menu install action"
  export LANQIN_MENU_ACTION=12
  assert_eq "12" "$(prompt_menu_choice 1 12)" "menu uninstall action"
  if (has_tty() { return 1; }; LANQIN_MENU_ACTION=13 prompt_menu_choice 1 12 >/dev/null 2>&1); then
    fail_test "out-of-range menu action accepted"
  fi
  unset LANQIN_MENU_ACTION
}

test_admin_credentials() (
  local temp_dir output
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/install"
  mkdir -p "${INSTALL_DIR}"
  cat > "${INSTALL_DIR}/.env" <<'EOF'
LANQIN_PUBLIC_BASE_URL=https://mail.example.com
LANQIN_ADMIN_USERNAME=admin
LANQIN_ADMIN_PASSWORD=recorded-password
EOF
  output="$(do_show_admin_credentials 2>&1)"
  [[ "${output}" == *'登录地址：https://mail.example.com'* ]] || fail_test "administrator login URL missing"
  [[ "${output}" == *'管理员用户名：admin'* ]] || fail_test "administrator username missing"
  [[ "${output}" == *'记录密码：recorded-password'* ]] || fail_test "recorded administrator password missing"
  [[ "${output}" == *'无法从数据库反向查看'* ]] || fail_test "password hash warning missing"
)

test_admin_password_hash_parsing() (
  compose() {
    # shellcheck disable=SC2016
    printf '{BLF-CRYPT}$2y$10$123456789012345678901u1234567890123456789012345678901\n'
  }
  # shellcheck disable=SC2016
  assert_eq '$2y$10$123456789012345678901u1234567890123456789012345678901' "$(generate_admin_password_hash 'unused')" "Dovecot bcrypt hash parsing"
)

test_admin_password_reset_only_updates_admin_account() (
  local temp_dir compose_calls backup_path
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/install"
  compose_calls="${temp_dir}/compose-calls"
  mkdir -p "${INSTALL_DIR}/data/backups"
  cat > "${INSTALL_DIR}/.env" <<'EOF'
LANQIN_ADMIN_USERNAME=admin
LANQIN_ADMIN_PASSWORD=old-password
EOF
  printf 'database\n' > "${INSTALL_DIR}/data/lanqin.db"

  ensure_docker() { return 0; }
  current_image_id() { printf 'sha256:test-image\n'; }
  backup_database() {
    backup_path="$1"
    printf 'backup\n' > "${backup_path}"
  }
  prompt_reset_password() { printf 'new-password'; }
  # shellcheck disable=SC2016
  generate_admin_password_hash() { printf '$2y$10$123456789012345678901u1234567890123456789012345678901'; }
  compose() {
    printf '%s\n' "$*" >> "${compose_calls}"
    if [[ "$*" == *'SELECT id FROM users'* ]]; then
      printf 'admin-user-id\n'
    elif [[ "$*" == *'UPDATE users SET password_hash'* ]]; then
      printf 'user=1\nmailboxes=2\n'
    fi
  }

  do_reset_admin_password >/dev/null
  assert_eq "new-password" "$(env_value LANQIN_ADMIN_PASSWORD)" "recorded reset password"
  [[ -s "${backup_path}" ]] || fail_test "password reset database backup missing"
  grep -Fq "login_name='admin' AND role='admin'" "${compose_calls}" || fail_test "administrator lookup is not role restricted"
  grep -Fq "UPDATE users SET password_hash=" "${compose_calls}" || fail_test "administrator user password was not updated"
  grep -Fq "UPDATE mailboxes SET password_hash=" "${compose_calls}" || fail_test "administrator mailbox passwords were not synchronized"
  grep -Fq "WHERE user_id='admin-user-id'" "${compose_calls}" || fail_test "mailbox password update is not restricted to the administrator"
)

test_offline_database_backup() (
  local temp_dir destination
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/install"
  mkdir -p "${INSTALL_DIR}/data/backups"
  sqlite3 "${INSTALL_DIR}/data/lanqin.db" 'CREATE TABLE test_items (id INTEGER PRIMARY KEY, value TEXT); INSERT INTO test_items(value) VALUES ("saved");'
  compose() { return 0; }
  destination="${INSTALL_DIR}/data/backups/offline.db"
  backup_database "${destination}" "unused-image"
  [[ -s "${destination}" ]] || fail_test "offline database backup missing"
  assert_eq "saved" "$(sqlite3 "${destination}" 'SELECT value FROM test_items LIMIT 1;')" "offline database content"
)

test_guide_generation() (
  local temp_dir
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/install"
  CERT_DIR="${INSTALL_DIR}/certs"
  GUIDE_FILE="${temp_dir}/guide.txt"
  mkdir -p "${CERT_DIR}"
  cp "${ROOT_DIR}/deploy/.env.example" "${INSTALL_DIR}/.env"
  set_env LANQIN_PUBLIC_HOSTNAME "mail.example.com"
  set_env LANQIN_PUBLIC_BASE_URL "https://mail.example.com"
  set_env LANQIN_ADMIN_USERNAME "admin"
  generate_guide
  grep -Fq '邮箱前台：https://mail.example.com' "${GUIDE_FILE}" || fail_test "guide frontend URL missing"
  grep -Fq '管理后台：https://mail.example.com/admin' "${GUIDE_FILE}" || fail_test "guide admin URL missing"
  grep -Fq '管理员密码：仅在安装完成时显示' "${GUIDE_FILE}" || fail_test "guide password safety text missing"
  [[ "$(stat -c '%a' "${GUIDE_FILE}" 2>/dev/null || stat -f '%Lp' "${GUIDE_FILE}")" == "600" ]] || fail_test "guide permissions are not 600"
)

test_acme_cron_detection() (
  crontab() {
    printf '49 0,6,12,18 * * * "/root/.acme.sh"/acme.sh --cron --home "/root/.acme.sh" > /dev/null\n'
  }
  acme_cron_enabled || fail_test "quoted acme.sh Cron entry was not detected"
)

test_cli_alias_safety() (
  local temp_dir
  temp_dir="$(mktemp -d)"
  CLI_PATH="${temp_dir}/newszxcn-email"
  CLI_ALIAS_PATH="${temp_dir}/ns"
  printf '#!/bin/sh\nexit 0\n' > "${CLI_PATH}"
  chmod 0755 "${CLI_PATH}"
  ensure_cli_alias
  [[ -L "${CLI_ALIAS_PATH}" ]] || fail_test "ns alias was not created"
  assert_eq "${CLI_PATH}" "$(readlink "${CLI_ALIAS_PATH}")" "ns alias target"
  rm -f "${CLI_ALIAS_PATH}"
  printf 'occupied\n' > "${CLI_ALIAS_PATH}"
  ensure_cli_alias
  grep -Fq 'occupied' "${CLI_ALIAS_PATH}" || fail_test "existing ns command was overwritten"
)

test_compose_runtime_image_pin() (
  local temp_dir calls
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/install"
  RUNTIME_IMAGE_PIN="${INSTALL_DIR}/.rollback-runtime-image"
  calls="${temp_dir}/docker-calls"
  mkdir -p "${INSTALL_DIR}"
  printf 'services: {}\n' > "${INSTALL_DIR}/docker-compose.yml"
  printf 'sha256:rollback-image\n' > "${RUNTIME_IMAGE_PIN}"
  docker() {
    printf '%s|%s\n' "${LANQIN_IMAGE:-}" "$*" >> "${calls}"
  }

  compose ps
  grep -Fq 'sha256:rollback-image|compose ' "${calls}" || fail_test "rollback image pin was not applied to Compose"
  clear_runtime_image_pin
  compose ps
  [[ "$(tail -n 1 "${calls}" | cut -d '|' -f 1)" == "" ]] || fail_test "cleared image pin still affected Compose"
)

test_update_snapshot_restore() (
  local temp_dir snapshot
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/install"
  CERT_DIR="${INSTALL_DIR}/certs"
  NGINX_CONFIG="${temp_dir}/newszxcn-email.conf"
  CLI_PATH="${temp_dir}/newszxcn-email-cli"
  CLI_ALIAS_PATH="${temp_dir}/ns"
  ROLLBACK_FILE="${INSTALL_DIR}/.rollback-image"
  ROLLBACK_POINTER="${INSTALL_DIR}/.rollback-manifest"
  RUNTIME_IMAGE_PIN="${INSTALL_DIR}/.rollback-runtime-image"
  mkdir -p "${INSTALL_DIR}/data/backups" "${CERT_DIR}"
  printf 'old-compose\n' > "${INSTALL_DIR}/docker-compose.yml"
  printf 'LANQIN_IMAGE=ghcr.io/example/mail:latest\nOLD_ENV=yes\n' > "${INSTALL_DIR}/.env"
  printf 'old-example\n' > "${INSTALL_DIR}/.env.example"
  printf '#!/bin/sh\necho old-installer\n' > "${CLI_PATH}"
  chmod 0755 "${CLI_PATH}"
  printf 'old-nginx\n' > "${NGINX_CONFIG}"
  printf 'old-certificate\n' > "${CERT_DIR}/fullchain.pem"
  sqlite3 "${INSTALL_DIR}/data/lanqin.db" 'CREATE TABLE test_items (value TEXT); INSERT INTO test_items VALUES ("before-update");'

  current_image_id() { printf 'sha256:old-image\n'; }
  docker() {
    if [[ "$*" == *'org.opencontainers.image.version'* ]]; then
      printf '1.2.4\n'
    fi
    return 0
  }
  compose() {
    if [[ "${1:-}" == "up" ]]; then
      grep -Fq 'sha256:old-image' "${RUNTIME_IMAGE_PIN}" || fail_test "restore started without image pin"
    fi
    return 0
  }
  nginx() { return 0; }
  systemctl() { return 0; }
  wait_for_health() { return 0; }
  ensure_cli_alias() { return 0; }

  create_update_snapshot
  snapshot="$(tr -d '\r\n' < "${ROLLBACK_POINTER}")"
  [[ -s "${snapshot}/rollback-manifest.json" ]] || fail_test "rollback manifest missing"

  printf 'new-compose\n' > "${INSTALL_DIR}/docker-compose.yml"
  printf 'NEW_ENV=yes\n' > "${INSTALL_DIR}/.env"
  printf 'new-example\n' > "${INSTALL_DIR}/.env.example"
  printf '#!/bin/sh\necho new-installer\n' > "${CLI_PATH}"
  printf 'new-nginx\n' > "${NGINX_CONFIG}"
  printf 'new-certificate\n' > "${CERT_DIR}/fullchain.pem"
  sqlite3 "${INSTALL_DIR}/data/lanqin.db" 'DELETE FROM test_items; INSERT INTO test_items VALUES ("after-update");'

  restore_update_snapshot "${snapshot}"
  grep -Fq 'old-compose' "${INSTALL_DIR}/docker-compose.yml" || fail_test "Compose file was not restored"
  grep -Fq 'OLD_ENV=yes' "${INSTALL_DIR}/.env" || fail_test "environment file was not restored"
  grep -Fq 'old-example' "${INSTALL_DIR}/.env.example" || fail_test "environment example was not restored"
  grep -Fq 'old-installer' "${CLI_PATH}" || fail_test "installer was not restored"
  grep -Fq 'old-nginx' "${NGINX_CONFIG}" || fail_test "Nginx configuration was not restored"
  grep -Fq 'old-certificate' "${CERT_DIR}/fullchain.pem" || fail_test "certificate was not restored"
  assert_eq "before-update" "$(sqlite3 "${INSTALL_DIR}/data/lanqin.db" 'SELECT value FROM test_items;')" "restored database content"
  assert_eq "sha256:old-image" "$(tr -d '\r\n' < "${RUNTIME_IMAGE_PIN}")" "restored runtime image pin"
)

test_snapshot_restores_absent_optional_files() (
  local temp_dir snapshot
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/install"
  CERT_DIR="${INSTALL_DIR}/certs"
  NGINX_CONFIG="${temp_dir}/newszxcn-email.conf"
  CLI_PATH="${temp_dir}/newszxcn-email-cli"
  CLI_ALIAS_PATH="${temp_dir}/ns"
  ROLLBACK_FILE="${INSTALL_DIR}/.rollback-image"
  ROLLBACK_POINTER="${INSTALL_DIR}/.rollback-manifest"
  RUNTIME_IMAGE_PIN="${INSTALL_DIR}/.rollback-runtime-image"
  mkdir -p "${INSTALL_DIR}/data/backups"
  printf 'services: {}\n' > "${INSTALL_DIR}/docker-compose.yml"
  printf 'LANQIN_IMAGE=ghcr.io/example/mail:latest\n' > "${INSTALL_DIR}/.env"
  sqlite3 "${INSTALL_DIR}/data/lanqin.db" 'CREATE TABLE test_items (value TEXT); INSERT INTO test_items VALUES ("saved");'

  current_image_id() { printf 'sha256:old-image\n'; }
  docker() { return 0; }
  compose() { return 0; }
  nginx() { return 0; }
  systemctl() { return 0; }
  wait_for_health() { return 0; }
  ensure_cli_alias() { return 0; }

  create_update_snapshot
  snapshot="$(tr -d '\r\n' < "${ROLLBACK_POINTER}")"
  [[ -f "${snapshot}/env-example.absent" ]] || fail_test "missing env example marker"
  [[ -f "${snapshot}/installer.absent" ]] || fail_test "missing installer marker"
  [[ -f "${snapshot}/nginx.absent" ]] || fail_test "missing Nginx marker"
  [[ -f "${snapshot}/certs.absent" ]] || fail_test "missing certificate marker"

  mkdir -p "${CERT_DIR}"
  printf 'new-example\n' > "${INSTALL_DIR}/.env.example"
  printf '#!/bin/sh\n' > "${CLI_PATH}"
  printf 'new-nginx\n' > "${NGINX_CONFIG}"
  printf 'new-certificate\n' > "${CERT_DIR}/fullchain.pem"
  restore_update_snapshot "${snapshot}"
  [[ ! -e "${INSTALL_DIR}/.env.example" ]] || fail_test "new env example survived rollback"
  [[ ! -e "${CLI_PATH}" ]] || fail_test "new installer survived rollback"
  [[ ! -e "${NGINX_CONFIG}" ]] || fail_test "new Nginx configuration survived rollback"
  [[ ! -e "${CERT_DIR}" ]] || fail_test "new certificate directory survived rollback"
)

test_pre_start_restore_preserves_current_database() (
  local temp_dir snapshot
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/install"
  CERT_DIR="${INSTALL_DIR}/certs"
  NGINX_CONFIG="${temp_dir}/newszxcn-email.conf"
  CLI_PATH="${temp_dir}/newszxcn-email-cli"
  CLI_ALIAS_PATH="${temp_dir}/ns"
  ROLLBACK_FILE="${INSTALL_DIR}/.rollback-image"
  ROLLBACK_POINTER="${INSTALL_DIR}/.rollback-manifest"
  RUNTIME_IMAGE_PIN="${INSTALL_DIR}/.rollback-runtime-image"
  mkdir -p "${INSTALL_DIR}/data/backups"
  printf 'services: {}\n' > "${INSTALL_DIR}/docker-compose.yml"
  printf 'LANQIN_IMAGE=ghcr.io/example/mail:latest\n' > "${INSTALL_DIR}/.env"
  sqlite3 "${INSTALL_DIR}/data/lanqin.db" 'CREATE TABLE received_mail (subject TEXT); INSERT INTO received_mail VALUES ("before-snapshot");'

  current_image_id() { printf 'sha256:old-image\n'; }
  docker() { return 0; }
  compose() { return 0; }
  reload_nginx() { return 0; }
  wait_for_health() { return 0; }
  ensure_cli_alias() { return 0; }

  create_update_snapshot
  snapshot="$(tr -d '\r\n' < "${ROLLBACK_POINTER}")"
  sqlite3 "${INSTALL_DIR}/data/lanqin.db" 'INSERT INTO received_mail VALUES ("received-during-pull");'
  restore_update_snapshot "${snapshot}" false
  assert_eq "2" "$(sqlite3 "${INSTALL_DIR}/data/lanqin.db" 'SELECT COUNT(*) FROM received_mail;')" "database preserved before new container start"
  assert_eq "received-during-pull" "$(sqlite3 "${INSTALL_DIR}/data/lanqin.db" 'SELECT subject FROM received_mail ORDER BY rowid DESC LIMIT 1;')" "mail received during pull"
)

test_failed_asset_validation_preserves_production() (
  local temp_dir source_dir
  temp_dir="$(mktemp -d)"
  source_dir="${temp_dir}/source"
  INSTALL_DIR="${temp_dir}/install"
  CLI_PATH="${temp_dir}/newszxcn-email-cli"
  RUNTIME_IMAGE_PIN="${INSTALL_DIR}/.rollback-runtime-image"
  mkdir -p "${source_dir}/deploy" "${INSTALL_DIR}"
  printf 'old-compose\n' > "${INSTALL_DIR}/docker-compose.yml"
  printf 'OLD_ENV=yes\n' > "${INSTALL_DIR}/.env"
  printf 'old-example\n' > "${INSTALL_DIR}/.env.example"
  printf '#!/bin/sh\necho old-installer\n' > "${CLI_PATH}"
  printf 'sha256:pinned-image\n' > "${RUNTIME_IMAGE_PIN}"
  printf 'invalid compose\n' > "${source_dir}/deploy/docker-compose.yml"
  cp "${ROOT_DIR}/deploy/.env.example" "${source_dir}/deploy/.env.example"
  cp "${ROOT_DIR}/install.sh" "${source_dir}/install.sh"

  script_dir() { printf '%s\n' "${source_dir}"; }
  docker() { return 1; }
  if (stage_assets >/dev/null 2>&1); then
    fail_test "invalid Compose file passed staging validation"
  fi
  grep -Fq 'old-compose' "${INSTALL_DIR}/docker-compose.yml" || fail_test "production Compose changed after failed validation"
  grep -Fq 'old-example' "${INSTALL_DIR}/.env.example" || fail_test "production env example changed after failed validation"
  grep -Fq 'old-installer' "${CLI_PATH}" || fail_test "production installer changed after failed validation"
  grep -Fq 'sha256:pinned-image' "${RUNTIME_IMAGE_PIN}" || fail_test "runtime image pin changed after failed validation"
)

test_backup_reinstall_restores_on_failure() (
  local temp_dir failed_dir
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/newszxcn-email"
  NGINX_CONFIG="${temp_dir}/newszxcn-email.conf"
  CLI_PATH="${temp_dir}/newszxcn-email-cli"
  CLI_ALIAS_PATH="${temp_dir}/ns"
  mkdir -p "${INSTALL_DIR}"
  printf 'existing-data\n' > "${INSTALL_DIR}/marker"
  printf 'services: {}\n' > "${INSTALL_DIR}/docker-compose.yml"
  printf 'old-nginx\n' > "${NGINX_CONFIG}"
  printf '#!/bin/sh\nexit 0\n' > "${CLI_PATH}"
  chmod 0755 "${CLI_PATH}"

  ensure_docker() { return 0; }
  current_image_id() { printf 'sha256:old-image\n'; }
  compose() { return 0; }
  nginx() { return 0; }
  systemctl() { return 0; }
  wait_for_health() { return 0; }
  ensure_cli_alias() { return 0; }
  do_install() {
    mkdir -p "${INSTALL_DIR}"
    printf 'failed-install\n' > "${INSTALL_DIR}/failed-marker"
    return 1
  }

  if (do_backup_reinstall); then
    fail_test "failed reinstall unexpectedly succeeded"
  fi
  grep -Fq 'existing-data' "${INSTALL_DIR}/marker" || fail_test "old install directory was not restored"
  grep -Fq 'old-nginx' "${NGINX_CONFIG}" || fail_test "old Nginx configuration was not restored"
  failed_dir="$(find "${temp_dir}" -maxdepth 1 -type d -name 'newszxcn-email.failed-*' -print -quit)"
  [[ -n "${failed_dir}" ]] || fail_test "failed reinstall directory was not preserved"
)

test_backup_reinstall_recovers_from_nginx_reload_failure() (
  local temp_dir compose_calls reload_count_file
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/newszxcn-email"
  NGINX_CONFIG="${temp_dir}/newszxcn-email.conf"
  CLI_PATH="${temp_dir}/newszxcn-email-cli"
  CLI_ALIAS_PATH="${temp_dir}/ns"
  compose_calls="${temp_dir}/compose-calls"
  reload_count_file="${temp_dir}/reload-count"
  mkdir -p "${INSTALL_DIR}"
  printf 'existing-data\n' > "${INSTALL_DIR}/marker"
  printf 'services: {}\n' > "${INSTALL_DIR}/docker-compose.yml"
  printf 'old-nginx\n' > "${NGINX_CONFIG}"
  printf '0\n' > "${reload_count_file}"

  ensure_docker() { return 0; }
  current_image_id() { printf 'sha256:old-image\n'; }
  compose() { printf '%s\n' "$*" >> "${compose_calls}"; return 0; }
  reload_nginx() {
    local count
    count="$(cat "${reload_count_file}")"
    printf '%s\n' "$((count + 1))" > "${reload_count_file}"
    [[ "${count}" -gt 0 ]]
  }
  wait_for_health() { return 0; }
  do_install() { fail_test "fresh install started after Nginx reload failure"; }

  if (do_backup_reinstall >/dev/null 2>&1); then
    fail_test "reinstall continued after Nginx reload failure"
  fi
  grep -Fq 'existing-data' "${INSTALL_DIR}/marker" || fail_test "old install changed after Nginx reload failure"
  grep -Fq 'old-nginx' "${NGINX_CONFIG}" || fail_test "Nginx configuration was not restored after reload failure"
  grep -Fq 'up -d --remove-orphans --force-recreate' "${compose_calls}" || fail_test "old containers were not restarted after Nginx reload failure"
)

test_hostname_validation
test_password_validation
test_install_configuration 1 1 "127.0.0.1:8088" "https://mail.example.com" "false"
test_install_configuration 2 2 "127.0.0.1:8088" "https://mail.example.com" "false"
test_nginx_configuration
test_compose_configuration
test_legacy_configuration_is_preserved
test_menu_choice
test_admin_credentials
test_admin_password_hash_parsing
test_admin_password_reset_only_updates_admin_account
test_offline_database_backup
test_guide_generation
test_acme_cron_detection
test_cli_alias_safety
test_compose_runtime_image_pin
test_update_snapshot_restore
test_snapshot_restores_absent_optional_files
test_pre_start_restore_preserves_current_database
test_failed_asset_validation_preserves_production
test_backup_reinstall_restores_on_failure
test_backup_reinstall_recovers_from_nginx_reload_failure

printf 'install.sh tests passed\n'
