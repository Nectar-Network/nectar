# Tranche 2 — Testnet Demonstration Evidence

On-chain proof for the Tranche 2 deliverables, captured on Soroban **testnet**
(mock USDC SAC). All transactions are verifiable on stellar.expert.

## Contracts (current — Tranche 1 hardened, 2026-05-24)
| Contract | Address |
|---|---|
| KeeperRegistry | `CDT257SL2IYDZJIDXEVKI67MYLCKE73JY6WGUTGZOEFXJHG26FJHJDRB` |
| NectarVault | `CDZR6VDCPQFOFFKKZ2KMVB67Z54LI5OY73NHBFVI6DR6RE6TL7NN7345` |
| Mock USDC (SAC) | `CD34YC6FFI2KIE2U4ZPCGQIRPH7UPG5YY2QBYNP25ATSFOQSG73J4VBW` |
| Blend pool (testnet V2) | `CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF` |
| Soroswap router (testnet) | `CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD` |

## DEX integration — live Soroswap swap (Deliverable 1)
Collateral→USDC conversion executed against the **live Soroswap testnet router**
using the exact ABI the keeper calls (`router_get_amounts_out` +
`swap_exact_tokens_for_tokens`, see `keeper/dex/soroswap.go`).

- XLM↔USDC pair created (seeded 100 XLM / 100 mock-USDC): `CDXJG5B6DLLA64TJWLVHTLQFBNBNPT4BFUZ6PZYJOIAH5FJYHR6KZIDD`
  - `add_liquidity` tx: `99c46bec9763b389cc2f64cdd2f7f5864e63670fadd8d9dc814bb2b1a0ddf4c6`
- **Swap** 10 XLM → 9.066 USDC, 1% slippage bound (`amount_out_min`): tx `0049bcca65ae68d1a40b9a5a8ceb683109177c901583370caca0570035a1e59e`
- Keeper testnet env now sets `SOROSWAP_ROUTER` + `SLIPPAGE_BPS`
  (`scripts/railway-keeper-env.sh`, `scripts/keeper-blend-testnet.sh`) so the
  deployed keeper runs with the DEX enabled.

Scope note: the swap above proves the live-Soroswap path with the keeper's ABI.
A single fully-automated auction→fill→swap→return cycle against an *organic*
Blend auction is not included (testnet Blend auctions cannot be summoned on
demand); the LiquidationLab harness covers the fill path in E2E tests.

## Multi-protocol adapters — both adapters in the loop (Deliverable 2)
Keeper boots with `adapters registered count=2` and scans **both** Blend and
DeFindex every cycle (see `keeper/main.go` `cycle()`):

```
INFO adapters registered count=2
INFO keeper started pool=CCEB..4HGF interval=8
WARN get tasks failed protocol=defindex err=fetch_total_managed_funds: ... MissingValue
```

The DeFindex scan above points at a placeholder vault (no live DeFindex testnet
vault is deployed yet), so it returns a graceful warning rather than an
actionable rebalance — the loop stays healthy. A real on-chain rebalance tx is
pending a deployed DeFindex vault + rebalancer role (Tranche 3).

## Keeper network — 3rd operator registered (Deliverable 4)
A third keeper was registered on the live registry (`keeper_count` is now 3;
`get_keepers()` returns alpha, beta, gamma — the dashboard leaderboard
enumerates them on-chain):

- keeper-gamma: `GA472SZPEXVDKEN7BAGJAFVBDB74G37GOAHYFWUPC4Q62DDPTAGIQQXT`
  - mint 200 USDC: tx `e88e445fb3fd5481f6b6daa0baec5b9f06c1fffa55dfc7e79005972882ec4eec`
  - register (100 USDC stake): tx `fbada22902d911e9e5809dcb0b75728f75c1830d8bfe127baa5dfcf314e879e6`

The three operators are team-run for the milestone, not independent third parties.

## Dashboard — per-address deposit/withdraw history (Deliverable 3)
The keeper now indexes NectarVault `deposit`/`withdraw` events via `getEvents`
and exposes them as `DepositorRow.history` on `/api/performance`; the depositor
dashboard renders a per-address history table with stellar.expert tx links.
Soroban RPC only retains events within a bounded ledger window, so history is
accumulated forward from the indexed window (full historical backfill would need
a dedicated event store — out of scope).
