# Session C evidence — bad-debt auctions live on the Nectar Sandbox

Date: 2026-08-09/10 (IST). Keeper: `keeper-alpha`
(`GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF`), running the
Session C build (commit `fe78bad`) against pool `CBUBTHATT…` with production
defaults: `MIN_PROFIT=1.02`, `BAD_DEBT_MAX_SPEND=1000000000` (100 USDC),
`BAD_DEBT_LP_HAIRCUT_BPS=5000`. Full log:
`docs/evidence/c-bad-debt-keeper-log.txt` (verbatim run log; `*.log` is
gitignored, so the evidence copy carries the Session-B `.txt` convention).

The bad debt is REAL residue: the Sprint-B full liquidation of the underwater
borrower moved its unfilled dTokens to the backstop (`bad_debt` event in tx
`ff90b7a2…`, b-full-cycle.md), leaving the backstop with
`liabilities {0: 235995067}` (23.5995067 Circle-USDC dTokens) — read live
before this session's run.

## Setup

- Operator float: 25 USDC transferred admin → keeper-alpha (tx
  `a1dc809d11a9f36b908efe1238399677b1ab2bc5e17cdecf67e55d4c86a05a0a`) —
  bad-debt fills are float-funded by design (FACTS.md Decisions); the vault
  is never touched (`drew=0` on every result below).
- Keeper LP balance before: **0** (clean baseline).
- Backstop `pool_data`: `token_spot_price=12500000` ($1.25/LP),
  `tokens=500010000000`.

## Run 1 — detection, creation, RPC-stall ride-out, free capture

1. **Detection** (23:34:39): first scan reports
   `backstop carries bad debt in 1 reserve(s)` and emits a `bad_debt` task.
2. **Creation** (23:34:47): keeper creates the type-1 auction itself —
   permissionless `new_auction(1, backstop, [Circle-USDC], [comet LP], 100)`,
   tx `80f1c64c3e21fd8fc1bd6af7f7c8164aedcde27b145591f7d175c42ffd468e7d`,
   start block 4055575. Lot: 226738569 (22.6738569 LP = 23.6 × 1.2 / 1.25,
   matching the contract's sizing formula exactly).
3. **Profitability gating on the live curve**: ratio climbed exactly as the
   verified two-phase model predicts under the 50% haircut — 0.0000 (t≈0),
   0.2730 (t≈96), 0.5100 (t≈163), 0.5460 (t≈185) — every cycle skipped with
   `not profitable`, no capital moving.
4. **Testnet RPC stalled** ~23:50→00:08 (two silent gaps ending in a logged
   `context deadline exceeded`). The keeper blocked on RPC, missed its
   t≈283 repay-path window (expected ratio 1.026 ≥ 1.02), and resumed with
   the curve past t=400.
5. **Boundary artifact** (00:08:14): the first recovered cycle read
   elapsed=399, took the repay path, and the submit SIMULATION reverted
   `Error(Contract, #1219)` (`InvalidDTokenBurnAmount`): by simulation time
   the bid had scaled to empty, so the atomic repay had no dTokens to burn.
   Nothing was sent, nothing spent — the atomic design turned a mistimed
   fill into a no-op. Hardened post-run: `badDebtBoundaryLedgers=2` now waits
   out the last ledgers before t=400 (commit `9facb54`, regression-tested).
6. **Free capture** (00:08:31): next cycle, elapsed ≥ 400 → bare
   `submit([FillBadDebt(100%)])` landed — tx
   `6ba804e809fe54cd67b8d7718f6ad486add7826cb8f918e3475ae20ab190a8dd`.
   Fill event: `fill_auction, 1, backstop → filler keeper-alpha, 100%, lot
   {comet LP: 226738569}, bid {}`. The keeper received **22.6738569 LP for
   the tx fee alone**; USDC float unchanged (verified on-chain: LP balance
   226738569, USDC still 257537980). Result: `drew=0 proceeds=0` — vault
   untouched.
7. **Value tracked every scan** (00:08:40 onward):
   `keeper holds 226738569 backstop LP ≈ $28.3423 spot / $14.1712 after
   5000 bps haircut (fill-and-hold, unwind deferred)`.

Free capture assumes no debt, so the backstop still owed the full
235995067 dTokens — which set up run 2.

## Run 2 — the repay-carrying fill (the primary C1 path)

1. **Re-creation** (00:08:46): the next cycle saw the remaining bad debt and
   created a fresh auction — tx
   `e1be3367c691b4e875223cec73cfb180b2f25415d1c615670cc14860d952b516`,
   start block 4055982 — restarting the price curve.
2. **Gated across the whole curve** (00:08→00:32): 24 minutes of
   `not profitable` cycles, the ratio climbing monotonically under the 50%
   haircut — 0.7895 (00:29:32), 0.8571, 0.9023, 0.9524, 0.9756, 0.9917,
   1.0084 (00:32:21, still refused: below 1.02). No capital moved while
   waiting.
3. **Fill at the gate crossing** (00:32:31→00:32:43): the first cycle whose
   ratio cleared `MIN_PROFIT` took the repay-carrying path —
   `pool submit landed requests=2`, tx
   `a2bd3aaaabcf844697f293d7ee5e5ed825a11875bfbcca088cfc5b170c6a5388`
   (ledger 4056268, `successful: true`, fee 56704 stroops). Two requests =
   `[FillBadDebtAuction(pct), Repay(Circle-USDC)]` — the atomic fill+repay
   this task set out to implement. Elapsed at fill ≈ 286 blocks, so the
   scaled bid was ≈57% of the debt while the lot was at 100%.
4. **Measured settlement** (Horizon `asset_balance_changes` for the tx):
   `13.8358230 USDC keeper → pool` and `0.3731749 USDC pool → keeper` —
   gross repay with separate refund, independently re-confirming the
   FACTS.md "Repay settlement is GROSS" entry. Net cost **13.4626481 USDC**.
5. **What the keeper got** (live balance reads after the run):

   | Quantity | Before run 2 | After run 2 | Delta |
   |---|---|---|---|
   | Keeper USDC float | 257537980 | 122911499 | **−134626481** (−13.4626481) |
   | Keeper backstop LP | 226738569 | 453477586 | **+226739017** (+22.6739017) |
   | Backstop dToken debt | 235995067 | 101477878 | −134517189 (retired) |

   13.46 USDC of operator float bought 22.67 LP — $28.34 at the backstop's
   own `token_spot_price`, $14.17 after the 50% haircut the gate actually
   required. The USDC delta exceeds the retired dToken count by 0.0109 USDC,
   which is the d_rate (≈1.0008) converting dTokens to underlying — the
   padded repay, refunded down to the true debt.
6. **Vault untouched**: `task executed … drew=0 proceeds=0 profit=0`. The
   registry's slash clock was never started, because no draw ever happened.
7. **Residual debt re-auctioned** (00:33:03): the unfilled 43% of the debt
   stayed with the backstop, so the next cycle created a fresh type-1
   auction — tx
   `6c472da1d8406c48ee15f99c35fce147aa98854e4c01dfcd2be13dc23dbdef7b`,
   start block 4056273, bid 101477878 dTokens / lot 97497913 LP. The run
   ended here (00:33:42).

### Sandbox state left behind (read live 2026-08-11)

- Backstop bad debt: `liabilities {0: 101477878}` — still outstanding.
- Type-1 auction at block 4056273 still exists; at ledger 4089735 that is
  t ≈ 33,462, far past the curve, so its bid has scaled to empty and it is
  free to capture (or stale-deletable) by any keeper.
- keeper-alpha holds 122911499 Circle USDC float and 453477586 backstop LP.
- Vault balances unchanged by this session's bad-debt work.

## Interest auctions (C2)

No interest auction existed on the sandbox during the run (none creatable:
`backstop_credit` interest below the 200-unit creation minimum), so the
deferral path is proven at the unit level instead: `scanBackstop` notes
`interest auction seen, deferred: bid requires pre-held backstop LP
inventory` and logs once per auction (dedupe by start block), and no task is
ever emitted — `TestScanBackstop_InterestDeferredAndLoggedOnce` +
`TestProfitability_BackstopLPLegs_NeverGreenlit`. Stated plainly: the
interest-auction deferral was NOT exercised against a live interest auction.

Note: the session brief described the interest bid as "~140% of interest
value"; the verified constant is **1.2×** (`interest_value × 1.2 /
token_spot_price`, `backstop_interest_auction.rs:70-81` @ ba22b48). FACTS.md
wins; all session docs say 120%.

## What this proves / does not prove

Proven live on-chain:
- Chain-derived backstop discovery (instance-storage read), bad-debt
  detection from backstop positions, permissionless type-1 creation with
  contract-exact lot sizing, haircut profitability gating on the real curve,
  past-curve free capture, LP measurement + fill-and-hold reporting, vault
  untouched throughout, and — the primary C1 path, run 2 — the atomic
  fill+repay funded entirely from the keeper float, with the settlement
  measured against Horizon and the resulting LP position confirmed by live
  balance reads.

Not proven live (explicitly):
- LP unwind — deferred by design to mainnet (FACTS.md Decisions); no unwind
  code is wired.
- Interest-auction deferral against a live interest auction (unit-tested
  only, see above).
- Multi-asset bad debt (sandbox debt is single-reserve Circle USDC; non-settle
  legs are skipped with a note by design).
