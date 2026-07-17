#!/bin/bash
# setup-client.sh — Zero-login client setup for Kiro CLI Pool Proxy.
#
# Bản chất của pool: máy khách KHÔNG cần login. Nó chỉ cần:
#   1) trỏ endpoint kiro-cli vào proxy
#   2) có 1 token "giả" trong data.sqlite3 để qua cổng login local
# Proxy sẽ ghi đè Authorization bằng account thật trong pool → chat chạy bình thường.
#
# Verified (RE kiro-cli 2.12.2): cổng login chỉ check sự tồn tại của
# `kirocli:odic:token` chưa hết hạn trong bảng `auth_kv`; access/refresh token
# KHÔNG cần thật vì proxy swap. expiry xa (2099) nên CLI không refresh.
#
# Usage:  ./setup-client.sh http://PROXY_IP:9999 [region]
# Reset:  ./setup-client.sh --reset
set -euo pipefail

KIRO="${KIRO_CLI:-kiro-cli}"
DB="${HOME}/.local/share/kiro-cli/data.sqlite3"

command -v "$KIRO" >/dev/null 2>&1 || { echo "❌ kiro-cli not found (set KIRO_CLI=/path)"; exit 1; }

if [ "${1:-}" == "--reset" ]; then
    "$KIRO" settings -d api.krs.service 2>/dev/null || true
    "$KIRO" settings -d api.cps.service 2>/dev/null || true
    "$KIRO" settings -d api.codewhisperer.service 2>/dev/null || true
    echo "✅ Endpoints reset. (Token giả trong auth_kv vẫn còn — chạy 'kiro-cli login' nếu muốn dùng account thật.)"
    exit 0
fi

PROXY="${1:-}"
REGION="${2:-us-east-1}"
if [ -z "$PROXY" ]; then
    echo "Usage: $0 http://PROXY_IP:9999 [region]"; echo "       $0 --reset"; exit 1
fi

VAL="{\"endpoint\":\"$PROXY\",\"region\":\"$REGION\"}"

echo "[1/3] Trỏ kiro-cli vào proxy: $PROXY (region=$REGION)"
"$KIRO" settings api.krs.service "$VAL"           # chat / assistant
"$KIRO" settings api.cps.service "$VAL"           # profiles / usage limits
"$KIRO" settings api.codewhisperer.service "$VAL" # telemetry (optional)

# Chạy 1 lệnh kiro-cli để chắc chắn data.sqlite3 + migrations (bảng auth_kv) tồn tại.
"$KIRO" settings api.krs.service >/dev/null 2>&1 || true
if [ ! -f "$DB" ]; then
    echo "❌ Không tìm thấy $DB (kiro-cli chưa khởi tạo). Chạy 'kiro-cli settings all' rồi thử lại."; exit 1
fi

echo "[2/3] Seed token giả vào auth_kv (bỏ qua login)"
TOKEN_JSON='{"access_token":"POOL_PLACEHOLDER","refresh_token":"POOL_PLACEHOLDER","expires_at":"2099-01-01T00:00:00Z","region":"'"$REGION"'","start_url":"https://pool.local/start","scopes":["codewhisperer:completions","codewhisperer:analysis","codewhisperer:conversations"],"oauth_flow":"DeviceCode"}'
REG_JSON='{"client_id":"pool","client_secret":"pool","client_secret_expires_at":"2099-01-01T00:00:00Z","region":"'"$REGION"'","scopes":["codewhisperer:completions","codewhisperer:analysis","codewhisperer:conversations"],"oauth_flow":"DeviceCode"}'

seed_with_python() {
    python3 - "$DB" "$TOKEN_JSON" "$REG_JSON" <<'PY'
import sqlite3,sys
db,tok,reg=sys.argv[1],sys.argv[2],sys.argv[3]
c=sqlite3.connect(db)
c.execute("insert or replace into auth_kv(key,value) values(?,?)",("kirocli:odic:token",tok))
c.execute("insert or replace into auth_kv(key,value) values(?,?)",("kirocli:odic:device-registration",reg))
c.commit(); print("   seeded:",[r[0] for r in c.execute("select key from auth_kv")])
PY
}
seed_with_sqlite3() {
    sqlite3 "$DB" "INSERT OR REPLACE INTO auth_kv(key,value) VALUES('kirocli:odic:token','$(printf '%s' "$TOKEN_JSON" | sed "s/'/''/g")');"
    sqlite3 "$DB" "INSERT OR REPLACE INTO auth_kv(key,value) VALUES('kirocli:odic:device-registration','$(printf '%s' "$REG_JSON" | sed "s/'/''/g")');"
    echo "   seeded via sqlite3"
}
if command -v python3 >/dev/null 2>&1; then seed_with_python
elif command -v sqlite3 >/dev/null 2>&1; then seed_with_sqlite3
else echo "❌ Cần python3 hoặc sqlite3 để seed token."; exit 1; fi

echo "[3/3] Kiểm tra proxy: $PROXY/health"
if command -v curl >/dev/null 2>&1; then
    printf '   -> '; curl -s --max-time 5 "$PROXY/health" || echo "(không tới được proxy — kiểm tra firewall/IP)"; echo
fi

echo ""
echo "✅ Xong. Máy khách KHÔNG cần login. Chạy:"
echo "     kiro-cli chat"
echo "   Mọi request đi qua pool proxy (account rotation + credit accounting)."
echo "   Khôi phục: $0 --reset"
