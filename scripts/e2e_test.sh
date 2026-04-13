#!/usr/bin/env bash
# End-to-end FUSE test against petstore3.swagger.io
# Run this in a real terminal (not a sandboxed executor).
# Usage: ./scripts/e2e_test.sh

set -uo pipefail   # -e intentionally omitted: we handle failures manually

BINARY="./bin/apimount"
MOUNT="$(pwd)/mnt"
SPEC="testdata/petstore.yaml"
BASE_URL="https://petstore3.swagger.io/api/v3"

PASS=0
FAIL=0

green() { printf "\033[32m✓ %s\033[0m\n" "$*"; }
red()   { printf "\033[31m✗ %s\033[0m\n" "$*"; }

# check: runs a command silently; passes if it exits 0
check() {
  local desc="$1"; shift
  if "$@" &>/dev/null; then
    green "$desc"
    PASS=$(( PASS + 1 ))
  else
    red "$desc"
    FAIL=$(( FAIL + 1 ))
  fi
}

# check_contains: passes if $expected is a fixed substring of $actual
check_contains() {
  local desc="$1"
  local expected="$2"
  local actual="$3"
  if printf '%s' "$actual" | grep -qF "$expected"; then
    green "$desc"
    PASS=$(( PASS + 1 ))
  else
    red "$desc  (expected: '$expected')"
    FAIL=$(( FAIL + 1 ))
  fi
}

unmount_fuse() {
  fusermount -u "$1" 2>/dev/null || \
  umount "$1" 2>/dev/null || \
  diskutil unmount "$1" 2>/dev/null || true
}

cleanup() {
  printf "\nCleaning up...\n"
  unmount_fuse "$MOUNT"
  rmdir "$MOUNT" 2>/dev/null || true
}
trap cleanup EXIT

# ── Build ──────────────────────────────────────────────────────────────────────
printf "Building apimount...\n"
if make build >/dev/null 2>&1; then
  green "build succeeded"
  PASS=$(( PASS + 1 ))
else
  red "build FAILED — aborting"
  exit 1
fi

# ── Dry run ────────────────────────────────────────────────────────────────────
printf "\n── Dry run ──\n"
TREE=$("$BINARY" tree --spec "$SPEC" --group-by path 2>/dev/null)
check_contains "tree: pet/ directory present"             "pet/"           "$TREE"
check_contains "tree: store/ directory present"           "store/"         "$TREE"
check_contains "tree: user/ directory present"            "user/"          "$TREE"
check_contains "tree: {petId}/ param dir present"         "{petId}/"       "$TREE"
check_contains "tree: findByStatus/.query present"        ".query"         "$TREE"
check_contains "tree: pet/.schema present"                ".schema"        "$TREE"
check_contains "tree: {petId}/.delete present"            ".delete"        "$TREE"
check_contains "tree: {petId}/.data present"              ".data"          "$TREE"

# ── Validate ───────────────────────────────────────────────────────────────────
printf "\n── Validate ──\n"
VAL=$("$BINARY" validate --spec "$SPEC" 2>&1)
check_contains "validate: spec is valid"             "Valid OpenAPI"   "$VAL"
check_contains "validate: shows title"               "Petstore"        "$VAL"
check_contains "validate: shows operation count"     "Operations:"     "$VAL"
check_contains "validate: shows GET methods"         "GET"             "$VAL"
check_contains "validate: shows auth schemes"        "Auth schemes"    "$VAL"

# ── Mount ──────────────────────────────────────────────────────────────────────
printf "\n── Mount ──\n"
mkdir -p "$MOUNT"
"$BINARY" \
  --spec "$SPEC" \
  --base-url "$BASE_URL" \
  --mount "$MOUNT" \
  --group-by path \
  --cache-ttl 15s &
MOUNT_PID=$!
sleep 3

if ls "$MOUNT" &>/dev/null; then
  green "mount: filesystem accessible at $MOUNT"
  PASS=$(( PASS + 1 ))
else
  red "mount: FAILED — cannot access $MOUNT"
  FAIL=$(( FAIL + 1 ))
  kill "$MOUNT_PID" 2>/dev/null || true
  exit 1
fi

# ── Directory listing ─────────────────────────────────────────────────────────
printf "\n── Directory listing ──\n"
LS_ROOT=$(ls "$MOUNT" 2>&1)
check_contains "ls /: pet present"   "pet"   "$LS_ROOT"
check_contains "ls /: store present" "store" "$LS_ROOT"
check_contains "ls /: user present"  "user"  "$LS_ROOT"

LS_PET=$(ls -a "$MOUNT/pet/" 2>&1)
check_contains "ls /pet/: .post present"        ".post"        "$LS_PET"
check_contains "ls /pet/: .help present"        ".help"        "$LS_PET"
check_contains "ls /pet/: .response present"    ".response"    "$LS_PET"
check_contains "ls /pet/: {petId}/ present"     "{petId}"      "$LS_PET"
check_contains "ls /pet/: findByStatus/ present" "findByStatus" "$LS_PET"

# ── Static file reads ─────────────────────────────────────────────────────────
printf "\n── Static file reads ──\n"
HELP=$(cat "$MOUNT/pet/.help" 2>&1)
check_contains "cat pet/.help: shows directory path"  "/pet"          "$HELP"
check_contains "cat pet/.help: lists .post file"      ".post"         "$HELP"
check_contains "cat pet/.help: shows API footer"      "API:"          "$HELP"

SCHEMA=$(cat "$MOUNT/pet/.schema" 2>&1)
check_contains "cat pet/.schema: has 'type' key"       '"type"'       "$SCHEMA"
check_contains "cat pet/.schema: has 'properties' key" '"properties"' "$SCHEMA"
check_contains "cat pet/.schema: has 'name' property"  '"name"'       "$SCHEMA"

# ── Live GET ──────────────────────────────────────────────────────────────────
printf "\n── Live HTTP GET (needs internet) ──\n"
# findByStatus requires ?status param — without it petstore returns 400/422
# Set via .query first
printf "status=available" > "$MOUNT/pet/findByStatus/.query"
sleep 0.5
DATA_Q=$(cat "$MOUNT/pet/findByStatus/.data" 2>&1)
if printf '%s' "$DATA_Q" | grep -qiF '"id"'; then
  green "cat findByStatus/.data with .query: got JSON array"
  PASS=$(( PASS + 1 ))
elif printf '%s' "$DATA_Q" | grep -qiE '\[\]|empty'; then
  green "cat findByStatus/.data with .query: got empty array (no available pets)"
  PASS=$(( PASS + 1 ))
else
  red "cat findByStatus/.data with .query: unexpected: ${DATA_Q:0:120}"
  FAIL=$(( FAIL + 1 ))
fi

# ── Dynamic path param ────────────────────────────────────────────────────────
printf "\n── Dynamic path parameter ──\n"
LS_PET1=$(ls -a "$MOUNT/pet/1/" 2>&1)
check_contains "ls /pet/1/: .data present"   ".data"   "$LS_PET1"
check_contains "ls /pet/1/: .delete present" ".delete" "$LS_PET1"
check_contains "ls /pet/1/: .help present"   ".help"   "$LS_PET1"

# .help in a concrete param dir should show the resolved path (1 not {petId})
HELP_1=$(cat "$MOUNT/pet/1/.help" 2>&1)
check_contains "cat pet/1/.help: param resolved to /1"  "/1"  "$HELP_1"

# GET /pet/1 — either returns the pet or 404 (both are valid responses)
PET_DATA=$(cat "$MOUNT/pet/1/.data" 2>&1)
if printf '%s' "$PET_DATA" | grep -qiF '"id"'; then
  green "cat pet/1/.data: got pet object (GET /pet/1)"
  PASS=$(( PASS + 1 ))
elif printf '%s' "$PET_DATA" | grep -qiE '404|Not Found|500|code'; then
  green "cat pet/1/.data: got API response (correct passthrough)"
  PASS=$(( PASS + 1 ))
else
  red "cat pet/1/.data: unexpected: ${PET_DATA:0:120}"
  FAIL=$(( FAIL + 1 ))
fi

# ── .response file ────────────────────────────────────────────────────────────
printf "\n── .response file ──\n"
RESP=$(cat "$MOUNT/pet/1/.response" 2>&1)
if printf '%s' "$RESP" | grep -qiE '"id"|404|500|code|no response'; then
  green "cat pet/1/.response: has content from prior read"
  PASS=$(( PASS + 1 ))
else
  red "cat pet/1/.response: unexpected: ${RESP:0:120}"
  FAIL=$(( FAIL + 1 ))
fi

# ── POST (creates a pet) ──────────────────────────────────────────────────────
printf "\n── POST operation ──\n"
SCHEMA_POST=$(cat "$MOUNT/pet/.schema" 2>&1)
check_contains "cat pet/.schema before POST: valid"  '"type"'  "$SCHEMA_POST"

printf '{"name":"E2ETestPet","photoUrls":["http://example.com"],"status":"available"}' \
  > "$MOUNT/pet/.post" 2>/dev/null && WRITE_OK=true || WRITE_OK=false

if [ "$WRITE_OK" = "true" ]; then
  green "echo > pet/.post: write accepted"
  PASS=$(( PASS + 1 ))
  POST_RESP=$(cat "$MOUNT/pet/.post" 2>&1)
  if printf '%s' "$POST_RESP" | grep -qiEF '"name"'; then
    green "cat pet/.post after write: response contains created pet"
    PASS=$(( PASS + 1 ))
  elif printf '%s' "$POST_RESP" | grep -qiE '4[0-9][0-9]'; then
    green "cat pet/.post after write: got HTTP error (API rejected — error handling correct)"
    PASS=$(( PASS + 1 ))
  else
    red "cat pet/.post after write: unexpected: ${POST_RESP:0:120}"
    FAIL=$(( FAIL + 1 ))
  fi
else
  red "echo > pet/.post: write was rejected (unexpected)"
  FAIL=$(( FAIL + 1 ))
fi

# ── Unmount and read-only re-mount ────────────────────────────────────────────
printf "\n── Read-only mode ──\n"
unmount_fuse "$MOUNT"
sleep 1

"$BINARY" \
  --spec "$SPEC" \
  --base-url "$BASE_URL" \
  --mount "$MOUNT" \
  --group-by path \
  --read-only &
RO_PID=$!
sleep 3

if ls "$MOUNT" &>/dev/null; then
  if printf '{}' > "$MOUNT/pet/.post" 2>/dev/null; then
    red "read-only: write to .post was NOT blocked (expected EPERM)"
    FAIL=$(( FAIL + 1 ))
  else
    green "read-only: write correctly blocked with EPERM"
    PASS=$(( PASS + 1 ))
  fi
  # reads should still work
  RO_HELP=$(cat "$MOUNT/pet/.help" 2>&1)
  check_contains "read-only: cat .help still works" "/pet" "$RO_HELP"
else
  red "read-only: mount not accessible"
  FAIL=$(( FAIL + 1 ))
fi
kill "$RO_PID" 2>/dev/null || true
unmount_fuse "$MOUNT"

# ── Summary ───────────────────────────────────────────────────────────────────
printf "\n────────────────────────────────────\n"
printf "Results: \033[32m%d passed\033[0m, \033[31m%d failed\033[0m\n" "$PASS" "$FAIL"
printf "────────────────────────────────────\n"

[ "$FAIL" -eq 0 ]
