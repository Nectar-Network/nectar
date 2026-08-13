# FACTS.md — Verified Blend Protocol Facts

Single source of truth for versions, contract IDs, and decoded shapes used by
Nectar Network's Blend integration. **Every entry must carry a date and a
source** (URL, repo-path@commit, command, or tx hash). Entries without a source
are invalid — do not add them.

Entry format: `claim — value — date — source`.

Status: SKELETON — populated by verification Gates 0.1–0.7 (see
VERIFICATION-REPORT.md once complete).

---

## Reference baselines

| Claim | Value | Date | Source |
|---|---|---|---|
| blend-contracts-v2 pinned commit | `ba22b487b2c5057a4ecc28b05b5193c28e4bd117` (authored 2025-08-14 11:26:04 -0400) | 2026-08-03 | `git rev-parse HEAD` / `git log -1 --format=%ci` in local clone of https://github.com/blend-capital/blend-contracts-v2 |
| blend-utils pinned commit | `b05242df30b6b6caf9d317646f754541824a5a8b` (authored 2025-12-18 08:22:46 -0500) | 2026-08-03 | `git rev-parse HEAD` / `git log -1 --format=%ci` in local clone of https://github.com/blend-capital/blend-utils |
| Clones live in `reference/` (gitignored) | pinned by hash above, not vendored | 2026-08-03 | `.gitignore` `reference/` entry |

## Request struct & RequestType integers

All entries verified 2026-08-03 against blend-contracts-v2 @ `ba22b48` (paths repo-relative).

`Request` struct (`pool/src/pool/actions.rs:12-18`):
```rust
#[derive(Clone)]
#[contracttype]
pub struct Request {
    pub request_type: u32,
    pub address: Address, // asset address or liquidatee
    pub amount: i128,
}
```

Complete `RequestType` enum — exactly 10 variants, `#[repr(u32)]`, no other fill/cancel variants exist (`pool/src/pool/actions.rs:21-34`):

| Variant | Integer | Dispatcher action | Source (file:line @ ba22b48) |
|---|---|---|---|
| `Supply` | 0 | `apply_supply`: mint bTokens (floor), spender→pool transfer; supply-cap enforced | `actions.rs:24` (enum), `:143-152` (dispatch), `:288-305` |
| `Withdraw` | 1 | `apply_withdraw`: burn bTokens (ceil, clamped to balance), pool→to transfer; no health check | `actions.rs:25`, `:153-163`, `:312-332` |
| `SupplyCollateral` | 2 | `apply_supply_collateral`: like Supply but counts as collateral | `actions.rs:26`, `:164-174`, `:339-356` |
| `WithdrawCollateral` | 3 | `apply_withdraw_collateral`: like Withdraw + triggers health check | `actions.rs:27`, `:175-185`, `:363-384` |
| `Borrow` | 4 | `apply_borrow`: mint dTokens (ceil), pool→to transfer, max-util check, health check | `actions.rs:28`, `:186-195`, `:391-408` |
| `Repay` | 5 | `apply_repay`: burn dTokens (floor); overpayment refunded | `actions.rs:29`, `:196-206`, `:415-441` |
| `FillUserLiquidationAuction` | 6 | `auctions::fill(…, 0, request.address=liquidatee, amount as u64 = fill %)` + health check | `actions.rs:30`, `:207-226` |
| `FillBadDebtAuction` | 7 | `auctions::fill(…, 1, request.address=backstop, amount as u64 = fill %)` + health check | `actions.rs:31`, `:227-247` |
| `FillInterestAuction` | 8 | `auctions::fill(…, 2, request.address=backstop, amount as u64 = fill %)`; the ONLY fill without a health check | `actions.rs:32`, `:248-266` |
| `DeleteLiquidationAuction` | 9 | deletes the SENDER's own user-liquidation auction; `request.address`/`amount` ignored | `actions.rs:33`, `:267-276` |

Additional verified behavior:

| Claim | Value | Date | Source |
|---|---|---|---|
| Invalid `request_type` handling | `RequestType::from_u32` panics `PoolError::BadRequest` (=1200); no `Result`/`TryFrom` path exists | 2026-08-03 | `pool/src/pool/actions.rs:41-55` (panic at `:53`); `pool/src/errors.rs:20` |
| Fill `amount` semantics | For request types 6/7/8, `Request.amount` is a fill PERCENTAGE cast `as u64`, must be 1..=100 (`scale_auction` panics `BadRequest` if `percent_filled > 100 \|\| == 0`) — not a token amount | 2026-08-03 | `pool/src/pool/actions.rs:214,235,255`; `pool/src/auctions/auction.rs:194-196` |
| Pre-dispatch checks | Per request: `require_nonnegative(amount)` (panics `NegativeAmountError`=8), then `pool.require_action_allowed(raw u32)` BEFORE `from_u32` | 2026-08-03 | `pool/src/pool/actions.rs:131-142`; `pool/src/validator.rs:12-15`; `pool/src/errors.rs:15` |
| Pool-status gating uses raw integers | status>1 blocks types 4,9 (Borrow, DeleteLiquidationAuction); status>3 blocks types 2,0 (SupplyCollateral, Supply); panics `InvalidPoolStatus`=1206 | 2026-08-03 | `pool/src/pool/pool.rs:75-82`; `pool/src/errors.rs:28` |
| Disabled-reserve gating | disabled reserve blocks types 0,2,4 with `ReserveDisabled`=1223 | 2026-08-03 | `pool/src/pool/reserve.rs:146-156`; `pool/src/errors.rs:53` |
| Health check enforcement | `validate_submit` runs HF check only `if check_health && from_state.has_liabilities()`; min HF 1_0000100 → `InvalidHf` | 2026-08-03 | `pool/src/pool/submit.rs:159-196` (check at `:188-191`) |
| `submit` guard | panics `BadRequest` if `from`, `spender`, or `to` is the pool contract itself | 2026-08-03 | `pool/src/pool/submit.rs:33-38` |
| Self-fill prohibited | `auctions::fill` panics `InvalidLiquidation` if auctioned user == filler | 2026-08-03 | `pool/src/auctions/auction.rs:148-150` |
| `FlashLoan` struct (same file) | `{ contract: Address, asset: Address, amount: i128 }`; flash-loan path gates as Borrow-class action | 2026-08-03 | `pool/src/pool/actions.rs:58-63`; `pool/src/pool/submit.rs:87-91` |

## Auction fill price curve

**Contradiction resolved (2026-08-03):** the curve is a **400-ledger two-phase Dutch auction**, NOT a "200-block decay" with simultaneous lot-up/bid-down. The prior CLAUDE.md note "lot scales 0%→100% over 200 blocks, bid scales 100%→0%" conflates two sequential phases. Decisive source: `scale_auction`, `pool/src/auctions/auction.rs:189-264` @ `ba22b48`:

```rust
let per_block_scalar: i128 = 0_0050000; // modifier moves 0.5% every block
let block_dif = i128(e.ledger().sequence() - auction_data.block);
if block_dif > 200 {
    // lot 100%, bid scaling down from 100% to 0%
    lot_modifier = SCALAR_7;
    if block_dif < 400 {
        bid_modifier = SCALAR_7 - (block_dif - 200) * per_block_scalar;
    } else {
        bid_modifier = 0;
    }
} else {
    // lot scaling from 0% to 100%, bid 100%
    lot_modifier = block_dif * per_block_scalar;
    bid_modifier = SCALAR_7;
}
```

With `t = block_dif = current_ledger − auction_data.block`:

| Claim | Value | Date | Source (file:line @ ba22b48) |
|---|---|---|---|
| Phase 1 (0 ≤ t ≤ 200) | `lot%(t) = t × 0.5%` (0→100%), `bid%(t) = 100%` | 2026-08-03 | `pool/src/auctions/auction.rs:222-225` |
| Fair point (lot=100% AND bid=100%) | exactly `t = 200` (200×0_0050000 = SCALAR_7; corroborated by unit test at seq 1200 vs block 1000) | 2026-08-03 | `auction.rs:212-226`; test `auction.rs:2618-2638` |
| Phase 2 (200 < t < 400) | `lot%(t) = 100%`, `bid%(t) = 100% − (t−200) × 0.5%` (100→0%) | 2026-08-03 | `auction.rs:214-219` |
| t ≥ 400 | `lot = 100%`, `bid = 0` — filler pays nothing, receives full lot; auction persists indefinitely (no auto-expiry) | 2026-08-03 | `auction.rs:217-221` |
| Total price-curve duration | 400 ledgers (~33 min at 5s ledgers); stale-DELETE allowed only after 500 blocks (`delete_stale_auction` panics if `auction.block + 500 > sequence`) | 2026-08-03 | `auction.rs:217`, `:103-106` |
| Auction start block | `AuctionData.block = e.ledger().sequence() + 1` at creation (auction begins the NEXT ledger); `AuctionData { bid: Map<Address,i128>, lot: Map<Address,i128>, block: u32 }` | 2026-08-03 | `auction.rs:55-57`; `user_liquidation_auction.rs:32,37`; `bad_debt_auction.rs:35`; `backstop_interest_auction.rs:38` |
| Fixed point + rounding | all modifiers 7-dec over `SCALAR_7 = 1_0000000`; bid amounts round UP twice (`fixed_mul_ceil` ×2), lot amounts round DOWN twice (`fixed_mul_floor` ×2) — always against the filler | 2026-08-03 | `auction.rs:229-257`; `pool/src/constants.rs:7` |
| Partial fills don't shift the curve | `percent_filled` (u64, whole percents 1..=100) scales amounts BEFORE the block modifier; the remainder is stored back with the ORIGINAL `block`, so the clock never resets; full fill deletes the auction; zero-amount scaled entries are dropped from the maps | 2026-08-03 | `auction.rs:194-196, 203-207, 229-243, 254-263` |
| Curve corroborated by unit test | t=0: bid 100%/lot empty; t=100: bid 100%/lot 50%; t=200: both 100%; t=300: bid 50%/lot 100%; t=400: bid empty/lot 100% | 2026-08-03 | test `test_scale_auction_100_fill_pct`, `auction.rs:2565-2680` |

## Auction asset flows

All verified 2026-08-03 @ blend-contracts-v2 `ba22b48`. `AuctionType`: `UserLiquidation = 0`, `BadDebtAuction = 1`, `InterestAuction = 2` (`pool/src/auctions/auction.rs:19-23`; `from_u32` panics `BadRequest` for >2, `:26-33`). Both `bid` and `lot` maps are keyed by the reserve's UNDERLYING asset address even when values are bToken/dToken amounts.

| Auction type | Lot asset(s) | Bid asset(s) | Filler provides | Filler receives | Backstop LP involved? | Source (file:line @ ba22b48) |
|---|---|---|---|---|---|---|
| User liquidation (0) | bTokens of the user's collateral (keyed by underlying) | dTokens of the user's liabilities (keyed by underlying) | Nothing transferred at fill — filler ASSUMES the dToken debt positions | The user's collateral bToken positions (plus the debt). Pure `Positions` bookkeeping; **no token transfers, no backstop involvement** | No | create: `user_liquidation_auction.rs:152-170`; fill: `:214-221`; position ops `pool/src/pool/user.rs:229-250` |
| Bad debt (1) | Backstop LP tokens (`backstop_token`, BLND:USDC comet LP) — lot must be exactly `[backstop_token]`; `lot_amount = debt_value × 1.2 / token_spot_price`, capped at the pool's backstop balance | The backstop's dToken liabilities (keyed by underlying) | Nothing paid at fill — filler ASSUMES the backstop's dToken debt positions (repaid later via normal Repay) | Backstop LP tokens via `backstop.draw(pool, lot_amount, filler)` — transfer backstop contract → filler | **Yes — as the LOT** | create: `bad_debt_auction.rs:21-27, 50-58, 70-90`; fill: `:104-127`; draw transfer: `backstop/src/backstop/fund_management.rs:15-19` |
| Interest (2) | Underlying tokens equal to each reserve's accrued `backstop_credit` (min total value 200 × oracle_scalar at creation) | Backstop LP tokens — bid must be exactly `[backstop_token]`; `bid_amount = interest_value × 1.2 / token_spot_price` | Backstop LP tokens via `backstop.donate(filler, pool, amount)` (`transfer_from` filler → backstop; **filler must pre-approve the backstop contract**) | Underlying tokens transferred pool contract → filler (`backstop_credit` decremented per reserve). Never touches filler `Positions` | **Yes — as the BID** | create: `backstop_interest_auction.rs:18-24, 46-67, 70-81`; fill: `:86-119`; donate transfer: `backstop/src/backstop/fund_management.rs:32-40` |

Answers to the two special-attention questions:
- **Bad-debt fills do NOT pay underlying debt.** The filler takes over the dToken liability positions (`bad_debt_auction.rs:107-111` — "bid only contains d_token asset amounts") and is paid in backstop LP tokens drawn to their address. They repay the assumed debt later through normal pool `Repay`.
- **Interest fills DO require backstop LP as the bid.** Creation panics `InvalidBid` unless `bid == [backstop_token]` (`backstop_interest_auction.rs:72-74`); fill collects it via `backstop.donate` using `transfer_from`.

Additional verified behavior:

| Claim | Value | Date | Source |
|---|---|---|---|
| Creation percent rules | User liquidation: percent 1..=100, but >95 with all positions ⇒ treated as full (100%) liquidation; bad debt and interest: `user` must be the backstop and `percent` must be exactly 100 | 2026-08-03 | `user_liquidation_auction.rs:25-27, 92-105`; `bad_debt_auction.rs:21-27`; `backstop_interest_auction.rs:18-24` |
| **Missing auction = wasm trap, NOT a contract error** | `get_auction` on an absent auction panics via `.unwrap_optimized()` (`storage.rs:607-616`; trait doc "Panics — If the auction does not exist", `contract.rs:293-295`), and the fill path shares the same lookup (`auctions/auction.rs:152`). Release wasm surfaces this as `Error(WasmVm, InvalidAction)` / `"VM call trapped: UnreachableCodeReached"` — no numbered code, so code-based error matching never fires. Keeper classifiers must match the trap string (`keeper/blend/auction.go isMissingAuctionTrap`) | 2026-08-03 | live sim on Nectar Sandbox (`get_auction` pre-create + first money-test run, where every Execute failed at the existence check); source @ ba22b48 |
| `new_auction` on an EMPTY position → `InvalidLiquidation` (1211) | With no positions, `liability_base <= collateral_base` (0 ≤ 0) trips the not-liquidatable guard at `user_liquidation_auction.rs:51` for every percent — observed live after the borrower was fully liquidated (probes in `docs/evidence/b1-percent-probes.txt`); diagnostic trace showed no `lastprice` calls (nothing to price) | 2026-08-03 | live sims + `user_liquidation_auction.rs:49-52` @ ba22b48 |
| Live band verdicts on a real position (100 XLM coll / 20 USDC debt, XLM=$0.098, HF 0.349) | pct 1/25/50 → `#1214 InvalidLiqTooSmall` (a hardcoded 50% provably reverts); pct 99/100 → accepted as full liquidation, bid `{Circle-USDC: 199992045}` dTokens, lot `{XLM: 1000000000}` bTokens | 2026-08-03 | `docs/evidence/b1-percent-probes.txt` (read-only `new_auction` sims, Nectar Sandbox) |
| **Repay settlement is GROSS** | `apply_repay` adds a spender transfer of the FULL `Request.amount` (`actions.rs:430,436`); when amount exceeds the debt the refund is a SEPARATE pool→spender transfer (`add_for_pool_transfer`, `:431`). The keeper must therefore HOLD the entire repay request amount at settlement — the draw estimator's "draw = padded repay request" covers it exactly. Confirmed live: fill tx `ff90b7a2…` moved 20.2154468 keeper→pool gross and refunded 12.6096347 back in the same tx | 2026-08-09 | `pool/src/pool/actions.rs:415-441` @ ba22b48 (read directly); Horizon `asset_balance_changes` for `ff90b7a2…` |
| Full fill of a deeply underwater position emits `bad_debt` live | The borrower's residual dTokens (unfilled scaled portion) transferred to the backstop inside the fill tx — corroborates `check_and_handle_user_bad_debt` ("Full-fill side effects" above) on a real position | 2026-08-09 | diagnostic events of the failed sim + landed fill `ff90b7a2…` (b-full-cycle.md) |
| `del_auction` is the pool's stale-auction remedy, verified live | Permissionless from t=500 (`contract.rs:584-590`, `auction.rs:97-109`); deleting a 110k-block-old auction and re-creating restarted the price curve cleanly | 2026-08-09 | tx `b8bc5283…` + `e7af9d8e…` (b-full-cycle.md) |
| Oraclemock never goes stale | `lastprice` timestamps with the CURRENT ledger time (observed identical to read time across 6 days without a set), so the pool's 24h staleness rule cannot trip on the sandbox oracle | 2026-08-09 | live `lastprice` reads 2026-08-03 vs 2026-08-09 |
| User-liquidation lot embeds incentive | `est_incentive = (1 − avg_CF/avg_LF)/2 + 1`; partial liquidations must land HF in [1.03, 1.15] (`InvalidLiqTooSmall`/`InvalidLiqTooLarge`) | 2026-08-03 | `user_liquidation_auction.rs:108-139, 193-201` |
| One auction per (type,user) | creation panics `AuctionInProgress` if one exists; bid/lot vectors must have unique addresses | 2026-08-03 | `user_liquidation_auction.rs:22-24`; `bad_debt_auction.rs:28-30`; `backstop_interest_auction.rs:25-27`; `auction.rs:83-84, 270-278` |
| Fill guards | filler ≠ auctioned user (`InvalidLiquidation`); for bad-debt and interest fills, filler ≠ backstop (`BadRequest`) | 2026-08-03 | `auction.rs:148-150`; `bad_debt_auction.rs:104-106`; `backstop_interest_auction.rs:93-95` |
| Full-fill side effects | user-liq full fill: leftover user debt → `check_and_handle_user_bad_debt` (fires only if liabilities remain AND collateral is zero); bad-debt full fill: remaining bad debt defaulted via `check_and_handle_backstop_bad_debt` only when backstop threshold < 0_0000003 (b_rate reduced — suppliers absorb loss) | 2026-08-03 | `user_liquidation_auction.rs:218-220`; `bad_debt_auction.rs:124-127`; `pool/src/pool/bad_debt.rs:59-75, 100+` |
| Interest-auction free-lot edge | after block 400 (bid=0) an interest fill transfers the lot for free (corroborating test `test_fill_interest_auction_empty_bid`) | 2026-08-03 | `backstop_interest_auction.rs:86-119, 1347+` |
| Donate/draw guards | `execute_donate`: `from` may not be the pool or the backstop itself; `execute_draw` requires pool auth + nonnegative amount | 2026-08-03 | `backstop/src/backstop/fund_management.rs:25-30, 44-50` |

## Bad-debt auction creation & backstop LP (Session C)

All source refs @ blend-contracts-v2 `ba22b48` unless noted; verified 2026-08-09.

| Claim | Value | Date | Source |
|---|---|---|---|
| `new_auction` is permissionless | No `require_auth` anywhere in the path: `contract.rs:564-578` → `auctions::create_auction` (`auction.rs:75-95`) → `create_bad_debt_auction_data`. Anyone can create any auction type (validation, not auth, constrains it) | 2026-08-09 | `pool/src/contract.rs:564-578`; `pool/src/auctions/auction.rs:75-95` |
| Bad-debt creation: required args | `new_auction(1, user=backstop, bid, lot, percent=100)`. `bid` = underlying asset addresses where the backstop's liability > 0 — EVERY listed asset must have a positive backstop liability or `InvalidBid`; empty bid or zero debt value also `InvalidBid`; `bid.len() < max_positions`. `lot` must be EXACTLY `[backstop_token]` or `InvalidLot` | 2026-08-09 | `pool/src/auctions/bad_debt_auction.rs:14-74` |
| Bad-debt lot sizing | `lot_amount = debt_value ×fixed_mul_floor 1.2 ÷fixed_div_floor token_spot_price`, then `min(backstop tokens)`; `debt_value` = Σ oracle price × dToken→underlying per bid asset; `token_spot_price` from `backstop.pool_data()` | 2026-08-09 | `bad_debt_auction.rs:59-96` |
| Bad-debt fill mechanics | Scaled bid dTokens move backstop→filler positions (NO tokens transferred; filler must handle the debt, e.g. atomic Repay in the same submit — type-7 fill IS health-checked); scaled lot LP drawn `backstop.draw(pool, lot, filler)`. Full fill (100 of the scaled auction) deletes the auction; UNSCALED remainder debt stays with the backstop (defaults only if backstop tokens < 0_0000003 threshold) | 2026-08-09 | `bad_debt_auction.rs:98-127`; `pool/src/pool/bad_debt.rs:59-75` |
| Sandbox backstop HAS real bad debt | `get_positions(backstop CCT4FMLH…)` on pool `CBUBTHATT…` → `liabilities {0: 235995067}` (23.5995067 Circle-USDC dTokens, residue of the Sprint-B full fill `ff90b7a2…`); `pool_data` → `token_spot_price=12500000` ($1.25/LP), `tokens=500010000000` (50,001 LP) | 2026-08-09 | live `stellar contract invoke … --send=no` reads (simulation only) |
| Deployed Comet (sandbox `CAMYNQY4…`) exit/valuation interface | `exit_pool(pool_amount_in, min_amounts_out, user)` (proportional exit); `wdr_tokn_amt_in_get_lp_tokns_out(token_out, pool_amount_in, min_amount_out, user)` (single-sided exit, exact LP in); `wdr_tokn_amt_out_get_lp_tokns_in(token_out, token_amount_out, max_pool_amount_in, user)`; `swap_exact_amount_in/out`; getters `get_total_supply`, `get_balance(token)`, `get_normalized_weight(token)`, `get_spot_price(in,out)`, `get_swap_fee` | 2026-08-09 | `stellar contract info interface --id CAMYNQY4… --network testnet` (deployed wasm, authoritative for the artifact we hold) |

## Mainnet LP unwind — verified venue, and the vault blocker (2026-08-11)

Read-only mainnet verification (`--send=no`, nothing sent) of whether bad-debt
backstop LP can become vault USDC. Mainnet comet
`CAS3FL6TLZKDGGSISDBWGGPXT3NRR4DYTZD7YOD3HMYO6LTJUVGRVEAM`, snapshot ledger
≈63,906,500. Full write-up, route comparison and recommended path:
`docs/evidence/c-lp-unwind.md`.

| Claim | Value | Date | Source |
|---|---|---|---|
| Mainnet comet legs ARE BLND + the vault's Circle USDC | `get_tokens = [CD25MNVT…(BLND), CCW67TSZ…(USDC)]`; weights 80/20 (`8000000`/`2000000`); `get_swap_fee=30000` (0.30%); `backstopV2.backstop_token()` returns this same comet | 2026-08-11 | live mainnet reads |
| The designated unwind call EXISTS on mainnet with the expected signature | `wdr_tokn_amt_in_get_lp_tokns_out(token_out, pool_amount_in, min_amount_out, user) -> i128`. Mainnet and our sandbox comet spec dumps `diff` EMPTY (34 exports identical) | 2026-08-11 | `stellar contract info interface` on both networks |
| Mainnet comet, sandbox comet and blend-utils `wasm_v1/comet.wasm` are the SAME binary | sha256 `8abc28913035c07411ed5d134e6bfeab4723d97ddd4d1a22a0605d35c94d1a36`, byte-identical to CometDEX `comet-contracts-v1` release v1.0.0. CAVEAT: `reference/blend-contracts-v2/comet.wasm` is a DIFFERENT artifact (`5d721fa5…`), deployed nowhere — it only feeds a `contractimport!` client stub | 2026-08-11 | `stellar contract fetch` + `shasum`; blend-utils `{mainnet,testnet}.contracts.json` `hashes.comet` |
| Comet has NO exit fee; single-sided exit costs `swap_fee × (1 − weight_out)` | Proportional `exit_pool` is exact pro rata (no fee term in `calc_exit_ratio`/`calc_exit_withdrawal_amount`). Single-sided into USDC: floor = 0.30% × 0.80 = **0.2400%** + convexity. `EXIT_FEE` does not exist in comet v1.0.0 source | 2026-08-11 | CometDEX comet-contracts-v1 @ v1.0.0 (release wasm byte-identical to deployed); live quotes match the formula |
| **Exit cost is CONVEX in lot size** — a flat haircut cannot track it | Live mainnet sims, loss vs `token_spot_price`: 22.67 LP → 0.240%; 100 LP → 0.241%; 1,000 LP → 0.254%; 10,000 LP → 0.378%; 100,000 LP → 1.615%; 450,000 LP → 0.861%…1,000,000 LP → 13.16%. Roughly: ≈0.24% at ~$5 of bad debt, ≈1.6% at ~$22k, ≈6.3% at ~$100k | 2026-08-11 | live `wdr_tokn_amt_in_get_lp_tokns_out` simulations |
| A single exit call is capped at ≈ `USDC_balance / 3` | 1,100,000 LP → 250,361.83 USDC OK; 1,140,000 LP → `Error(Contract, #21)`. Pool USDC side is only 765,737.84 total, so ceiling ≈255,246 USDC out | 2026-08-11 | live sims |
| The exit quote can be simulated by a NON-HOLDER | `min_amount_out`/max-out-ratio are asserted BEFORE the withdraw event; `pull_shares`/`burn_shares` come after. A zero-LP account's sim fails `#29` but still emits the identical `token_amount_out`. So the keeper can quote its exit before holding the LP | 2026-08-11 | live sim from a 0-LP account; comet v1.0.0 source ordering |
| Bad-debt LP is NOT subject to the backstop withdrawal queue | `execute_draw` transfers raw comet LP directly to the filler, so Q4W (a 17-day queue for backstop DEPOSITORS) never applies to auction proceeds — the exit is immediate | 2026-08-11 | `backstop/src/backstop/fund_management.rs` @ ba22b48 |
| **The external BLND market cannot absorb Nectar size** | Only one callable Soroban venue: Soroswap USDC/BLND pair `CCCDU62T…` holding ≈$25,986 USDC, 5 swaps / ≈$206 volume in 7 days. Selling the BLND leg externally is not viable above ≈$250 of LP — the comet itself is the venue | 2026-08-11 | live Soroswap router/pair reads |
| **BLOCKER: the vault REFUSES proceeds from a keeper with no outstanding draw** | `return_proceeds` returns `NoDraw` (=13) when `drawn <= 0` — the deliberate VLT-2 anti-donation guard. Bad-debt fills are float-funded, so `drawn == 0` by construction and the unwound USDC can never be credited to depositors. `deposit` is not a substitute: it mints shares to the operator. Verified live: keeper-alpha `get_keeper_draw` = `0` | 2026-08-11 | `contracts/nectar-vault/src/lib.rs:307-315`; `src/types.rs:60` (`NoDraw = 13`); test `src/test.rs:376`; live read on deployed vault `CDOGQY7N…` |
| `token_spot_price` is a marginal, zero-slippage, sans-fee mark — and it drifts down | Defined `(total_usdc / total_shares) × 5` (`backstop/src/backstop/pool.rs:41-53`); reproduced bit-for-bit from live reserves. BLND-only deposits (the backstop's own emission auto-compounding: 290 of 312 single-sided BLND deposits in a 7.8-day window) raise supply without raising the USDC side, walking the mark DOWN. Measured BLND peak-to-trough over that quiet week: 2.21%, ≈80% pass-through to LP NAV | 2026-08-11 | live comet/backstop reads; 670 POOL events over full RPC retention |

## Testnet addresses

### Pool public read interface (from pinned source)

Verified in `pool/src/contract.rs` @ `ba22b48` (blend-contracts-v2), 2026-08-03:

| Function | Signature | Source |
|---|---|---|
| `get_config` | `fn get_config(e: Env) -> PoolConfig` — `PoolConfig { oracle: Address, min_collateral: i128, bstop_rate: u32, status: u32, max_positions: u32 }` | `pool/src/contract.rs:81`, `pool/src/storage.rs:26-33` |
| `get_reserve_list` | `fn get_reserve_list(e: Env) -> Vec<Address>` | `pool/src/contract.rs:88` |
| `get_reserve` | `fn get_reserve(e: Env, asset: Address) -> Reserve` | `pool/src/contract.rs:94` |
| `get_positions` | `fn get_positions(e: Env, address: Address) -> Positions` | `pool/src/contract.rs:101` |
| `get_auction` | `fn get_auction(e: Env, auction_type: u32, user: Address) -> AuctionData` | `pool/src/contract.rs:295` |
| `submit` | `fn submit(e: Env, from: Address, spender: Address, to: Address, requests: Vec<Request>) -> Positions` | `pool/src/contract.rs:116-122` |
| (oracle getter) | none — the oracle address is a field of `PoolConfig`, read via `get_config` | `pool/src/storage.rs:27` |
| (backstop getter on pool) | none in the pool trait; backstop is a `__constructor` arg (`backstop_id`) | `pool/src/contract.rs:340-352` |
| backstop: `backstop_token` | `fn backstop_token(e: Env) -> Address` (on the backstop contract) | `backstop/src/contract.rs:76` |
| backstop: `pool_data` | `fn pool_data(e: Env, pool: Address) -> PoolBackstopData` | `backstop/src/contract.rs:73` |

### Canonical testnet deployment (live-read 2026-08-03)

All live reads: `stellar contract invoke --id <C…> --source-account GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W --network testnet --send=no -- <fn>` (simulation only, nothing sent).

| Claim | Value | Date | Source (command / URL) |
|---|---|---|---|
| Canonical testnet address list | `blend-utils/testnet.contracts.json` | 2026-08-03 | https://docs.blend.capital/mainnet-deployments.md ("All testnet contracts addresses can be found here …blend-utils/blob/main/testnet.contracts.json"); file read at blend-utils@`b05242d` |
| "Blend TestnetV2 pool" and "RegionalStarterPack Pool V2" are the SAME contract | `CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF` — there is one canonical V2 testnet pool, not two | 2026-08-03 | `testnet.contracts.json` key `TestnetV2` @ blend-utils@`b05242d`; on-chain instance storage `Name = "TestnetV2"` via `stellar contract read --id CCEBVDYM… --network testnet --durability persistent` |
| TestnetV2 pool on-chain name | `"TestnetV2"` (NOT "RegionalStarterPack") | 2026-08-03 | `stellar contract read --id CCEBVDYM… --network testnet --durability persistent` → `{key: {symbol: "Name"}, val: {string: "TestnetV2"}}` |
| TestnetV2 pool wasm sha256 | `a41fc53d6753b6c04eb15b021c55052366a4c8e0e21bc72700f461264ec1350e` = blend-utils `hashes.lendingPoolV2` (live pool runs blend-utils' shipped V2 wasm) | 2026-08-03 | `stellar contract fetch --id CCEBVDYM… --network testnet` + `shasum -a 256`; `testnet.contracts.json` `hashes.lendingPoolV2` |
| TestnetV2 reserve list (4 reserves, in index order) | `[CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC, CAZAQB3D7KSLSNOSQKYD2V4JP5V2Y3B4RDJZRLBFCCIXDCTE3WHSY3UE, CAP5AMC2OHNVREO66DFIN6DHJMPOBAJ2KCDDIMFBR7WWJH5RZBFM3UEI, CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU]` | 2026-08-03 | live `get_reserve_list` |
| Reserve 0: XLM (native SAC) | `CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC` — `symbol()="native"` | 2026-08-03 | live `symbol`/`name` |
| Reserve 1: wETH SAC | `CAZAQB3D7KSLSNOSQKYD2V4JP5V2Y3B4RDJZRLBFCCIXDCTE3WHSY3UE` — `wETH:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56` | 2026-08-03 | live `symbol`/`name` |
| Reserve 2: wBTC SAC | `CAP5AMC2OHNVREO66DFIN6DHJMPOBAJ2KCDDIMFBR7WWJH5RZBFM3UEI` — `wBTC:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56` | 2026-08-03 | live `symbol`/`name` |
| Reserve 3: pool USDC SAC (Blend testnet USDC) | `CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU` — `USDC:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56` | 2026-08-03 | live `symbol`/`name` |
| TestnetV2 oracle | `CAZOKR2Y5E2OSWSIBRVZMJ47RUTQPIGVWSAQ2UISGAVC46XKPGDG5PKI` — this is Blend's **mock oracle** (`oraclemock`), NOT Reflector: its on-chain wasm sha256 `66c0b87b5eb481be594175d59e66ec9a9ac8945be0fec4e09f6c28bf7a1708be` equals `testnet.contracts.json` `hashes.oraclemock` | 2026-08-03 | live `get_config` → `oracle`; `stellar contract fetch --id CAZOKR2Y… ` + `shasum -a 256`; blend-utils@`b05242d` `testnet.contracts.json` |
| TestnetV2 pool config (live) | `bstop_rate=1000000` (10% in 7-dec), `max_positions=8`, `min_collateral=0`, `status=0` | 2026-08-03 | live `get_config` |
| Backstop V2 | `CBDVWXT433PRVTUNM56C3JREF3HIZHRBA64NB2C3B2UNCKIS65ZYCLZA` — serves the TestnetV2 pool (live `pool_data(CCEBVDYM…)` returns `tokens=547206929909`, `shares=547206929909`, `usdc=323702229332`, `blnd=4417757006644`, `token_spot_price=29577680`, `q4w_pct=241068`) | 2026-08-03 | `testnet.contracts.json` `backstopV2`; live `pool_data` |
| Backstop token (comet BLND:USDC LP) | `CA5UTUUPHYL5K22UBRUVC37EARZUGYOSGK3IKIXG2JLCC5ZZLI4BDWDM` — `symbol()="CPAL"`, `name()="Comet Pool Token"` | 2026-08-03 | live `backstop_token()` on backstop; live `symbol`/`name` on token |
| BLND token (testnet) | `CB22KRA3YZVCNCQI64JQ5WE7UY2VAV7WFLK6A2JN3HEX56T2EDAFO7QF` — `BLND:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56` | 2026-08-03 | `testnet.contracts.json` `BLND`; live `symbol`/`name` |
| Pool factory V2 (testnet) | `CDV6RX4CGPCOKGTBFS52V3LMWQGZN3LCQTXF5RVPOOCG4XVMHXQ4NTF6` | 2026-08-03 | `testnet.contracts.json` `poolFactoryV2` @ blend-utils@`b05242d` (not live-read) |

## Mainnet settlement asset (decisive for the USDC-bridging decision)

| Claim | Value | Date | Source |
|---|---|---|---|
| **Blend mainnet settles the SAME USDC our vault does** | blend-utils `mainnet.contracts.json` `USDC` = `CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75`, identical to the mainnet Circle USDC SAC the vault settles in (CLAUDE.md / docs/NETWORKS.md). Confirmed live on a real mainnet pool: `FixedV2` (`CAJJZSGM…`) `get_reserve_list` → `[CAS3J7GY… (XLM), CCW67TSZ… (Circle USDC), CDTKPWPL…]` | 2026-08-03 | `reference/blend-utils/mainnet.contracts.json` @ `b05242d`; live `get_reserve_list` via `--rpc-url https://mainnet.sorobanrpc.com` (simulation only, nothing sent) |
| Consequence | The cross-USDC problem is a **testnet-only artifact** (Blend's testnet deployment mints its own `USDC:GATALTGT…`, while we chose Circle's testnet USDC so the trustline/faucet/deposit flow matches mainnet). No asset bridging is required at the mainnet target | 2026-08-03 | this session; see Decisions |
| Testnet Soroswap XLM price ≠ oracle price | The XLM/Circle-USDC Soroswap pair is deep (≈4,084,363 XLM / 401,857 USDC) and prices XLM at **≈$0.098**; 100 XLM → 9.809 USDC. A sandbox oracle price above that makes any exit swap breach the keeper's slippage floor — for coherent testnet liquidation economics the mock oracle must be set near the DEX price | 2026-08-03 | live `router_get_amounts_out` + `get_reserves` |

## Our testnet USDC

| Claim | Value | Date | Source |
|---|---|---|---|
| NectarVault settlement asset (Circle testnet USDC SAC) | `CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA` — `USDC:GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5` (Circle issuer, faucet.circle.com, not mintable by us) | 2026-08-03 | live `symbol`/`name` read; CLAUDE.md deployed-contracts section |
| **Our Circle USDC ≠ Blend TestnetV2 pool USDC** | The pool's USDC reserve is `CAQCFVLO…` (`USDC:GATALTGT…`, Blend's testnet issuer). Our vault settles in `CBIELTK6…` (`USDC:GBBD47IF…`, Circle). They are different assets; a keeper filling TestnetV2 auctions pays/receives Blend-USDC, not Circle-USDC. | 2026-08-03 | live `get_reserve_list` + `symbol`/`name` reads on both SACs |

## blend-utils pool deploy

All verified 2026-08-03 @ blend-utils `b05242d` (paths repo-relative). Use the **v2** tree (`src/v2/` → compiled `lib/v2/`); the parallel `src/v1/` tree targets `poolFactory`/wasm_v1 and is not what we want. SDK versions used by the scripts: `@blend-capital/blend-sdk v3.2.0-beta.3`, `@stellar/stellar-sdk 14.0.0-rc.3` (`package.json:26-27`).

| Claim | Value | Date | Source |
|---|---|---|---|
| Build | `npm i && npm run build` (tsc → `lib/`) | 2026-08-03 | `package.json:7`; `README.md:10-17` |
| Required `.env` | `RPC_URL`, `FRIENDBOT_URL`, `NETWORK_PASSPHRASE`, `ADMIN` (secret key); testnet: `RPC_URL=https://soroban-testnet.stellar.org`, passphrase `Test SDF Network ; September 2015` | 2026-08-03 | `src/utils/env_config.ts:28-39`; `.env.example` |
| Deploy a new pool (testnet) | `node ./lib/v2/user-scripts/deploy-pool.js testnet` — only `network` is validated; README's `<REVOKE-ADMIN>` arg is IGNORED by the v2 script (revoke is a separate `revoke-admin.js`, which needs a third `new_admin` arg the README omits) | 2026-08-03 | `README.md:93`; `src/v2/user-scripts/deploy-pool.ts:19-21`; `src/v2/user-scripts/revoke-admin.ts:20-27` |
| Pool params (edit constants in script) | `pool_name`, `backstop_take_rate` (e.g. 0.5e7), `max_positions`, `min_collateral`, `reserves: string[]` (addressBook names), `reserve_configs: ReserveConfigV2[]`, `poolEmissionMetadata` | 2026-08-03 | `src/v2/user-scripts/deploy-pool.ts:39-87` |
| Creation mechanism | `PoolFactoryContractV2('poolFactoryV2').deployPool(DeployV2Args)` with random 32-byte salt; new address saved into `<network>.contracts.json` under `pool_name` | 2026-08-03 | `src/v2/pool/pool-setup.ts:10-21`; `deploy-pool.ts:91` |
| Reserves + risk params | Per reserve: `pool.queueSetReserve({asset, metadata})` then immediately `pool.setReserve(asset)` in try/catch — fails (logged) if the queue timelock hasn't elapsed; re-run `setReserve` later. `ReserveConfigV2` fields: index, decimals, c_factor, l_factor, util, max_util, r_base, r_one, r_two, r_three, reactivity, supply_cap, enabled | 2026-08-03 | `src/v2/pool/reserve-setup.ts:11-27`; `deploy-pool.ts:44-75` |
| Reserve-queue timelock (contract side) | `unlock_time = now + SECONDS_PER_WEEK` (604800 s = 7 days) UNLESS `pool_config.status == 6` (Setup) — a fresh pool still in Setup adds reserves instantly; once out of Setup, every reserve change waits 7 days | 2026-08-03 | blend-contracts-v2 `pool/src/pool/config.rs:78-81`, `pool/src/constants.rs:13` @ `ba22b48` |
| Emissions config | `pool.setEmissionsConfig(ReserveEmissionMetadata[])`; **contract source truth: `res_type` 0 = dTokens (borrow), 1 = bTokens (supply)** (`reserve_token_id = res_index*2 + res_type`); blend-utils README.md:83 states the OPPOSITE ("0=lenders") and is wrong — its own code comments agree with the contract | 2026-08-03 | contract: blend-contracts-v2 `pool/src/contract.rs:229-241` doc + `pool/src/emissions/manager.rs:42-43`; utils: `deploy-pool.ts:76-87`, `README.md:83` |
| Oracle | A pool-CONSTRUCTOR arg (`DeployV2Args.oracle`), not settable afterwards by any script here. GOTCHA: `deploy-pool.ts:98` looks up addressBook key `'oracle'` but `testnet.contracts.json` only has `'oraclemock'` — testnet deploy throws unless you add an `oracle` entry (or edit the key) | 2026-08-03 | `src/v2/user-scripts/deploy-pool.ts:98`; `src/utils/address-book.ts:70-72`; `testnet.contracts.json` |
| Backstop funding token | BLND:USDC comet LP (80/20 weights); comet = `CA5UTUUP…` on testnet | 2026-08-03 | `README.md:114,135`; `src/v2/testing-scripts/mock-example.ts:85-87` |
| Mint LP | `node ./lib/v2/user-scripts/mint-lp.js <net> <user> <deposit-asset 0=BLND\|1=USDC\|2=both> <amount>` (comet `joinPool`/single-sided deposit) | 2026-08-03 | `src/v2/user-scripts/mint-lp.ts:24-83`; `README.md:130` |
| Seed backstop | `node ./lib/v2/user-scripts/fund-backstop.js <net> <user> <pool> <amount>` → `backstopV2.deposit({from, pool_address, amount})` (7-dec) | 2026-08-03 | `src/v2/user-scripts/fund-backstop.ts:38-47`; `README.md:147` |
| Backstop threshold | `k = BLND^0.8 × USDC^0.2 ≥ 100,000` required for activation/reward zone; check with `get-backstop-threshold.js <net> <pool>` (README wrongly says `<user>`); output is ADDITIONAL LP needed (subtracts existing) padded ×1.003 | 2026-08-03 | `src/v2/user-scripts/get-backstop-threshold.ts:13-22, 74-104` |
| Activate + reward zone | `set-status.js <net> <admin> <pool> 0` (Active) then `reward-zone-add.js <net> <user> <pool_to_add> [pool_to_remove]`; reference flow used a 50,001-LP backstop deposit | 2026-08-03 | `src/v2/testing-scripts/backstop-pool-setup.ts:26-62`; `set-status.ts:42-45`; `reward-zone-add.ts:43-51` |
| **Testnet BLND: NO public faucet** | BLND is a classic asset (`BLND`, issuer = Blend's ADMIN deployer) wrapped as SAC; the repo mints via classic payment FROM THE ISSUER (`classic_mint`), then `set_admin` → emitter. Third parties cannot mint; without the issuer key, BLND flows only via emissions (`distribute.js`: emitter.distribute → backstop.distribute → pool.gulpEmissions) or a DEX/comet swap | 2026-08-03 | `src/v2/testing-scripts/backstop-pool-setup.ts:19-29`; `src/external/token.ts:23-32`; `mock-example.ts:247-253`; `src/v2/user-scripts/distribute.ts:44-68` |
| Known script bugs (testnet) | v2 `distribute.ts:50` looks up key `'backstop'` (absent on testnet — only `'backstopV2'`) → throws unless aliased; several v2 user-scripts have off-by-one argv checks (`mint-lp.ts`, `set-status.ts`, `fund-backstop.ts`: check `<5` but read `argv[5]`) | 2026-08-03 | `src/v2/user-scripts/distribute.ts:50`; `mint-lp.ts:16-22`; `set-status.ts:17-23` |
| Auction test toolkit exists | `src/v2/auctionTest/` (setupEnv, setPrice, createUser, liquidateUser, createBadDebtAuction, createInterestAuction, submitAction) — deploys a dedicated mock-oracle env; useful for keeper end-to-end tests | 2026-08-03 | `src/v2/auctionTest/` directory @ `b05242d` |
| Mock oracle prices (testnet flow) | mock-example `oracle-setup` registers [USDC, XLM, wETH, wBTC], decimals=7, resolution=300, `setPriceStable([1, 0.42, 4000, 100000] × 1e7)` — FIXED prices, admin-settable | 2026-08-03 | `src/v2/testing-scripts/oracle-setup.ts:22-56` |

## Nectar Sandbox (self-owned Blend V2 testnet environment, deployed 2026-08-03)

Full deploy trail + rationale: `docs/evidence/a4-sandbox.md`; scripts:
`scripts/nectar-sandbox/` (blend-utils lib @ `b05242d`).

| Claim | Value | Date | Source |
|---|---|---|---|
| Pool "Nectar Sandbox" | `CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V` — live `get_config`: `bstop_rate=1000000, max_positions=4, min_collateral=0, status=0` | 2026-08-03 | deploy tx `0bacf5bf…`; live read |
| Reserves | idx 0 = Circle USDC `CBIELTK6…` (c=l=0.95), idx 1 = native XLM `CDLZFC3S…` (c=l=0.75) — the REAL vault asset, no mock in the capital path | 2026-08-03 | live `get_reserve_list`; `scripts/nectar-sandbox/01-deploy-sandbox.mjs` |
| Oracle (ours, admin-settable) | `CAPLUDWOVS6IEXZ35MLNDLXDYEDZMGFCK6QU4JBUAYANITAV3ZH2I27N` — oraclemock wasm, decimals 7, assets [Circle-USDC, XLM], resting prices [1.00, 0.42] | 2026-08-03 | deploy txs in a4-sandbox.md; live `lastprice` |
| Backstop / emitter / factory / comet | `CCT4FMLH…` / `CAHQB47P…` / `CDYY5FJ6…` / `CAMYNQY4…`; comet pairs admin-issued `BLND:GATK27P6…` (`CB627JMA…`) and `USDC:GATK27P6…` (`CAKGVZ34…`) 80/20; backstop seeded with 50,001 LP (issuer-minted 500,100 BLND + 12,501 USDC) | 2026-08-03 | a4-sandbox.md tx table |
| Pool Active is genuine | status 0 set via `set_status(0)` AFTER the backstop met the 100k threshold — `execute_set_pool_status` has no admin bypass (`pool/src/pool/status.rs:81-90` @ ba22b48); a canonical-factory pool was impossible for us (no BLND faucet) | 2026-08-03 | status.rs source; a4-sandbox.md |
| On-demand insolvency lever | borrower `GCCTPHRT…`: 100 XLM collateral / 20 Circle-USDC debt; `run.sh 03 0.15` → HF 0.5344 (keeper-detected, log in a4-sandbox.md), `run.sh 03 0.42` → HF ≈ 1.50 (resting) | 2026-08-03 | txs `0adf3634…`, `f33b1835…`, `a568a996…`; keeper log |
| Pool event topic layouts | SUPERSEDED 2026-08-13 by the full section "Pool event schemas — complete emission surface (Session D1)" below, which is derived from the `publish()` calls rather than the doc comments and adds the data payloads, the `bad_debt` topic inversion, and the filler-in-data case | 2026-08-03 | `pool/src/events.rs` @ ba22b48 |

## RPC/XDR calling convention (Gate 0.7)

| Claim | Value | Date | Source |
|---|---|---|---|
| Our Go XDR encoding of `submit()` matches the deployed contract | Request encoded as `ScMap` with lexicographically sorted symbol keys (`address`, `amount`, `request_type`), types Address/i128/u32; `submit(from, spender, to, Vec<Request>)` simulated against live TestnetV2 pool → returns well-formed `Positions` (`ScvMap` with keys `collateral`/`liabilities`/`supply`) + 1 decodable auth entry + decodable `transactionData`. Simulation only; nothing signed or sent | 2026-08-03 | test `keeper/soroban/gate07_verification_test.go` (run: `GATE07_LIVE=1 go test ./soroban -run TestGate07SubmitSimulateEncoding`); evidence `docs/evidence/gate-0-7-simulate.json` (latestLedger 3935602) |
| `Positions` return shape | `{ liabilities: Map<u32,i128>, collateral: Map<u32,i128>, supply: Map<u32,i128> }` keyed by reserve index | 2026-08-03 | blend-contracts-v2 `pool/src/pool/user.rs:11-15` @ `ba22b48`; live simulate result |
| `getEvents` paginates by ledger SEGMENT, not just event count | One request scans a bounded segment (~10k ledgers) and returns `cursor` + possibly ZERO events even when events exist later in the window (observed live: startLedger=latest−17000 → `events:[]` + cursor, while 8 pool events sat 2 segments later; testnet RPC retention `oldestLedger` ≈ latest−121k). Clients MUST follow `cursor` (omitting `startLedger` on paged requests) until the cursor's TOID ledger reaches `latestLedger` | 2026-08-03 | live curl comparison in session; fix + regression test `keeper/soroban/rpc.go` `GetEvents` / `rpc_events_test.go` |

## Pool event schemas — complete emission surface (Session D1)

All entries verified 2026-08-13 against blend-contracts-v2 @ `ba22b48`; paths repo-relative.
Each row was independently re-derived by an adversarial verifier that read the cited lines.

**Emission surface is closed.** `grep -rn '\.publish(' pool/` returns 23 hits, all in
`pool/src/events.rs` (lines 18, 38, 53, 66, 79, 91, 104, 118, 130, 144, 159, 172, 187, 208, 229,
250, 265, 280, 303, 316, 337, 360, 374). Nothing in `pool/src/auctions/` publishes anything.
`grep -rn 'contractevent'` repo-wide: zero hits (soroban-sdk 22.0.7, no SDK-23 derive macro).

**`symbol_short!` is used ZERO times in the checkout.** Every `topic[0]` is
`Symbol::new(e, "literal")`, so long names survive in full — `withdraw_collateral` (19 chars),
`supply_collateral` (17), `defaulted_debt` (14). Topics are `ScVal::Symbol` (`ScvSymbol`),
never `ScvString`, for both short and long names (soroban-sdk-22.0.11 `src/symbol.rs:147-161`).

| Event | Topics | Data | Def site | User topic idx | Debt effect |
|---|---|---|---|---|---|
| `supply` | `[Sym, asset, from]` | `(i128 tokens_in, i128 b_tokens_minted)` | `events.rs:185-188` | 2 | NEUTRAL |
| `withdraw` | `[Sym, asset, from]` | `(i128 tokens_out, i128 b_tokens_burnt)` | `events.rs:200-209` | 2 | NEUTRAL |
| `supply_collateral` | `[Sym, asset, from]` | `(i128 tokens_in, i128 b_tokens_minted)` | `events.rs:221-230` | 2 | NEUTRAL |
| `withdraw_collateral` | `[Sym, asset, from]` | `(i128 tokens_out, i128 b_tokens_burnt)` | `events.rs:242-251` | 2 | NEUTRAL (lowers HF) |
| `borrow` | `[Sym, asset, from]` | `(i128 tokens_out, i128 d_tokens_minted)` | `events.rs:263-266` | 2 | **INCURS** (`actions.rs:400-401` → `user.rs:69-79`) |
| `repay` | `[Sym, asset, from]` | `(i128 tokens_in, i128 d_tokens_burnt)` | `events.rs:278-281` | 2 | REDUCES (`actions.rs:432/437`) |
| `flash_loan` | `[Sym, asset, from, contract]` — **4 topics** | `(i128 tokens_out, i128 d_tokens_minted)` | `events.rs:294-304` | 2 | INCURS, may be repaid in the same tx (`submit.rs:88-103`) |
| `new_auction` | `[Sym, u32 auction_type, user]` | `(u32 percent, AuctionData)` | `events.rs:329-338` | 2 | NEUTRAL (announcement only) |
| `fill_auction` | `[Sym, u32 auction_type, user]` | `(Address filler, i128 fill_percent, AuctionData)` | `events.rs:351-362` | 2 | **two-sided — see below** |
| `delete_auction` | `[Sym, u32 auction_type, user]` | `()` → **ScVal Void**, not an empty vec | `events.rs:372-375` | 2 | NEUTRAL |
| `bad_debt` | `[Sym, user, asset]` — **INVERTED vs the action events** | bare `i128 d_tokens` | `events.rs:157-160` | **1** | **CLEARS** for the user (`bad_debt.rs:65-73`) |
| `defaulted_debt` | `[Sym, asset]` — 2 topics, **no debtor address at all** | bare `i128 d_tokens_burnt` | `events.rs:170-173` | — | CLEARS for the backstop (`bad_debt.rs:113-119`) |
| `claim` | `[Sym, from]` — 2 topics | `(Vec<u32> reserve_token_ids, i128 amount)` | `events.rs:142-146` | 1 | NEUTRAL (BLND emissions only) |

Remaining pool events are admin/config/emissions-only and carry no user position:
`set_admin`, `update_pool`, `queue_set_reserve`, `cancel_set_reserve`, `set_reserve`,
`set_status` (×2), `reserve_emission_update`, `gulp_emissions`, `gulp`.

| Claim | Value | Date | Source |
|---|---|---|---|
| **v1 auction event names DO NOT EXIST in v2** | `new_liquidation_auction`, `delete_liquidation_auction`, `fill_liquidation_auction` appear nowhere as Symbol literals. `grep -rn 'liquidation_auction' --include=*.rs` hits only Rust fn/test identifiers (`auction.rs:121`, `actions.rs:269`, test fn `actions.rs:1907`). v2 collapsed them into `new_auction`/`fill_auction`/`delete_auction` keyed by an `auction_type: u32` topic | 2026-08-13 | source grep @ ba22b48; corroborated by 3 independent agents + live wire capture |
| `AuctionType` u32 discriminants | `UserLiquidation = 0`, `BadDebtAuction = 1`, `InterestAuction = 2`; `from_u32` panics `BadRequest` above 2 | 2026-08-13 | `pool/src/auctions/auction.rs:17-23` (enum), `:25-34` (from_u32) |
| **For `auction_type` 1 and 2, `topic[2]` is ALWAYS the backstop contract, never a borrower** | Creation panics unless `user == backstop` | 2026-08-13 | `pool/src/auctions/bad_debt_auction.rs:21-24`; `pool/src/auctions/backstop_interest_auction.rs:18-21`; live-observed type-1 user = sandbox backstop `CCT4FMLH…` |
| **The FILLER of a `fill_auction` (types 0 and 1) BECOMES A BORROWER — and is only in the DATA, never a topic** | `filler_state.add_positions(e, pool, lot, bid)` routes the `bid` d-token map through `add_liabilities`; the filler is then health-checked and the tx panics `InvalidHf` below 1.0000100. So a filler is a debt-carrying position that **cannot be found by scanning topics** and cannot be server-side filtered by `getEvents` | 2026-08-13 | `user_liquidation_auction.rs:214-216`, `bad_debt_auction.rs:109-111` → `pool/src/pool/user.rs:243-249`; health check `actions.rs:236` → `submit.rs:186-195` |
| **"Debt reached zero" is NOT derivable from `repay` events** | `repay` publishes only deltas `(tokens_in, d_tokens_burnt)`; `apply_repay` never produces a post-action balance. An over-repay that zeroes the position is indistinguishable from a partial repay of the same size. Liabilities are also mutated by paths with different event shapes (`fill_auction` bid map, `bad_debt`, `defaulted_debt`, `flash_loan`). **Consequence: events are triggers; `get_positions` is the authority on remaining liability** | 2026-08-13 | `events.rs:278-281`; `actions.rs:415-441`; `user.rs:89-97`; `contract.rs:448-450` |
| Two events whose payload IS the full remaining liability | `bad_debt` and `defaulted_debt` both iterate the entire liabilities map and emit the full per-asset `liability_balance`, which is then removed in full. After `bad_debt(user, asset)` that user's liability for that asset is exactly 0 by construction. `bad_debt` is gated on `has_liabilities() && !has_collateral()` | 2026-08-13 | `pool/src/pool/bad_debt.rs:59`, `:65-73`, `:113-119` |
| `bad_debt` fires INSIDE a full liquidation fill, before `fill_auction` | Two emit paths: the standalone `bad_debt` entry point (`contract.rs:592-596`) and the full-fill path (`user_liquidation_auction.rs:218-220`). Published during `auctions::fill` (`actions.rs:208-215`), i.e. BEFORE the `fill_auction` publish at `actions.rs:218-225` | 2026-08-13 | source read @ ba22b48 |
| A liquidation fill emits **no** supply/withdraw/borrow/repay events | Positions move directly via `rm_positions`/`add_positions`. An indexer keyed only on the six action events misses every position change caused by a fill | 2026-08-13 | `user_liquidation_auction.rs:214-216` |
| **No event outside the pool contract names a pool borrower** | Backstop events (`deposit`, `queue_withdrawal`, `dequeue_withdrawal`, `withdraw`, `claim`) key on `UserBalance(PoolUserKey{pool,user})` holding LP **shares** — a ledger disjoint from pool debt. `distribute`/`rw_zone_*`/`gulp_emissions`/factory `deploy` carry contract addresses or none. `draw`'s `to` (the bad-debt filler) does acquire debt, but that address is already in `fill_auction` data | 2026-08-13 | `backstop/src/storage.rs:69-72,77`; `backstop/src/events.rs` (11 publish sites); `backstop/src/contract.rs:315-322` |
| `emitter/` has no source in this pin | Contains only `README.md`; not a workspace member. v2 reuses the v1 Emitter. Its event shapes are **UNVERIFIED** from this pin | 2026-08-13 | `reference/blend-contracts-v2/emitter/README.md`; root `Cargo.toml` members |
| Doc-comment brackets in `events.rs` are decorative — single-value payloads publish as BARE ScVals | `bad_debt` doc says `[d_tokens: i128]` (`:151`) but `:159` publishes the scalar. Same mismatch at `:164/172` (defaulted_debt), `:124/130` (gulp_emissions), `:308/316` (gulp), `:11/18` (set_admin), `:85/91` + `:97/104` (set_status), `:59/66` (cancel_set_reserve). Trust the `publish()` call, never the bracket | 2026-08-13 | source read + live wire capture (`bad_debt` body decoded as `SCV_I128`) |
| `AuctionData` declaration order vs wire order differ | Declared `bid: Map<Address,i128>` (`:46`), `lot: Map<Address,i128>` (`:54`), `block: u32` (`:57`). On the wire it serializes as an `SCV_MAP` with keys in **alphabetical** symbol order `bid, block, lot`. (The ScMap ordering is soroban-sdk behavior, not fixed by blend source — recorded here from live decode) | 2026-08-13 | `pool/src/auctions/auction.rs:36-58`; live decode of a sandbox `new_auction` |
| **`AuctionData.bid` can legitimately be an EMPTY map on a real `fill_auction`** | Observed live at sandbox ledger 4055978 (`block_dif` 403 > 400): `scale_auction` only writes a bid entry `if to_fill_scaled > 0`, and past t=400 the bid modifier is 0, so the entry is dropped rather than stored as 0. Code that indexes `bid[0]` or divides by a bid amount panics or divides by zero on real data | 2026-08-13 | `pool/src/auctions/auction.rs:214-221`, `:238-242`; live event decode |
| Live-observed vs source-only event names | **Observed on the wire (11)**: `new_auction`, `fill_auction`, `delete_auction`, `bad_debt`, `supply`, `withdraw`, `supply_collateral`, `withdraw_collateral`, `borrow`, `repay`, `claim`. **Source-defined but NOT observed (11)**: `set_admin`, `update_pool`, `queue_set_reserve`, `cancel_set_reserve`, `set_reserve`, `set_status`, `reserve_emission_update`, `gulp_emissions`, `defaulted_debt`, `flash_loan`, `gulp` — their wire shapes are source-derived only. Auction type 2 was not observed either | 2026-08-13 | live `getEvents` over the full retention window on `CBUBTHAT…` (12 events) and `CCEBVDYM…` (1560 events) |

## Soroban RPC `getEvents` — limits observed live (Session D1)

All measured 2026-08-13 by direct `curl` against `https://soroban-testnet.stellar.org:443`.
These are **observations, not documentation**; every row names the error text actually returned.

| Claim | Value | Date | Source |
|---|---|---|---|
| Max `pagination.limit` | **10000 inclusive**. 10001 → code `-32602`, `"limit must not exceed 10000"`. `limit: 0` is accepted and means "no limit" (returns the whole segment). `limit: -1` → `-32602` `"json: cannot unmarshal number -1 into Go struct field PaginationOptions.pagination.limit of type uint"` | 2026-08-13 | live curl binary search (200/1000/5000/10000/10001) |
| `startLedger` valid range | Must lie in `[oldestLedger, latestLedger]` **inclusive**; both endpoints accepted. Outside → code `-32600`, `"startLedger must be within the ledger range: <oldest> - <latest>"`. **The window slides ~1 ledger / 5 s**, so a `startLedger` computed from a slightly stale `getHealth` can be rejected seconds later (observed: 4003580 rejected 3 s after being read, oldest had advanced to 4003583) | 2026-08-13 | live curl at oldestLedger, oldestLedger−1, latestLedger, latestLedger+1 |
| **Retention window** | `ledgerRetentionWindow` = **120960 ledgers (~7 days)**, and `latestLedger − oldestLedger + 1` held at exactly 120960 throughout the session. **This supersedes the repo's stale "~24h / 17280 ledgers" claim** (was in `keeper/config.go:138` and `.env.example`) — a backfill sized off 17280 under-reaches by 7× | 2026-08-13 | `getHealth`: latest 4124536 / oldest 4003577 at session start, 4124594 / 4003635 at end |
| **Per-request ledger SEGMENT** | Exactly **10000 ledgers**, constant across start points, **independent of `pagination.limit`**, and not extendable | 2026-08-13 | repeated live probes from different start points |
| Cursor has TWO distinct semantics | (a) **segment exhausted** → cursor is the synthetic sentinel `"<toid>-4294967295"` with `tx = 0xFFFFF`, `op = 0xFFF`, where `toid` encodes `endLedger = startLedger + 9999`. (b) **limit reached first** → cursor is the id of the last RETURNED event, so the ledger advance is data-dependent and much smaller | 2026-08-13 | live probes; limit=12 → last-event cursor, limit=13 → sentinel, same 12 events |
| **The exactly-`limit` ambiguity is a real correctness hazard** | If a page returns exactly `limit` events you CANNOT conclude the segment is drained; skipping ahead silently drops events. Page until a cursor ending in `-4294967295` | 2026-08-13 | limit=11/12/13 boundary test on `CBUBTHAT…` |
| `startLedger` and `pagination.cursor` are mutually exclusive | Both → `-32602` `"ledger ranges and cursor cannot both be set"`. Neither → `-32602` `"startLedger must be positive"`. Cursor alone resumes correctly | 2026-08-13 | live curl |
| `endLedger` is supported and **exclusive** | `startLedger=4050000, endLedger=4050100` → sentinel at ledger 4050099. Values beyond `startLedger+10000` are silently clamped to the 10000 cap. `endLedger < startLedger` returns empty with no error | 2026-08-13 | live curl |
| Filter fan-out ceiling | **5 contractIds per filter** (6 → `-32602` `"filter 1 invalid: maximum 5 contract IDs per filter"`) and **5 filters per request** (6 → `"maximum 5 filters per request"`). Combined ceiling **25 contracts per request** | 2026-08-13 | live curl at 1/5/6/7 ids and 1/5/6 filters |
| Cursor TOID encoding | `"<toid>-<idx>"`, `toid = ledger<<32 \| tx<<12 \| op`, so `ledger = toid >> 32`. Confirmed against each event's own integer `ledger` field | 2026-08-13 | live decode |
| Response field names (this RPC version) | Topics under **`topic`** (singular) as an array of base64 ScVal; body under **`value`** as a plain base64 string (NOT an object with an `xdr` field). Also `id`, `txHash`, `ledger`, `operationIndex`, `transactionIndex`, `inSuccessfulContractCall` | 2026-08-13 | live response inspection |
| Minimum cost of a full-retention backfill | `ceil(120960 / 10000)` = **13 requests per contract**, at any limit. Cannot be done in one call | 2026-08-13 | derived from the 10000-ledger segment |

### Measured cost of the pre-D2 discovery path (the reason D2 exists)

| Claim | Value | Date | Source |
|---|---|---|---|
| **The shipped default lookback (1000 ledgers) discovers ZERO positions on the Nectar Sandbox** | The sandbox pool's entire retained event history sits in ledgers 4054311–4056272, ~68k–70k ledgers behind head. `GetPositions(lookback=1000)` → 0 positions in 554 ms; `lookback=114000` → 1 position, 5.5 s | 2026-08-13 | live run against `CBUBTHAT…` |
| **A full-window scan on the Blend testnet V2 pool costs 34.75 s — 3.5× the 10 s poll interval** | 1446 events over 12 pages → 53 distinct decoded addresses → 49 non-empty positions in 34.75 s (≈670 ms per address, strictly sequential). **Only 3 of the 49 carry any liability**; the other 46 `get_positions` round-trips are pure waste | 2026-08-13 | live run against `CCEBVDYM…` |
| The borrower set was derived, never remembered | A borrower who last transacted more than `BLEND_EVENT_LOOKBACK` ledgers ago is invisible regardless of how underwater it is; widening the lookback pays the full rediscovery cost every cycle and still cannot reach past the 120960-ledger retention window | 2026-08-13 | `keeper/adapters/blend/adapter.go:234-246`; `keeper/blend/positions.go:38-72` |

## Decisions

| Decision | Rationale | Date | Source / evidence |
|---|---|---|---|
| Reference facts are pinned to blend-contracts-v2 `ba22b48` and blend-utils `b05242d`; re-verify on any upstream bump | Upstream may change enums/curves; FACTS.md entries cite these commits | 2026-08-03 | this session (Gates 0.1–0.7) |
| **Liquidation size is chosen by simulating the contract, never computed locally** | Blend requires the post-liquidation HF to land in [1.03, 1.15] (`InvalidLiqTooSmall` 1214 / `InvalidLiqTooLarge` 1213). Reimplementing `est_incentive` and the scaled-percent math off-chain would silently drift from upstream on any Blend bump, and a wrong size makes every liquidation revert (the previous hardcoded 50% did exactly that on the live sandbox borrower). `FindLiquidationPercent` binary-searches with read-only `new_auction` simulations — the pool is the authority | Correctness that cannot drift; cost is ~8 simulations per position, no transactions | 2026-08-03 | `keeper/blend/auction.go`; `user_liquidation_auction.rs:92-205` @ `ba22b48`; live sandbox run |
| **At most ONE keeper process per keeper address** | Stale-draw recovery is chain-derived (`get_keeper_draw` + balances) and cannot distinguish a crashed keeper's abandoned draw from a sibling process's in-flight one. Observed live (2026-08-09): a second instance's recovery returned the first instance's 5-second-old draw (`063d1da1…`), making the first instance's fill sim fail `#10` with its capital gone. No funds were lost (measured accounting + rollback), but the race wastes cycles and fees. Deploy one process per key; scale by registering more keepers | Chain state alone cannot carry "who is mid-operation"; a vault-side draw timestamp would allow a grace window (candidate T3 contract hardening) | 2026-08-09 | `docs/evidence/b-full-cycle.md` "Failure recovery, demonstrated live" |
| **Fill and unwind are one atomic submit** | A user-liquidation fill moves no tokens — the filler assumes dToken debt — so a fill alone leaves the keeper holding debt plus bToken positions, not proceeds. `submit([fill, repay, withdraw_collateral…])` applies the requests in order against the same position with ONE health check at the end, so either the keeper ends holding real tokens and no debt, or nothing happened | Removes the stranded-debt window that a fill-then-unwind sequence would open between transactions | 2026-08-03 | `keeper/blend/submit.go`; `pool/src/pool/submit.rs:159-196` @ `ba22b48` |
| **RESOLVED (was OPEN) — USDC asset bridging on testnet**: a direct Soroswap pair Circle-USDC/Blend-USDC EXISTS (live `router_get_amounts_out` + `get_reserves`, 2026-08-03): reserves ≈ 51.0 Circle / 49.0 Blend USDC, marginal rate ≈ 0.94–1.02, but only ~$50 depth per side, so any ≥~0.5 USDC conversion breaches a 1% par bound. Decision: **option (a) — our own pool settling the vault's real asset (Nectar Sandbox, Task A4) is the capital path; cross-USDC pools are MONITOR-ONLY.** The measured route is recorded and re-quoted every scan (surfaced in the pool's scan note), and the conversion primitives exist and are unit-tested (`dex.QuoteConvertIn` / `ConvertExactOut`, par-anchored), but they are deliberately NOT wired into an autonomous capital path: a Blend user-liquidation fill transfers no tokens (the filler assumes dToken debt — "Auction asset flows" above), so pre-converted capital would sit stranded in the pool's USDC, and a failed fill or ambiguous conversion send has no sound recovery. `BLEND_POOLS=ADDR:mode:POOL_USDC` declares a pool's settle asset; when it differs from `USDC_CONTRACT` the adapter emits no tasks and Execute refuses | Shipping the conversion as an active capital path would have risked real loss (three critical findings in the Sprint-A adversarial review); monitoring + an own-asset pool achieves the goal without faking a capital path | 2026-08-03 | `docs/evidence/a2-route-checks.json`; `keeper/dex/convert.go` + `convert_test.go`; `keeper/adapters/blend/adapter.go` (crossUsdc guard) |
| **SUPERSEDED IN PART 2026-08-11 (venue only — see the AMENDED row below: the vault still refuses the proceeds, so this does NOT credit depositors). Bad-debt fills are FILL-AND-HOLD (option b), funded by the KEEPER FLOAT — never vault draws** | The fill's compensation is backstop LP (comet BLND:USDC), not USDC. On every pre-mainnet environment the LP cannot be consolidated into vault USDC: the sandbox comet's legs are admin-issued test tokens with no market, and canonical-testnet BLND has no faucet and no liquid venue — while vault draws must return within the registry's `slash_timeout` (3600 s), so a vault-funded fill would guarantee slashing. A full unwind (option a: comet exit → sell BLND → vault USDC) has an unvalidatable middle leg pre-mainnet, and shipping an untested capital path is what the Sprint-A adversarial review flagged. So: `submit([FillBadDebt, Repay])` paid from the keeper's own USDC float (capped by `BAD_DEBT_MAX_SPEND`, partial fill percent when short), LP held at the keeper and reported at spot + haircut every scan. The designated mainnet unwind is ONE verified call — `wdr_tokn_amt_in_get_lp_tokns_out(token_out = comet USDC leg, …)` (deployed-comet interface above) — viable there because mainnet Blend settles the SAME Circle USDC as the vault ("Mainnet settlement asset" above); the mainnet comet's leg composition must be re-verified via `get_tokens` at T3 before wiring it | 2026-08-09 | `keeper/blend/baddebt.go`; `keeper/adapters/blend/adapter.go` executeBadDebt; live fill evidence `docs/evidence/c-bad-debt.md` |
| **AMENDED 2026-08-11 — the mainnet unwind is verified at the VENUE but blocked at the VAULT.** The one-call exit is real (`wdr_tokn_amt_in_get_lp_tokns_out` into the comet's Circle-USDC leg, 0.24% at small size, no exit fee, no external DEX needed — see "Mainnet LP unwind" above). What is NOT true is that this reaches depositors: `return_proceeds` rejects `NoDraw` (#13) unless the caller has an outstanding draw, and float-funded fills have `drawn == 0` by construction. So under today's contracts, bad-debt profit accrues to the OPERATOR, not the vault — any doc claiming otherwise is an overclaim. Crediting depositors needs a vault entry point that accepts proceeds without a prior draw (e.g. a registration-gated `add_profit`), which is a contract change, not a keeper change | Recorded so the next session does not re-derive the venue work or mistake the venue for the whole path | 2026-08-11 | `contracts/nectar-vault/src/lib.rs:307-315`; live mainnet comet sims; 3-lens adversarial review (all three refuted the "reaches vault USDC" framing while confirming the comet mechanics) |
| **Bad-debt LP lot is valued at the backstop's own `token_spot_price` minus a configurable haircut (default 5000 bps)** | The spot price is the SAME number the pool itself uses to size the lot (`bad_debt_auction.rs:85-96`), read live from `backstop.pool_data()` — no keeper-side comet math to drift. The haircut (BAD_DEBT_LP_HAIRCUT_BPS) prices in what spot ignores: the unwind is deferred (illiquid until mainnet), comet exit pays swap fees/slippage on the 80% BLND leg, and spot itself trusts the comet's implied BLND price. At 50% the keeper only fills once the Dutch curve discounts the debt below half the lot's face value (t ≈ 283 at MinProfit 1.02) — conservative by construction, tunable by the operator | 2026-08-09 | `keeper/blend/baddebt.go` BackstopLPValueUSD/BadDebtProfitability + curve tests |
| **Interest auctions (type 2): DEFERRED — detect and log, never fill** | The bid is backstop LP tokens sized at `interest_value × 1.2 / token_spot_price` ("Auction asset flows" above), collected via `backstop.donate` `transfer_from` — the filler must PRE-HOLD the LP and PRE-APPROVE the backstop. A keeper operating vault USDC carries no LP inventory, and acquiring it (comet join with BLND) has no viable path on testnet (no BLND faucet/market) and no economic fit with the vault's USDC-denominated capital model even on mainnet. The keeper logs `interest auction seen, deferred` once per auction and surfaces it in every scan report; no fill is ever attempted. Revisit only if Nectar deliberately runs backstop-LP inventory as a strategy | 2026-08-09 | `keeper/adapters/blend/adapter.go` scanBackstop; operator docs `docs/ADAPTER-GUIDE.md`; `docs/tranche-notes/auction-scope.md` |
| **Past-curve (t≥400) bad-debt auctions are FREE-CAPTURED, not deleted** | Diverges deliberately from the user-liquidation stale path (delete + re-create): there, the repay-carrying fill reverts on the empty scaled bid; for bad debt an empty bid means a bare `submit([FillBadDebt])` assumes NO debt and draws the full LP lot for the tx fee — strictly better than deletion for the Blend pool (its bad debt gets retired) and for the operator, who keeps the lot. The contract itself guards misuse: before t=400 a bare fill would assume real debt and revert on the final health check (keeper holds no collateral) | 2026-08-09 | `keeper/blend/baddebt.go` FillBadDebtFree; `auction.rs:217-221` @ ba22b48; TestExecuteBadDebt_FreeCaptureAfterCurve |
| **RESOLVED (was OPEN) — CLAUDE.md corrections applied** (Sprint A): (1) oracle label fixed to "Blend testnet oracle (oraclemock, NOT Reflector)"; (2) auction curve note replaced with the verified two-phase 400-ledger description; (3) BLEND_POOL docs superseded by BLEND_POOLS (+FAUCET_* vars); Nectar Sandbox addresses added. "RegionalStarterPack" naming needed no CLAUDE.md edit (it never appeared there) but is corrected in evidence files | Keep CLAUDE.md consistent with verified facts | 2026-08-03 | CLAUDE.md diff in Sprint A commits; a1/a4 evidence files |
