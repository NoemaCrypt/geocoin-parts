# GeoCoin Parts — реестр автозапчастей «банкнота» (UTXO / One-Time Claim)

Система защиты автозапчастей от подделки на базе GeoCoin. Этот репозиторий
изолирован от основного репозитория L1 (`GeoCoin`) и эксплуатирует **только
изолированные сети** (`degree-load`, `degree-test`) со своими каталогами
данных — работающий mainnet не затрагивается.

## Модель

- **Секретные коды никогда не попадают в блокчейн**: на-цепи только
  `claim_hash = SHA-256("geocoin:parts:claim:v1" || unit_id || code)`.
- Каждая деталь — «банкнота»: публичный QR на упаковке (проверка до покупки)
  + секретный код под скретч-слоем (установка на авто, необратимое погашение).
- Клоны этикетки выявляются мгновенно: повторное погашение → 409; сканы
  клона с чужой геолокацией → гео-скор и аномальный счётчик.
- Минт партии через `batch_root` (Merkle), чекпоинты батчей/погашений/
  custody-событий якорятся в L1 через учётный сайдчейн.

## Содержание

| Путь | Назначение |
| --- | --- |
| `docs/PARTS_REGISTRY_MVP_RU.md` | План MVP: модель, компоненты, фазы, угрозы |
| `docs/B2B_FOUNDATION_RU.md` | Контракт B2B-продуктов с L1 (policy, metadata hash, scope) |
| `docs/BLOCKCHAIN_CAPABILITIES_RU.md` | Возможности L1: NFT, сайдчейны, мультиподписи |
| `schemas/` | Схемы метаданных (`com.geocoin.parts.unit/v1`, custody/v1, canonical CBOR) |
| `libs/merkle/` | Общий Merkle-модуль: потоковая редукция, canonical-паринг, пруфы, тесты |
| `services/provisioning/` | Потоковый генератор батчей: unit_id + claim_code (Base58), batch_root |
| `services/backend/` | Verification backend: API проверки/сканов/погашения, risk engine, custody, якорение в L1, web-checker |
| `services/labelgen/` | Тиражный профиль печати: этикетки PDF/SVG (открытый QR + скретч-зона), print-manifest |
| `tools/falcon-signer/` | C++ подпись Falcon-512 для транзакций L1 (линкуется к стат. библиотекам GeoCoin) |
| `scripts/` | Bootstrap ноды, демо-циклы, печать этикеток |
| `config/` | Activation-манифесты изолированных сетей |

## Быстрый старт

```bash
# Go 1.23: ~/.local/go/bin
bash scripts/bootstrap-node.sh        # нода degree-load + кошелёк + falcon-signer
bash scripts/demo-node-cycle.sh 1000  # полный цикл на реальной изолированной ноде:
#   генерация батча → genesis учётного сайдчейна → LOCK_DEPOSIT →
#   REGISTER_ACCOUNTING_SIDECHAIN → чекпоинт batch_root → custody
#   «Завод → Дилер Минск» → сканы (Минск/Борисов/Алматы) →
#   погашение → чекпоинт → проверка клонов
bash scripts/demo-cycle.sh 1000       # то же на mock-цепи, без ноды
bash scripts/demo-print.sh            # тираж этикеток (PDF/SVG) из батча
```

Web-checker: `http://127.0.0.1:8087/` — проверка детали, путь детали
(Завод → дилер), вердикты (оригинал / серый импорт / критическая аномалия /
уже погашена), панель погашения кодом под скретчем. Кнопка «Демо-скан»
появляется только при запуске backend с `-demo-scan` (стенды).

## API backend (кратко)

| Метод | Путь | Описание |
| --- | --- | --- |
| GET | `/api/unit/{id}` | статус, счётчик сканов, risk score, флаги, custody-путь |
| POST | `/api/scan` | фиксация скана `{unit_id, lat, lon, ts?, source?}` |
| POST | `/api/redeem` | погашение `{unit_id, claim_code, vin?}` |
| POST | `/api/custody/transfer` | передача партии «Завод → Дилер» с геозоной |
| GET | `/api/unit/{id}/proof` | Merkle-пруф против `batch_root` |

Risk engine: гео-скор 900 км/ч, счётчик уникальных ячеек ~1 км за 24 ч,
custody-геозоны дилеров (радиус 150–300 км + ISO-3166):
`custody_mismatch` (жёлтый, серый импорт) и `custody_critical` (красный,
совмещён с признаками клонирования → рекомендация freeze).

## Изолированная нода

Сеть `load` (`degree-load`) полностью изолирована: собственный P2P-порт
23030, RPC 23033, каталог данных `./data/loadnet`. Никакого соединения с
mainnet `degree`. Бинарник ноды и статические библиотеки берутся из основного
репозитория (`GEOCOIN_REPO`, по умолчанию `~/Рабочий стол/GeoCoin`) только
на чтение.

## Статус

Пилот MVP **закрыт** (см. `PROGRESS_MEMO.md`):

1. **(a)** Якорение в реальную ноду degree-load: учётный сайдчейн
   (genesis → LOCK_DEPOSIT → REGISTER), чекпоинты батчей/погашений с
   SCA1-доказательствами, falcon-подпись транзакций.
2. **(б)** Custody-цепочка: геозоны дилеров, переходы «Завод → Дилер»,
   риск-уровни custody_mismatch/custody_critical, custody-события в
   state_root чекпоинтов L1.
3. **(в)** Тиражный профиль печати: labelgen (векторный PDF/SVG, открытый QR
   + скретч-зона с кодом установки), print-manifest для сверки тиража.

Пилот: **Porsche, 1 000 000 единиц**; backend эксплуатирует завод; VIN —
только хеш с off-chain солью. Следующие шаги — раздел «Дальше» в
`PROGRESS_MEMO.md` (типография, Android-кошелёк, freeze-кворум, RocksDB,
защита `/api/scan`).
