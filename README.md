# GeoCoin Parts — реестр автозапчастей «банкнота» (UTXO / One-Time Claim)

Разработка системы защиты автозапчастей от подделки на базе GeoCoin.
Этот репозиторий изолирован от основного репозитория L1 (`GeoCoin`) и
эксплуатирует **только изолированные сети** (`degree-load`, `degree-test`) со
своими каталогами данных — работающий mainnet не затрагивается.

## Содержание

| Путь | Назначение |
| --- | --- |
| `docs/PARTS_REGISTRY_MVP_RU.md` | План MVP: модель «банкноты», компоненты, фазы, угрозы |
| `docs/B2B_FOUNDATION_RU.md` | Контракт B2B-продуктов с L1 (policy, metadata hash, scope) |
| `docs/BLOCKCHAIN_CAPABILITIES_RU.md` | Возможности L1: NFT, сайдчейны, мультиподписи |
| `config/` | Activation-манифесты для изолированных сетей (копия из GeoCoin) |
| `schemas/` | Схемы метаданных (`com.geocoin.parts.*`, canonical CBOR / RFC 8785) |
| `services/provisioning/` | Генерация unit_id + claim_code, экспорт этикеток |
| `services/verification-backend/` | Публичный API проверки, relay burn-транзакций |
| `services/risk-engine/` | Счётчик сканов, гео-скор (GeographicLib), risk score |
| `web-checker/` | PWA проверки коробки до покупки |

## Локальная изолированная нода

Узел запускается из основного репозитория GeoCoin (сборка см. там), но с
отдельным каталогом данных и манифестом из этого репозитория:

```bash
# бинарник после сборки в GeoCoin:
# out/build/linux-release-headless/geocoin-node
GEOCOIN_NODE=~/Рабочий\ стол/GeoCoin/out/build/linux-release-headless/geocoin-node \
  bash scripts/run-local-node.sh load
```

Сеть `load` (`degree-load`) полностью изолирована: собственный P2P-порт 23030,
RPC 23033, собственный каталог данных `./data/loadnet`. Никакого соединения с
mainnet `degree`.

## Статус

Фаза 0 плана MVP: schema registry + генератор батчей + макет web-проверки.

Пилот: **Porsche, 1 000 000 единиц**; backend эксплуатирует завод; выпуск на
учётном сайдчейне; VIN — только хеш с off-chain солью. Принятые решения — см.
раздел 7 плана (`docs/PARTS_REGISTRY_MVP_RU.md`).
