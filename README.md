# Nectar Network

Multi-operator keeper infrastructure for Soroban DeFi. Distributed liquidation network for Blend Protocol on Stellar — no single point of failure.

**Live:** [nectarnetwork.fun](https://nectarnetwork.fun) · [Docs](https://docs.nectar.monster) · [Keeper SDK](https://github.com/Nectar-Network/keeper-sdk) · [Twitter](https://x.com/nectar_xlm) · [GitHub](https://github.com/Nectar-Network/nectar)

## The Problem

On Feb 22, 2026, a USTRY/XLM oracle manipulation drained **$10.8M** from a Blend pool. Two pre-positioned single-operator bots captured nearly all of it — 60 auction fills over 4 hours, one Docker container, one keypair, no fallback. The rest of Stellar DeFi (~$187M TVL) had no coordinated response.

Nectar replaces single-bot liquidation systems with a distributed network of competing keepers, funded by a shared vault.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  SOROBAN TESTNET                                                │
│                                                                 │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────┐  │
│  │  KeeperRegistry  │  │   NectarVault    │  │ Blend Pool V2 │  │
│  │  register()+stake│  │   deposit()      │  │ get_positions()│ │
│  │  slash()         │  │   withdraw()     │  │ new_auction() │  │
│  │  get_keepers()   │  │   draw()         │  │ get_auction() │  │
│  │  record_execution│  │   return_proceeds│  │ submit()      │  │
│  └──────────────────┘  └──────────────────┘  └──────────────┘  │
│           ↑                    ↑                    ↑           │
└───────────┼────────────────────┼────────────────────┼───────────┘
            │                    │                    │
     ┌──────┴────────────────────┴────────────────────┴──────┐
     │  OFF-CHAIN (Railway)                                   │
     │                                                        │
     │  ┌────────────────────┐  ┌────────────────────┐       │
     │  │  Keeper Alpha      │  │  Keeper Beta       │       │
     │  │  monitor → detect  │  │  monitor → detect  │       │
     │  │  → draw → fill     │  │  → draw → fill     │       │
     │  │  → return proceeds │  │  → return proceeds │       │
     │  └────────────────────┘  └────────────────────┘       │
     └────────────────────────────────────────────────────────┘
            │
     ┌──────┴──────────────────────────────────────────────┐
     │  FRONTEND (Vercel) — nectarnetwork.fun              │
     │  Next.js 14 · SSE live stream · REST polling        │
     └─────────────────────────────────────────────────────┘
```

### Liquidation Flow

1. **Discover** — Each keeper maintains a persistent, event-driven borrower index (no configured address list); every cycle it reads only the pool events since its last ledger
2. **Detect** — Tracked borrowers are health-checked via `get_positions`; when HF < 1.0 the keeper creates a Dutch auction on-chain, with the liquidation percent chosen by read-only simulation (the pool enforces a post-liquidation HF band)
3. **Wait** — The keeper waits on the verified two-phase price curve (400 ledgers, fair point at t=200) until lot/bid ≥ `MIN_PROFIT`
4. **Draw** — Keeper draws USDC capital from the NectarVault, sized from the auction's actual debt legs
5. **Fill** — ONE atomic `submit([fill, repay…, withdraw_collateral…])`: either the keeper ends holding real tokens and no debt, or nothing happened. First confirmed transaction wins the race
6. **Return** — Seized collateral is swapped to USDC (oracle-anchored slippage floor) and the MEASURED proceeds are returned to the vault; depositors' shares appreciate by the realized profit
7. **Compete** — The losing keeper detects the already-filled auction and rolls its draw back — no capital lost

## Live Testnet Deployment

| Service | URL |
|---------|-----|
| Frontend | [nectarnetwork.fun](https://nectarnetwork.fun) |
| Keeper Alpha API | [keeper-alpha-production.up.railway.app](https://keeper-alpha-production.up.railway.app) |
| Keeper Beta API | [keeper-beta-production.up.railway.app](https://keeper-beta-production.up.railway.app) |

Both keepers run on Railway from `keeper/Dockerfile`. `./scripts/railway-keeper-env.sh keeper-alpha` (and `… keeper-beta`) pushes the current contract IDs into a service's env, then `railway up` redeploys it.

### On-Chain Contracts (Soroban Testnet)

Tranche 3 hardened deploy settling in **Circle testnet USDC**, 2026-07-22 (see [wallets.md](wallets.md) for the full address book, including the deprecated-deployment archive). These contracts ship the staking + slashing + performance-tracking + cap/cooldown surface with the VLT-1..6 and NEW-cap/reconcile/drain fixes and atomic `__constructor` init.

| Contract | Address | Explorer |
|----------|---------|----------|
| KeeperRegistry | `CD33A7IGNCOLVQ4EEINBVMVA7IHWXGN57R6YLE5AJEEKPA6VKC2E4IQD` | [View](https://stellar.expert/explorer/testnet/contract/CD33A7IGNCOLVQ4EEINBVMVA7IHWXGN57R6YLE5AJEEKPA6VKC2E4IQD) |
| NectarVault | `CDOGQY7NAE3BP4Q7RWBCBLW23Z36RNWNDNXX5DWNIEVMFEWP3GVEPXLR` | [View](https://stellar.expert/explorer/testnet/contract/CDOGQY7NAE3BP4Q7RWBCBLW23Z36RNWNDNXX5DWNIEVMFEWP3GVEPXLR) |
| USDC (Circle testnet SAC) | `CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA` | [View](https://stellar.expert/explorer/testnet/contract/CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA) |
| Nectar Sandbox pool (our Blend V2 stack — active fills) | `CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V` | [View](https://stellar.expert/explorer/testnet/contract/CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V) |
| Blend testnet pool V2 (official — monitor-only) | `CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF` | [View](https://stellar.expert/explorer/testnet/contract/CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF) |
| Soroswap router | `CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD` | [View](https://stellar.expert/explorer/testnet/contract/CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD) |

Blend's official TestnetV2 pool (from [blend-utils/testnet.contracts.json](https://github.com/blend-capital/blend-utils/blob/main/testnet.contracts.json)) settles Blend's mock USDC — a **different asset** from the vault's Circle USDC — so the keeper runs it in `monitor` mode; active fills run on the Nectar Sandbox, whose reserves are the real vault asset (docs/FACTS.md "Decisions"; docs/evidence/a4-sandbox.md). On mainnet the split disappears: Blend settles the same Circle USDC the vault does.

### Testnet configuration + proven results

- **Vault config**: deposit cap 10M USDC, withdraw cooldown 3600s (1h), max draw 10k USDC/keeper
- **Registry config**: 100 USDC min stake, slash timeout 3600s, slash rate 1000 bps (10%)
- **Keepers**: keeper-alpha registered with 100 Circle USDC staked on-chain (beta/gamma re-register as testnet USDC is faucet-ed)
- **Proven live full cycle** (2026-08-09, Nectar Sandbox): one liquidation moved the share price 1.0000000 → 1.0432672 — auction create, vault draw, atomic fill+repay+withdraw, collateral swap, measured proceeds returned, every step tx-hashed ([docs/evidence/b-full-cycle.md](docs/evidence/b-full-cycle.md))
- **Profit model**: profit = measured USDC proceeds − drawn capital, credited to depositors via share-price appreciation on `return_proceeds`. (Bad-debt fills are operator-float-funded and their profit accrues to the operator — see [docs/CORRECTION-REPORT.md](docs/CORRECTION-REPORT.md))

## Tranche 1 Status

Each Tranche 1 deliverable below cites the on-chain code + tests that prove the measurement criteria. Run `cargo test --workspace` (90 contract tests) and `cd keeper && go test ./...` to reproduce locally. A full re-review of these deliverables against the corrected code (2026-08-15) is in [docs/tranche-notes/t1-re-review-2026-08-15.md](docs/tranche-notes/t1-re-review-2026-08-15.md).

### 1. KeeperRegistry v1 — Staking & Performance Tracking ✓

**Status: code complete, on-chain proof from `./scripts/tranche-1-e2e.sh`**

- **Staking enforced on-chain**: `register()` pulls `min_stake` USDC from the operator via SAC `transfer` ([contracts/keeper-registry/src/lib.rs:76-86](contracts/keeper-registry/src/lib.rs#L76-L86)). Registration fails with `InsufficientStake` (#7) when `min_stake = 0`. Tests: `test_register_with_stake`, `test_register_insufficient_stake`, `test_register_zero_min_stake_rejected`.
- **Performance metrics on-chain**: `KeeperInfo` carries `total_executions`, `successful_fills`, `total_profit`, `total_response_time_ms`, `response_count` ([types.rs:5-18](contracts/keeper-registry/src/types.rs#L5-L18)). `avg_response_time_ms(operator)` returns the per-keeper average ([lib.rs:212](contracts/keeper-registry/src/lib.rs#L212)). Recorded by `record_execution()` invoked from `vault.return_proceeds()`.
- **Slashing**: `slash(keeper)` ([lib.rs:331-370](contracts/keeper-registry/src/lib.rs#L331-L370)) transfers `slash_rate_bps` of stake to the vault when `now - last_draw_time > slash_timeout`, and deactivates the slashed keeper (it must re-stake to draw again). Tests: `test_slash_after_timeout`, `test_slash_before_timeout_fails`, `test_slash_without_active_draw_fails`.

### 2. NectarVault v1 — Production Deposit/Withdraw ✓

**Status: code complete, on-chain proof from `./scripts/tranche-1-e2e.sh`**

- **Deposit cap**: `deposit()` rejects with `DepositCapExceeded` (#8) when `state.total_usdc + amount > cfg.deposit_cap` ([contracts/nectar-vault/src/lib.rs:86-87](contracts/nectar-vault/src/lib.rs#L86-L87)). Tests: `test_deposit_exceeds_cap`, `test_deposit_at_exact_cap`, `test_deposit_cap_with_existing_balance`.
- **Withdraw cooldown**: `withdraw()` rejects with `WithdrawalCooldown` (#9) when `now - depositor.last_deposit_time < cfg.withdraw_cooldown` ([lib.rs:154-155](contracts/nectar-vault/src/lib.rs#L154-L155)). Tests: `test_withdraw_before_cooldown`, `test_withdraw_after_cooldown`, `test_cooldown_resets_on_new_deposit`.
- **Share-price hardening at 7-decimal precision**: virtual-offset conversion (VLT-1 inflation-attack defense) floors toward zero on both deposit (caller gets ≤ fair shares, [lib.rs:90-97](contracts/nectar-vault/src/lib.rs#L90-L97)) and withdraw (caller gets ≤ fair USDC, clamped so `total_usdc` can never underflow, [lib.rs:167-179](contracts/nectar-vault/src/lib.rs#L167-L179)). Zero-share deposits rejected. Tests: `test_share_math_first_deposit`, `test_share_math_large_amounts`, `test_share_math_tiny_amounts`, `test_share_math_with_profit`, `test_share_rounding_bounded`, `test_inflation_attack_unprofitable`, `test_multiple_depositors_proportional_shares`, `test_multiple_depositors_proportional_with_profit`, `test_withdraw_with_zero_shares_fails`, `test_withdraw_more_than_owned_fails`, `test_withdraw_more_than_available_fails`. **47 vault tests total — more than the 10+ measurement asked for.**

### 3. Blend Liquidation Adapter — Auction Integration (scope below) ✓

**Status: user-liquidation fills proven live on testnet; bad-debt fills implemented (fill-and-hold); interest auctions detected and deferred**

- **Per-auction-type scope, matching the verified mechanics** (docs/FACTS.md "Auction asset flows"):
  - **User liquidation (type 0, request_type 6) — FULL**: atomic `submit([fill, repay…, withdraw_collateral…])` ([keeper/blend/submit.go](keeper/blend/submit.go)), size chosen by on-chain simulation, collateral swapped to USDC, proceeds returned to the vault. Proven live (docs/evidence/b-full-cycle.md).
  - **Bad debt (type 1, request_type 7) — FILL-AND-HOLD**: the keeper creates the backstop's auction (settle-asset legs only), fills it atomically with the debt repay from its OWN float (never vault draws), and HOLDS the backstop-LP lot, valued at the backstop's `token_spot_price` minus a configurable haircut (`BAD_DEBT_LP_HAIRCUT_BPS`, default 50%). Unwinding the LP is deferred: on mainnet it is one verified call into the comet's Circle-USDC leg costing ~0.24% at small size ([docs/evidence/c-lp-unwind.md](docs/evidence/c-lp-unwind.md)). **Bad-debt profit accrues to the operator, not to depositors** — `return_proceeds` rejects a keeper with no outstanding draw (the VLT-2 anti-donation guard), and these fills are float-funded, so crediting the vault would need a new contract entry point. See [keeper/blend/baddebt.go](keeper/blend/baddebt.go) and docs/FACTS.md Decisions.
  - **Interest (type 2, request_type 8) — DETECTED, DEFERRED**: the bid is backstop LP tokens (~120% of interest value at spot) the filler must pre-hold and pre-approve — a vault-USDC keeper carries no LP inventory, so the keeper logs "interest auction seen, deferred" and never attempts a fill.
- **Blend ABI compatibility**: the submit payload encodes `request_type` as `ScvU32` and `amount` as `ScvI128` to match Blend's `#[contracttype] struct Request { request_type: u32, address: Address, amount: i128 }` ([keeper/blend/submit.go:38-56](keeper/blend/submit.go#L38-L56)). Locked in by `TestSubmitPayload_BlendABITypes` and `TestBadDebtRequestABITypes`.
- **Dutch auction profitability** on the verified TWO-PHASE curve (400 ledgers, docs/FACTS.md "Auction fill price curve"): phase 1 (t=0–200) lot scales 0→100% with bid at 100%; phase 2 (t=200–400) bid scales 100→0% with lot at 100%; fair point at t=200; past t=400 the bid is empty. Tests: `TestProfitability_Block0_LotZero`, `…Block200_FairPrice`, `…Block100_LotScaling`, `…Block300_BidScaling`, `…Block400_BidZero`, `…PastExpiry_StaysInfinite`, `TestPhaseAt_Boundaries`, `TestBadDebtProfitability_Curve`.
- **Retry wrapper** with exponential backoff (3 attempts, 2.0× backoff): classifies `sequence`, `resource_exhaust`, `timeout`, `tx_too_late` as retryable; `already filled`, `already registered`, `insufficient_balance`, `contract error` as non-retryable ([keeper/soroban/retry.go:25-67](keeper/soroban/retry.go#L25-L67)). Tests: `TestSubmitRetry_RetriesOnSequenceError`, `…RetriesOnResourceExhaust`, plus ambiguity-resolution coverage in `keeper/adapters/blend`.
- **`/api/state` carries response_time_ms** on each liquidation record ([keeper/main.go:25-33, 325-332](keeper/main.go#L25-L33)). On a successful fill the keeper measures `time.Since(drawStart).Milliseconds()` between `vault.draw` and `vault.return_proceeds`, populates the `response_time_ms` field on the appended `LiquidationRecord`, and forwards the same value on-chain via `vault.return_proceeds → registry.record_execution`.
- **Live Blend pools**: `BLEND_POOLS=CCEBVDYM…:monitor:CAQCFVLO…,CBUBTHATT…:active` — the official TestnetV2 pool is scanned in **monitor** mode (it settles Blend-mock USDC, not the vault's Circle USDC, so execution is disabled by the settle-asset guard), while active fills run against the **Nectar Sandbox**, our own Blend V2 stack whose reserves are the real vault asset and whose admin oracle provides on-demand insolvency (`scripts/nectar-sandbox/run.sh 03 0.15`). Every end-to-end result in `docs/evidence/` was produced there.

### What `LiquidationLab` is for

`contracts/liquidation-lab/` ([lib.rs](contracts/liquidation-lab/src/lib.rs)) is a Blend-ABI-compatible test pool used by the keeper's local integration tests. It is **not** required for the live deployment — the keeper points at real Blend pools via the `BLEND_POOLS` env var. LiquidationLab exists for hermetic CI / replayable demo scenarios where you control both sides of the auction.

## Tranche 2 Status

### 1. DEX Integration for Collateral Conversion ✓

Non-USDC collateral seized from auction fills is automatically converted to USDC before proceeds return to the vault ([keeper/dex/](keeper/dex/)).

- **Soroswap primary, Phoenix XYK fallback** — routers configured via `SOROSWAP_ROUTER` / `PHOENIX_ROUTER`; either venue can be disabled by leaving it empty.
- **Slippage enforced three ways**: an oracle-anchored floor before trading (a manipulated pool quote is rejected globally — no venue fallback), the on-chain `amount_out_min`, and post-trade measurement. Default `SLIPPAGE_BPS=100` (1%). Phoenix is quote-less, so it refuses to swap at all without an oracle reference.
- **Proceeds are measured, never synthesized** — output is the keeper's real USDC balance delta. Failed swaps hold the asset instead of booking phantom profit.
- **Swaps are never auto-retried** and a possibly-broadcast swap never falls back to the second venue — re-selling the same collateral is the failure mode this is built to avoid. The next cycle's stale-draw recovery (`get_keeper_draw` + `recoverStaleDraw`) reconciles any capital left outstanding.

### 2. Multi-Protocol Adapter Interface ✓

All protocol work runs through one minimal interface, [`adapters.ProtocolAdapter`](keeper/adapters/adapter.go) — `Name / GetTasks / Execute / EstimateCapital`.

- **Blend** is a thin adapter over the fully-tested core package ([keeper/adapters/blend/](keeper/adapters/blend/)).
- **DeFindex** rebalancing adapter as proof of extensibility ([keeper/adapters/defindex/](keeper/adapters/defindex/)): reads `fetch_total_managed_funds`, detects allocation drift vs target weights (`DEFINDEX_DRIFT_BPS`, default 5%), plans Unwind→Invest instructions capped to idle funds, pre-checks the on-chain RebalanceManager/Manager role, and submits a role-gated `rebalance`. It moves only the DeFindex vault's own funds — never Nectar capital.
- The keeper loop scans every adapter, then executes **across protocols in one priority order**; `EstimateCapital` gates tasks that exceed the vault's available capital.
- **[docs/ADAPTER-GUIDE.md](docs/ADAPTER-GUIDE.md)** documents the interface, encode/decode conventions, and the capital-safety rules for third-party adapter authors.

### 3. Monitoring Dashboard v2 ✓

Four routes under `/dashboard` (see the Frontend table below): network overview with a share-price/APY chart derived only from realized on-chain profit (short windows render as cumulative return, never a fabricated annualized figure), a keeper leaderboard read from the on-chain registry (sortable by executions, win rate, avg response, stake, profit), a real-time liquidation feed with fill-tx explorer links and keeper attribution, and per-depositor analytics with clearly-labeled cost-basis estimates.

### 4. Keeper SDK & Operator Documentation — complete

Extracted into a standalone, `go get`-able module at [github.com/Nectar-Network/keeper-sdk](https://github.com/Nectar-Network/keeper-sdk): the `adapters.ProtocolAdapter` interface plus the `soroban`/`vault`/`dex`/`registry`/`blend` packages, a reference Blend adapter, runnable examples, and operator setup/strategy/risk docs. Hardened for funds-safety — non-retryable post-send transaction classification (no double-draw/double-fill), oracle-priced + overflow-guarded draw sizing, and structured `Error(Contract, #N)` matching — with CI and a race-clean test suite. Publishes on merge of the release PR plus a `v0.1.0` tag (the Go module proxy indexes on first fetch). Full operator guides live at [docs.nectar.monster](https://docs.nectar.monster).

## Repository Structure

```
nectar/
├── contracts/
│   ├── keeper-registry/      # Soroban (Rust) — operator registration + stake + slash, 26 tests
│   ├── nectar-vault/         # Soroban (Rust) — USDC vault + LP shares + cap + cooldown, 37 tests
│   ├── liquidation-lab/      # Soroban (Rust) — Blend-compatible test pool, 12 tests
│   └── mock-token/           # Soroban (Rust) — admin-mint mock USDC SAC, 5 tests
├── keeper/                   # Go 1.22 — keeper binary
│   ├── main.go               # Entry point, HTTP API, SSE, keeper loop
│   ├── config.go             # Env config with validation
│   ├── soroban/              # Soroban JSON-RPC client + tx assembly
│   ├── adapters/             # Generic ProtocolAdapter + blend/ + defindex/ adapters
│   ├── dex/                  # Collateral→USDC swaps (Soroswap primary, Phoenix fallback)
│   ├── blend/                # Pool, positions, auction (Blend-compatible)
│   ├── vault/                # Vault draw/return/balance queries + stale-draw recovery
│   └── registry/             # Keeper register/check
├── frontend/                 # Next.js 14 + Tailwind CSS
│   ├── app/
│   │   ├── page.tsx          # Home — hero, live log stream, architecture
│   │   ├── features/         # How It Works — 5 core features explained
│   │   ├── vault/            # Deposit/Withdraw UI with Freighter wallet
│   │   └── performance/      # Live dashboard — depositors, keepers, liquidations
│   └── lib/
│       ├── api.ts            # REST API client + types
│       ├── sse.ts            # SSE hook with exponential backoff
│       └── stellar.ts        # Freighter wallet integration
├── scripts/                  # Deployment + provisioning scripts
├── docker-compose.yml        # Keeper Alpha + Beta + Frontend
├── keeper/railway.toml       # Railway deployment config (keeper)
└── wallets.md                # All testnet wallet addresses (public keys)
```

## Smart Contracts

### KeeperRegistry (`contracts/keeper-registry/`)

On-chain registry for keeper operators. Any operator can self-register with a keypair. Admin can pause in emergencies.

| Function | Description |
|----------|-------------|
| `__constructor(admin, usdc, config)` | Atomic init at deploy (no separate initialize call to front-run); linked to the vault once via `set_vault` |
| `register(keeper, name)` | Register a new keeper operator — pulls `min_stake` USDC as stake |
| `deregister(keeper)` | Remove a keeper, returning its stake (fails with an active draw) |
| `slash(keeper)` | Permissionless after `slash_timeout`: transfers `slash_rate_bps` of stake to the vault, deactivates the keeper |
| `record_execution / mark_draw / clear_draw` | Vault-auth-only performance + draw-state hooks |
| `get_keepers() / get_keeper(op) / avg_response_time_ms(op)` | Read the registry + per-keeper metrics |
| `pause() / unpause()` | Emergency admin controls |

### NectarVault (`contracts/nectar-vault/`)

Pooled USDC vault that funds liquidations. Depositors receive LP shares proportional to their deposit. Shares appreciate as keepers return profits.

| Function | Description |
|----------|-------------|
| `__constructor(admin, usdc_token, registry, config)` | Atomic init at deploy (cap, cooldown, per-keeper draw limit) |
| `deposit(depositor, amount)` | Deposit USDC, receive LP shares (virtual-offset share math, deposit cap) |
| `withdraw(depositor, shares)` | Redeem shares for USDC at current share price (cooldown-gated) |
| `draw(keeper, amount)` | Keeper draws USDC for liquidation (registered keepers only; cumulative per-keeper cap) |
| `return_proceeds(keeper, amount, response_time_ms)` | Return capital + profit; rejects callers with no outstanding draw (`NoDraw` — the anti-donation guard) |
| `balance(user)` | Query user's shares and USDC value |

### LiquidationLab (`contracts/liquidation-lab/`)

A simplified pool stand-in used by early keeper tests. It models the **read**
side of a Blend pool faithfully and enough of the auction lifecycle to
exercise the keeper's decoding, but it is **not** interface-identical to
Blend v2 (see the mismatch note below).

| Function | Description |
|----------|-------------|
| `get_reserve_list()` | List reserve assets (XLM, USDC) |
| `get_reserve(asset)` | Reserve config (collateral/liability factors, rates) |
| `get_positions(user)` | User's collateral and liability maps |
| `new_liquidation_auction(user, pct)` | Create a Dutch auction (lab-only entry point) |
| `get_auction(type, user)` | Fetch active auction data |
| `submit(from, spender, to, requests)` | Fill a user-liquidation auction (type 6 only) |

> **Superseded for end-to-end work.** Blend v2 creates auctions through
> `new_auction(auction_type, user, bid, lot, percent)` and deletes stale ones
> with `del_auction` — neither exists on the lab, and the keeper calls both
> ([keeper/blend/auction.go](keeper/blend/auction.go), [keeper/blend/baddebt.go](keeper/blend/baddebt.go)).
> The lab also models no backstop, so bad-debt and interest auctions cannot
> occur on it at all. Real end-to-end validation therefore runs against the
> **Nectar Sandbox** — our own Blend V2 stack on testnet
> (docs/evidence/a4-sandbox.md), where every result in
> docs/evidence/b-full-cycle.md and docs/evidence/c-bad-debt.md was produced.

## Go Keeper

The keeper binary is a single Go process that:
- Registers itself on the KeeperRegistry (staking `min_stake` USDC)
- Discovers borrowers from pool events (persistent index, no address list) and probes their positions every cycle
- Computes health factors using reserve configs and oracle prices
- Creates simulation-sized Dutch auctions when HF < 1.0 and fills them atomically (fill + repay + withdraw in one submit)
- Draws capital from the vault, swaps seized collateral to USDC, returns measured proceeds
- Serves a REST API + SSE stream for the frontend

### API Endpoints

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/api/state` | GET | Current positions, keepers, events, vault state |
| `/api/performance` | GET | TVL, depositors, keeper stats, liquidation history |
| `/api/events` | GET | SSE stream of real-time keeper events |
| `/metrics` | GET | Prometheus metrics (cycles, liquidations, TVL) |
| `/healthz` | GET | Health check |

### Key Technical Details

- **Dutch Auction Profitability** (verified two-phase curve, 400 ledgers — docs/FACTS.md): t=0–200 `lotPct = t/200` with bid at 100%; t=200–400 lot at 100% with `bidPct = (400-t)/200`; fair point at t=200; bid empty past t=400. Keeper fills when `lot_value / bid_cost > MIN_PROFIT`
- **Multi-Operator Race**: First confirmed tx wins. The loser detects the filled auction, rolls back its acquisitions and returns the draw — no capital lost
- **Vault Capital Safety**: proceeds are the keeper's measured USDC balance delta, never synthesized; deterministic fill failures roll the draw back; ambiguous (possibly-broadcast) outcomes are resolved by tx hash before any further capital action, and unresolved ones are reconciled by the next cycle's stale-draw recovery
- **SSE Client Limit**: Max 100 concurrent connections, 503 if exceeded
- **Graceful Shutdown**: `SIGTERM`/`SIGINT` → drain in-flight cycle → clean exit
- **XDR Encoding**: ScMap keys sorted lexicographically (Soroban requirement), `ScVal.Vec` is `**xdr.ScVec` (double deref)

## Frontend

Next.js 14 with Tailwind CSS. Dark theme, monospace design.

| Page | Route | Description |
|------|-------|-------------|
| Home | `/` | Hero with live SSE log stream, problem stats, architecture diagram, keeper registry, position monitor |
| Features | `/features` | 5 core features explained with getting-started guides |
| Vault | `/vault` | Freighter wallet integration, deposit/withdraw, live balance queries |
| Dashboard | `/dashboard` | Network overview — TVL, share-price/APY chart (real returns, never annualized under 7 days), top keepers, recent fills |
| Keeper Leaderboard | `/dashboard/keepers` | All registered operators read from the on-chain registry — executions, win rate, avg response, stake, profit |
| Liquidation Feed | `/dashboard/liquidations` | Live SSE ticker + full fill history with amounts and fill-tx links |
| Depositor Analytics | `/dashboard/depositor` | Per-address position lookup (or wallet connect) — shares, value, estimated yield; deep-linkable as `/dashboard/<G-address>` |
| Performance | `/performance` | Legacy dashboard — depositors, keepers, vault TVL, liquidation history |

### Wallet Integration

The vault page integrates with [Freighter](https://freighter.app/) wallet:
- Detect Freighter extension
- Connect and read balances (XLM + USDC)
- Submit deposit/withdraw transactions to Soroban
- Query vault share balances on-chain
- Link to Stellar Expert for transaction verification

## Quick Start

### Prerequisites

- Go 1.22+
- Rust + `wasm32-unknown-unknown` target
- Node.js 18+
- [Stellar CLI](https://github.com/stellar/stellar-cli) (for contract deployment)

### 1. Build Contracts

```bash
cargo build --release --target wasm32-unknown-unknown
cargo test --workspace  # 90 tests across 4 contracts
```

### 2. Deploy to Testnet

```bash
# Generate and fund wallets
stellar keys generate admin --network testnet

# Deploy contracts
stellar contract deploy --wasm target/.../keeper_registry.optimized.wasm --source admin --network testnet
stellar contract deploy --wasm target/.../nectar_vault.optimized.wasm --source admin --network testnet
stellar contract deploy --wasm target/.../liquidation_lab.optimized.wasm --source admin --network testnet
```

### 3. Run Keeper

```bash
cd keeper
cp ../.env.example .env  # configure with your contract IDs + keypair
go run .
```

### 4. Run Frontend

```bash
cd frontend
npm install && npm run dev
# → http://localhost:3000
```

### 5. Docker (both keepers + frontend)

```bash
docker-compose up
# keeper-alpha: localhost:8080
# keeper-beta:  localhost:8081
# frontend:     localhost:3000
```

## Environment Variables

| Variable | Required | Description |
|----------|----------|-------------|
| `KEEPER_SECRET` | yes | Stellar secret key (S...) for the keeper operator |
| `KEEPER_NAME` | no | Display name (default: `keeper-alpha`) |
| `REGISTRY_CONTRACT` | yes | KeeperRegistry contract ID |
| `VAULT_CONTRACT` | yes | NectarVault contract ID |
| `BLEND_POOLS` | yes | Comma-separated `ADDR[:mode[:POOL_USDC]]` pool entries; `mode` = `active` \| `monitor`, `POOL_USDC` declares a pool that settles a different USDC (execution-disabled). Legacy single-pool `BLEND_POOL` still honored |
| `BORROWER_CACHE` | no | Path for the persisted borrower index (mount a volume); empty disables persistence |
| `WATCH_ADDRESSES` | no | Optional additive addresses always probed — discovery is event-driven and needs no list |
| `USDC_CONTRACT` | no | USDC token contract; enables collateral→USDC swaps + stale-draw recovery |
| `SOROSWAP_ROUTER` | no | Soroswap router contract (primary DEX); empty disables swaps |
| `PHOENIX_ROUTER` | no | Phoenix XYK pool (pair) contract for the collateral/USDC pair (fallback DEX) |
| `SLIPPAGE_BPS` | no | Max swap slippage in basis points (default: `100` = 1%, range 0-10000) |
| `DEFINDEX_VAULT` | no | DeFindex vault to monitor for rebalancing; empty disables the adapter |
| `DEFINDEX_DRIFT_BPS` | no | DeFindex allocation drift threshold in bps (default: `500` = 5%) |
| `POLL_INTERVAL` | no | Seconds between cycles (default: `10`, range: 3-300) |
| `MIN_PROFIT` | no | Minimum lot/bid ratio to fill (default: `1.02`) |
| `BAD_DEBT_MAX_SPEND` | no | Keeper-float USDC cap (stroops) per bad-debt fill; `0` disables. Bad-debt fills never draw vault capital |
| `BAD_DEBT_LP_HAIRCUT_BPS` | no | Discount on the backstop-LP spot valuation in bad-debt profitability (default: `5000` = 50%) |
| `FAUCET_SECRET` / `FAUCET_AMOUNT` / `FAUCET_COOLDOWN_SECS` | no | Testnet USDC faucet served by the keeper API; empty secret disables |
| `KEEPER_XLM_RESERVE` | no | Native XLM fee floor in stroops; stale-draw recovery never sells below it |
| `KNOWN_DEPOSITORS` | no | Comma-separated G-addresses for performance page |
| `API_PORT` | no | HTTP API port (default: `8080`) |

## Test Suite

```bash
# Rust contract tests (90 total)
cargo test -p keeper-registry     # 26 tests (incl. staking + slashing scenarios)
cargo test -p nectar-vault        # 47 tests (incl. cap + cooldown + share-math + partial-return edges)
cargo test -p liquidation-lab     # 12 tests
cargo test -p mock-token          # 5 tests

# Go keeper tests (279 test functions across 9 packages)
cd keeper && go test -race -count=1 ./...
# unit tests, integration tests, stress tests, benchmarks

# Frontend build
cd frontend && npm run build
```

## Security

- **Depositor TTL**: 535,680 ledgers (~30 days) — prevents share loss from expiration
- **Division-by-zero guard**: Withdraw checks `total_shares > 0`
- **Config validation**: Poll interval bounds [3, 300], min profit > 0, env parse errors crash fast
- **Capital safety**: Vault draw only returns proceeds on successful fill or `ErrAlreadyFilled`
- **Deadlock prevention**: Separate mutex for SSE subscriber list vs data fields
- **SSE limit**: Max 100 concurrent clients, 503 rejection
- **Graceful shutdown**: Signal handling with in-flight cycle drain

## Deployment

### Railway (Keepers)

Keepers run as Railway services using `keeper/Dockerfile`. From `keeper/`:

```bash
railway login
railway init                # one-time, links to a Railway project
railway up                  # builds via Dockerfile and deploys
```

Required env vars in the Railway dashboard (mark `KEEPER_SECRET` as secret):

```
KEEPER_SECRET       S...                                                         # operator key (mark as secret)
KEEPER_NAME         keeper-alpha
REGISTRY_CONTRACT   CD33A7IGNCOLVQ4EEINBVMVA7IHWXGN57R6YLE5AJEEKPA6VKC2E4IQD
VAULT_CONTRACT      CDOGQY7NAE3BP4Q7RWBCBLW23Z36RNWNDNXX5DWNIEVMFEWP3GVEPXLR
USDC_CONTRACT       CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA      # Circle testnet USDC
BLEND_POOLS         CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF:monitor:CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU,CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V:active
BORROWER_CACHE      /app/state/borrowers.json                                     # mount a volume
SOROSWAP_ROUTER     CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD      # collateral→USDC swaps
SLIPPAGE_BPS        100
SOROBAN_RPC         https://soroban-testnet.stellar.org:443
HORIZON_URL         https://horizon-testnet.stellar.org
POLL_INTERVAL       10
MIN_PROFIT          1.02
API_PORT            8080
```

The repo's [scripts/railway-keeper-env.sh](scripts/railway-keeper-env.sh) wraps `railway variables --set …` with these IDs pre-filled. After a contract redeploy, run it once per service (`./scripts/railway-keeper-env.sh keeper-alpha` and `… keeper-beta`) and then `railway up` to redeploy each Railway service.

Healthcheck endpoint: `/healthz` (configured in `railway.toml`).

### Vercel (Frontend)

Next.js deployed to Vercel with `output: "standalone"`. The build is network-aware (`NEXT_PUBLIC_NETWORK=testnet|mainnet` drives RPC/Horizon/USDC-issuer defaults — see [frontend/lib/stellar.ts](frontend/lib/stellar.ts)). Required env vars (testnet):

```
NEXT_PUBLIC_REGISTRY_CONTRACT  CD33A7IGNCOLVQ4EEINBVMVA7IHWXGN57R6YLE5AJEEKPA6VKC2E4IQD
NEXT_PUBLIC_VAULT_CONTRACT     CDOGQY7NAE3BP4Q7RWBCBLW23Z36RNWNDNXX5DWNIEVMFEWP3GVEPXLR
NEXT_PUBLIC_API_URL            https://<your-railway-keeper>.up.railway.app
```

## License

MIT
