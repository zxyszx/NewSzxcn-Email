#!/bin/sh
set -eu

: "${LANQIN_DB_PATH:=/data/lanqin.db}"
: "${LANQIN_RSPAMD_DKIM_DIR:=/var/lib/rspamd/dkim}"
: "${LANQIN_RSPAMD_DKIM_SYNC_SECONDS:=60}"

chown_dkim_dir() {
  if id _rspamd >/dev/null 2>&1; then
    chown -R _rspamd:_rspamd "$LANQIN_RSPAMD_DKIM_DIR" 2>/dev/null || true
  elif id rspamd >/dev/null 2>&1; then
    chown -R rspamd:rspamd "$LANQIN_RSPAMD_DKIM_DIR" 2>/dev/null || true
  fi
}

reload_rspamd() {
  if command -v rspamadm >/dev/null 2>&1 && rspamadm control reload >/dev/null 2>&1; then
    echo "Rspamd reloaded after DKIM key update"
    return 0
  fi
  if command -v pkill >/dev/null 2>&1 && pkill -HUP -x rspamd 2>/dev/null; then
    echo "Rspamd reloaded after DKIM key update"
  fi
}

sync_keys() {
  changed_marker="$LANQIN_RSPAMD_DKIM_DIR/.reload-required.$$"
  mkdir -p "$LANQIN_RSPAMD_DKIM_DIR"
  rm -f "$changed_marker"
  if [ ! -f "$LANQIN_DB_PATH" ]; then
    chown_dkim_dir
    return 0
  fi

  sqlite3 -separator '|' "$LANQIN_DB_PATH" "SELECT name, dkim_selector, dkim_private_key FROM domains WHERE status='active';" 2>/dev/null | while IFS='|' read -r domain selector private_key; do
    [ -n "$domain" ] || continue
    [ -n "$selector" ] || selector="lanqin"
    keyfile="$LANQIN_RSPAMD_DKIM_DIR/${domain}.${selector}.key"
    tmpfile="${keyfile}.tmp.$$"
    printf '%s' "$private_key" | base64 -d > "$tmpfile"
    chmod 0640 "$tmpfile"
    if [ -f "$keyfile" ] && cmp -s "$tmpfile" "$keyfile"; then
      rm -f "$tmpfile"
    else
      mv "$tmpfile" "$keyfile"
      : > "$changed_marker"
    fi
  done

  chown_dkim_dir
  if [ -f "$changed_marker" ]; then
    rm -f "$changed_marker"
    reload_rspamd
  fi
}

if [ "${1:-}" = "--once" ]; then
  sync_keys
  exit 0
fi

while true; do
  sync_keys || true
  sleep "$LANQIN_RSPAMD_DKIM_SYNC_SECONDS"
done
