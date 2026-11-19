#!/bin/sh
# Fills .env with everything compose.yaml needs. Values already in .env are never changed, so it
# is safe to run again, for example after an update adds a new setting.
# Usage: ./init-env.sh [app-host] [admin-host]
set -eu
cd "$(dirname "$0")"
umask 077
touch .env

added=""
add() { # name value
  if ! grep -q "^$1=" .env; then
    printf '%s=%s\n' "$1" "$2" >> .env
    added="$added $1"
  fi
}

add APP_HOST "${1:-msp.hiksoda.com}"
add ADMIN_HOST "${2:-mspadmin.hiksoda.com}"
add POSTGRES_PASSWORD "$(openssl rand -hex 24)"
add DB_OWNER_PASSWORD "$(openssl rand -hex 24)"
add DB_APP_PASSWORD "$(openssl rand -hex 24)"
add DB_PLATFORM_PASSWORD "$(openssl rand -hex 24)"
add DB_SYSTEM_PASSWORD "$(openssl rand -hex 24)"
add ENCRYPTION_KEY "$(openssl rand -base64 32)"
add HMAC_KEY "$(openssl rand -base64 32)"
add S3_ACCESS_KEY "$(openssl rand -hex 12)"
add S3_SECRET_KEY "$(openssl rand -hex 24)"
add BACKUP_CIPHER_PASS "$(openssl rand -base64 36)"

if [ -n "$added" ]; then
  echo "Added to .env:$added"
else
  echo ".env already has every setting"
fi
case "$added" in *BACKUP_CIPHER_PASS*)
  echo "Copy BACKUP_CIPHER_PASS, ENCRYPTION_KEY and HMAC_KEY from .env somewhere off this server."
  echo "Without them, backups and stored secrets cannot be read." ;;
esac
