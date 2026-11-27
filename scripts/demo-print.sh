#!/usr/bin/env bash
# Демо тиражного профиля печати: этикетки для первых 24 единиц батча.
# Использование: bash scripts/demo-print.sh [batch_dir] [n]
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
BATCH="${1:-$ROOT/provisioning-out/batch-0001}"
N="${2:-24}"
OUT="$ROOT/provisioning-out/labels-$(basename "$BATCH")"

echo "== labelgen: $N этикеток из $(basename "$BATCH") =="
(cd "$ROOT/services/labelgen" && go run ./cmd/labelgen \
  -batch "$BATCH" -out "$OUT" -limit "$N")

echo
echo "Готово: $OUT/labels.pdf, $OUT/sheet-*.svg, $OUT/print-manifest.csv"
echo "Просмотр: xdg-open $OUT/preview.html"
echo "ВАЖНО: print-manifest.csv содержит сырые коды — уничтожить после печати и наклейки скретч-слоя."
