#!/usr/bin/env bash
#
# End-to-end smoke test against a running API (start it first with `make up`).
#
# Every run registers fresh users, so it can be repeated against the same
# database. Checks that need direct access to PostgreSQL or to the API
# container logs are skipped when those containers are not reachable
# (e.g. with `make up-external-db`).
#
# Rate limiting of /auth is not exercised here: tripping it would lock the
# endpoints for everyone for a minute. A run sends about 19 /auth requests,
# within the default AUTH_RATE_LIMIT of 60 per minute.
#
# Requirements: bash, curl, python3. Optional: docker.
#
# Environment:
#   BASE_URL            API base URL              (default http://localhost:8080)
#   PG_CONTAINER        PostgreSQL container name (default task-management-postgres)
#   API_CONTAINER       API container name        (default task-management-api)
#   DB_USER / DB_NAME   database credentials      (default postgres / task_management)

set -uo pipefail

BASE=${BASE_URL:-http://localhost:8080}
API=$BASE/api/v1
PG_CONTAINER=${PG_CONTAINER:-task-management-postgres}
API_CONTAINER=${API_CONTAINER:-task-management-api}
DB_USER=${DB_USER:-postgres}
DB_NAME=${DB_NAME:-task_management}
JSON='Content-Type: application/json'

PASS=0 FAIL=0 SKIP=0
RATE_LIMITED=false
TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT

RUN=$(python3 -c 'import uuid;print(uuid.uuid4().hex[:8])')
PASSWORD=password123

# ---------------------------------------------------------------------------
# helpers
# ---------------------------------------------------------------------------

uuid4() { python3 -c 'import uuid;print(uuid.uuid4())'; }

has_db=false
if command -v docker >/dev/null 2>&1 && docker exec "$PG_CONTAINER" true >/dev/null 2>&1; then has_db=true; fi
has_api_logs=false
if command -v docker >/dev/null 2>&1 && docker logs --tail 1 "$API_CONTAINER" >/dev/null 2>&1; then has_api_logs=true; fi

psql_() { docker exec "$PG_CONTAINER" psql -U "$DB_USER" -d "$DB_NAME" -tAc "$1"; }
api_logs() { docker logs "$API_CONTAINER" 2>&1; }

# req METHOD URL [TOKEN] [BODY] [EXTRA_HEADER] -> sets CODE, BODY, HDRS
req() {
  local method=$1 url=$2 token=${3:-} body=${4:-} header=${5:-}
  local args=(-s -X "$method" -o "$TMP/body" -D "$TMP/hdr" -w '%{http_code}')
  [ -n "$token" ] && args+=(-H "Authorization: Bearer $token")
  [ -n "$body" ] && args+=(-H "$JSON" -d "$body")
  [ -n "$header" ] && args+=(-H "$header")
  rm -f "$TMP/body" "$TMP/hdr"
  CODE=$(curl "${args[@]}" "$url")
  [ "$CODE" = 429 ] && RATE_LIMITED=true
  BODY=$(cat "$TMP/body" 2>/dev/null)
  HDRS=$(tr -d '\r' <"$TMP/hdr" 2>/dev/null)
}

# jget EXPR: evaluate a python expression on the last JSON body (bound to d).
jget() { python3 -c 'import sys,json;d=json.load(sys.stdin);print(eval(sys.argv[1]))' "$1" <<<"$BODY" 2>/dev/null; }

header() { grep -i "^$1:" <<<"$HDRS" | head -1 | cut -d' ' -f2-; }

check() { # NAME EXPECTED ACTUAL
  if [ "$2" == "$3" ]; then
    PASS=$((PASS + 1)); printf '  \033[32mPASS\033[0m  %s\n' "$1"
  else
    FAIL=$((FAIL + 1)); printf '  \033[31mFAIL\033[0m  %s (expected %s, got %s)\n        body: %s\n' "$1" "$2" "$3" "${BODY:0:300}"
  fi
}

skip() { SKIP=$((SKIP + 1)); printf '  \033[33mSKIP\033[0m  %s (%s)\n' "$1" "$2"; }

error_shape() {
  jget 'all(k in d for k in ("status","code","message","timestamp","request_id"))'
}

section() { printf '\n== %s\n' "$1"; }

# ---------------------------------------------------------------------------

printf 'Target: %s  (run %s, db checks: %s, log checks: %s)\n' "$BASE" "$RUN" "$has_db" "$has_api_logs"

section "Health"
req GET "$BASE/health"
if [ "$CODE" != 200 ]; then
  echo "API is not reachable at $BASE (got '$CODE'). Start it with 'make up'." >&2
  exit 1
fi
check "GET /health 200" 200 "$CODE"

section "Auth: register"
ALICE_EMAIL="alice.$RUN@example.com"
BOB_EMAIL="bob.$RUN@example.com"
EVE_EMAIL="eve.$RUN@example.com"
MALLORY_EMAIL="mallory.$RUN@example.com"
TEAM="Alpha-$RUN"

# register NAME EMAIL FIELD VALUE, where FIELD is team_name or invite_code.
register() { req POST "$API/auth/register" "" "{\"name\":\"$1\",\"email\":\"$2\",\"password\":\"$PASSWORD\",\"$3\":\"$4\"}"; }

register Alice "$ALICE_EMAIL" team_name "$TEAM"
check "register creating a team 201" 201 "$CODE"
ALICE=$(jget 'd["data"]["id"]'); TEAM_ID=$(jget 'd["data"]["team_id"]'); INVITE=$(jget 'd["data"]["team"]["invite_code"]')
check "response includes the new team" "$TEAM_ID" "$(jget 'd["data"]["team"]["id"]')"
check "creator receives a 32 character invite code" 32 "${#INVITE}"
check "no password or hash in response" False "$(jget '"password" in d["data"] or "password_hash" in d["data"]')"

register Bob "$(tr '[:lower:]' '[:upper:]' <<<"$BOB_EMAIL")" invite_code "$(tr '[:lower:]' '[:upper:]' <<<"$INVITE")"
check "register with invite code (any case) 201" 201 "$CODE"
check "invite code joins the inviter's team" "$TEAM_ID" "$(jget 'd["data"]["team_id"]')"
check "email normalized to lowercase" "$BOB_EMAIL" "$(jget 'd["data"]["email"]')"
BOB=$(jget 'd["data"]["id"]')

register Eve "$EVE_EMAIL" team_name "Beta-$RUN"
check "register user in another team 201" 201 "$CODE"
EVE=$(jget 'd["data"]["id"]')

register Mallory "$MALLORY_EMAIL" team_name "$TEAM"
check "reusing a team name creates a separate team" True "$(jget "d['data']['team_id'] != '$TEAM_ID'")"
MALLORY=$(jget 'd["data"]["id"]')

register Alice2 "$(tr '[:lower:]' '[:upper:]' <<<"$ALICE_EMAIL")" team_name "$TEAM"
check "duplicate email (any case) 409" 409 "$CODE"
check "code EMAIL_ALREADY_REGISTERED" EMAIL_ALREADY_REGISTERED "$(jget 'd["code"]')"

register Nobody "nobody.$RUN@example.com" invite_code "$(uuid4 | tr -d '-')"
check "unknown invite code 422" 422 "$CODE"
check "code INVALID_INVITE_CODE" INVALID_INVITE_CODE "$(jget 'd["code"]')"

req POST "$API/auth/register" "" "{\"name\":\"Both\",\"email\":\"both.$RUN@example.com\",\"password\":\"$PASSWORD\",\"team_name\":\"X-$RUN\",\"invite_code\":\"$INVITE\"}"
check "team_name together with invite_code 400" 400 "$CODE"
check "code INVALID_TEAM_CHOICE" INVALID_TEAM_CHOICE "$(jget 'd["code"]')"
req POST "$API/auth/register" "" "{\"name\":\"Neither\",\"email\":\"neither.$RUN@example.com\",\"password\":\"$PASSWORD\"}"
check "neither team_name nor invite_code 400" 400 "$CODE"
check "code INVALID_TEAM_CHOICE" INVALID_TEAM_CHOICE "$(jget 'd["code"]')"

req POST "$API/auth/register" "" '{"name":"A","email":"bad","password":"x"}'
check "invalid payload 400" 400 "$CODE"
check "code VALIDATION_ERROR" VALIDATION_ERROR "$(jget 'd["code"]')"
check "one detail per invalid field" 3 "$(jget 'len(d["details"])')"
check "error has status/code/message/timestamp/request_id" True "$(error_shape)"

register "   " "blank.$RUN@example.com" team_name "Gamma-$RUN"
check "whitespace-only name 400" 400 "$CODE"
check "blank name reported on field name" name "$(jget 'd["details"][0]["field"]')"

register 'Nul\u0000Name' "nul.$RUN@example.com" team_name "Gamma-$RUN"
check "NUL character in name 400 (not 500)" 400 "$CODE"
check "NUL rejected with a field detail" "must be valid UTF-8 text without NUL characters" "$(jget 'd["details"][0]["message"]')"

LONG_PASSWORD=$(python3 -c 'print("é" * 40)') # 40 characters, 80 bytes
req POST "$API/auth/register" "" "{\"name\":\"Long\",\"email\":\"long.$RUN@example.com\",\"password\":\"$LONG_PASSWORD\",\"team_name\":\"Gamma-$RUN\"}"
check "password over 72 bytes 400 (not 500)" 400 "$CODE"
check "password limit explained in bytes" "must be at most 72 bytes" "$(jget 'd["details"][0]["message"]')"

req POST "$API/auth/register" "" '{"name":'
check "malformed JSON 400" 400 "$CODE"
check "code INVALID_REQUEST_BODY" INVALID_REQUEST_BODY "$(jget 'd["code"]')"

section "Auth: login"
login() { req POST "$API/auth/login" "" "{\"email\":\"$1\",\"password\":\"$2\"}"; }

login "$ALICE_EMAIL" "$PASSWORD"
check "login 200" 200 "$CODE"
check "token_type Bearer" Bearer "$(jget 'd["data"]["token_type"]')"
TA=$(jget 'd["data"]["access_token"]')
login "$BOB_EMAIL" "$PASSWORD"; TB=$(jget 'd["data"]["access_token"]')
login "$EVE_EMAIL" "$PASSWORD"; TE=$(jget 'd["data"]["access_token"]')

login "$ALICE_EMAIL" wrongpassword
check "wrong password 401" 401 "$CODE"
WRONG_MSG=$(jget 'd["message"]')
login "nobody.$RUN@example.com" "$PASSWORD"
check "unknown email 401" 401 "$CODE"
check "same message for unknown email (no user enumeration)" "$WRONG_MSG" "$(jget 'd["message"]')"
login "$ALICE_EMAIL" "$LONG_PASSWORD"
check "login with password over 72 bytes 400" 400 "$CODE"

section "JWT"
req GET "$API/tasks"
check "missing token 401" 401 "$CODE"
check "code MISSING_TOKEN" MISSING_TOKEN "$(jget 'd["code"]')"
req GET "$API/tasks" "garbage.token.value"
check "malformed token 401" 401 "$CODE"
check "code INVALID_TOKEN" INVALID_TOKEN "$(jget 'd["code"]')"
NONE_TOKEN=$(python3 - "$ALICE" <<'PY'
import base64, json, sys
enc = lambda o: base64.urlsafe_b64encode(json.dumps(o).encode()).rstrip(b"=").decode()
print(enc({"alg": "none", "typ": "JWT"}) + "." + enc({"sub": sys.argv[1], "iss": "task-management-api", "exp": 9999999999}) + ".")
PY
)
req GET "$API/tasks" "$NONE_TOKEN"
check "unsigned alg=none token 401" 401 "$CODE"
req GET "$API/users/me" "$TA"
check "GET /users/me 200" 200 "$CODE"
check "token identifies the user" "$ALICE" "$(jget 'd["data"]["id"]')"
check "profile shows the team invite code to members" "$INVITE" "$(jget 'd["data"]["team"]["invite_code"]')"
req GET "$API/users/team-members" "$TA"
check "GET /users/team-members 200" 200 "$CODE"
check "team has only its two members" 2 "$(jget 'len(d["data"])')"

section "Idempotency: sequential"
KEY=$(uuid4)
PAYLOAD='{"title":"Write report","description":"Q3","due_date":"2026-10-20T10:00:00+07:00"}'

req POST "$API/tasks" "$TA" "$PAYLOAD" "Idempotency-Key: $KEY"
check "first request 201" 201 "$CODE"
check "first request not flagged as replay" "" "$(header Idempotent-Replayed)"
FIRST_BODY=$BODY
T1=$(jget 'd["data"]["id"]')

req POST "$API/tasks" "$TA" "$PAYLOAD" "Idempotency-Key: $KEY"
check "retry 201" 201 "$CODE"
check "retry body identical to first" "$FIRST_BODY" "$BODY"
check "retry flagged Idempotent-Replayed: true" true "$(header Idempotent-Replayed)"

req GET "$API/tasks?search=Write%20report" "$TA"
check "only one task created" 1 "$(jget 'd["meta"]["total"]')"

req POST "$API/tasks" "$TA" '{"title":"Different"}' "Idempotency-Key: $KEY"
check "same key, different payload 422" 422 "$CODE"
check "code IDEMPOTENCY_KEY_REUSED" IDEMPOTENCY_KEY_REUSED "$(jget 'd["code"]')"

req POST "$API/tasks" "$TB" "$PAYLOAD" "Idempotency-Key: $KEY"
check "same key from another user is independent 201" 201 "$CODE"
check "another user gets its own task" True "$(jget "d['data']['id'] != '$T1'")"

req POST "$API/tasks" "$TA" "$PAYLOAD" "Idempotency-Key: not-a-uuid"
check "non-UUID key 400" 400 "$CODE"
check "code INVALID_IDEMPOTENCY_KEY" INVALID_IDEMPOTENCY_KEY "$(jget 'd["code"]')"

section "Idempotency: concurrent duplicates"
N=30
KEY=$(uuid4)
for i in $(seq 1 "$N"); do
  curl -s -o "$TMP/concurrent_$i" -w '%{http_code}\n' -X POST "$API/tasks" \
    -H "$JSON" -H "Authorization: Bearer $TA" -H "Idempotency-Key: $KEY" \
    -d '{"title":"Concurrent"}' >>"$TMP/concurrent_codes" &
done
wait
check "all $N responses 201" "$N" "$(grep -c '^201$' "$TMP/concurrent_codes")"
check "all $N bodies identical" 1 "$(cat "$TMP"/concurrent_[0-9]* | sort -u | wc -l | tr -d ' ')"
req GET "$API/tasks?search=Concurrent" "$TA"
check "exactly one task created" 1 "$(jget 'd["meta"]["total"]')"

section "Idempotency: 24h window"
if $has_db; then
  KEY=$(uuid4)
  req POST "$API/tasks" "$TA" '{"title":"Expiring"}' "Idempotency-Key: $KEY"
  EXPIRED_TASK=$(jget 'd["data"]["id"]')
  psql_ "UPDATE idempotency_keys SET expires_at = now() - interval '1 second' WHERE idempotency_key = '$KEY'" >/dev/null
  req POST "$API/tasks" "$TA" '{"title":"Expiring"}' "Idempotency-Key: $KEY"
  check "expired key accepted again 201" 201 "$CODE"
  check "expired key creates a new task" True "$(jget "d['data']['id'] != '$EXPIRED_TASK'")"
  check "replay header absent after expiry" "" "$(header Idempotent-Replayed)"
else
  skip "expired key creates a new task" "no database access"
fi

section "Tasks: CRUD"
for t in "Buy milk:todo" "Fix bug 100%:in_progress" "Deploy_v2:in_progress" "Done thing:done"; do
  req POST "$API/tasks" "$TA" "{\"title\":\"${t%%:*}\",\"status\":\"${t##*:}\"}"
done
check "create without Idempotency-Key 201" 201 "$CODE"
req POST "$API/tasks" "$TA" '{"title":"x","status":"invalid"}'
check "invalid status 400" 400 "$CODE"
req POST "$API/tasks" "$TA" '{"title":"   "}'
check "blank title 400" 400 "$CODE"
req POST "$API/tasks" "$TA" '{"title":"a\u0000b"}'
check "NUL character in title 400 (not 500)" 400 "$CODE"
req POST "$API/tasks" "$TA" '{"title":"ok","description":"a\u0000b"}'
check "NUL character in description 400 (not 500)" 400 "$CODE"

req GET "$API/tasks/$T1" "$TA"
check "GET detail 200" 200 "$CODE"
check "detail returns the task" "Write report" "$(jget 'd["data"]["title"]')"
check "due_date normalized to UTC" 2026-10-20T03:00:00Z "$(jget 'd["data"]["due_date"]')"
req GET "$API/tasks/$(uuid4)" "$TA"
check "unknown id 404" 404 "$CODE"
check "code TASK_NOT_FOUND" TASK_NOT_FOUND "$(jget 'd["code"]')"
req GET "$API/tasks/$(uuid4 | tr '[:lower:]' '[:upper:]')" "$TA"
check "uppercase UUID in path accepted (404, not 400)" 404 "$CODE"
req GET "$API/tasks/123" "$TA"
check "invalid id 400" 400 "$CODE"
check "code INVALID_ID" INVALID_ID "$(jget 'd["code"]')"
req GET "$API/tasks/$T1" "$TE"
check "user of another team cannot see task 404" 404 "$CODE"

req PUT "$API/tasks/$T1" "$TA" '{"title":"Write report v2","description":"updated","status":"in_progress"}'
check "PUT 200" 200 "$CODE"
check "PUT updates title" "Write report v2" "$(jget 'd["data"]["title"]')"
check "PUT keeps fields it did not receive (due_date)" 2026-10-20T03:00:00Z "$(jget 'd["data"]["due_date"]')"
req PUT "$API/tasks/$T1" "$TA" '{"status":"done"}'
check "PUT with only status 200" 200 "$CODE"
check "status changed" done "$(jget 'd["data"]["status"]')"
check "title untouched" "Write report v2" "$(jget 'd["data"]["title"]')"
check "description untouched" updated "$(jget 'd["data"]["description"]')"
req PUT "$API/tasks/$T1" "$TA" '{"due_date":null}'
check "due_date null clears the due date" None "$(jget 'd["data"]["due_date"]')"
check "clearing due_date keeps status" done "$(jget 'd["data"]["status"]')"
req PUT "$API/tasks/$T1" "$TA" '{}'
check "PUT without any field 400" 400 "$CODE"
check "code NO_FIELDS_TO_UPDATE" NO_FIELDS_TO_UPDATE "$(jget 'd["code"]')"
req PUT "$API/tasks/$T1" "$TA" '{"title":"   "}'
check "PUT blank title 400" 400 "$CODE"
check "blank title message" "must not be empty" "$(jget 'd["details"][0]["message"]')"
req PUT "$API/tasks/$T1" "$TA" '{"status":"bogus"}'
check "PUT invalid status 400" 400 "$CODE"
req PUT "$API/tasks/$T1" "$TA" '{"title":"a\u0000b"}'
check "PUT NUL character in title 400 (not 500)" 400 "$CODE"
req PUT "$API/tasks/$T1" "$TA" '{"due_date":"not-a-date"}'
check "PUT malformed due_date 400" 400 "$CODE"
req PUT "$API/tasks/$T1" "$TE" '{"title":"hack","status":"done"}'
check "PUT by user of another team 404" 404 "$CODE"

section "Tasks: list, filter, search, pagination"
# Alice owns: Write report v2, Concurrent, Expiring (x2 when db checks ran), Buy milk, Fix bug 100%, Deploy_v2, Done thing
req GET "$API/tasks" "$TA"
check "list 200" 200 "$CODE"
ALICE_TOTAL=$(jget 'd["meta"]["total"]')
EXPECTED_TOTAL=6; $has_db && EXPECTED_TOTAL=8
check "total counts only own tasks" "$EXPECTED_TOTAL" "$ALICE_TOTAL"
check "default limit 10" 10 "$(jget 'd["meta"]["limit"]')"
check "default page 1" 1 "$(jget 'd["meta"]["page"]')"
check "newest first" True "$(jget '[t["created_at"] for t in d["data"]] == sorted([t["created_at"] for t in d["data"]], reverse=True)')"

req GET "$API/tasks?page=1&limit=3" "$TA"
check "limit=3 returns 3 items" 3 "$(jget 'len(d["data"])')"
check "total_pages computed" $(((ALICE_TOTAL + 2) / 3)) "$(jget 'd["meta"]["total_pages"]')"
PAGE1=$(jget '",".join(t["id"] for t in d["data"])')
req GET "$API/tasks?page=2&limit=3" "$TA"
PAGE2=$(jget '",".join(t["id"] for t in d["data"])')
check "pages do not overlap" True "$(python3 -c 'import sys;print(not set(sys.argv[1].split(","))&set(sys.argv[2].split(",")))' "$PAGE1" "$PAGE2")"
req GET "$API/tasks?page=99&limit=3" "$TA"
check "page past the end is empty" 0 "$(jget 'len(d["data"])')"
check "page past the end still reports total" "$ALICE_TOTAL" "$(jget 'd["meta"]["total"]')"

req GET "$API/tasks?status=in_progress" "$TA"
check "filter by status" 2 "$(jget 'len(d["data"])')"
check "filter returns only that status" True "$(jget 'all(t["status"] == "in_progress" for t in d["data"])')"
req GET "$API/tasks?search=REPORT" "$TA"
check "search is case-insensitive" 1 "$(jget 'len(d["data"])')"
req GET "$API/tasks?search=100%25" "$TA"
check "search treats % literally" 1 "$(jget 'len(d["data"])')"
req GET "$API/tasks?search=_" "$TA"
check "search treats _ literally" 1 "$(jget 'len(d["data"])')"
req GET "$API/tasks?status=in_progress&search=fix" "$TA"
check "status + search combined" 1 "$(jget 'len(d["data"])')"

req GET "$API/tasks?status=bogus" "$TA"
check "invalid status filter 400" 400 "$CODE"
req GET "$API/tasks?limit=500" "$TA"
check "limit above 100 400" 400 "$CODE"
req GET "$API/tasks?page=abc" "$TA"
check "non-integer page 400" 400 "$CODE"
req GET "$API/tasks?page=1000001" "$TA"
check "page above 1000000 400" 400 "$CODE"
req GET "$API/tasks?page=9223372036854775807&limit=100" "$TA"
check "page that would overflow the offset 400 (not 500)" 400 "$CODE"
req GET "$API/tasks?search=a%00b" "$TA"
check "NUL character in search 400 (not 500)" 400 "$CODE"
req GET "$API/tasks?search=%FF" "$TA"
check "invalid UTF-8 in search 400 (not 500)" 400 "$CODE"
req GET "$API/tasks?search=%27%3B%20DROP%20TABLE%20tasks%3B--" "$TA"
check "SQL injection attempt is harmless 200" 200 "$CODE"
req GET "$API/tasks" "$TE"
check "other team user sees none of these tasks" 0 "$(jget 'd["meta"]["total"]')"

section "Assign (single transaction)"
req POST "$API/tasks/$T1/assign" "$TA" "{\"assignee_id\":\"$EVE\"}"
check "assignee from another team 422" 422 "$CODE"
check "code ASSIGNEE_NOT_IN_TEAM" ASSIGNEE_NOT_IN_TEAM "$(jget 'd["code"]')"
req POST "$API/tasks/$T1/assign" "$TA" "{\"assignee_id\":\"$MALLORY\"}"
check "assignee from a team with the same name 422" 422 "$CODE"
req POST "$API/tasks/$T1/assign" "$TA" "{\"assignee_id\":\"$ALICE\"}"
check "assign to the owner 422" 422 "$CODE"
check "code ASSIGNEE_IS_OWNER" ASSIGNEE_IS_OWNER "$(jget 'd["code"]')"
req POST "$API/tasks/$T1/assign" "$TA" "{\"assignee_id\":\"$(uuid4 | tr '[:lower:]' '[:upper:]')\"}"
check "unknown assignee (uppercase UUID) 422" 422 "$CODE"
check "code ASSIGNEE_NOT_FOUND" ASSIGNEE_NOT_FOUND "$(jget 'd["code"]')"
req POST "$API/tasks/$T1/assign" "$TA" '{"assignee_id":"nope"}'
check "malformed assignee id 400" 400 "$CODE"
req POST "$API/tasks/$T1/assign" "$TA" '{}'
check "missing assignee id 400" 400 "$CODE"
check "code VALIDATION_ERROR" VALIDATION_ERROR "$(jget 'd["code"]')"
if $has_db; then
  check "failed assigns wrote no task_logs" 0 "$(psql_ "SELECT count(*) FROM task_logs WHERE task_id = '$T1'")"
else
  skip "failed assigns wrote no task_logs" "no database access"
fi

req POST "$API/tasks/$T1/assign" "$TA" "{\"assignee_id\":\"$BOB\"}"
check "assign teammate 200" 200 "$CODE"
check "assignee updated" "$BOB" "$(jget 'd["data"]["assignee_id"]')"
if $has_db; then
  check "task_logs entry with old and new assignee" 1 "$(psql_ "SELECT count(*) FROM task_logs WHERE task_id = '$T1' AND action = 'assigned' AND old_value -> 'assignee_id' = 'null'::jsonb AND new_value ->> 'assignee_id' = '$BOB'")"
else
  skip "task_logs entry written" "no database access"
fi
if $has_api_logs; then
  check "assignment notification logged" 1 "$(api_logs | grep '"notification sent"' | grep -c "\"task_id\":\"$T1\"")"
else
  skip "assignment notification logged" "no access to API container logs"
fi

req POST "$API/tasks/$T1/assign" "$TA" "{\"assignee_id\":\"$BOB\"}"
check "assigning the same user again 200" 200 "$CODE"
if $has_db; then
  check "re-assigning the same user writes no task_logs" 1 "$(psql_ "SELECT count(*) FROM task_logs WHERE task_id = '$T1'")"
fi
if $has_api_logs; then
  check "re-assigning the same user sends no notification" 1 "$(api_logs | grep '"notification sent"' | grep -c "\"task_id\":\"$T1\"")"
fi

req GET "$API/tasks/$T1" "$TB"
check "assignee can view the task" 200 "$CODE"
req GET "$API/tasks" "$TB"
check "task appears in the assignee's list" True "$(jget "'$T1' in [t['id'] for t in d['data']]")"
req PUT "$API/tasks/$T1" "$TB" '{"title":"bob edit","status":"done"}'
check "assignee cannot update 403" 403 "$CODE"
check "code TASK_FORBIDDEN" TASK_FORBIDDEN "$(jget 'd["code"]')"
req POST "$API/tasks/$T1/assign" "$TB" "{\"assignee_id\":\"$BOB\"}"
check "assignee cannot reassign 403" 403 "$CODE"
req DELETE "$API/tasks/$T1" "$TB"
check "assignee cannot delete 403" 403 "$CODE"

section "Delete"
req POST "$API/tasks" "$TA" '{"title":"To delete"}'
DELETE_ID=$(jget 'd["data"]["id"]')
req DELETE "$API/tasks/$DELETE_ID" "$TE"
check "other team user delete 404" 404 "$CODE"
req DELETE "$API/tasks/$DELETE_ID" "$TA"
check "owner delete 204" 204 "$CODE"
req DELETE "$API/tasks/$DELETE_ID" "$TA"
check "delete twice 404" 404 "$CODE"
req GET "$API/tasks/$DELETE_ID" "$TA"
check "deleted task is gone 404" 404 "$CODE"

section "Global error handling"
req GET "$BASE/does-not-exist"
check "unknown route 404" 404 "$CODE"
check "unknown route uses the error format" True "$(error_shape)"
req GET "$API/does-not-exist"
check "unknown /api/v1 route without token 404 (not 401)" 404 "$CODE"
req PATCH "$API/tasks" "$TA"
check "unsupported method 405" 405 "$CODE"
check "code METHOD_NOT_ALLOWED" METHOD_NOT_ALLOWED "$(jget 'd["code"]')"
python3 -c 'print("{\"title\":\"" + "a" * 1100000 + "\"}")' >"$TMP/big.json"
CODE=$(curl -s -o "$TMP/body" -w '%{http_code}' -X POST "$API/tasks" -H "$JSON" -H "Authorization: Bearer $TA" --data-binary @"$TMP/big.json")
BODY=$(cat "$TMP/body")
check "body over 1MB 413" 413 "$CODE"
check "code PAYLOAD_TOO_LARGE" PAYLOAD_TOO_LARGE "$(jget 'd["code"]')"

section "Logging & request id"
CLIENT_RID=$(uuid4)
req GET "$API/tasks" "$TA" "" "X-Request-ID: $CLIENT_RID"
RID=$(header X-Request-Id)
check "server generates its own UUID request_id" True "$(python3 -c 'import sys,uuid;uuid.UUID(sys.argv[1]);print(sys.argv[1]!=sys.argv[2])' "$RID" "$CLIENT_RID" 2>/dev/null || echo False)"
req GET "$API/tasks" "$TA" "" "X-Request-ID: $CLIENT_RID"
check "same client X-Request-ID still gets a new request_id" True "$([ "$(header X-Request-Id)" != "$RID" ] && echo True || echo False)"
req GET "$API/tasks/$(uuid4)" "$TA"
ERROR_RID=$(jget 'd["request_id"]')
check "error body carries the same request_id as the header" "$(header X-Request-Id)" "$ERROR_RID"

if $has_api_logs; then
  sleep 0.2
  LOGS=$(api_logs | grep '"msg":"http request"')
  LINE=$(grep "\"request_id\":\"$RID\"" <<<"$LOGS" | tail -1)
  check "log line has request_id, method, path, status, latency" True "$(python3 -c 'import sys,json;d=json.loads(sys.argv[1]);print(all(k in d for k in ("request_id","method","path","status","latency_ms")))' "$LINE" 2>/dev/null || echo False)"
  check "client X-Request-ID logged as client_request_id" "$CLIENT_RID" "$(python3 -c 'import sys,json;print(json.loads(sys.argv[1]).get("client_request_id"))' "$LINE" 2>/dev/null)"
  check "request_id unique across all logged requests" True "$(python3 -c 'import sys,json;ids=[json.loads(l)["request_id"] for l in sys.stdin];print(len(ids)==len(set(ids)))' <<<"$LOGS")"
  check "2xx logged at INFO" True "$(python3 -c 'import sys,json;L=[json.loads(l) for l in sys.stdin];S=[l for l in L if l["status"]<400];print(bool(S) and all(l["level"]=="INFO" for l in S))' <<<"$LOGS")"
  check "4xx logged at WARN" True "$(python3 -c 'import sys,json;L=[json.loads(l) for l in sys.stdin];S=[l for l in L if 400<=l["status"]<500];print(bool(S) and all(l["level"]=="WARN" for l in S))' <<<"$LOGS")"
  check "5xx logged at ERROR" True "$(python3 -c 'import sys,json;L=[json.loads(l) for l in sys.stdin];print(all(l["level"]=="ERROR" for l in L if l["status"]>=500))' <<<"$LOGS")"
else
  skip "structured request log checks" "no access to API container logs"
fi

printf '\nRESULT: %d passed, %d failed, %d skipped\n' "$PASS" "$FAIL" "$SKIP"
if $RATE_LIMITED; then
  echo "Some requests got 429: the /auth rate limit was hit. Wait a minute or raise AUTH_RATE_LIMIT." >&2
fi
[ "$FAIL" -eq 0 ]
