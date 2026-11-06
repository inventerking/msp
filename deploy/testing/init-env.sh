#!/bin/sh
# Creates deploy/testing/.env with freshly generated secrets. It never overwrites an existing file.
set -eu
cd "$(dirname "$0")"
if [ -f .env ]; then
  echo ".env already exists; leaving it unchanged" >&2
  exit 1
fi
APP_HOST="${1:-app.localhost}"
ADMIN_HOST="${2:-admin.localhost}"
umask 077
cat > .env <<ENV
APP_HOST=$APP_HOST
ADMIN_HOST=$ADMIN_HOST
# Leave empty for a self-signed certificate. Set it to use Let's Encrypt with compose.acme.yaml.
ACME_EMAIL=
HTTP_PORT=80
HTTPS_PORT=443
# Set to :PORT only when HTTPS_PORT is not 443, for example :58443.
PUBLIC_PORT_SUFFIX=
POSTGRES_PASSWORD=$(openssl rand -hex 24)
DB_OWNER_PASSWORD=$(openssl rand -hex 24)
DB_APP_PASSWORD=$(openssl rand -hex 24)
DB_PLATFORM_PASSWORD=$(openssl rand -hex 24)
DB_SYSTEM_PASSWORD=$(openssl rand -hex 24)
ENCRYPTION_KEY=$(openssl rand -base64 32)
HMAC_KEY=$(openssl rand -base64 32)
ENV
echo "Created $(pwd)/.env for $APP_HOST and $ADMIN_HOST"
