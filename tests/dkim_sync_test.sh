#!/usr/bin/env bash
set -Eeuo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
TEMP_DIR="$(mktemp -d)"
trap 'rm -rf "${TEMP_DIR}"' EXIT

fail_test() {
  printf 'FAIL: %s\n' "$*" >&2
  exit 1
}

mkdir -p "${TEMP_DIR}/bin" "${TEMP_DIR}/keys"
touch "${TEMP_DIR}/lanqin.db" "${TEMP_DIR}/reload.log"

cat > "${TEMP_DIR}/bin/sqlite3" <<'EOF'
#!/bin/sh
printf 'example.com|lanqin|%s\n' "$(cat "${FAKE_PRIVATE_KEY_FILE}")"
EOF
cat > "${TEMP_DIR}/bin/id" <<'EOF'
#!/bin/sh
exit 1
EOF
cat > "${TEMP_DIR}/bin/rspamadm" <<'EOF'
#!/bin/sh
printf '%s\n' "$*" >> "${FAKE_RELOAD_LOG}"
EOF
cat > "${TEMP_DIR}/bin/pkill" <<'EOF'
#!/bin/sh
printf 'unexpected pkill fallback\n' >&2
exit 1
EOF
chmod 0755 "${TEMP_DIR}/bin/sqlite3" "${TEMP_DIR}/bin/id" "${TEMP_DIR}/bin/rspamadm" "${TEMP_DIR}/bin/pkill"

export PATH="${TEMP_DIR}/bin:${PATH}"
export LANQIN_DB_PATH="${TEMP_DIR}/lanqin.db"
export LANQIN_RSPAMD_DKIM_DIR="${TEMP_DIR}/keys"
export FAKE_PRIVATE_KEY_FILE="${TEMP_DIR}/private-key.b64"
export FAKE_RELOAD_LOG="${TEMP_DIR}/reload.log"

printf 'first-private-key' | base64 > "${FAKE_PRIVATE_KEY_FILE}"
sh "${ROOT_DIR}/deploy/rspamd/sync-dkim.sh" --once
[[ "$(cat "${TEMP_DIR}/keys/example.com.lanqin.key")" == "first-private-key" ]] || fail_test "initial DKIM key was not exported"
[[ "$(wc -l < "${FAKE_RELOAD_LOG}" | tr -d ' ')" == "1" ]] || fail_test "initial DKIM key did not reload Rspamd"

sh "${ROOT_DIR}/deploy/rspamd/sync-dkim.sh" --once
[[ "$(wc -l < "${FAKE_RELOAD_LOG}" | tr -d ' ')" == "1" ]] || fail_test "unchanged DKIM key reloaded Rspamd"

printf 'second-private-key' | base64 > "${FAKE_PRIVATE_KEY_FILE}"
sh "${ROOT_DIR}/deploy/rspamd/sync-dkim.sh" --once
[[ "$(cat "${TEMP_DIR}/keys/example.com.lanqin.key")" == "second-private-key" ]] || fail_test "changed DKIM key was not exported"
[[ "$(wc -l < "${FAKE_RELOAD_LOG}" | tr -d ' ')" == "2" ]] || fail_test "changed DKIM key did not reload Rspamd"

printf 'DKIM sync tests passed.\n'
