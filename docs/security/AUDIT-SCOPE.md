# Nectar Network — Audit Scope (audit-freeze-v1)

Prepared for the SCF Soroban Security Audit Bank engagement. Pairs with
[THREAT-MODEL.md](./THREAT-MODEL.md), [DATAFLOW.md](./DATAFLOW.md),
[SCOUT-REPORT.md](./SCOUT-REPORT.md) and
[../audit/FREEZE-NOTE.md](../audit/FREEZE-NOTE.md).
**Updated:** 2026-08-16 (Session G — supersedes the 2026-07 draft, which cited a
planned `v0.3.0-audit` tag that was never created).

## In scope — the two on-chain Soroban contracts

| Contract | Path | Functional LOC (tokei 14.0.0, excl. tests) |
|---|---|---|
| **NectarVault** | `contracts/nectar-vault/` | 672 |
| **KeeperRegistry** | `contracts/keeper-registry/` | 399 |

**Total: 1,071 functional LOC** (1,438 raw lines incl. comments/blanks), Rust,
soroban-sdk 22.x. These hold all value and all privileged logic: USDC
deposits/shares, keeper stake, capital draw/return, donated profit
(`add_profit`), slashing with atomic vault reconciliation, pause/circuit-breaker
controls, and the cross-contract link between the two.

The audited commit is tagged **`audit-freeze-v1`**
(= `dbf0e5cd0a0c11bd123643fe72cf45ca2f35ccf5`, branch `tranche-3`), frozen: no
contract edits past the tag except audit remediation (which lands as
`audit-freeze-v2`). Frozen optimized wasm hashes are recorded in
[FREEZE-NOTE.md](../audit/FREEZE-NOTE.md). All prior hardening rounds are
included at the tag: VLT-1..6, NEW-cap/reconcile/drain/init, the Scout-triage
underflow clamp, the T3 additions (pause flags, rate limit, `add_profit`, draw
timestamps), and all 9 confirmed findings of the 21-agent pre-freeze review.

## Out of scope

| Component | Why excluded |
|---|---|
| `contracts/liquidation-lab/` | Test harness — ABI-compatible auction simulator for E2E tests; never production-deployed. |
| `contracts/mock-token/` | Test-only mock SAC. Production settles **Circle USDC** (a Stellar-native SAC, not our code). |
| `keeper/` (Go daemon) | Off-chain; holds no user funds and cannot bypass any on-chain check. A daemon compromise is bounded to that operator's stake + outstanding draw. Its own hardening is Session H. |
| `frontend/` | Read-only + Freighter-signed tx builder; no keys, no privileged authority. |
| **keeper-sdk** (separate repo) | Third-party operator SDK; must adopt the frozen interface before T3 (Session H). |
| Blend, Soroswap, comet, oracle | External protocols in their own trust zones (Blend v2 itself was audited via Code4rena + Certora, Feb–Mar 2025). The vault never reads them; only the keeper mediates. |

## Trust assumptions

- **USDC SAC is well-behaved (non-reentrant).** Vault accounting is
  internal-state-based (`total_usdc`), not token-balance-derived; CEI ordering in
  `withdraw`/`draw` is defense-in-depth on top (THREAT-MODEL VLT-6).
- **Admin is a 2-of-3 multisig** — enforcement verified live at the classic auth
  layer before the freeze (`docs/evidence/f5-multisig.md`). What a hostile admin
  quorum can and cannot do is analyzed in THREAT-MODEL §3.5 / T3-4 (it can pause
  entry and throttle, it cannot block withdraw or confiscate).

## Explicit scope decisions (documented, not defects)

1. **Self-declared draw asset (T3-1 / DECISION F-2a).** `draw(keeper, amount,
   asset)` declares its target collateral; the per-asset pause gates the
   declaration, the GLOBAL pause gates every draw regardless, and slashing +
   deactivation backstop misdeclaration. Auditors are asked to review the
   economics (`max_draw_per_keeper` vs `min_stake`), not to re-report the
   documented limitation.
2. **ORA-1 — oracle circuit breaker is Tranche-3 keeper-side work, not at this
   tag.** The contract-side actuation surface (global/per-asset pause flags) IS
   in scope; the cross-referencing breaker module that will drive it is not yet
   built. Interim mitigation: liquidation sizing by on-chain simulation +
   oracle-anchored swap floors (proven live refusing a ~30% adverse quote).
3. **Fixed-window rate limit boundary** (~2× across a window rollover instant)
   and **multi-address sybil splitting** are accepted by design — the limit is a
   per-address bleed-rate brake, not a global exit throttle (THREAT-MODEL T3-2).
4. **Known accounting conservatisms:** slash recovery beyond the clamped
   write-down stays unaccounted (donation-like); `withdraw` can revert on
   insufficient *liquid* balance at high utilization (liveness, never loss);
   every deposit resets the depositor's cooldown.

## Assurance evidence at the tag

- **113 workspace tests passing, 96 audit-scope** (vault 69 incl. 4 property
  invariants × 2000 cases — solvency, inverse, monotonicity,
  inflation-attack-unprofitable with and without `add_profit` as the vector;
  registry 27), incl. real cross-contract draw→return and slash→`reconcile_default`
  cycles. Re-verified green 2026-08-16.
- **`go test -race ./...`** clean (9 keeper packages); frontend build clean;
  clippy clean on production code.
- **cargo-scout-audit 0.3.16** run against both frozen contracts —
  finding-by-finding triage in [SCOUT-REPORT.md](./SCOUT-REPORT.md).
- **Live evidence:** full liquidation cycle with rising share price
  (`b-full-cycle.md`), autonomous borrower discovery + liquidation
  (`d-discovery.md`), bad-debt fill (`c-bad-debt.md`), mainnet LP-unwind venue
  sims (`c-lp-unwind.md`), 2-of-3 multisig probes (`f5-multisig.md`).
- **Deployment note (honest):** the currently-live testnet pair predates this
  tag (pre-Session-F interface); the frozen code is redeployed in Session H so
  audited code == deployed code at the audited deployments
  (FREEZE-NOTE "known properties").
