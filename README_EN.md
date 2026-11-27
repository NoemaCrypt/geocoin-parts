# GeoCoin Parts — auto-parts registry "banknote" model (UTXO / One-Time Claim)

[🇷🇺 Русский](README.md) · **English**

An anti-counterfeit system for auto parts built on GeoCoin. This repository is
isolated from the main L1 repository (`GeoCoin`) and uses **only isolated
networks** (`degree-load`, `degree-test`) with their own data directories —
the working mainnet is never touched.

## Model

- **Secret codes never reach the blockchain**: only
  `claim_hash = SHA-256("geocoin:parts:claim:v1" || unit_id || code)` is on-chain.
- Every part is a "banknote": a public QR on the packaging (pre-purchase
  verification) + a secret code under a scratch layer (vehicle installation,
  irreversible redemption).
- Label clones are detected instantly: a second redemption → 409; clone scans
  from a foreign location → geo-velocity and abnormal scan counter.
- Batch minting via `batch_root` (Merkle); checkpoints of batches,
  redemptions and custody events are anchored to L1 through an accounting
  sidechain.

## Layout

| Path | Purpose |
| --- | --- |
| `docs/PARTS_REGISTRY_MVP_RU.md` | MVP plan: model, components, phases, threats (Russian) |
| `schemas/` | Metadata schemas (`com.geocoin.parts.unit/v1`, custody/v1, canonical CBOR) |
| `libs/merkle/` | Shared Merkle module: streaming reduction, canonical pairing, proofs, tests |
| `services/provisioning/` | Streaming batch generator: unit_id + claim_code (Base58), batch_root |
| `services/backend/` | Verification backend: check/scan/redeem API, risk engine, custody, L1 anchoring, web checker |
| `services/labelgen/` | Print profile: PDF/SVG labels (public QR + scratch zone), print manifest |
| `tools/falcon-signer/` | C++ Falcon-512 signer for L1 transactions (links to GeoCoin static libs) |
| `scripts/` | Node bootstrap, demo cycles, label printing |
| `config/` | Activation manifests for isolated networks |

## Quick start

```bash
# Go 1.23: ~/.local/go/bin
bash scripts/bootstrap-node.sh        # degree-load node + wallet + falcon-signer
bash scripts/demo-node-cycle.sh 1000  # full cycle on a real isolated node:
#   batch generation → accounting sidechain genesis → LOCK_DEPOSIT →
#   REGISTER_ACCOUNTING_SIDECHAIN → batch_root checkpoint → custody
#   "Factory → Minsk dealer" → scans (Minsk/Borisov/Almaty) →
#   redemption → checkpoint → clone verification
bash scripts/demo-cycle.sh 1000       # same on the mock chain, no node needed
bash scripts/demo-print.sh            # label print run (PDF/SVG) from a batch
```

Web checker: `http://127.0.0.1:8087/` — part verification, part journey
(Factory → dealer), verdicts (genuine / grey import / critical anomaly /
already redeemed), scratch-code redemption panel. The "Demo scan" button
appears only when the backend is started with `-demo-scan` (demo stands).
The checker is multilingual (RU / EN / DE / ES / ZH), same switcher as the
GeoCoin website; the choice is remembered in `localStorage`.

## Backend API (summary)

| Method | Path | Description |
| --- | --- | --- |
| GET | `/api/unit/{id}` | status, scan counter, risk score, flags, custody path |
| POST | `/api/scan` | record a scan `{unit_id, lat, lon, ts?, source?}` |
| POST | `/api/redeem` | redemption `{unit_id, claim_code, vin?}` |
| POST | `/api/custody/transfer` | batch transfer "Factory → Dealer" with a geofence |
| GET | `/api/unit/{id}/proof` | Merkle proof against `batch_root` |

Risk engine: geo-velocity 900 km/h, counter of unique ~1 km cells in 24 h,
dealer custody geofences (radius 150–300 km + ISO-3166 country):
`custody_mismatch` (yellow, grey import) and `custody_critical` (red,
combined with cloning signs → freeze recommendation).

## Isolated node

The `load` (`degree-load`) network is fully isolated: its own P2P port 23030,
RPC 23033, data directory `./data/loadnet`. No connection to the mainnet
`degree`. The node binary and static libraries are taken from the main
repository (`GEOCOIN_REPO`, default `~/Рабочий стол/GeoCoin`) read-only.

## Status

The MVP pilot is **closed** (see `PROGRESS_MEMO.md`, Russian):

1. **(a)** Anchoring to a real degree-load node: accounting sidechain
   (genesis → LOCK_DEPOSIT → REGISTER), batch/redemption checkpoints with
   SCA1 proofs, falcon-signed transactions.
2. **(б)** Custody chain: dealer geofences, "Factory → Dealer" transfers,
   custody_mismatch/custody_critical risk levels, custody events in the
   state_root of L1 checkpoints.
3. **(в)** Print profile: labelgen (vector PDF/SVG, public QR + scratch zone
   with the install code), print manifest for the print run.

Pilot: **Porsche, 1,000,000 units**; the backend is operated by the factory;
VIN — hash only with an off-chain salt. Next steps — the "Дальше" section in
`PROGRESS_MEMO.md` (print partner, Android wallet, freeze quorum, RocksDB,
`/api/scan` protection).
