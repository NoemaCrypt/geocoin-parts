# Schema: com.geocoin.parts.unit / v1

Статус: DRAFT (фаза 0). Канонизация — deterministic CBOR (см.
`docs/B2B_FOUNDATION_RU.md`, раздел 3). Изменение смысла поля → новая версия
схемы; опубликованные версии не меняются.

## Поля

| Поле | Тип | Описание |
| --- | --- | --- |
| `unit_id` | bytes (32) | Публичный идентификатор единицы; в QR и в цепи |
| `claim_hash` | bytes (32) | `SHA-256("geocoin:parts:claim:v1" \|\| unit_id \|\| claim_code)`; единственное представление секрета |
| `batch_id` | bytes (32) | Партия минта; минт батча идёт через `batch_root` (корень Меркла по unit_id\|\|claim_hash), цель пилота — 1M единиц (~100 батчей по 10k) |
| `status` | enum | `draft, issued, in_transit, in_stock, redeemed, flagged, frozen, void` |
| `custodian` | bytes (32) | GEO-адрес текущего звена custody |
| `vin_hash` | bytes (32), optional | `SHA-256(VIN \|\| unit_salt)` или HMAC с ключом в HSM заводского backend; `unit_salt` — случайные 16 байт, хранятся **только off-chain** (claim_code не годится: он публикуется в witness при погашении) |
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
| `dealer_id` | string | Идентификатор дилера/склада |
| `country` | string (2) | ISO-3166 alpha-2 страны региона |
| `geo_hash` | bytes (32) | Хеш координат склада (координаты — off-chain) |
| `radius_km` | uint32 | Допустимый регион: радиус 150–300 км вокруг склада |
| `ts` | uint64 | Время передачи |

Геозона вместо точки: покупатель в соседнем городе не получает ложного
срабатывания; скан вне радиуса — custody_mismatch (жёлтый «серый импорт»),
совмещённый с гео-скоростью/аномальным счётчиком — custody_critical (красный,
рекомендация freeze). Реализация Фазы 1 — `services/backend` (runtime реестра,
состояние в `custody-state.json`), события включаются в state_root чекпоинтов.

## Валидатор (сайдчейн)

- `unit_id` уникален в сети.
- Переходы статуса: `issued→in_transit→in_stock→redeemed`;
  `any→flagged|frozen|void` — только по авторизации policy (freeze quorum).
- Burn: witness содержит `claim_code`, `SHA-256(code) == claim_hash`.
- Минт батча: подпись mint quorum, проверка Membership в `batch_root`,
  `batch_size ≤ MAX_BATCH` (цель ~10 000).
