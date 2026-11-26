#!/usr/bin/env bash
# Демо полного цикла Фазы 0:
#   генерация тестового батча (1000 единиц) → mock-цепь →
#   скан (Минск) → гео-аномалия (Алматы, 2 ч) → погашение по claim_code +
#   Merkle-пруфу → попытка клона отклоняется → «красный экран».
#
# Использование: bash scripts/demo-cycle.sh [1000]
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
N="${1:-1000}"
export PATH="$HOME/.local/go/bin:$PATH"
API="http://127.0.0.1:8087"

cd "$ROOT"

# 1. Генерация тестового батча (если ещё нет)
BATCH_DIR="$ROOT/provisioning-out/batch-0001"
if [[ ! -f "$BATCH_DIR/batch.json" ]]; then
  echo "== Генерация тестового батча ($N единиц) =="
  (cd services/provisioning && go run ./cmd/provision -n "$N" -out "$BATCH_DIR" -label "PORSCHE-977-PILOT")
fi

# 2. Сборка и запуск backend
echo "== Сборка backend =="
(cd services/backend && go build -o "$ROOT/provisioning-out/backend" ./cmd/backend)

echo "== Запуск verification backend (mock-цепь) =="
rm -f "$ROOT/provisioning-out/scans.jsonl"  # чистый лог сканов для воспроизводимости демо"
"$ROOT/provisioning-out/backend" \
  -batches "$ROOT/provisioning-out" \
  -log "$ROOT/provisioning-out/scans.jsonl" \
  -addr 127.0.0.1:8087 >"$ROOT/provisioning-out/backend.log" 2>&1 &
BACK_PID=$!
trap 'kill $BACK_PID 2>/dev/null || true' EXIT
for i in $(seq 1 20); do curl -sf "$API/api/unit/$(head -c 64 /dev/zero | tr '\0' 'a')" >/dev/null 2>&1 && break || sleep 0.2; done

# 3. Первая единица из батча (unit + секрет — как на этикетке)
UNIT=$(sed -n '2p' "$BATCH_DIR/secrets.tsv" | cut -f1)
CODE=$(sed -n '2p' "$BATCH_DIR/secrets.tsv" | cut -f2)
JQ() { python3 -c "import sys,json;d=json.load(sys.stdin);print(d$1)"; }

echo
echo "== Тестовая единица =="
echo "  unit_id:    $UNIT"
echo "  claim_code: $CODE"

echo
echo "== Шаг 1: скан в Минске (публичный QR, до покупки) =="
curl -s "$API/api/scan" -H 'Content-Type: application/json' \
  -d "{\"unit_id\":\"$UNIT\",\"lat\":53.9,\"lon\":27.56,\"ts\":$(( $(date +%s) - 7200 ))}" \
  | JQ '["message"]'

echo
echo "== Шаг 2: клонированная этикетка отсканирована в Алматы через 2 ч =="
curl -s "$API/api/scan" -H 'Content-Type: application/json' \
  -d "{\"unit_id\":\"$UNIT\",\"lat\":43.24,\"lon\":76.92}" | tee /tmp/geo.json | JQ '["message"]'
echo "  risk_score: $(cat /tmp/geo.json | JQ '["risk_score"]'), flags: $(cat /tmp/geo.json | JQ '["flags"]')"

echo
echo "== Шаг 3: покупатель проверяет до покупки — видит предупреждение =="
curl -s "$API/api/unit/$UNIT" | JQ '["message"]'

echo
echo "== Шаг 4: мастер вскрывает упаковку и погашает деталь =="
VIN_HASH=$(python3 -c "import hashlib;print(hashlib.sha256(b'geocoin:parts:vinsalt:v0:WP0ZZZ99ZTS392125').hexdigest())")
curl -s "$API/api/redeem" -H 'Content-Type: application/json' \
  -d "{\"unit_id\":\"$UNIT\",\"claim_code\":\"$CODE\",\"vin\":\"$VIN_HASH\"}" \
  | JQ '["message"]'

echo
echo "== Шаг 5: 99 клонов при сканировании видят красный экран =="
curl -s -o /tmp/clone.json -w "  HTTP %{http_code}\n" "$API/api/redeem" -H 'Content-Type: application/json' \
  -d "{\"unit_id\":\"$UNIT\",\"claim_code\":\"$CODE\"}"
cat /tmp/clone.json | JQ '["message"]'
curl -s "$API/api/unit/$UNIT" | JQ '["message"]'

echo
echo "== Шаг 6: Merkle-прут единицы против batch_root =="
PROOF=$(curl -s "$API/api/unit/$UNIT/proof")
echo "$PROOF" | python3 -c "import sys,json;d=json.load(sys.stdin);print('  batch_root:',d['batch_root']);print('  прут:',len(d['proof']),'узлов')"

echo
echo "Демо завершено. Для web-интерфейса запустите:"
echo "  $ROOT/provisioning-out/backend -batches $ROOT/provisioning-out -log $ROOT/provisioning-out/scans.jsonl -addr 127.0.0.1:8087"
echo "  затем откройте http://127.0.0.1:8087/ и введите unit_id $UNIT"
