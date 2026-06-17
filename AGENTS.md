# AGENTS.md — Nectar Network Main Repository

## Project Overview

Nectar Network is a pooled liquidation protocol for Soroban DeFi on Stellar. Users deposit USDC into a vault, keeper operators use that capital to fill Blend Protocol liquidation auctions, and profits flow back to depositors as yield.

**Grant:** $75K SCF Build Award (SCF #42), approved March 2026.
**Timeline:** 4 months (April–October 2026), 3 tranches.
**Team:** Kunal (architect, Rust/Go), Daksh (contracts, backend), Priya (frontend).

## Repository Structure

This is a monorepo containing three components:

```
contracts/          # Soroban smart contracts (Rust)
  keeper-registry/  # Operator registration, staking, slashing, performance tracking
  nectar-vault/     # USDC deposit pool, share accounting, keeper capital draws
keeper/             # Off-chain keeper daemon (Go)
  cmd/              # Entry points
  blend/            # Blend Protocol adapter (pool monitoring, auction execution)
  vault/            # NectarVault client (draw, return proceeds)
  registry/         # KeeperRegistry client
  dex/              # DEX integration for collateral swaps (Tranche 2)
  adapters/         # Multi-protocol adapter interface (Tranche 2)
  oracle/           # Oracle circuit breaker (Tranche 3)
  soroban/          # Thin Soroban JSON-RPC client
frontend/           # Next.js 14 web application
  app/              # App router pages
  components/       # React components
  lib/              # Soroban client wrappers, hooks, stores
scripts/            # Deployment, seeding, testing scripts
docs/               # Internal documentation
```

## Key Technical Context

### Contracts (Rust/Soroban)
- Soroban SDK version: 22.x
- Build: `cargo build --target wasm32-unknown-unknown --release`
- Test: `cargo test` (uses soroban-sdk testutils, mock_all_auths)
- Deploy: `stellar contract deploy --wasm <path> --source $ADMIN_SECRET --rpc-url <url> --network-passphrase <passphrase>`
- All values use 7-decimal precision (Stellar native: 10^7 stroops)
- Storage: persistent for user data (KeeperInfo, Depositor), instance for config/state (VaultState, admin)
- Cross-contract: NectarVault calls KeeperRegistry.get_keeper() to verify keepers before draw()

### Keeper (Go)
- Go 1.22+, uses github.com/stellar/go SDK
- Interacts with Soroban via JSON-RPC (simulateTransaction, sendTransaction, getEvents)
- XDR encoding for contract arguments using github.com/stellar/go/xdr
- Stateless — all state read from chain each cycle. Restarts safely.
- Config via environment variables (KEEPER_SECRET, BLEND_POOL, REGISTRY_CONTRACT, VAULT_CONTRACT)
- Polling interval: 10 seconds default
- Blend Dutch auctions: lot scales 0%→100% over 200 blocks, bid scales 100%→0%
- Profitability threshold: lot_value/bid_cost > 1.02 (configurable)

### Frontend (Next.js/TypeScript)
- Next.js 14 App Router, TypeScript, Tailwind CSS
- Wallet: @creit.tech/stellar-wallets-kit (Freighter primary)
- Soroban interaction: @stellar/stellar-sdk v13
- State: Zustand stores
- SSE connection to keeper API for live log stream
- Deployed on Vercel (auto-deploy from main branch)

## Deployed Contracts (Testnet)
- KeeperRegistry: CAWT5HBM25OKGOMJHPFCXWXDWZ7FF436WXRKROTY2VW642FSKLYUKOUB
- NectarVault: CCXDLRE3IV5225LE3Z776KFB2VWD2MTXOJHAUKFA5RPYDJVOWCMHJ4U4
- USDC Token (SAC): CAVBAVD6CZ46FEDKJHBQIJF7EFAZDTRNS65G73QS5ZYI3VK5E2JFPQ4J

## Related Repositories
- **keeper-sdk** (github.com/Nectar-Network/keeper-sdk): Public Go SDK for third-party keeper operators. Built in Tranche 2. The adapter interface (ProtocolAdapter) defined there is implemented by adapters in this repo's keeper/adapters/ directory.
- **docs-site** (github.com/Nectar-Network/docs-site): Public documentation site. Built in Tranche 3.

## Current Tranche & Priorities

Check docs/TRANCHE-{1,2,3}-SPEC.md for detailed deliverable specifications.

### Tranche 1 (MVP, due June 15, 2026):
1. KeeperRegistry: add staking (USDC deposit on register), performance tracking (execution count, success rate, avg response time on-chain), slashing (auto-slash on draw timeout)
2. NectarVault: add deposit caps, withdrawal cooldowns, hardened share math (7-decimal edge cases, concurrent draw+withdraw)
3. Blend adapter: full auction integration (user/interest/bad debt auctions), Dutch auction profitability engine, retry logic with exponential backoff

### Tranche 2 (Testnet, due August 15, 2026):
1. DEX integration: Soroswap/Phoenix swap after auction fills (collateral → USDC)
2. Multi-protocol adapter interface: Go ProtocolAdapter interface, DeFindex adapter
3. Dashboard v2: APY chart, keeper leaderboard, liquidation feed, depositor analytics
4. keeper-sdk published as separate repo

### Tranche 3 (Mainnet, due October 15, 2026):
1. Mainnet deployment: all contracts on mainnet, Circle USDC, production parameters
2. Oracle circuit breaker: cross-reference Reflector, auto-pause on deviation
3. Docker packaging: one-command keeper setup, operator docs
4. Security hardening: rate limits, draw caps, admin multisig, benchmarks

## Code Style

### Rust
- 4-space indent, `cargo fmt` enforced
- `cargo clippy` with no warnings
- No `.unwrap()` in production paths (only tests)
- Domain names: `hf` for health factor, `pos` for position, `amt` for amount
- Error handling: `Result<T, ContractError>` everywhere
- Comments only when logic is counterintuitive

### Go
- `gofmt` standard formatting
- `golangci-lint` in CI
- No `panic()` in production paths (return errors)
- Structured logging with log/slog: `slog.Info("healthy", "pos", addr, "hf", hf)`
- No external dependencies beyond stellar/go SDK and testify (tests)
- Config via env vars only, no config files

### TypeScript
- 2-space indent, Prettier enforced
- Strict TypeScript config
- Zustand for state, no Redux
- Tailwind only, no CSS modules

## Testing

- Contracts: `cd contracts && cargo test` — all tests must pass
- Keeper: `cd keeper && go test -race ./...` — includes race condition detection
- Frontend: `cd frontend && npm run build` — type check + build
- Before any tranche submission: full CI green + recorded demo video

## Environment Variables (Keeper)

```
KEEPER_SECRET=S...          # Keeper's Stellar secret key
KEEPER_NAME=keeper-alpha    # Human-readable name
REGISTRY_CONTRACT=C...      # KeeperRegistry contract ID
VAULT_CONTRACT=C...         # NectarVault contract ID
BLEND_POOL=C...             # Blend pool contract ID to monitor
SOROBAN_RPC=https://...     # Soroban RPC endpoint
HORIZON_URL=https://...     # Horizon API endpoint
POLL_INTERVAL=10            # Seconds between monitoring cycles
MIN_PROFIT=1.02             # Minimum lot/bid ratio to fill auction
```

## Deployment

- **Contracts:** `./scripts/deploy.sh` (builds, optimizes, deploys, initializes)
- **Keeper:** Auto-deploys to Railway on push to main. Docker: `docker-compose up keeper`
- **Frontend:** Auto-deploys to Vercel on push to main.
- **Local dev:** `docker-compose up` starts all 3 services.