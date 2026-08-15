# CORRECTION-REPORT.md — What we got wrong, how we found out, what changed

**Audience:** grant reviewers and security auditors.
**Written:** 2026-08-15, at commit `68b21fc` (branch `tranche-3`), after the
Session A–E correction arc.
**Reading guide:** this document is deliberately blunt. Every claim Nectar
made that turned out to be wrong is listed here with the verified reality and
the evidence. We consider this an asset: an integration that has been
adversarially re-verified against protocol source and live chain state is
safer to audit than one whose claims were never tested.

The method behind every correction: `docs/FACTS.md` is the single source of
truth (every entry carries a date + source — contract source `file:line @
commit`, live tx hash, or measured RPC observation), and
`docs/VERIFICATION-REPORT.md` records the 93/93-claim verification session
that started the arc. Nothing below is asserted from memory.

---

## The five gaps: wrong → verified → changed → evidence

### Gap 1 — Blend's mechanics were asserted, not verified

**What was wrong.** The auction price curve was described as "lot scales
0→100% over 200 blocks while bid scales 100→0%" (simultaneous, 200 blocks).
The testnet oracle was called "Reflector". The two testnet pool names were
treated as two pools. Keeper code comments mis-described what bad-debt and
interest fills move.

**What was verified.** Every mechanic re-derived from pinned protocol source
(blend-contracts-v2 @ `ba22b48`) plus live reads: the curve is a 400-ledger
TWO-PHASE Dutch auction (t=0–200 lot 0→100% with bid at 100%; t=200–400 bid
100→0% with lot at 100%; fair point exactly t=200; bid empty past t=400); the
oracle at `CAZOKR2Y…` is Blend's admin-settable `oraclemock` (wasm-hash
proven), not Reflector; "TestnetV2" and "RegionalStarterPack" are one
contract; the three auction kinds move three different asset sets.

**What was changed.** CLAUDE.md/README curve text corrected; keeper
profitability engine implements the verified curve
(`keeper/blend/auction.go:314-338` + boundary tests); oracle labeled
honestly everywhere; the Tranche 3 circuit-breaker design was re-scoped to
acquire a REAL Reflector feed (the testnet "cross-reference" would have
cross-referenced a mock).

**Evidence.** `docs/VERIFICATION-REPORT.md` (7 gates, 93/93 claims);
FACTS.md "Auction fill price curve" (source `auction.rs:189-264` + unit-test
corroboration); wasm sha256 match for the oracle.

### Gap 2 — The keeper targeted a pool whose USDC is not the vault's USDC

**What was wrong.** The plan assumed filling Blend's testnet pool settles the
vault's asset. It does not: TestnetV2's USDC reserve is Blend's mock
`USDC:GATALTGT…`, while the vault settles Circle's `USDC:GBBD47IF…`. A keeper
filling TestnetV2 auctions would pay and receive an asset that cannot settle
vault P&L. The only direct conversion pair on testnet has ~$50 of depth.

**What was verified.** Both SAC identities live-read; the cross-USDC Soroswap
route measured (≈1% bound breached above ~0.5 USDC); mainnet Blend settles
the SAME Circle USDC as the vault — the mismatch is a testnet-only artifact.

**What was changed.** Deployed our own Blend V2 stack ("Nectar Sandbox":
pool + backstop + comet + admin oracle) whose reserves are the REAL vault
asset, with an on-demand insolvency lever for end-to-end tests. The keeper
grew `BLEND_POOLS=ADDR:mode:POOL_USDC`; any pool whose settle asset differs
from `USDC_CONTRACT` is execution-disabled (belt-and-braces guard in
`Execute` too). Conversion primitives exist and are unit-tested but are
deliberately NOT wired into an autonomous capital path.

**Evidence.** FACTS.md "Our testnet USDC", "Mainnet settlement asset",
Decisions (RESOLVED bridging row); `docs/evidence/a2-route-checks.json`,
`docs/evidence/a4-sandbox.md` (full deploy tx trail).

### Gap 3 — The full liquidation cycle had never moved real value

**What was wrong.** Prior demos exercised code paths but no one had proven
the whole chain — auction → draw → atomic fill → collateral swap → return —
increases the share price with real tokens under production defaults.
Earlier "profit" figures came from controlled/lab scenarios.

**What was verified & changed.** The full sequence ran live on the sandbox
under `MIN_PROFIT=1.02`: create (simulation-sized after the hardcoded-50%
approach provably reverted), wait on the verified curve, draw, ONE atomic
`submit([fill, repay, withdraw_collateral])`, swap seized XLM, return
measured proceeds. Share price 1.0000000 → 1.0432672, asserted
programmatically (`scripts/nectar-sandbox/04-money-test.sh`, exit 0), every
step tx-hashed. The work also surfaced and fixed live failure modes:
stale-auction delete/re-create, the missing-auction wasm trap, ambiguous-tx
resolution by hash, gross repay settlement, one-process-per-keeper.

**Evidence.** `docs/evidence/b-full-cycle.md` (tx table),
`b-money-test-run-log.txt`, `b-money-test-keeper-log.txt`;
FACTS.md rows "Repay settlement is GROSS", "Missing auction = wasm trap".

### Gap 4 — "All three auction types" was an overclaim

**What was wrong.** T1 wording claimed all three Blend auction kinds worked
via "same logic, different request type", and later drafts said bad-debt
profit is "donated" back to depositors. Both false: the three kinds move
different assets, and the vault's own anti-donation guard (VLT-2 `NoDraw`)
rejects proceeds from a keeper with no outstanding draw.

**What was verified & changed.** Per-type scope implemented to exactly what
verified mechanics allow — the support matrix below. Bad-debt fills are
float-funded (never vault draws — the LP lot is illiquid pre-mainnet and
draws would hit `slash_timeout`), atomic fill+repay, LP held at the
backstop's own spot price minus a configurable haircut; past-curve auctions
are free-captured. The mainnet LP unwind was verified at the venue (one
comet call, 0.24% at small size, convex in size, capped at ≈ USDC-side/3)
**and shown to be blocked at the vault**: under today's contracts bad-debt
profit accrues to the OPERATOR, not depositors. Every doc that said
otherwise was corrected; crediting depositors needs a new contract entry
point (Tranche 3 candidate).

**Evidence.** `docs/evidence/c-bad-debt.md` (live atomic fill+repay, LP
received, `drew=0` throughout); `docs/evidence/c-lp-unwind.md` (live mainnet
sims); FACTS.md "Bad-debt auction creation", "Mainnet LP unwind", Decisions
(SUPERSEDED + AMENDED rows); `docs/tranche-notes/auction-scope.md`.

### Gap 5 — Borrower discovery depended on configuration, not the chain

**What was wrong.** The keeper found borrowers from a bounded event lookback
(default 1000 ledgers) — on the sandbox that discovered ZERO positions, and
on the Blend testnet pool a full-window rescan cost 34.75 s EVERY cycle. The
RPC retention window was believed to be ~24h (it is 120960 ledgers ≈ 7
days), and `getEvents` segment/cursor semantics were misunderstood in a way
that silently dropped events. A follow-up overclaim ("the borrower cache is
an optimisation only") was itself caught and corrected: the cache is
load-bearing for borrowers idle longer than the retention window.

**What was verified & changed.** The complete pool event emission surface
was derived from the `publish()` calls (23 sites; topic layouts, payloads,
the `bad_debt` topic inversion, the filler-only-in-data case) and the RPC's
real limits measured live (10000-ledger segments, sentinel cursors, the
exactly-`limit` ambiguity, 5×5 filter fan-out). The keeper now maintains a
persistent event-driven borrower index (`keeper/blend/index.go`) with
correct segment paging; positions are always re-read from `get_positions`
(no event carries a post-action balance). Proven live: a borrower generated
at runtime, present in no config/cache/file, was discovered from events,
health-checked and liquidated end-to-end with no restart.

**Evidence.** FACTS.md "Pool event schemas", "getEvents — limits observed
live", "Borrower discovery"; `docs/evidence/d-discovery.md` (fresh-borrower
txs `e31a5ee8…`, `4294da5b…`, `a4af213d…`); A/B cold-start tests
(`TestSync_ColdAndCachedRestartAgree_WithinRetention`,
`TestSync_ColdStartCannotSeeBorrowersOlderThanRetention`).

---

## The honesty ledger — prior claims corrected, in one list

Each item: the prior claim → the verified reality (where fixed).

1. "Auction: lot 0→100% and bid 100→0% over 200 blocks" → two sequential
   phases over 400 ledgers, fair point t=200 (FACTS "Auction fill price
   curve"; CLAUDE.md/README corrected).
2. "Testnet oracle is Reflector" → Blend's `oraclemock`, admin-settable,
   wasm-hash proven (FACTS "Testnet addresses").
3. "TestnetV2 and RegionalStarterPack are two pools" → one contract
   (`CCEBVDYM…`).
4. "All three auction kinds: same logic, different request type" → three
   different asset flows; support matrix below (auction-scope.md; README T1
   status; docs-site).
5. "Bad-debt fills receive bToken collateral / interest fills pay BLND"
   (code comments) → bad-debt pays backstop LP while the filler assumes
   dToken debt; interest fills demand pre-approved backstop LP (FACTS
   "Auction asset flows").
6. "Bad-debt profit is donated to depositors" → `return_proceeds` rejects
   `NoDraw`; float-funded fills have `drawn == 0`, so the profit accrues to
   the operator. Corrected across README, docs-site, keeper log wording
   (commits `4d507cb`, `b18b875`, `e56edcf`; FACTS Decisions AMENDED row).
7. "The mainnet LP unwind reaches vault USDC" → verified at the VENUE,
   blocked at the VAULT (same NoDraw guard) — venue ≠ whole path
   (`c-lp-unwind.md`).
8. "A flat haircut can price LP exit cost" → exit cost is CONVEX in lot size
   (0.24% at ~$5 → 13.2% at ~1M LP, live-measured); haircut is a
   conservative config default, not a model (FACTS "Mainnet LP unwind").
9. "LiquidationLab is interface-identical to a real Blend pool; zero code
   changes to switch" → the lab lacks `new_auction`/`del_auction` and models
   no backstop; end-to-end validation runs on the Nectar Sandbox (commit
   `01bf655`).
10. "RPC retention ≈ 24h / 17280 ledgers" → 120960 ledgers ≈ 7 days,
    measured (FACTS "getEvents — limits observed live").
11. "getEvents returns the window from startLedger" → 10000-ledger segments
    with two cursor semantics; a page of exactly `limit` events proves
    nothing; clients must page to the sentinel (fixed in
    `keeper/soroban/rpc.go` + regression tests).
12. "The borrower cache is an optimisation only" → load-bearing outside the
    retention window; `WATCH_ADDRESSES` is the documented recovery
    (caught by adversarial review of the D2 change, commit `dc3abb9`).
13. "A hardcoded 50% liquidation works" → provably reverts
    (`InvalidLiqTooSmall`) on real positions; percent is chosen by on-chain
    simulation (FACTS Decisions).
14. "Missing auction returns a numbered contract error" → it is a wasm trap
    (`UnreachableCodeReached`) with no code; classifiers must match the trap
    string (live-caught, FACTS row).
15. "Fill then unwind as separate transactions" → a fill alone moves no
    tokens (filler assumes debt); fill+repay+withdraw is ONE atomic
    health-checked submit (FACTS Decisions).
16. "The cycle stays under the poll interval" → NOT met on the sandbox
    (11–12 s vs 10 s; `pool_load_ms` 2700–3500 dominates). Stated as an open
    limitation with the named follow-up, not papered over
    (`d-discovery.md` D3).
17. blend-utils README's `res_type` direction (upstream doc bug: says
    0=lenders; contract source says 0=dTokens/borrow) → recorded so our
    emissions config can't inherit the error (FACTS "blend-utils pool
    deploy").

Corrections 6, 7, 12 and 16 were found by adversarial review of our own
fresh work during this arc — the review loop applies to new claims, not just
legacy ones.

---

## Current auction-type support matrix (the claim we stand behind)

| Auction type | Detection | Fill | Capital source | Where proceeds go | Status |
|---|---|---|---|---|---|
| **User liquidation** (type 0, request 6) | Event-driven borrower index + HF probe every cycle | Create (simulation-sized) → verified-curve profitability → ONE atomic `submit([fill, repay…, withdraw_collateral…])` → DEX swap to USDC (oracle-anchored slippage floor) | Vault draw (estimator covers 100%-scale bid; capped by `max_draw_per_keeper`) | Measured proceeds returned via `return_proceeds`; depositor share price rises | **Proven live** (b-full-cycle.md; share price 1.0 → 1.0432672) |
| **Bad debt** (type 1, request 7) | Backstop position scan each cycle | Create (settle-asset legs only) → atomic fill+repay; past t=400: free-capture (bare fill) instead of delete | **Operator float only** (`BAD_DEBT_MAX_SPEND` cap; fill percent scales down when short) — never vault draws | Backstop LP held by the operator, valued at backstop spot − `BAD_DEBT_LP_HAIRCUT_BPS`; **profit accrues to the operator, not depositors** (vault `NoDraw` guard) | **Proven live** (c-bad-debt.md); mainnet unwind venue verified, vault credit blocked (c-lp-unwind.md) |
| **Interest** (type 2, request 8) | Detected in the backstop scan; logged once per auction | **Never filled** — bid requires pre-held, pre-approved backstop LP (~120% of interest value at spot) that a vault-USDC keeper does not carry | — | — | **Detect + defer, by design** (FACTS Decisions) |

---

## Known limitations that remain (and why)

1. **Interest auctions are not filled.** Requires holding backstop-LP
   inventory — no viable acquisition path on testnet (no BLND faucet or
   liquid market) and no fit with the vault's USDC-denominated capital model.
   Revisit only if LP inventory becomes a deliberate strategy.
2. **Bad-debt profit does not reach depositors.** The VLT-2 `NoDraw`
   anti-donation guard (a security feature) rejects float-funded proceeds.
   Fix requires a new registration-gated vault entry point (e.g.
   `add_profit`) — a contract change queued as a Tranche 3 hardening
   candidate, not a keeper change.
3. **Bad-debt LP unwind is deferred to mainnet.** Pre-mainnet comets hold
   admin-issued test tokens with no market. The mainnet exit is one verified
   call into the comet's Circle-USDC leg (0.24% at small size), but exit cost
   is convex in size and a single call caps at ≈ pool-USDC/3 — large lots
   need staged exits.
4. **A borrower idle longer than ~7 days is only reachable via cache or
   `WATCH_ADDRESSES`.** RPC retention is 120960 ledgers and interest accrual
   emits no event; run `BORROWER_CACHE` on a mounted volume (docker-compose
   does).
5. **Cycle time can exceed the 10 s poll interval** (11–12 s observed;
   `LoadPool` dominates at 2.7–3.5 s). Named follow-up: cache the reserve
   list/config and re-read only oracle prices. Overruns are counted
   (`nectar_cycle_overruns_total`), and correctness does not depend on the
   interval.
6. **One keeper process per keeper address.** Chain-derived stale-draw
   recovery cannot distinguish a crashed keeper's draw from a sibling's
   in-flight one (observed live; no funds lost, cycles wasted). Scale by
   registering more keepers; a vault-side draw timestamp is a T3 hardening
   candidate.
7. **Blend's canonical testnet pool is monitor-only** (settles a different
   USDC — Gap 2). Active fills run on the Nectar Sandbox; mainnet has no
   such split (same Circle USDC).
8. **Pinned-source ↔ deployed-wasm equivalence is cited, not reproduced.**
   The live pool runs blend-utils' shipped wasm, and facts cite pinned
   source `ba22b48`; we did not reproduce the wasm from source
   (no reproducible-build check). Divergence is theoretically possible.
9. **Oracle circuit breaker is not built yet** (Tranche 3 scope). Note the
   testnet constraint recorded in Gap 1: on testnet there is no Reflector to
   cross-reference; the breaker design must target mainnet feeds.
10. **Emitter event shapes are unverified** — the pinned v2 repo carries no
    emitter source (v2 reuses v1's). Irrelevant to current keeper paths;
    recorded so nobody builds on an unverified shape.
