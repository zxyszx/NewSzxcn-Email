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
  export LANQIN_MENU_ACTION=9
  assert_eq "9" "$(prompt_menu_choice 1)" "menu uninstall action"
  unset LANQIN_MENU_ACTION
}

test_backup_reinstall_preserves_existing_directory() (
  local temp_dir backup_dir
  temp_dir="$(mktemp -d)"
  INSTALL_DIR="${temp_dir}/newszxcn-email"
  NGINX_CONFIG="${temp_dir}/newszxcn-email.conf"
  mkdir -p "${INSTALL_DIR}"
  printf 'existing-data\n' > "${INSTALL_DIR}/marker"

  do_install() {
    [[ ! -e "${INSTALL_DIR}" ]] || fail_test "fresh install started before old directory was moved"
  }

  do_backup_reinstall
  backup_dir="$(find "${temp_dir}" -maxdepth 1 -type d -name 'newszxcn-email.backup-*' -print -quit)"
  [[ -n "${backup_dir}" ]] || fail_test "existing install backup directory missing"
  grep -Fq 'existing-data' "${backup_dir}/marker" || fail_test "existing install data was not preserved"
)

test_hostname_validation
test_password_validation
test_install_configuration 1 1 "127.0.0.1:8088" "https://mail.example.com" "false"
test_install_configuration 2 2 "127.0.0.1:8088" "https://mail.example.com" "false"
test_install_configuration 3 3 "80" "http://mail.example.com" "true"
test_nginx_configuration
test_compose_configuration
test_legacy_configuration_is_preserved
test_menu_choice
test_backup_reinstall_preserves_existing_directory

printf 'install.sh tests passed\n'
