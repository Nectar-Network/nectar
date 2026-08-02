# Nectar Sandbox — self-owned Blend testnet environment

Deploys a complete, Nectar-controlled Blend V2 stack on testnet so we can
force genuine underwater positions on demand (Task A4). Everything runs
through the pinned blend-utils clone (`reference/blend-utils` @ `b05242d`,
see docs/FACTS.md) — its compiled `lib/` + shipped wasms are the exact code
that deployed the canonical TestnetV2 environment.

Why a full stack instead of a pool on the canonical factory: a pool can only
be set Active (borrowing enabled) when its backstop meets the 100k threshold
(`execute_set_pool_status`, blend-contracts-v2 `pool/src/pool/status.rs:81-90`
@ ba22b48 — no admin bypass), and the canonical backstop's LP token requires
BLND we cannot mint (FACTS.md: no BLND faucet). In our own stack we issue
BLND/backstop-USDC ourselves, so the threshold is met honestly with real
comet LP.

What stays REAL vs what we control:
- Pool reserves: **Circle testnet USDC** (`CBIELTK6…` — the vault's actual
  settlement asset) + native XLM SAC. Same code paths, same asset the keeper
  and vault use. No fake USDC in the capital path.
- Oracle: blend-utils' `oraclemock` wasm (same wasm hash as the canonical
  testnet oracle), admin-settable prices — that is the insolvency lever.
- Backstop economics: comet LP over `BLND:ADMIN` / `USDC:ADMIN` classic
  assets we issue. Clearly testnet-only; never touches the vault.

## Prereqs
- `reference/blend-utils` cloned at `b05242d`, `npm i && npm run build` done.
- `reference/blend-utils/.env` with RPC_URL, FRIENDBOT_URL,
  NETWORK_PASSPHRASE, ADMIN (deployer secret), WHALE (backstop funder
  secret), BORROWER (test borrower secret). Never commit secrets.

## Scripts (run in order via ./run.sh)
- `01-deploy-sandbox.mjs` — SACs (BLND/USDC:ADMIN), comet factory + comet
  (80/20 BLND:USDC), mock oracle (prices USDC=1.00, XLM=0.42), emitter +
  backstop + pool factory, pool "Nectar Sandbox" (Circle-USDC + XLM
  reserves), backstop seed (50,001 LP), setStatus(0).
- `02-borrower.mjs` — admin supplies 40 Circle-USDC liquidity; borrower
  (friendbot-funded) adds a Circle-USDC trustline, supplies 100 XLM
  collateral, borrows 20 Circle-USDC. At XLM=$0.42: HF ≈ 1.50.
- `03-set-price.mjs <xlm_price>` — admin-sets the mock oracle XLM price.
  `0.15` → borrower HF ≈ 0.53 (underwater); `0.42` restores health.

Usage: `./run.sh 01` / `./run.sh 02` / `./run.sh 03 0.15`

All transaction hashes are printed by blend-utils' invoke helpers; capture
stdout into docs/evidence/.
