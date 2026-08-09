# Sprint B evidence — full fill-and-unwind cycle with rising share price

**Claim proven:** the keeper runs the COMPLETE verified liquidation sequence on
the Nectar Sandbox — create auction (contract-sized) → wait for the verified
two-phase price curve → draw → ONE atomic fill+repay+withdraw → swap seized
collateral → return proceeds — and the vault share price INCREASES, asserted
programmatically by `scripts/nectar-sandbox/04-money-test.sh` (exit 0).

Run date: 2026-08-09 (UTC times below). Pool `CBUBTHAT…`, vault `CDOGQY7N…`,
keeper-alpha `GCC52N6U…`, borrower `GCCTPHRT…` (100 XLM collateral / 20
Circle-USDC debt, oracle XLM=$0.098 → HF 0.349).

## The money test result (programmatic assertion)

```
BEFORE: total_usdc=600000000 total_shares=600000000 total_profit=0 active_liq=0
AFTER:  total_usdc=625960334 total_shares=600000000 total_profit=25960334 active_liq=0
share price: 1.0000000 -> 1.0432672 (delta_usdc=25960334 stroops)
MONEY TEST PASS: share price increased
```

Depositor yield: +2.5960334 USDC on 60 USDC TVL from one liquidation.
Keeper's outstanding draw at exit: 0. Full poller log:
`b-money-test-run-log.txt`; keeper log: `b-money-test-keeper-log.txt`.

## Transaction trail (every step, on-chain, keeper-signed)

| # | Step | Tx hash | Token moves |
|---|---|---|---|
| 1 | Stale-auction delete (`del_auction`; prior auction 110k blocks old) | `b8bc528356c9781087139b84a48860327049ac9307685d525e6a124ea868af86` | none |
| 2 | `new_auction` type 0, percent 100 (chosen by `FindLiquidationPercent`) | `e7af9d8ed7669c8da442d29dcc4c14be0ab336ff798162166c5a62408449b383` | none (start block 4054313) |
| — | ~25 min wait: profitability re-checked every cycle against the verified curve (log shows the ratio climbing 0.00→1.02) | | |
| 3 | `vault.draw` #1 | `49114876a8e783044eb8f9eb9a780301b83176649d494ba15c1f41affe30c494` | USDC 20.2154450 vault→keeper |
| 4 | **Rollback return** (see incident below) | `063d1da17d9b32727a48dfa48e31799afc5d70b9548fc702976aba6dd50c99f2` | USDC 20.2154450 keeper→vault |
| 5 | `vault.draw` #2 | `96bad3cc6a02b5ab8f62ad92269133e6ecbc6beee42cf04615a18d39774d8b33` | USDC 20.2154468 vault→keeper |
| 6 | **Atomic fill+unwind** — ONE `submit([fill 100%, repay, withdraw_collateral])` | `ff90b7a288923490527b3009faf8d5913299b5ce0371171019dd32bdee2b2aea` | USDC 20.2154468 keeper→pool (gross repay), USDC 12.6096347 pool→keeper (refund), XLM 100 pool→keeper (withdraw). Net repaid: 7.6058121 |
| 7 | Collateral swap (Soroswap, oracle-anchored floor) | `754aa6d09a8547f5ff56c51ae148c087c8ceafe4c4843dd8f1a5b928382433d3` | XLM 99.9794363 keeper→pair, USDC 10.2018455 pair→keeper |
| 8 | `vault.return_proceeds` | `f644a9ca37fa9188cfc44b4f8c6af6c94294459df397898460b85eaf67ccb491` | USDC 22.8114802 keeper→vault (= draw #2 + 2.5960334 profit) |

Reconciliation (7-dec stroops): vault −202154450 +202154450 −202154468
+228114802 = **+25960334** (booked as `total_profit`, matches `get_state`
exactly). Keeper USDC float unchanged (0.7537980, the Aug 3 remainder — see
history). The fill emitted a `bad_debt` event: the borrower's residual
~11.5 USDC of dTokens (the unfilled 55%-scaled portion) transferred to the
backstop per `check_and_handle_user_bad_debt` — expected for a full fill of a
deeply underwater position (FACTS.md "Full-fill side effects").

## Failure recovery, demonstrated live (not injected)

Steps 3–4 above are a REAL failure-and-rollback: seconds after draw #1, a
**second keeper process** (an orphaned instance from an earlier launch of the
same test) ran its stale-draw recovery, saw the in-flight draw, and returned
it to the vault (`063d1da1…`). Our keeper's fill simulation then failed with
`Error(Contract, #10)` — balance 7537980 − 202154450 = **−194616470**, the
exact diagnostic value — because its drawn USDC had been reclaimed mid-flight.
Execute's deterministic-failure path closed the books ("fill failed …
draw outstanding" note; capital already home), and the next cycle re-drew and
completed. Zero stroops lost; the vault was whole at every instant.

**Operational rule derived (recorded in FACTS.md):** run at most ONE keeper
process per keeper address. Stale-draw recovery is chain-derived and cannot
distinguish a crashed keeper's abandoned draw from a sibling process's
in-flight one.

## History: the Aug 3 partial run (first borrower consumption)

A concurrent keeper run on 2026-08-03 (intermediate build) consumed the
original borrower: draw `340d38db…`, atomic fill `1f57b865…`, return
`73ae5b7a…` (11.2004290), swap `25460d51…` (97.4 XLM → 9.5541355), recovery
return `0d0c42e9…` (8.8003375). The vault got back exactly its draw —
zero profit booked — because the exit swap failed in-cycle and the later
recovery capped its return at the outstanding draw, stranding 0.7537980 USDC
in the keeper float. The Sprint B Execute fixes that path (sweep before
proceeds are measured); the stranded amount remains as keeper float and is
visible in the reconciliation above.

## Defects caught BY the live runs and fixed this sprint

1. **Missing-auction wasm trap** (`Error(WasmVm, InvalidAction)` /
   `UnreachableCodeReached`, storage.rs:607-616 @ ba22b48): the pre-create
   existence check errored every cycle; no auction could ever be created.
   Fixed + tested (`isMissingAuctionTrap`).
2. **Past-curve auctions live-loop**: at t≥400 the scaled bid is empty and the
   repay-carrying fill reverts; Execute now deletes stale auctions (t≥500,
   permissionless `del_auction`) and re-creates — proven live in step 1 —
   and explicitly waits in the 400–500 window.
3. **Gross repay settlement** (read from actions.rs:415-441 after the #10
   failure): `apply_repay` transfers the FULL `request.amount` from the
   spender and refunds the excess in a separate pool→spender transfer. The
   draw estimator covers this exactly (draw = padded repay request).

## B1 — percent solver agreement (live probes)

`docs/evidence/b1-percent-probes.txt`: on the re-armed borrower, read-only
`new_auction` sims: 1%/25%/50% → `#1214 InvalidLiqTooSmall` (the old
hardcoded 50% provably reverts), 99%/100% → accepted (full liquidation).
`FindLiquidationPercent` picked 100 on the first simulation live (keeper log
11:59:40 IST / 16:19:25 UTC entries) — agreement is by construction: the
precheck IS the pool's own simulation.

## Test suite

`go test -race ./...` green across all keeper packages (run at each commit;
final run recorded in the closing commit). Failure-injection coverage:
11 Execute scenarios (rollbacks, ambiguous holds/resolutions, oracle caps,
baseline aborts), 9 recovery scenarios (fee-floor, unpriced assets, monitor
pools, partial returns, idempotency), plus dex both-direction and
stale-auction unit tests.

## Known limitations (explicit)

- **Profit stranding across restarts**: recovery returns at most the
  outstanding draw; profit realized after a crash stays in the keeper float
  (vault capital is never short). Fixing this needs a persisted float
  baseline or vault-side accounting — deferred.
- **Bad-debt and interest auctions**: detection only (`DetectAuctions`); the
  deleted standalone fill entry points were wrong-as-implemented, and correct
  fills for those auction kinds are future work.
- **Sandbox state after this run**: borrower consumed (re-arm with
  `run.sh 03 0.42 && run.sh 02 0 && run.sh 03 0.098`); oracle restored to
  $0.42 resting.

## Adversarial review (45-agent workflow: 5 lenses → 2 refuters per finding)

20 raw findings → **9 confirmed** (majority non-refuted), 3 contested.
Disposition:

| Finding | Status |
|---|---|
| Ambiguous `ConvertExactOut` rolled back instead of held (3 lenses) | **Fixed** — acquisition loop resolves by hash (AwaitTx over the tx's remaining timebound life): landed → continue with measured delta; failed → rollback; unknown → hold. 5 new tests |
| Native-XLM fee contaminates `ConvertExactOut`'s measured output → spurious `got < debt` rollback loop | **Fixed** — native acquisitions padded by `nativeFeePad` (1 XLM) through quote, oracle cap and exact-out request; adapter Config gains `NativeSAC`. 2 new tests |
| Recovery sweep sells into the XLM fee floor from a stale balance snapshot | **Fixed** — `nativeSweepMargin` (1 XLM) reserved above the floor |
| `SubmitRequests` retry masks an earlier ambiguous attempt behind a later transient error | **Fixed** — `RetryAmbiguous=false`; ambiguity now surfaces faithfully and Execute resolves it by hash |
| Post-draw failure notes logged at INFO only | **Fixed** — WARN + SSE event when `Drew > 0` |
| e2e retry gate voided by test rename (vacuous `-run` pattern) | **Fixed** — pattern updated, 8 tests match again |
| Past-curve (t≥400) auctions are free to capture instead of delete+recreate | **Deferred** (minor) — free-capture needs a no-repay/no-draw fill path and a drawn==0 return; delete+recreate is safe and proven live |
| Cross-restart profit stranding (recovery caps at drawn) | **Documented** known limitation (above) |
| Dead-code nits (`requestType` doc, unused `dexClient.Swap`) | **Fixed** — doc corrected, unused interface method removed |
