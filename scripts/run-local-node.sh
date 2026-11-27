#!/usr/bin/env bash
# Запуск изолированной ноды GeoCoin для разработки Parts Registry.
# Использование: bash scripts/run-local-node.sh [load|test]
#
# Требуется собранный geocoin-node из основного репозитория GeoCoin
# (headless-сборка без Qt достаточна).
set -euo pipefail

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
GEOCOIN_NODE="${GEOCOIN_NODE:-$REPO_ROOT/../GeoCoin/out/build/linux-release-headless/geocoin-node}"

if [[ ! -x "$GEOCOIN_NODE" ]]; then
  echo "geocoin-node не найден: $GEOCOIN_NODE"
  echo "Соберите его в основном репозитории GeoCoin (headless-пресет) и задайте GEOCOIN_NODE."
  exit 1
fi

case "${1:-load}" in
  load)
    NETWORK="degree-load"
    MANIFEST="$REPO_ROOT/config/activation-manifest.testing-loadnet.json"
    ;;
  test)
    NETWORK="degree-test"
    MANIFEST="$REPO_ROOT/config/activation-manifest.testing-testnet.json"
    ;;
  *)
    echo "Неизвестная сеть: $1 (доступно: load, test)"
    exit 1
    ;;
esac

DATA_DIR="$REPO_ROOT/data/${1:-load}net"
mkdir -p "$DATA_DIR"

echo "Сеть:       $NETWORK"
echo "RPC:        $( [[ $NETWORK == degree-load ]] && echo 23033 || echo 13033 )"
echo "Каталог:    $DATA_DIR"
echo "Манифест:   $MANIFEST"
echo

exec "$GEOCOIN_NODE" \
  --network "$NETWORK" \
  --data-dir "$DATA_DIR" \
  --activation-manifest "$MANIFEST" \
  "$@"
