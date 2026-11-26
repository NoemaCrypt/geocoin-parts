# Schema: com.geocoin.parts.unit / v1

Статус: DRAFT (фаза 0). Канонизация — deterministic CBOR (см.
`docs/B2B_FOUNDATION_RU.md`, раздел 3). Изменение смысла поля → новая версия
схемы; опубликованные версии не меняются.

## Поля

| Поле | Тип | Описание |
| --- | --- | --- |
| `unit_id` | bytes (32) | Публичный идентификатор единицы; в QR и в цепи |
| `claim_hash` | bytes (32) | `SHA-256("geocoin:parts:claim:v1" \|\| unit_id \|\| claim_code)`; единственное представление секрета |
| `batch_id` | bytes (32) | Партия минта (по одному на выпуск ОТК) |
| `status` | enum | `draft, issued, in_transit, in_stock, redeemed, flagged, frozen, void` |
| `custodian` | bytes (32) | GEO-адрес текущего звена custody |
| `vin_hash` | bytes (32), optional | Хеш VIN при погашении; раскрытие VIN — по запросу (open question) |
| `issued_at` | uint64 | Unix-время минта |
| `redeemed_at` | uint64, optional | Время погашения |

## Хеш метаданных

`metadata_hash = SHA-256("geocoin:b2b:metadata:v1" || network_id || tenant_id ||
object_type || schema_id || schema_version || canonical_payload)` — по формуле
из B2B_FOUNDATION. `schema_id = com.geocoin.parts.unit`.

## Схема custody-события: com.geocoin.parts.custody / v1

| Поле | Тип | Описание |
| --- | --- | --- |
| `unit_batch` | bytes (32) | unit_id или batch_id (передача партией) |
| `from_addr` | bytes (32) | Отправитель |
| `to_addr` | bytes (32) | Получатель |
| `geo_hash` | bytes (32) | Хеш гео-метки склада (координаты — off-chain) |
| `ts` | uint64 | Время передачи |

## Валидатор (сайдчейн)

- `unit_id` уникален в сети.
- Переходы статуса: `issued→in_transit→in_stock→redeemed`;
  `any→flagged|frozen|void` — только по авторизации policy (freeze quorum).
- Burn: witness содержит `claim_code`, `SHA-256(code) == claim_hash`.
- Минт батча: подпись mint quorum, `batch_size ≤ MAX_BATCH`.
