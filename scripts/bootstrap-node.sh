#!/usr/bin/env bash
# Bootstrap изолированной ноды degree-load для якорения Parts Registry.
#
# 1. Запускает geocoin-node (основной репозиторий, сборка не трогается).
# 2. Создаёт кошелёк, включает майнинг, даёт монетам дозреть.
# 3. Готовит ключ владельца (data/node-key) и falcon-signer.
# 4. Регистрирует учётный сайдчейн parts-units через backend -anchor.
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# GeoCoin checkout рядом с этим репозиторием (кроссплатформенный дефолт)
GEOCOIN_REPO="${GEOCOIN_REPO:-$ROOT/../GeoCoin}"
NODE_BIN="${GEOCOIN_NODE:-$GEOCOIN_REPO/out/build/linux-release-gui-gpu/geocoin-node}"
export PATH="$HOME/.local/go/bin:$PATH"

cd "$ROOT"
mkdir -p data

# --- falcon-signer (сборка из готовых библиотек L1, исходники не меняются) ---
if [[ ! -x tools/falcon-signer/falcon-signer ]]; then
  echo "== Сборка falcon-signer =="
  BLD="$GEOCOIN_REPO/out/build/linux-release-gui-gpu"
  g++ -std=c++20 -I"$GEOCOIN_REPO/include" -I"$BLD" -O2 \
    tools/falcon-signer/falcon-signer.cpp -o tools/falcon-signer/falcon-signer \
    -L"$BLD" -lgeocoin_core -lgeocoin_geographiclib -lgeocoin_crypto -lgeocoin_consensus \
    -lcrypto -lpthread
fi

# --- нода ---
if ! pgrep -f 'geocoin-node --network degree-load' >/dev/null 2>&1 && \
   ! pgrep -f 'geocoin-node.*loadnet' >/dev/null 2>&1; then
  echo "== Запуск ноды degree-load =="
  echo "dev-load-admin-pw" > data/rpc-admin-pass
  echo "dev-load-miner-pw" > data/rpc-miner-pass
  echo "dev-wallet-pw"    > data/wallet-pass
  chmod 600 data/rpc-admin-pass data/rpc-miner-pass data/wallet-pass
  nohup "$NODE_BIN" \
    --network degree-load \
    --data-dir "$ROOT/data/loadnet" \
    --activation-manifest "$ROOT/config/activation-manifest.testing-loadnet.json" \
    --rpc --rpc-user admin --rpc-pass-file "$ROOT/data/rpc-admin-pass" \
    --rpc-miner-user miner --rpc-miner-pass-file "$ROOT/data/rpc-miner-pass" \
    --wallet-password-file "$ROOT/data/wallet-pass" \
    > "$ROOT/data/loadnet/node.log" 2>&1 &
  echo "нода: PID $!"
  for i in $(seq 1 60); do
    RPC '{"jsonrpc":"2.0","id":1,"method":"getblockcount","params":{}}' >/dev/null 2>&1 && break
    sleep 0.5
  done
fi

RPC() { curl -sS -u "admin:dev-load-admin-pw" -H 'Content-Type: application/json' --data "$1" http://127.0.0.1:23033/; }
# дождаться RPC (нода могла ещё подниматься)
for i in $(seq 1 60); do
  RPC '{"jsonrpc":"2.0","id":1,"method":"getblockcount","params":{}}' >/dev/null 2>&1 && break
  [[ $i == 60 ]] && { echo "RPC ноды недоступен"; exit 1; }
  sleep 0.5
done

echo "== Кошелёк и майнинг =="
RPC '{"jsonrpc":"2.0","id":1,"method":"getnewaddress","params":{}}' >/dev/null 2>&1 || true
RPC '{"jsonrpc":"2.0","id":2,"method":"setgenerate","params":[true,4]}' >/dev/null 2>&1 || true

# --- ключ владельца: первый адрес кошелька ---
ADDR=$(RPC '{"jsonrpc":"2.0","id":1,"method":"listaddressbalances","params":{}}' | python3 -c "import sys,json;d=json.load(sys.stdin)['result'];print(list(d.keys())[0] if d else '')")
if [[ -z "$ADDR" ]]; then
  echo "кошелёк пуст — дождитесь монет и перезапустите скрипт"
  exit 1
fi
PK=$(RPC '{"jsonrpc":"2.0","id":2,"method":"dumpprivkey","params":["'"$ADDR"'","dev-wallet-pw"]}' | python3 -c "import sys,json;print(json.load(sys.stdin)['result'])")
echo "$PK" > data/node-key
chmod 600 data/node-key
echo "владелец/комитет: $ADDR"

echo
echo "Дальше запустите backend с якорением:"
echo "  ./provisioning-out/backend -anchor -batches provisioning-out \\"
echo "    -signer tools/falcon-signer/falcon-signer -addr :8087"
echo "Демо полного цикла: bash scripts/demo-node-cycle.sh"
