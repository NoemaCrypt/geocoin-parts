# Памятка о ходе работ над GeoCoin Parts (реестр автозапчастей)

Последнее обновление: 2026-11-27

## Цель

MVP системы защиты автозапчастей от подделки на базе GeoCoin: модель «банкноты»
(UTXO / one-time claim), публичная проверка коробки, гео-аномалии, chain of
custody на учётном сайдчейне. Разработка изолирована от основного репозитория
L1 (`~/Рабочий стол/GeoCoin`) — работающий mainnet не затрагивается.

## Исходная точка

- Приватный репозиторий `NoemaCrypt/geocoin-parts` (SSH, аккаунт `matbar14`).
- План MVP: `docs/PARTS_REGISTRY_MVP_RU.md`; принятые решения — раздел 7
  (двойной формат кода, VIN только хеш с off-chain солью, backend завода,
  доказательный якорь, пилот Porsche 1 000 000 единиц).
- Схемы: `schemas/parts-unit-v1.md` (`com.geocoin.parts.unit/v1`, custody-события,
  правила валидатора, минт батча через `batch_root`).
- Инструменты: Go 1.23.4 (`~/.local/go/bin`), ссылка на ноду из основного репозитория.

## Выполнено

- Создан приватный репозиторий, перенесены план и ссылочные документы
  (`B2B_FOUNDATION_RU.md`, `BLOCKCHAIN_CAPABILITIES_RU.md`), манифесты
  изолированных сетей в `config/`, каркас сервисов.
- Фаза 0 (коммит `5be8b0d`, Go):
  - `libs/merkle` — общий модуль Merkle-дерева: потоковая редукция уровней через
    файлы (bufio), canonical-паринг, офлайн-проверка пруфов; тесты на
    n = 1…1000, включая нечётные уровни (найден и исправлен баг «EOF на хвосте»).
  - `services/provisioning/cmd/provision` — потоковый генератор батчей:
    CSPRNG unit_id + 128-битный секрет, `claim_hash =
    SHA-256("geocoin:parts:claim:v1" || unit_id || code)`, Base58-код с
    группировкой по 6 без 0/O/I/l, QR-payload, экспорт `units.csv` /
    `secrets.tsv` / `batch.json`.
  - `services/backend` — verification backend + mock-цепь:
    - проверка погашения совпадает с будущей консенсусной: witness `claim_code`
      против `claim_hash` + Merkle-принадлежность `batch_root`;
    - счётчик сканов (уникальные ячейки ~1 км за 24 ч; сканы дилера не
      завышают), гео-скор 900 км/ч (ортодромия; на проде GeographicLib WGS84),
      JSONL-лог append-only;
    - API: `GET /api/unit/{id}`, `POST /api/scan`, `GET /api/unit/{id}/proof`,
      `POST /api/redeem`; web-checker вшит в бинарник (go:embed).
  - `scripts/demo-cycle.sh` — полный цикл на тестовом батче 1000 единиц:
    скан (Минск) → гео-аномалия (Алматы, 2 ч; risk 50, flag `geo_velocity`) →
    погашение с VIN-хешем → клоны получают HTTP 409 «Код уже был погашен».
    Цикл проверен, репозиторий запушен.
- Найденные и закрытые дефекты: Base58 регистрочувствителен (ранний ToUpper в
  redeem ломал код); web-checker переехал в `internal/api/web` (go:embed);
  детерминизм порядка листьев при якорении батча (LeafOrder из units.csv).

## Текущий этап: (a) привязка backend к изолированной ноде degree-load — ГОТОВО

- Нода: собранный бинарник основного репозитория
  (`out/build/linux-release-gui-gpu/geocoin-node`), сеть `degree-load`
  (RPC 23033), каталог `data/loadnet`, майнинг `setgenerate`. Mainnet не
  затрагивается; сборка L1 используется только на чтение.
- Подписи: `tools/falcon-signer` (C++), линкуется к готовым статическим
  библиотекам L1 (`libgeocoin_core/crypto/...`), исходники L1 не модифицируются.
  Режимы: `payload` (recoverable message, для finalize...), `tx` (полная
  обработка: sidechain_signature → затем witness входов — порядок критичен,
  т.к. hash входа включает sidechain_signature), `addr`, `roundtrip`.
- Проверенный вручную конвейер учётного сайдчейна (policy 3/1):
  `createsidechaingenesis` (sidechain_kind=accounting, schema_hash в genesis
  v2) → `LOCK_DEPOSIT` (fee = input − amount − change; обязательно наличие
  network fee, иначе fee-too-low; owner + witness) →
  `REGISTER_ACCOUNTING_SIDECHAIN` (namespace глобально уникален — в dev
  добавляется суффикс; депозит должен подтвердиться до регистрации) →
  чекпоинты ТОЛЬКО через `createbatchsidechaincheckpointpayload` (одиночный
  SIDECHAIN_CHECKPOINT — легаси и отклоняется) с `proof_hex` SCA1 от
  `createaccountingcheckpointproofv1`; `checkpoint_height` — внутренний счётчик
  сайдчейна (1,2,3…), НЕ высота L1; интервал ≥ 5 блоков L1; fee из депозита
  (5000 angles); высота обязана расти (дубликат отклонён — проверено).
- Интеграция в backend (`services/backend/internal/node`):
  - `-anchor` режим: EnsureRegistered (идемпотентно, состояние в
    `anchor-state.json`) + якорение batch_root чекпоинтом #1;
  - погашения копятся в очередь; `POST /api/anchor` и фоновый retry (10 c)
    якорят их чекпоинтом: state_root =
    SHA-256("geocoin:parts:state:v1" || batch_root || N || SHA-256(последнего
    погашения));
  - предфлайт `testmempoolaccept` перед отправкой LOCK.
- `scripts/bootstrap-node.sh` — нода + кошелёк + майнинг + ключ владельца
  (`data/node-key`) + сборка falcon-signer; `scripts/demo-node-cycle.sh` —
  полный цикл на реальной ноде: батч → genesis/LOCK/REGISTER → чекпоинт
  batch_root → скан → гео-аномалия → погашение → чекпоинт погашения.
  Проверено: чекпоинты приняты консенсусом, `anchor-state.next_height` растёт.

## Текущий этап: (б) custody-mismatch — ГОТОВО

- Модель геозон (`services/backend/internal/chain/custody.go`): дилер/склад —
  не точка, а допустимый регион (радиус 150–300 км + ISO-3166). История
  custody по партиям и unit-override, персистентность `custody-state.json`.
- API: `POST /api/custody/transfer` (batch по batch_id/label или unit) →
  custody-событие добавляется в state_root чекпоинта L1
  ("geocoin:parts:custody:v1"); `GET /api/unit/{id}` отдаёт путь детали
  (Завод → дилер, страны, время) и зону текущего владельца.
- Risk engine (`internal/risk`): скан вне радиуса зоны custodian →
  `custody_mismatch` (жёлтый, +30: «серый импорт», с указанием дилера и
  страны); mismatch + geo_velocity/аномальный счётчик → `custody_critical`
  (красный, +60, рекомендация freeze). Сканы дилера (source=dealer) не
  проверяются против собственной зоны.
- Демо (`scripts/demo-node-cycle.sh`) дополнен: перевод партии
  «Завод Вайссах → Официальный дилер Минск» (300 км, BY); сканы Минск и
  Борисов (~105 км) — без ложных срабатываний; Алматы с гео-скор → красный
  (risk 100, custody_critical); одиночный скан Алматы другой единицы →
  жёлтый (risk 30, custody_mismatch); custody-событие + погашение якорятся
  одним чекпоинтом L1. Проверено.

## Текущий этап: (в) тиражный профиль печати — ГОТОВО (проект закрыт)

- `services/labelgen` (go, единственная внешняя зависимость — boombuler/qr):
  печатные листы A4 из каталога батча provisioning. Этикетка 90×54 мм, сетка
  2×5; выход: векторный PDF (base-14 шрифты, QR прямоугольниками — печать
  любой плотности), постраничные SVG, preview.html и print-manifest.csv
  (лист/позиция ↔ unit ↔ код, 0600, уничтожается после печати).
- На этикетке: шапка (бренд/партия/публичный серийник), открытый QR с URL
  проверки (`-public-url`, по умолчанию https://check.geocoin.parts/u/{unit}),
  скретч-зона (пунктирная рамка) с QR инсталляционного кода и текстовым
  дублем Base58 по 6 символов, подпись «PUBLIC · verify».
- Манифест включает batch_root — лист привязан к партии на-chain.
- Проверено: 24 этикетки из batch-0001 → 3 листа A4, PDF структурно валиден,
  превью в браузере — компоненты на местах (см. скриншот в memo).
- `scripts/demo-print.sh` — обёртка для тиража (batch_dir, n).

## Итог пилота (все три этапа закрыты)

1. (a) Якорение в реальную ноду degree-load: учётный сайдчейн, чекпоинты
   батчей и погашений с SCA1-доказательствами, falcon-подпись.
2. (б) Custody-цепочка: геозоны дилеров (радиус + ISO-страна), переходы
   «Завод → Дилер», риск-уровни custody_mismatch/custody_critical, путь
   детали в web-checker, события в state_root чекпоинтов L1.
3. (в) Тиражный профиль печати: labelgen (PDF/SVG, открытый QR + скретч-зона
   с кодом установки), print-manifest для сверки тиража.

## Дальше (когда решите продолжать)

Порядок — по практической ценности для пилота:

1. **Пилотная типография**: выдать labelgen на реальный тираж (рулонная печать,
   скретч-лента), согласовать домен проверки и заменить
   `https://check.geocoin.parts/u/{unit}` на боевой URL.
2. **Кошелёк кассира / мобильное приложение (Android, фаза 2)**: скан QR
   покупателем и кассиром, погашение с подписью устройства, оффлайн-очередь.
3. **Freeze-кворум**: реакция на `custody_critical` — приостановка проверки
   партии по решению завода + разбор инцидента (серый импорт/подделка).
4. **География продакшн-уровня**: GeographicLib WGS84 вместо ортодромии;
   параметризация геозон (регионы, таможенные союзы).
5. **RocksDB-состояние backend**: батчи/погашения/custody вместо in-memory +
   JSON (пор进去 к 1 000 000 единиц и пиковой нагрузке проверки).
6. **Фаза 2–3 по плану MVP**: учётный сайдчейн с комитетом и депозитами
   (operations quorum вместо dev-маркера), полная цепочка custody в
   консенсусе сайдчейна, VIN-соль в HSM завода.

## Ограничения

- Sidechain-роутинг (учётный сайдчейн с комитетом и депозитами) — фаза 2–3 по
  плану; сейчас якорение в L1 напрямую из изолированной ноды.
- Гео-скор — сфера; замена на GeographicLib WGS84 в фазе 1.
- Состояние батчей в backend — в памяти (тестовые объёмы); на проде RocksDB.
- Соль VIN в демо фиксированная — на проде off-chain у backend завода.
- Custody Фазы 1 — в runtime backend (dev: JSON-файл); на проде — таблица
  custody в RocksDB сайдчейна + operations quorum вместо dev-маркера.
- Геозона — евклидов радиус по ортодромии; городские структуры (EPA-регионы,
  таможенные союзы) — параметризация в фазе 3.
