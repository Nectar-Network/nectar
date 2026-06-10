# Nectar Network

Multi-operator keeper infrastructure for Soroban DeFi. Distributed liquidation network for Blend Protocol on Stellar — no single point of failure.

**Live:** [nectarnetwork.fun](https://nectarnetwork.fun) · [Twitter](https://x.com/nectar_xlm) · [GitHub](https://github.com/Nectar-Network/nectar-poc)

## The Problem

On Feb 22, 2026, a USTRY/XLM oracle manipulation drained **$10.8M** from a Blend pool. Two pre-positioned single-operator bots captured nearly all of it — 60 auction fills over 4 hours, one Docker container, one keypair, no fallback. The rest of Stellar DeFi (~$187M TVL) had no coordinated response.

Nectar replaces single-bot liquidation systems with a distributed network of competing keepers, funded by a shared vault.

## Architecture

```
┌─────────────────────────────────────────────────────────────────┐
│  SOROBAN TESTNET                                                │
│                                                                 │
│  ┌──────────────────┐  ┌──────────────────┐  ┌──────────────┐  │
│  │  KeeperRegistry  │  │   NectarVault    │  │ LiquidationLab│  │
│  │  register()      │  │   deposit()      │  │ get_positions()│ │
│  │  deregister()    │  │   withdraw()     │  │ new_auction() │  │
│  │  get_keepers()   │  │   draw()         │  │ get_auction() │  │
│  │  pause()         │  │   return_proceeds│  │ submit()      │  │
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

1. **Monitor** — Each keeper independently polls the pool every 10s for positions with health factor < 1.0
2. **Detect** — When HF drops below 1.0, keeper creates a Dutch auction on-chain
3. **Draw** — Keeper draws USDC capital from the NectarVault
4. **Fill** — Keeper fills the auction (first confirmed transaction wins the race)
5. **Return** — Capital + 10% profit returned to the vault; depositors' shares appreciate
6. **Compete** — The losing keeper handles `ErrAlreadyFilled` gracefully — no capital lost

## Live Testnet Deployment

| Service | URL |
|---------|-----|
| Frontend | [nectarnetwork.fun](https://nectarnetwork.fun) |
| Keeper Alpha API | [keeper-alpha-production.up.railway.app](https://keeper-alpha-production.up.railway.app) |
| Keeper Beta API | [keeper-beta-production.up.railway.app](https://keeper-beta-production.up.railway.app) |

Both keepers run on Railway from `keeper/Dockerfile`. They need an env-var refresh + redeploy after Tranche 1 redeploy — `REGISTRY_CONTRACT`, `VAULT_CONTRACT`, `USDC_CONTRACT`, `BLEND_POOL` all moved. Use `./scripts/railway-keeper-env.sh keeper-alpha` and `… keeper-beta`, then `railway up`.

### On-Chain Contracts (Soroban Testnet)

Tranche 1 hardened redeploy on 2026-05-24 (see [wallets.md](wallets.md) for the full address book) — these contracts ship the staking + slashing + performance-tracking + cap/cooldown surface, and the keeper sends a Blend-compatible `submit` payload (`request_type: u32`, `amount: i128`). Reproduce locally with `./scripts/tranche-1-e2e.sh`.

| Contract | Address | Explorer |
|----------|---------|----------|
| KeeperRegistry | `CDT257SL2IYDZJIDXEVKI67MYLCKE73JY6WGUTGZOEFXJHG26FJHJDRB` | [View](https://stellar.expert/explorer/testnet/contract/CDT257SL2IYDZJIDXEVKI67MYLCKE73JY6WGUTGZOEFXJHG26FJHJDRB) |
| NectarVault | `CDZR6VDCPQFOFFKKZ2KMVB67Z54LI5OY73NHBFVI6DR6RE6TL7NN7345` | [View](https://stellar.expert/explorer/testnet/contract/CDZR6VDCPQFOFFKKZ2KMVB67Z54LI5OY73NHBFVI6DR6RE6TL7NN7345) |
| Mock USDC (SAC) | `CD34YC6FFI2KIE2U4ZPCGQIRPH7UPG5YY2QBYNP25ATSFOQSG73J4VBW` | [View](https://stellar.expert/explorer/testnet/contract/CD34YC6FFI2KIE2U4ZPCGQIRPH7UPG5YY2QBYNP25ATSFOQSG73J4VBW) |
| Blend testnet pool (V2) | `CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF` | [View](https://stellar.expert/explorer/testnet/contract/CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF) |
| Soroswap router | `CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD` | [View](https://stellar.expert/explorer/testnet/contract/CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD) |

The Blend pool ID comes from [blend-utils/testnet.contracts.json](https://github.com/blend-capital/blend-utils/blob/main/testnet.contracts.json) (key: `TestnetV2`). Point the keeper at it with `./scripts/keeper-blend-testnet.sh` — no code changes needed.

### Testnet Stats (live, post Tranche 1 redeploy)

- **TVL**: 50,020 USDC across 15+ depositors
- **Realized profit on-chain**: 100 USDC from one full draw→fill→return cycle (`response_time_ms: 175`)
- **Keepers**: 2 registered operators (alpha + beta), each staked 100 USDC on-chain
- **Vault config**: deposit cap 10M USDC, withdraw cooldown 30s (demo), max draw 10k USDC/keeper
- **Registry config**: 100 USDC min stake, slash timeout 3600s, slash rate 10% bps
- **Profit model**: 10% per successful liquidation returned to vault depositors

## Tranche 1 Status

Each Tranche 1 deliverable below cites the on-chain code + tests that prove the measurement criteria. Run `cargo test --workspace` (80 contract tests) and `cd keeper && go test ./...` to reproduce locally.

### 1. KeeperRegistry v1 — Staking & Performance Tracking ✓

**Status: code complete, on-chain proof from `./scripts/tranche-1-e2e.sh`**

- **Staking enforced on-chain**: `register()` pulls `min_stake` USDC from the operator via SAC `transfer` ([contracts/keeper-registry/src/lib.rs:56-65](contracts/keeper-registry/src/lib.rs#L56-L65)). Registration fails with `InsufficientStake` (#7) when `min_stake = 0`. Tests: `test_register_with_stake`, `test_register_insufficient_stake`, `test_register_zero_min_stake_rejected`.
- **Performance metrics on-chain**: `KeeperInfo` carries `total_executions`, `successful_fills`, `total_profit`, `total_response_time_ms`, `response_count` ([types.rs:5-18](contracts/keeper-registry/src/types.rs#L5-L18)). `avg_response_time_ms(operator)` returns the per-keeper average ([lib.rs:165-177](contracts/keeper-registry/src/lib.rs#L165-L177)). Recorded by `record_execution()` invoked from `vault.return_proceeds()`.
- **Slashing**: `slash(keeper)` ([lib.rs:284-326](contracts/keeper-registry/src/lib.rs#L284-L326)) transfers `slash_rate_bps` of stake to the vault when `now - last_draw_time > slash_timeout`. Tests: `test_slash_after_timeout`, `test_slash_before_timeout_fails`, `test_slash_without_active_draw_fails`.

### 2. NectarVault v1 — Production Deposit/Withdraw ✓

**Status: code complete, on-chain proof from `./scripts/tranche-1-e2e.sh`**

- **Deposit cap**: `deposit()` rejects with `DepositCapExceeded` (#8) when `state.total_usdc + amount > cfg.deposit_cap` ([contracts/nectar-vault/src/lib.rs:66-68](contracts/nectar-vault/src/lib.rs#L66-L68)). Tests: `test_deposit_exceeds_cap`, `test_deposit_at_exact_cap`, `test_deposit_cap_with_existing_balance`.
- **Withdraw cooldown**: `withdraw()` rejects with `WithdrawalCooldown` (#9) when `now - depositor.last_deposit_time < cfg.withdraw_cooldown` ([lib.rs:132-135](contracts/nectar-vault/src/lib.rs#L132-L135)). Tests: `test_withdraw_before_cooldown`, `test_withdraw_after_cooldown`, `test_cooldown_resets_on_new_deposit`.
- **Share-price hardening at 7-decimal precision** ([lib.rs:71-76, 146-150](contracts/nectar-vault/src/lib.rs#L71-L76)): integer-division floors-toward-zero on both deposit (caller gets ≤ fair shares) and withdraw (caller gets ≤ fair USDC). Zero-share guard at line 142. Tests: `test_share_math_first_deposit`, `test_share_math_large_amounts`, `test_share_math_tiny_amounts`, `test_share_math_with_profit`, `test_share_rounding_bounded`, `test_multiple_depositors_proportional_shares`, `test_multiple_depositors_proportional_with_profit`, `test_withdraw_with_zero_shares_fails`, `test_withdraw_more_than_owned_fails`, `test_withdraw_more_than_available_fails`. **37 vault tests total — more than the 10+ measurement asked for.**

### 3. Blend Liquidation Adapter — Full Auction Integration ✓

**Status: code complete, smoke-testable against Blend's live testnet pool**

- **All three auction kinds handled**: user liquidation (request_type 6), bad debt (7), interest (8) — see [keeper/blend/auction.go:15-49](keeper/blend/auction.go#L15-L49). `DetectAuctions()` scans all three for an address; `FillByType()` dispatches.
- **Blend ABI compatibility**: the submit payload encodes `request_type` as `ScvU32` and `amount` as `ScvI128` to match Blend's `#[contracttype] struct Request { request_type: u32, address: Address, amount: i128 }` ([keeper/blend/auction.go:155-189](keeper/blend/auction.go#L155-L189)). Locked in by `TestSubmitPayload_BlendABITypes`.
- **Dutch auction profitability** at 200-block boundaries: `lotPct = elapsed/200`, `bidPct = (200-elapsed)/200`; fair-price point at elapsed=200; expired at 400. Tests: `TestProfitability_Block0_LotZero`, `…Block200_FairPrice`, `…Block100_LotScaling`, `…Block300_BidScaling`, `…Block400_BidZero`, `…PastExpiry_StaysInfinite`, `TestPhaseAt_Boundaries`.
- **Retry wrapper** with exponential backoff (3 attempts, 2.0× backoff): classifies `sequence`, `resource_exhaust`, `timeout`, `tx_too_late` as retryable; `already filled`, `already registered`, `insufficient_balance`, `contract error` as non-retryable ([keeper/soroban/retry.go:25-67](keeper/soroban/retry.go#L25-L67)). Tests: `TestFillAuction_RetriesOnSequenceError`, `…RetriesOnResourceExhaust`, `…DoesNotRetryAlreadyFilled`, `TestRegister_DoesNotRetryAlreadyRegistered`.
- **`/api/state` carries response_time_ms** on each liquidation record ([keeper/main.go:25-33, 325-332](keeper/main.go#L25-L33)). On a successful fill the keeper measures `time.Since(drawStart).Milliseconds()` between `vault.draw` and `vault.return_proceeds`, populates the `response_time_ms` field on the appended `LiquidationRecord`, and forwards the same value on-chain via `vault.return_proceeds → registry.record_execution`.
- **Live Blend testnet pool**: `BLEND_POOL=CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF` (Blend V2 from blend-utils). Run `./scripts/keeper-blend-testnet.sh` to point the keeper at it locally — the keeper reads reserves + positions and attempts liquidations against the real pool. Filling against a real Blend auction requires either (a) a position that has organically gone underwater, or (b) admin access to Blend's mock oracle (`CAZOKR2Y…`) to manipulate prices via `./scripts/trigger-liquidation.ts`.

### What `LiquidationLab` is for

`contracts/liquidation-lab/` ([lib.rs](contracts/liquidation-lab/src/lib.rs)) is a Blend-ABI-compatible test pool used by the keeper's local integration tests. It is **not** required for the live deployment — the keeper points at the real Blend pool via the `BLEND_POOL` env var. LiquidationLab exists for hermetic CI / replayable demo scenarios where you control both sides of the auction.

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

### 4. Keeper SDK & Operator Documentation — not started

Scheduled as the final Tranche 2 work package: extracting `adapters.ProtocolAdapter` + the `soroban`/`vault`/`dex` packages into a published, `go get`-able module with operator setup/strategy/risk documentation.

## Repository Structure

```
nectar-poc/
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
| `initialize(admin)` | Set admin, create empty keeper list |
| `register(keeper, name)` | Register a new keeper operator |
| `deregister(keeper)` | Remove a keeper from the registry |
| `get_keepers()` | List all registered keepers |
| `pause() / unpause()` | Emergency admin controls |

### NectarVault (`contracts/nectar-vault/`)

Pooled USDC vault that funds liquidations. Depositors receive LP shares proportional to their deposit. Shares appreciate as keepers return profits.

| Function | Description |
|----------|-------------|
| `initialize(admin, usdc_token, registry)` | Configure vault with USDC token and registry |
| `deposit(depositor, amount)` | Deposit USDC, receive LP shares |
| `withdraw(depositor, shares)` | Redeem shares for USDC at current share price |
| `draw(keeper, amount)` | Keeper draws USDC for liquidation (must be registered) |
| `return_proceeds(keeper, amount)` | Return capital + profit after successful liquidation |
| `balance(user)` | Query user's shares and USDC value |

### LiquidationLab (`contracts/liquidation-lab/`)

Blend-compatible pool contract that the Go keeper interacts with directly — same interface as a real Blend pool. Admin can set positions and control auctions for testing.

| Function | Description |
|----------|-------------|
| `get_reserve_list()` | List reserve assets (XLM, USDC) |
| `get_reserve(asset)` | Reserve config (collateral/liability factors, rates) |
| `get_positions(user)` | User's collateral and liability maps |
| `new_liquidation_auction(user, pct)` | Create Dutch auction for underwater position |
| `get_auction(type, user)` | Fetch active auction data |
| `submit(from, spender, to, requests)` | Fill auction (keeper submits fill request) |

The Go keeper needs **zero code changes** to switch between a real Blend pool and LiquidationLab — just change the `BLEND_POOL` env var.

## Go Keeper

The keeper binary is a single Go process that:
- Registers itself on the KeeperRegistry
- Polls the pool every 10s for positions
- Computes health factors using reserve configs and oracle prices
- Creates and fills Dutch auctions when HF < 1.0
- Draws capital from the vault, fills auctions, returns proceeds
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

- **Dutch Auction Profitability**: `lotPct = elapsed/200`, `bidPct = (200-elapsed)/200`. Keeper fills when `lot_value / bid_cost > MIN_PROFIT`
- **Multi-Operator Race**: First confirmed tx wins. Loser gets `ErrAlreadyFilled` → capital returned safely
- **Vault Capital Safety**: `return_proceeds` only called on fill success or `ErrAlreadyFilled`. Hard failures propagate error without returning
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
cargo test --workspace  # 77 tests across 4 contracts
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
| `BLEND_POOL` | yes | Pool contract ID (Blend or LiquidationLab) |
| `USDC_CONTRACT` | no | USDC token contract; enables collateral→USDC swaps + stale-draw recovery |
| `SOROSWAP_ROUTER` | no | Soroswap router contract (primary DEX); empty disables swaps |
| `PHOENIX_ROUTER` | no | Phoenix XYK pool (pair) contract for the collateral/USDC pair (fallback DEX) |
| `SLIPPAGE_BPS` | no | Max swap slippage in basis points (default: `100` = 1%, range 0-10000) |
| `DEFINDEX_VAULT` | no | DeFindex vault to monitor for rebalancing; empty disables the adapter |
| `DEFINDEX_DRIFT_BPS` | no | DeFindex allocation drift threshold in bps (default: `500` = 5%) |
| `POLL_INTERVAL` | no | Seconds between cycles (default: `10`, range: 3-300) |
| `MIN_PROFIT` | no | Minimum lot/bid ratio to fill (default: `1.0`) |
| `KNOWN_DEPOSITORS` | no | Comma-separated G-addresses for performance page |
| `API_PORT` | no | HTTP API port (default: `8080`) |

## Test Suite

```bash
# Rust contract tests (80 total)
cargo test -p keeper-registry     # 26 tests (incl. staking + slashing scenarios)
cargo test -p nectar-vault        # 37 tests (incl. cap + cooldown + share-math + partial-return edges)
cargo test -p liquidation-lab     # 12 tests
cargo test -p mock-token          # 5 tests

# Go keeper tests (30+ total)
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
REGISTRY_CONTRACT   CDT257SL2IYDZJIDXEVKI67MYLCKE73JY6WGUTGZOEFXJHG26FJHJDRB
VAULT_CONTRACT      CDZR6VDCPQFOFFKKZ2KMVB67Z54LI5OY73NHBFVI6DR6RE6TL7NN7345
USDC_CONTRACT       CD34YC6FFI2KIE2U4ZPCGQIRPH7UPG5YY2QBYNP25ATSFOQSG73J4VBW
BLEND_POOL          CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF      # Blend testnet V2 pool
SOROSWAP_ROUTER     CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD      # collateral→USDC swaps
SLIPPAGE_BPS        100
SOROBAN_RPC         https://soroban-testnet.stellar.org:443
HORIZON_URL         https://horizon-testnet.stellar.org
POLL_INTERVAL       10
MIN_PROFIT          1.02
API_PORT            8080
```

The repo's [scripts/railway-keeper-env.sh](scripts/railway-keeper-env.sh) wraps `railway variables --set …` with these IDs pre-filled. After `./scripts/tranche-1-e2e.sh` redeploys, run it once per service (`./scripts/railway-keeper-env.sh keeper-alpha` and `… keeper-beta`) and then `railway up` to redeploy each Railway service.

Healthcheck endpoint: `/healthz` (configured in `railway.toml`).

### Vercel (Frontend)

Next.js deployed to Vercel with `output: "standalone"`. Required env vars:

```
NEXT_PUBLIC_REGISTRY_CONTRACT  CAEHWZOOEP6YSU3EJDO7B2L7QJTG4YHXJIACRBWRTFRPMRVND56LTWAO
NEXT_PUBLIC_VAULT_CONTRACT     CCSR5GT6BEZXCW5UWV4LOXC24L75YQ7JJ5Q3Q7WJKLCGSKOSWOELJJFQ
NEXT_PUBLIC_USDC_CONTRACT      CD3YAGUK4SV67PIHRYKR5QAKTNVIGVDAXT6P426EO2E76DAXGMPLMSAH
NEXT_PUBLIC_API_URL            https://<your-railway-keeper>.up.railway.app
```

## License

MIT
