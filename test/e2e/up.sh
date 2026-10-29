#!/usr/bin/env bash
# Start the disposable local BookStack and write test/e2e/.env.local with
# BOOKSTACK_URL, BOOKSTACK_TOKEN_ID and BOOKSTACK_TOKEN_SECRET for bsk.
# Re-running reuses the stack and issues a fresh API token.
set -euo pipefail

cd "$(dirname "$0")"
umask 077

STATE=.state.local   # generated stack secrets (git-ignored)
OUT=.env.local       # bsk connection variables (git-ignored)

if [[ ! -f $STATE ]]; then
  {
    printf 'BSK_E2E_DB_PASSWORD=%s\n' "$(head -c 24 /dev/urandom | base64 | tr -dc 'A-Za-z0-9')"
    printf 'BSK_E2E_APP_KEY=base64:%s\n' "$(head -c 32 /dev/urandom | base64)"
  } > "$STATE"
fi
set -a
# shellcheck disable=SC1090
source "$STATE"
set +a
port=${BSK_E2E_PORT:-6875}
url="http://127.0.0.1:$port"

docker compose up -d --wait db
docker compose up -d bookstack

printf 'Waiting for BookStack at %s ' "$url"
for _ in $(seq 1 90); do
  if [[ $(curl -s -o /dev/null -w '%{http_code}' "$url/login") == 200 ]]; then
    echo ok
    break
  fi
  printf .
  sleep 2
done
if [[ $(curl -s -o /dev/null -w '%{http_code}' "$url/login") != 200 ]]; then
  echo " timed out" >&2
  docker compose logs --tail 50 bookstack >&2
  exit 1
fi

# Issue an API token for the sandbox admin (admin@admin.com, which has the
# "Access System API" permission by default). Only this sandbox is affected.
token=$(docker compose exec -T bookstack php /app/www/artisan tinker --execute='
$user = \BookStack\Users\Models\User::where("email", "admin@admin.com")->firstOrFail();
$id = \Illuminate\Support\Str::random(32);
$secret = \Illuminate\Support\Str::random(32);
$token = new \BookStack\Api\ApiToken();
$token->user_id = $user->id;
$token->name = "bsk-e2e " . now()->toDateTimeString();
$token->token_id = $id;
$token->secret = \Illuminate\Support\Facades\Hash::make($secret);
$token->expires_at = now()->addYear();
$token->save();
echo $id . ":" . $secret;
' | tail -n 1 | tr -d '\r')

if [[ ! $token =~ ^[A-Za-z0-9]{32}:[A-Za-z0-9]{32}$ ]]; then
  echo "could not create an API token" >&2
  exit 1
fi

{
  printf 'export BOOKSTACK_URL=%s\n' "$url"
  printf 'export BOOKSTACK_TOKEN_ID=%s\n' "${token%%:*}"
  printf 'export BOOKSTACK_TOKEN_SECRET=%s\n' "${token#*:}"
} > "$OUT"
echo "Sandbox ready. Load it with: source test/e2e/$OUT"
