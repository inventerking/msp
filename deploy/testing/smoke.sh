#!/bin/sh
# Checks a running testing server from the outside. Usage: ./smoke.sh [https-port]
set -u
cd "$(dirname "$0")"
. ./.env
PORT="${1:-${HTTPS_PORT:-443}}"
APP="https://$APP_HOST:$PORT"
ADMIN="https://$ADMIN_HOST:$PORT"
C="curl -sk --max-time 10 --resolve $APP_HOST:$PORT:127.0.0.1 --resolve $ADMIN_HOST:$PORT:127.0.0.1"
fails=0
check() { # name expected actual
  if [ "$2" = "$3" ]; then echo "ok    $1"; else echo "FAIL  $1: expected $2, got $3"; fails=$((fails + 1)); fi
}
code() { $C -o /dev/null -w '%{http_code}' "$@"; }

# A route that is not proxied to the API falls through to the app's HTML page.
from_api() { # Prints yes if the response came from the API.
  if $C -o /dev/null -w '%{content_type}' "$1" | grep -q 'json'; then echo yes; else echo no; fi
}

check "client page loads" 200 "$(code "$APP/")"
check "admin page loads" 200 "$(code "$ADMIN/")"
check "client deep link reaches the app" 200 "$(code "$APP/team")"
check "plain http redirects to the public https port" "https://$APP_HOST:$PORT/" "$(curl -s --max-time 10 -o /dev/null -w '%{redirect_url}' -H "Host: $APP_HOST" "http://127.0.0.1:${HTTP_PORT:-80}/" | sed "s#https://$APP_HOST/#https://$APP_HOST:443/#")"
check "api answers on the client host" 401 "$(code "$APP/api/v1/me/context")"
check "admin api answers on the admin host" 401 "$(code "$ADMIN/api/v1/admin/me")"
check "health endpoint is not public" no "$(from_api "$APP/healthz")"
check "admin api is not on the client host" no "$(from_api "$APP/api/v1/admin/me")"
check "tenant api is not on the admin host" no "$(from_api "$ADMIN/api/v1/me/context")"
check "client page sends a CSP" 1 "$($C -I "$APP/" | grep -ci '^content-security-policy:')"
check "admin page sends a CSP" 1 "$($C -I "$ADMIN/" | grep -ci '^content-security-policy:')"
check "foreign origin is refused" 403 "$(code -X POST -H 'Content-Type: application/json' -H 'Origin: https://evil.example' -d '{"email":"a@b.co","password":"x"}' "$APP/api/v1/auth/login")"

# A full sign-up against the real stack. Email stays in Mailpit.
email="smoke-$(date +%s)@example.test"
check "registration is accepted" 202 "$(code -X POST -H 'Content-Type: application/json' -H "Origin: $APP" -d "{\"email\":\"$email\",\"password\":\"a-long-smoke-passphrase\",\"display_name\":\"Smoke\"}" "$APP/api/v1/auth/register")"
sleep 2
token=$(curl -s --max-time 10 "http://127.0.0.1:${MAILPIT_PORT:-58025}/api/v1/search?query=to:$email" | sed -n 's/.*"ID":"\([^"]*\)".*/\1/p' | head -1)
link=$(curl -s --max-time 10 "http://127.0.0.1:${MAILPIT_PORT:-58025}/api/v1/message/$token" | grep -o "verify-email?token=[A-Za-z0-9_-]*" | head -1 | cut -d= -f2)
check "verification email reached Mailpit" yes "$([ -n "$link" ] && echo yes || echo no)"
check "email verifies" 204 "$(code -X POST -H 'Content-Type: application/json' -H "Origin: $APP" -d "{\"token\":\"$link\"}" "$APP/api/v1/auth/verify-email")"
headers=$($C -D - -o /dev/null -X POST -H 'Content-Type: application/json' -H "Origin: $APP" -d "{\"email\":\"$email\",\"password\":\"a-long-smoke-passphrase\"}" "$APP/api/v1/auth/login")
check "session cookie is __Host- and Secure" 1 "$(echo "$headers" | grep -i '^set-cookie: __Host-session=' | grep -i 'httponly' | grep -i 'secure' | grep -ci 'samesite=lax')"
echo "$fails failed"
exit "$fails"
