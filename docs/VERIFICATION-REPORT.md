# VERIFICATION-REPORT.md — Blend Facts Verification Session (2026-08-03)

Scope: verify every Blend Protocol fact used by Nectar's integration **from
source and live chain state** before any corrective code is written. This
session produced zero application code — only `docs/FACTS.md`, pinned reference
clones, one throwaway simulate test, and this report.

Method: each decode gate ran three independent layers — (1) a reader agent
extracting claims with file:line citations from the pinned clones, (2) an
adversarial verifier agent re-reading every cited line and grepping for missed
facts, (3) my own direct reads of the decisive lines. Live facts were read from
testnet via `stellar contract invoke --send=no` (simulation only) and
`stellar contract fetch` + sha256. Result: **93/93 claims confirmed, 0 refuted,
0 unclear.** All facts recorded in [FACTS.md](FACTS.md) with date + source.

## Gate status

| Gate | Status | Evidence |
|---|---|---|
| 0.1 Pin blend-contracts-v2 + blend-utils | PASS | FACTS.md "Reference baselines" — `ba22b48` (2025-08-14) / `b05242d` (2025-12-18) |
| 0.2 Request struct + RequestType enum | PASS | FACTS.md "Request struct & RequestType integers" — complete 0–9 table from `pool/src/pool/actions.rs` |
| 0.3 Auction fill price curve | PASS | FACTS.md "Auction fill price curve" — contradiction resolved from `auction.rs:189-264` |
| 0.4 Three auction asset flows | PASS | FACTS.md "Auction asset flows" — creation + fill handlers cited for all three types |
| 0.5 Testnet addresses from live reads | PASS | FACTS.md "Testnet addresses" / "Our testnet USDC" — reserves, USDC SACs, oracle, backstop token all live-read |
| 0.6 blend-utils deploy path | PASS | FACTS.md "blend-utils pool deploy" — deploy/reserve/oracle/backstop-seed commands + BLND path |
| 0.7 RPC/XDR calling convention | PASS | `keeper/soroban/gate07_verification_test.go` + `docs/evidence/gate-0-7-simulate.json` (simulate-only `submit()` on live TestnetV2 pool returned well-formed `Positions`) |

## What was CONFIRMED (prior assumptions that held)

1. **RequestType fill integers 6/7/8 and Request shape.** The keeper's existing
   encoding (`keeper/blend/auction.go`: ScMap with sorted keys
   `address`/`amount`/`request_type`, u32/Address/i128) exactly matches
   `pool/src/pool/actions.rs:12-34`, and Gate 0.7 proved it round-trips against
   the deployed contract.
2. **The keeper's phase model of the curve is correct.**
   `keeper/blend/auction.go:227-251` already implements the 400-ledger
   two-phase curve with the fair point at elapsed=200 — matching source. The
   wrong "200-block" one-liner lives in CLAUDE.md prose, not in keeper code.
3. **Canonical testnet deployment addresses.** blend-utils
   `testnet.contracts.json` values match live chain state (reserve list, oracle
   from `get_config`, `backstop_token()`, pool wasm hash = shipped
   `lendingPoolV2` wasm).
4. **Our Circle USDC SAC identity** (`CBIELTK6…` = `USDC:GBBD47IF…`, Circle
   issuer) confirmed by live `symbol`/`name` reads.

## What CHANGED vs prior assumptions (each correction explicit)

1. **Auction curve (CLAUDE.md wrong).** Prior: "lot scales 0%→100% over 200
   blocks, bid scales 100%→0%" (simultaneous). Verified: two SEQUENTIAL phases —
   lot ramps 0→100% over blocks 0–200 with bid at 100%; bid decays 100→0% over
   blocks 200–400 with lot at 100%; bid=0 from block 400; stale-delete only
   after 500 blocks. Source: `pool/src/auctions/auction.rs:212-226`.
2. **The "Reflector oracle" is actually Blend's mock oracle (CLAUDE.md wrong).**
   `CAZOKR2Y…`'s on-chain wasm sha256 equals blend-utils' `oraclemock` hash; it
   is a fixed-price, admin-settable mock (`setData`/`setPriceStable`), not
   Reflector. **Impact:** the Tranche 3 "oracle circuit breaker
   cross-referencing Reflector" currently cross-references a mock on testnet;
   the design must acquire a real Reflector feed (mainnet or self-deployed).
3. **"RegionalStarterPack Pool V2" and "Blend TestnetV2 pool" are the same
   contract** (`CCEBVDYM…`; on-chain instance-storage `Name = "TestnetV2"`).
   There is one canonical V2 testnet pool, not two — the session task's "read
   BOTH pools" premise collapses to a single pool, read once.
4. **USDC asset mismatch (most consequential).** The TestnetV2 pool's USDC
   reserve is Blend's mock `USDC:GATALTGT…` (`CAQCFVLO…`) — NOT our vault's
   Circle `USDC:GBBD47IF…` (`CBIELTK6…`). A keeper filling TestnetV2 auctions
   pays/receives Blend-USDC, which cannot settle vault P&L directly. Decision
   recorded as OPEN in FACTS.md "Decisions" (own pool vs swap layer vs
   Blend-USDC accounting).
5. **Keeper code comments mis-describe bad-debt and interest auctions.**
   `keeper/blend/auction.go:206-215` says bad-debt fills receive "bToken
   collateral" (actually: backstop LP tokens via `backstop.draw`, while
   assuming dToken debt) and interest fills "pay BLND" (actually: backstop
   BLND:USDC comet LP tokens via `backstop.donate` `transfer_from`, which
   requires a prior approval). The submit encoding itself is fine; any
   profitability/asset-handling logic built on those comments must be audited
   in the next session.
6. **blend-utils README `res_type` direction is backwards.** Contract source:
   `res_type` 0 = dTokens (borrow), 1 = bTokens (supply)
   (`pool/src/contract.rs:229-241`, `emissions/manager.rs:42-43`). README.md:83
   says the opposite; the repo's code comments agree with the contract.
7. **Testnet BLND has no faucet.** BLND is classic-asset minted only by Blend's
   issuer/ADMIN key; third parties get it via emissions or a DEX/comet trade.
   Any plan assuming "faucet BLND for backstop seeding" is invalid.

## What remains UNKNOWN / open

1. **Pinned-source ↔ deployed-wasm equivalence.** The live pool runs the exact
   wasm blend-utils ships (`a41fc53d…`), but we did not reproduce that wasm
   from `ba22b48` source (no reproducible-build check). Facts are cited to the
   pinned source; treat any source-vs-chain divergence as theoretically
   possible until a reproducible build confirms it.
2. **USDC bridging decision** (FACTS.md Decisions, OPEN): own Blend pool with a
   Circle-USDC reserve vs DEX swap layer vs Blend-USDC P&L accounting on
   testnet. Constraint discovered: deploying our own pool still requires
   backstop LP (BLND:USDC comet, threshold k ≥ 100,000) and BLND is
   issuer-gated.
3. **Keeper profitability engine audit** against the verified asset flows
   (correction 5) — next session, before any corrective code.
4. **Interest-auction pre-approval:** whether our keeper ever grants the
   backstop the required `transfer_from` allowance for comet LP — to check in
   the next session.
5. **CLAUDE.md corrections** (curve one-liner, oracle label, pool label) —
   recorded as OPEN in FACTS.md Decisions; deferred because this session
   produces verified facts only.

## Session artifacts

- `docs/FACTS.md` — all sections populated, every entry dated + cited
- `reference/blend-contracts-v2` @ `ba22b48`, `reference/blend-utils` @
  `b05242d` (gitignored; hashes pinned in FACTS.md)
- `keeper/soroban/gate07_verification_test.go` (env-gated, skips in CI; full
  keeper suite `go test -race ./...` green 2026-08-03)
- `docs/evidence/gate-0-7-simulate.json` (trimmed simulate response,
  latestLedger 3935602)
- Workflow verification: 8 agents (4 readers + 4 adversarial verifiers),
  93/93 claims confirmed, plus ~40 additional cited facts from completeness
  sweeps folded into FACTS.md.
