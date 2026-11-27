#!/usr/bin/env bash
# Mainnet-клиент GeoCoin для Parts Registry (B2B-якорение).
#
# Нода работает как КЛИЕНТ сети degree: синхронизация, RPC, кошелёк.
# Без майнинга. Пиры — публичный bootstrap-эндпоинт из deploy-документации
# GeoCoin (45.12.114.4:3030). Софт GeoCoin не модифицируется.
#
# Использование:
#   bash scripts/mainnet-client.sh start          # запустить ноду
#   bash scripts/mainnet-client.sh status         # высота, пинги, peers
#   bash scripts/mainnet-client.sh address        # адрес owner-кошелька (для пополнения)
#   bash scripts/mainnet-client.sh balance        # баланс адреса
#   bash scripts/mainnet-client.sh export-key     # privkey → data/mainnet-key (для backend -anchor)
#   bash scripts/mainnet-client.sh stop
set -euo pipefail

ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
# GeoCoin checkout рядом с этим репозиторием (кроссплатформенный дефолт)
GEOCOIN_REPO="${GEOCOIN_REPO:-$ROOT/../GeoCoin}"
NODE_BIN="${GEOCOIN_NODE:-$GEOCOIN_REPO/out/build/linux-release-gui-gpu/geocoin-node}"
ACT_MANIFEST="${GEOCOIN_MANIFEST:-$GEOCOIN_REPO/config/activation-manifest.production.json}"

DATA="$ROOT/data/mainnet"
RPC_PORT=3033
RPC_USER=admin
RPC_PASS_FILE="$DATA/rpc-admin-pass"
WALLET_PASS_FILE="$DATA/wallet-pass"
# Публичный bootstrap-узел mainnet (docs/production-mainnet.md в репо GeoCoin)
ADDNODE="${GEOCOIN_ADDNODE:-45.12.114.4:3030}"

RPC() { curl -sS -u "$RPC_USER:$(cat "$RPC_PASS_FILE")" -H 'Content-Type: application/json' --data "$1" "http://127.0.0.1:$RPC_PORT/"; }

ensure_secrets() {
  mkdir -p "$DATA"
  if [[ ! -s "$RPC_PASS_FILE" ]]; then
    openssl rand -hex 24 > "$RPC_PASS_FILE"; chmod 600 "$RPC_PASS_FILE"
  fi
  if [[ ! -s "$WALLET_PASS_FILE" ]]; then
    openssl rand -hex 16 > "$WALLET_PASS_FILE"; chmod 600 "$WALLET_PASS_FILE"
  fi
}

ensure_address() {
  # ровно один owner-адрес: создаётся один раз и фиксируется в data/mainnet-addr
  if [[ -s "$DATA/mainnet-addr" ]]; then
    cat "$DATA/mainnet-addr"
    return
  fi
  local a
  a=$(RPC '{"jsonrpc":"2.0","id":1,"method":"getnewaddress","params":{}}' | python3 -c "import sys,json;print(json.load(sys.stdin)['result'])" 2>/dev/null || true)
  if [[ -n "$a" ]]; then
    echo "$a" > "$DATA/mainnet-addr"; chmod 600 "$DATA/mainnet-addr"
  fi
  echo "$a"
}

case "${1:-}" in
  start)
    ensure_secrets
    if pgrep -f "geocoin-node.*mainnet" >/dev/null 2>&1; then
      echo "нода mainnet уже запущена ($(pgrep -f 'geocoin-node.*mainnet' | head -1))"
    else
      echo "== Запуск mainnet-клиента (degree) =="
      nohup "$NODE_BIN" \
        --network degree \
        --data-dir "$DATA" \
        --activation-manifest "$ACT_MANIFEST" \
        --rpc --rpc-port "$RPC_PORT" --rpc-user "$RPC_USER" --rpc-pass-file "$RPC_PASS_FILE" \
        --addnode "$ADDNODE" \
        --wallet-password-file "$WALLET_PASS_FILE" \
        --status-interval 60 \
        > "$DATA/node.log" 2>&1 &
      echo "нода: PID $!"
    fi
    for i in $(seq 1 120); do
      RPC '{"jsonrpc":"2.0","id":1,"method":"getblockcount","params":{}}' >/dev/null 2>&1 && break
      sleep 0.5
    done
    "$0" status
    ;;
  status)
    echo "высота:      $(RPC '{"jsonrpc":"2.0","id":1,"method":"getblockcount","params":{}}' | python3 -c "import sys,json;print(json.load(sys.stdin)['result'])")"
    echo "peers:       $(RPC '{"jsonrpc":"2.0","id":1,"method":"getpeerinfo","params":{}}' | python3 -c "import sys,json;print(len(json.load(sys.stdin)['result']))")"
    echo "адрес:       $(ensure_address)"
    ;;
  address)
    a=$(ensure_address)
    [[ -z "$a" ]] && { echo "кошелёк не готов"; exit 1; }
    echo "$a"
    ;;
  balance)
    a=$(ensure_address)
    RPC '{"jsonrpc":"2.0","id":1,"method":"getaddressbalance","params":["'"$a"'"]}' | python3 -m json.tool
    ;;
  export-key)
    a=$(ensure_address)
    RPC '{"jsonrpc":"2.0","id":1,"method":"dumpprivkey","params":["'"$a"'","'"$(cat "$WALLET_PASS_FILE")"'"]}' \
      | python3 -c "import sys,json;print(json.load(sys.stdin)['result'])" > "$ROOT/data/mainnet-key"
    chmod 600 "$ROOT/data/mainnet-key"
    echo "privkey сохранён: data/mainnet-key (chmod 600) — для backend: -node-key-file data/mainnet-key"
    ;;
  stop)
    pkill -f "geocoin-node.*mainnet" && echo "остановлено" || echo "не запущена"
    ;;
  *)
    grep '^#   bash' "$0" | sed 's/^#   //'
    ;;
esac
