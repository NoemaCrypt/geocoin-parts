# backend — verification backend + mock-цепь (Фаза 0)

Публичный API проверки по `unit_id`, relay погашения, счётчик сканов,
гео-скор, Merkle-пруфы против `batch_root`.

## Запуск

```bash
# батч (если ещё не сгенерирован) — см. ../provisioning
go build -o ../../provisioning-out/backend ./cmd/backend
../../provisioning-out/backend \
  -batches ../../provisioning-out \
  -log ../../provisioning-out/scans.jsonl \
  -addr 127.0.0.1:8087
# web-checker: http://127.0.0.1:8087/
```

Либо полный цикл разом: `bash scripts/demo-cycle.sh` из корня репозитория.

## API

| Метод | Путь | Описание |
| --- | --- | --- |
| GET | `/api/unit/{id}` | статус, счётчик сканов, risk score, флаги |
| POST | `/api/scan` | `{unit_id, lat, lon, ts?, source?}` — фиксация скана |
| GET | `/api/unit/{id}/proof` | Merkle-прут против `batch_root` |
| POST | `/api/redeem` | `{unit_id, claim_code, vin?}` — погашение |

## Что mock, что прод

- Состояние батчей — в памяти, лог сканов — JSONL-файл. На проде — RocksDB
  сайдчейна и индексы, а `batch_root` якорится чекпоинтами в L1.
- Погашение проверяет `claim_code` против `claim_hash` и Merkle-принадлежность
  батчу — логика совпадает с будущей консенсусной (вариант «А»).
- Гео-скор — ортодромия на сфере; на проде заменить на GeographicLib WGS84.
- VIN-соль в демо фиксированная — на проде соль off-chain у backend завода.
