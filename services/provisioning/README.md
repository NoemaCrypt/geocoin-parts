# provisioning

Генерация батчей единиц (фаза 0 — реализована).

## Запуск

```bash
go run ./cmd/provision -n 1000 -out ../../provisioning-out/batch-0001 \
  -label "PORSCHE-977-PILOT"
```

Тестовый батч: 1000 единиц (хватает с запасом для mock-цепи и полного цикла
скан → гео-аномалия → погашение по Merkle-пруфу).

## Обязательства:
- `unit_id` и `claim_code` — CSPRNG, `claim_code` ≥ 128 бит энтропии.
- Экспорт: открытый QR (`unit_id`) + secret под скретчем.
- В базах и в сети — только `claim_hash`; сырые коды существуют один раз
  в момент печати этикеток (`provisioning-out/` в .gitignore).
- Минт батча на сайдчейн — по RPC ноды (scripts/run-local-node.sh).
