#!/usr/bin/env bash
# Fills a running API with sample students, so paging and search have something to show.
#
#   ./scripts/seed.sh            # 60 students
#   ./scripts/seed.sh 200        # 200 students (make seed N=200 does the same)
#
# It signs in as an administrator (creating the account if it does not exist), so
# SEED_EMAIL must be one of the ADMIN_EMAILS the API was started with. Defaults match
# .env.example. Safe to run again: students whose email already exists are skipped.
set -euo pipefail

COUNT="${1:-60}"
API_URL="${API_URL:-http://localhost:8080}"
SEED_EMAIL="${SEED_EMAIL:-admin@campus.edu}"
SEED_PASSWORD="${SEED_PASSWORD:-campus-demo-pass}"

if ! [[ "$COUNT" =~ ^[0-9]+$ ]] || [ "$COUNT" -lt 1 ]; then
  echo "usage: $0 [count]   (count must be a positive whole number)" >&2
  exit 2
fi

# Create the account if needed (409 = it exists already, which is fine).
status=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API_URL/api/register" \
  -H 'Content-Type: application/json' \
  -d "{\"email\":\"$SEED_EMAIL\",\"password\":\"$SEED_PASSWORD\"}") || {
  echo "cannot reach the API at $API_URL; is it running? (docker compose up)" >&2
  exit 1
}
case "$status" in
  201|409) ;;
  403) echo "registration is closed and $SEED_EMAIL is not in ADMIN_EMAILS" >&2; exit 1 ;;
  *) echo "registering $SEED_EMAIL failed with HTTP $status" >&2; exit 1 ;;
esac

login=$(curl -s -X POST "$API_URL/api/login" -H 'Content-Type: application/json' \
  -d "{\"email\":\"$SEED_EMAIL\",\"password\":\"$SEED_PASSWORD\"}")
token=$(printf '%s' "$login" | sed -n 's/.*"token":"\([^"]*\)".*/\1/p')
role=$(printf '%s' "$login" | sed -n 's/.*"role":"\([^"]*\)".*/\1/p')
if [ -z "$token" ]; then
  echo "could not sign in as $SEED_EMAIL (wrong SEED_PASSWORD for an existing account?): $login" >&2
  exit 1
fi
if [ "$role" != "admin" ]; then
  echo "$SEED_EMAIL signed in as '$role', but only admins can add students." >&2
  echo "Add it to ADMIN_EMAILS in .env and restart the API (docker compose up -d --force-recreate api)." >&2
  exit 1
fi

first=(Amara Daniel Sofia Liam Noor Mateo Hana Omar Chloe Kofi Yuki Elena Samuel Layla Tariq Ingrid Mei Andre Zainab Lucas)
last=(Okafor Larsen Haddad Nguyen Rossi Bekele Tanaka Mensah Petrov Silva Kaya Adeyemi Jensen Moreau Ibrahim Costa Walker Singh Abara Keller)
major=("Computer Science" Mathematics Physics Biology Chemistry Economics "Civil Engineering" Architecture Psychology "Data Science")

created=0 skipped=0
for ((i = 0; i < COUNT; i++)); do
  f="${first[$((i % ${#first[@]}))]}"
  l="${last[$(((i / ${#first[@]} + i) % ${#last[@]}))]}"
  m="${major[$(((i * 7 + i / 3) % ${#major[@]}))]}"
  email="$(printf '%s.%s.%d' "$f" "$l" "$((i + 1))" | tr '[:upper:]' '[:lower:]')@campus.edu"

  code=$(curl -s -o /dev/null -w '%{http_code}' -X POST "$API_URL/api/students" \
    -H "Authorization: Bearer $token" -H 'Content-Type: application/json' \
    -d "{\"name\":\"$f $l\",\"email\":\"$email\",\"major\":\"$m\"}")
  case "$code" in
    201) created=$((created + 1)) ;;
    409) skipped=$((skipped + 1)) ;;
    *) echo "adding $email failed with HTTP $code" >&2; exit 1 ;;
  esac
done

echo "done: $created added, $skipped already existed ($API_URL)"
