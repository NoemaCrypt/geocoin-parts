#!/usr/bin/env bash
# Демо полного цикла Фазы 0 НА РЕАЛЬНОЙ ИЗОЛИРОВАННОЙ НОДЕ degree-load:
#   батч 1000 единиц → учётный сайдчейн в L1 (genesis, LOCK_DEPOSIT,
#   REGISTER_ACCOUNTING) → batch_root закреплён чекпоинтом →
#   скан (Минск) → гео-аномалия (Алматы, 2 ч) → погашение →
#   погашение якорится очередным чекпоинтом в L1.
#
# Требует: scripts/bootstrap-node.sh уже выполнен (нода, ключ, miner).
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
N="${1:-1000}"
export PATH="$HOME/.local/go/bin:$PATH"
API="http://127.0.0.1:8087"
SIGNER="$ROOT/tools/falcon-signer/falcon-signer"

cd "$ROOT"
# нода/кошелёк/ключ/singer — идемпотентный bootstrap
bash "$ROOT/scripts/bootstrap-node.sh" 2>&1 | tail -3
[[ -x "$SIGNER" ]] || { echo "falcon-signer не собран"; exit 1; }
[[ -f data/node-key ]] || { echo "нет data/node-key"; exit 1; }

# 1. Тестовый батч (если нет)
BATCH_DIR="$ROOT/provisioning-out/batch-0001"
if [[ ! -f "$BATCH_DIR/batch.json" ]]; then
  echo "== Генерация тестового батча ($N единиц) =="
  (cd services/provisioning && go run ./cmd/provision -n "$N" -out "$BATCH_DIR" -label "PORSCHE-977-PILOT")
fi

# 2. Backend с якорением (mock-цепь остаётся для мгновенных проверок)
echo "== Сборка backend =="
(cd services/backend && go build -o "$ROOT/provisioning-out/backend" ./cmd/backend)
pkill -x backend 2>/dev/null || true
rm -f "$ROOT/provisioning-out/scans.jsonl"
echo "== Запуск backend с якорением в L1 =="
"$ROOT/provisioning-out/backend" -anchor \
  -batches "$ROOT/provisioning-out" \
  -log "$ROOT/provisioning-out/scans.jsonl" \
  -rpc-pass-file "$ROOT/data/rpc-admin-pass" \
  -node-key-file "$ROOT/data/node-key" \
  -signer "$SIGNER" \
  -anchor-state "$ROOT/provisioning-out/anchor-state.json" \
  -addr 127.0.0.1:8087 >"$ROOT/provisioning-out/backend.log" 2>&1 &
BACK_PID=$!
trap 'kill $BACK_PID 2>/dev/null || true' EXIT
for i in $(seq 1 50); do curl -sf "$API/api/unit/0000" >/dev/null 2>&1 && break || sleep 0.5; done
grep -E "genesis|LOCK|REGISTER|чекпоинт|batch_root" "$ROOT/provisioning-out/backend.log" | head -6

# 3. Первая единица батча
UNIT=$(sed -n '2p' "$BATCH_DIR/secrets.tsv" | cut -f1)
CODE=$(sed -n '2p' "$BATCH_DIR/secrets.tsv" | cut -f2)
JQ() { python3 -c "import sys,json;d=json.load(sys.stdin);print(d$1)"; }

echo
echo "== Тестовая единица =="
echo "  unit_id:    $UNIT"
echo "  claim_code: $CODE"
UNIT2=$(sed -n '3p' "$BATCH_DIR/secrets.tsv" | cut -f1)
echo "  unit_id2 (сценарий custody): $UNIT2"

echo
echo "== Шаг 0: custody: Завод (Вайссах) → Официальный дилер Минск (геозона 300 км, BY) =="
curl -s "$API/api/custody/transfer" -H 'Content-Type: application/json' \
  -d '{"label":"PORSCHE-977-PILOT","from":"factory-weissach","zone":{"dealer_id":"dealer-minsk-01","name":"Официальный дилер Минск","country":"BY","lat":53.9,"lon":27.56,"radius_km":300}}' | JQ '["holder"]'

echo
echo "== Шаг 1: скан в Минске (у дилера, внутри геозоны) =="
curl -s "$API/api/scan" -H 'Content-Type: application/json' \
  -d "{\"unit_id\":\"$UNIT\",\"lat\":53.9,\"lon\":27.56,\"ts\":$(( $(date +%s) - 7200 ))}" | JQ '["message"]'

echo
echo "== Шаг 1b: скан в Борисове (~105 км, внутри зоны 300 км) — без ложного срабатывания =="
curl -s "$API/api/scan" -H 'Content-Type: application/json' \
  -d "{\"unit_id\":\"$UNIT\",\"lat\":53.05,\"lon\":28.37,\"ts\":$(( $(date +%s) - 3600 ))}" | JQ '["message"]'

echo
echo "== Шаг 2: клонированная этикетка в Алматы через 2 ч (вне зоны + гео-скор → красный) =="
curl -s "$API/api/scan" -H 'Content-Type: application/json' \
  -d "{\"unit_id\":\"$UNIT\",\"lat\":43.24,\"lon\":76.92}" | tee /tmp/geo.json | JQ '["message"]'
echo "  risk_score: $(cat /tmp/geo.json | JQ '["risk_score"]'), flags: $(cat /tmp/geo.json | JQ '["flags"]')"

echo
echo "== Шаг 2b: одиночный скан в Алматы другой единицы — только custody-mismatch (жёлтый) =="
curl -s "$API/api/scan" -H 'Content-Type: application/json' \
  -d "{\"unit_id\":\"$UNIT2\",\"lat\":43.24,\"lon\":76.92}" | tee /tmp/yellow.json | JQ '["message"]'
echo "  risk_score: $(cat /tmp/yellow.json | JQ '["risk_score"]'), flags: $(cat /tmp/yellow.json | JQ '["flags"]')"

echo
echo "== Шаг 3: погашение мастером =="
curl -s "$API/api/redeem" -H 'Content-Type: application/json' \
  -d "{\"unit_id\":\"$UNIT\",\"claim_code\":\"$CODE\",\"vin\":\"WP0ZZZ99ZTS392125\"}" | JQ '["message"]'
sleep 3
grep -E "чекпоинт L1|anchor deferred" "$ROOT/provisioning-out/backend.log" | tail -2

echo
echo "== Шаг 4: клоны видят красный экран =="
curl -s -o /tmp/clone.json -w "  HTTP %{http_code}\n" "$API/api/redeem" -H 'Content-Type: application/json' \
  -d "{\"unit_id\":\"$UNIT\",\"claim_code\":\"$CODE\"}"
cat /tmp/clone.json | JQ '["message"]'
curl -s "$API/api/unit/$UNIT" | JQ '["message"]'

echo
echo "== Шаг 5: custody-событие и погашение — чекпоинтом в L1 (retry-цикл) =="
for i in $(seq 1 40); do
  grep -q "чекпоинт L1" "$ROOT/provisioning-out/backend.log" && break
  sleep 1
done
grep -E "чекпоинт L1" "$ROOT/provisioning-out/backend.log" | tail -2
SCID=$(python3 -c "import json;print(json.load(open('$ROOT/provisioning-out/anchor-state.json'))['genesis_hash'])")
curl -sS -u "admin:$(cat data/rpc-admin-pass)" -H 'Content-Type: application/json' \
  --data '{"jsonrpc":"2.0","id":1,"method":"getsidechaingenesis","params":["'"$SCID"'"]}' \
  http://127.0.0.1:23033/ | python3 -c "
import sys,json
d=json.load(sys.stdin)['result']
print('  sidechain:', d['sidechain_kind'], d['status'], 'namespace:', d['namespace'])
print('  schema_hash:', d['schema_hash'][:16]+'...')"
echo "  anchor-state: $(cat "$ROOT/provisioning-out/anchor-state.json" | tr -d '\n ')"

echo
echo "Демо завершено. Web-checker: http://127.0.0.1:8087/"
