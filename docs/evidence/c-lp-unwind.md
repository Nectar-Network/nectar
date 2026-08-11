# Backstop-LP unwind — mainnet verification and the vault last mile

Date: 2026-08-11. Follow-up to `docs/evidence/c-bad-debt.md`, which shipped
bad-debt fills as **fill-and-hold** and deferred the LP unwind. This document
answers the deferred question: *can the LP become vault USDC, how, and at what
cost?*

**Everything here is read-only.** Every mainnet call used `--send=no`
(simulation), no secret keys, nothing signed or sent. Simulation executes the
contract, so a successful simulation's return value is the contract's own
arithmetic — that is the standard of evidence used throughout.

Facts summarised in `docs/FACTS.md` → "Mainnet LP unwind — verified venue, and
the vault blocker".

---

## TL;DR

The **venue** problem is solved and cheap. The **last mile** is not, and it is
a contract limitation rather than a keeper one.

| Question | Answer |
|---|---|
| Can the LP be converted to Circle USDC on mainnet? | **Yes — one call**, `wdr_tokn_amt_in_get_lp_tokns_out` |
| What does it cost? | **0.2400% floor**, rising with size (convex) |
| Does it need an external DEX? | **No** — the comet's own USDC leg IS Circle USDC |
| Is the LP time-locked? | **No** — auction LP bypasses the backstop's Q4W queue |
| Can that USDC be credited to depositors? | **No** — `return_proceeds` rejects `NoDraw` (#13) |
| So who earns bad-debt profit today? | **The operator**, not the vault |

---

## 1. The venue: one call, no external market

Mainnet comet `CAS3FL6TLZKDGGSISDBWGGPXT3NRR4DYTZD7YOD3HMYO6LTJUVGRVEAM`.

```
get_tokens                  -> [CD25MNVT…(BLND), CCW67TSZ…(USDC)]
get_normalized_weight(BLND) -> 8000000   (80%)
get_normalized_weight(USDC) -> 2000000   (20%)
get_swap_fee                -> 30000     (0.30%)
get_total_supply            -> 144072593796752      (14,407,259.38 LP)
get_balance(BLND)           -> 728905457705532      (72,890,545.77)
get_balance(USDC)           -> 7657378410070        (765,737.84)
```

The decisive fact: **`CCW67TSZ…` is Circle USDC — the same asset NectarVault
settles in.** The comet redeems LP into the vault's asset internally, so the
unwind needs no external venue, no BLND sale, and no price oracle.

The call, present on mainnet with exactly this signature:

```
wdr_tokn_amt_in_get_lp_tokns_out(token_out: Address, pool_amount_in: i128,
                                 min_amount_out: i128, user: Address) -> i128
```

### Verified quotes (live mainnet simulation)

Loss is measured against Blend's own `token_spot_price` (`2657470` =
0.2657470 USDC/LP), read from `backstopV2.pool_data()` and independently
re-derived to the stroop from live reserves.

| LP in | USDC out | USDC per LP | Loss vs spot |
|---|---|---|---|
| 22.6739017 | 6.0110462 | 0.265108594 | 0.2402% |
| 100 | 26.5105751 | 0.265105751 | 0.2413% |
| 1,000 | 265.0726323 | 0.265072632 | 0.2538% |
| 10,000 | 2,647.4166534 | 0.264741665 | 0.3783% |
| 100,000 | — | — | 1.6153% |
| 1,000,000 | — | — | 13.1602% |

### Why 0.24% is a floor, not a coincidence

Comet v1.0.0 has **no exit fee** — not a zero value, the concept is absent
(unlike Balancer v1's `EXIT_FEE`). Proportional `exit_pool` is exact pro rata
with no fee term at all. Single-sided exit charges the swap fee only on the
*non-target* weight:

```
zaz  = (1 − weight_out) × swap_fee = 0.80 × 0.30% = 0.2400%
```

Everything above 0.2400% is price impact. The symmetric prediction holds too:
exiting into BLND costs 0.20 × 0.30% = 0.06% (observed 0.0599%).

### Route comparison

- **Route A — single-sided into USDC. RECOMMENDED.** One call, lands in the
  vault's settlement asset. 0.24–0.38% at Nectar scale.
- **Route B — single-sided into BLND.** Cheaper inside the comet (0.06%) but
  pays in BLND, which does not solve the problem; realising it means Route C.
- **Route C — proportional `exit_pool` then sell the BLND leg.** Produces a
  byte-identical result at small size (+0.0006% at 10,000 LP), for two
  transactions, an unhedged BLND leg between them, and atomicity risk. Not
  worth it.

### The external BLND market is not an option

One callable Soroban venue exists: Soroswap pair `CCCDU62T…`, holding roughly
**$25,986 USDC** with 5 swaps / ~$206 of volume over 7 days. Selling the BLND
leg externally is unviable above roughly $250 of LP face value. The comet
itself is the venue.

### Provenance and rehearsability

| Artifact | sha256 |
|---|---|
| Mainnet comet (deployed) | `8abc28913035c07411ed5d134e6bfeab4723d97ddd4d1a22a0605d35c94d1a36` |
| Nectar Sandbox comet (testnet) | `8abc2891…` — **identical** |
| `blend-utils/wasm_v1/comet.wasm` | `8abc2891…` — identical |
| CometDEX `comet-contracts-v1` v1.0.0 release | `8abc2891…` — identical |
| `reference/blend-contracts-v2/comet.wasm` | `5d721fa5…` — **different, deployed nowhere** |

Mainnet and sandbox interface dumps `diff` **empty** (34 exports). The unwind
can therefore be rehearsed end-to-end on the Sandbox.

> Rehearsal caveat: the sandbox comet settles Blend's testnet USDC
> (`CAKGVZ34…`), **not** the Circle testnet USDC (`CBIELTK6…`) the vault uses.
> A testnet rehearsal proves the mechanics and yields the wrong asset.

Last: the LP paid by a bad-debt fill arrives via a direct `transfer` in
`execute_draw`, so the backstop's 17-day Q4W withdrawal queue — which applies
to backstop *depositors* — never applies here. The exit is immediate.

---

## 2. The blocker: the vault refuses proceeds without a draw

```rust
// contracts/nectar-vault/src/lib.rs:307-315
let drawn: i128 = env.storage().persistent().get(&draw_key).unwrap_or(0);
if drawn <= 0 {
    return Err(VaultError::NoDraw);        // types.rs:60 — NoDraw = 13
}
```

This is the deliberate **VLT-2 anti-donation guard**: without it anyone could
inflate the share price by "returning" funds they never drew. It is correct
security, has a test (`src/test.rs:376`), and should not simply be removed.

But bad-debt fills are **float-funded by design** — they never draw, precisely
so vault capital is never exposed to an asset the recovery path cannot sell.
So `drawn == 0` by construction, and `return_proceeds` always reverts.
`deposit()` is not a substitute: it mints shares to the operator, which is
buying in, not contributing profit.

Verified live: keeper-alpha `get_keeper_draw` → `0`.

**Consequence: "never vault capital" and "unwind into vault USDC" are mutually
exclusive as currently built. Bad-debt profit accrues to the operator.**

---

## 3. Recommended path

### 3.1 Decide the intent first

Is bad-debt profit *supposed* to be depositor yield? Today's contracts answer
"no". That is a coherent product position — the operator takes the risk with
its own float and keeps the upside — but it must be stated, not implied.

### 3.2 If yes: add a vault entry point (contract change)

Add `add_profit(keeper, amount)` gated on **registry registration** rather
than on an outstanding draw: raise `total_usdc` / `total_profit`, mint no
shares. This preserves VLT-2's intent (only known keepers can contribute)
while allowing draw-less profit. Belongs in T3 hardening scope with the
redeploy.

> **Rejected alternative:** `draw(1)` immediately before `return_proceeds` so
> that `drawn > 0`. It needs no contract change but arms a *permissionless*
> `slash` against a one-stroop draw, and slashing sets `info.active = false`,
> forcing a re-stake. Not worth it.

### 3.3 Replace the flat haircut with a live quote

Exit cost is **convex** (0.24% at ~$5 of bad debt, ~1.6% at $22k, ~6.3% at
$100k) while `BackstopLPValueUSD` is **linear** — so a flat haircut always
overvalues exactly the large lots that can hurt.

The fix falls out of the verification: `min_amount_out` and the max-out-ratio
are asserted *before* the withdraw event, and `pull_shares`/`burn_shares` come
after, so **a non-holder's simulation returns the true quote**. The keeper can
simulate its own exit for the actual lot at decision time and use the returned
USDC directly.

Until that lands, **keep `BAD_DEBT_LP_HAIRCUT_BPS=5000`.** Lowering it to
100–200 bps now would multiply perceived lot value by ~1.97×, firing the
keeper roughly twice as early on the Dutch curve and pushing it off the
free-capture path at t≥400, where it currently takes the full lot for a
transaction fee.

### 3.4 Make the LP recoverable

`sweepHeldCollateral` iterates `pool.Reserves`, and the comet LP is never a
pool reserve — so held LP is invisible to recovery. Any move toward
vault-funded bad-debt fills **must** be preceded by teaching recovery to exit
LP, or a failed unwind strands vault capital in an asset no recovery path can
reach while the `slash_timeout` clock runs.

### 3.5 Operational limits to encode

- **Per-call ceiling** ≈ `USDC_balance / 3` (~255,246 USDC today); above it
  the call reverts `Error(Contract, #21)`. A large bad-debt lot cannot be
  exited in one call.
- **`min_amount_out` must be a real guard**, never 0. Re-quote by simulation
  immediately before executing.
- **The mark drifts down.** `token_spot_price = (total_usdc / supply) × 5` is
  marginal, zero-slippage and sans-fee. The backstop's own emission
  auto-compounding deposits BLND *only* (290 of 312 single-sided deposits in a
  7.8-day window), raising supply without raising the USDC side and walking
  the mark downward.
- **Hold exposure is real.** BLND moved 2.21% peak-to-trough in a quiet
  7.8-day window, ~80% of which passes through to LP NAV — larger than the
  entire mechanical exit cost at Nectar's sizes.
- **Transaction fees are not in these figures.** At a ~6 USDC exit the Soroban
  fee is comparable to the 0.0145 USDC of slippage, so there is a minimum
  economical unwind size worth measuring before wiring this up.

---

## 4. What is verified vs. not

**Verified (live mainnet simulation or pinned source):** the call exists with
the expected signature; interface and wasm parity with our sandbox; no exit
fee; the 0.24% floor and the full cost curve; the per-call ceiling; quote-by
non-holder simulation; Q4W does not apply; the Soroswap BLND pair's depth; the
`NoDraw` guard and its error number.

**NOT verified:**

- **No end-to-end run.** No successful simulation has come from an account
  that could satisfy `user.require_auth()` *and* holds LP — successful quotes
  impersonated the backstop *contract*. `pull_shares`, `burn_shares` and the
  final USDC transfer have never executed in any form.
- **No implementation.** `grep` for the comet exit functions across `keeper/`
  returns zero matches. This document describes a verified venue, not shipped
  code.
- **Comet error codes** `#21` and `#29` are raw codes; the readings
  (max-out-ratio, insufficient balance) are inference from context.
- **BLND's external price** was never cross-checked against an oracle or CEX.
  This does not affect the USDC that Route A returns, but it does affect
  whether `token_spot_price` — which Blend also uses to *size* the lot — is a
  fair yardstick.
- **All figures are a snapshot** at ledger ≈63,906,500 and will drift; the
  0.2400% floor is structural, the convexity term is not.
