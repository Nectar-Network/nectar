# FREEZE-NOTE — audit-freeze-v1

**Written:** 2026-08-16 (Session F). **Tag:** `audit-freeze-v1` (the commit
this file lands in; the exact hash is recorded in docs/FACTS.md next to the
tag, and `git rev-parse audit-freeze-v1` is authoritative).

**The rule:** from this tag until the audit closes, **no contract edits except
audit remediation.** Remediation lands as clearly-labeled commits on top of
the tag; anything else that touches `contracts/` waits.

## In scope at this tag (what the auditors read)

| File | LOC |
|---|---|
| `contracts/nectar-vault/src/lib.rs` | 834 |
| `contracts/nectar-vault/src/types.rs` | 99 |
| `contracts/keeper-registry/src/lib.rs` | 452 |
| `contracts/keeper-registry/src/types.rs` | 53 |
| **Production total** | **1,438** |
| Tests: vault `test.rs` 1,947 + `prop_test.rs` 89 + registry `test.rs` 564 | 2,600 |

Frozen build artifacts (build: rustup 1.90.0 `cargo build --target
wasm32-unknown-unknown --release`, then `stellar contract optimize`; the
Homebrew rustc must NOT be first in PATH — it has no wasm sysroot, and the
raw 1.90 wasm needs the optimize pass or the VM rejects it):

| Artifact | sha256 |
|---|---|
| `nectar_vault.optimized.wasm` (32,815 B) | `adcbaada469d3d10561dfa79ea7fce2499c6b041c62f65d40281e63b83e60b89` |
| `keeper_registry.optimized.wasm` (23,623 B) | `5346d779f6bf6f90dec3fb54916e0310b77307b71cbdc571a61b1957a866b704` |

## Explicitly OUT of scope

- `keeper/` (Go daemon) — updated in this session only as the interface
  proof (new draw arity, tuple `get_keeper_draw`, pause-flag reads); its own
  hardening is Session H.
- `frontend/`, `scripts/`, `docs/`, `contracts/mock-token`,
  `contracts/liquidation-lab` (test scaffolding, deployed nowhere that
  matters).
- The **keeper-sdk** repo's `VaultClient` mirror (separate repo; must adopt
  `Draw(amount, asset)` + `LiqPaused` before third-party keepers build against
  T3 — Session H).

## What Session F changed (audit focus areas)

1. **F1 — circuit-breaker pause plumbing** (DECISION F-2a): `draw(keeper,
   amount, asset)`; global + per-asset liquidation pause (admin-set, events
   on every flip); invariant: depositor exit is NEVER blocked by a
   liquidation pause.
2. **F2 — withdrawal rate limiting**: per-address fixed 24h window
   (`max_withdraw_per_24h`, 0 = off), `WithdrawalRateLimited` (#16).
3. **F3 — add_profit** (DECISION F-1a): registration-gated donation path for
   float-funded (bad-debt) profit; no shares minted; distinct `profit_added`
   event; registry now enforces `min_stake > 0` (constructor + set_config).
4. **F4 — draw timestamps**: `KeeperDraw` → `DrawInfo { amount, since }`;
   `get_keeper_draw` → `(amount, since)`; `since` mirrors the registry's
   `last_draw_time`; preserved on partial return.
5. **Freeze-review fixes** (16 findings from a 21-agent adversarial review;
   9 confirmed, 7 refuted, all confirmed ones fixed pre-tag): `draw` rejects
   `amount <= 0`; `reconcile_default` clamps the loss write-down at zero;
   `add_profit` rejects an empty vault (`NoShares`); keeper checks ALL lot
   assets against the pause flags (fail closed) before creating auctions and
   before drawing; stale-draw recovery no longer fails silently.

## Verified live before the tag

- 2-of-3 classic multisig admin: 1 sig rejected (`OpBadAuth`), any 2 of 3
  accepted, on a throwaway vault built from Session F code
  (`docs/evidence/f5-multisig.md`; note: that deployment used the
  intermediate Session-F build, predating the review fixes — it proved auth
  mechanics, not the final byte code).

## Suite status at the tag

| Suite | Result |
|---|---|
| `cargo test` (workspace) | 113 passed / 0 failed — vault 69 (incl. 4 proptest suites × 2000 cases), registry 27, liquidation-lab 12, mock-token 5 |
| Audit-scope tests (vault + registry) | 96 |
| `go test -race ./...` (keeper, 9 packages) | all ok |
| `npm run build` (frontend) | clean |
| `cargo clippy` (vault + registry prod code) | no code warnings (2 pre-existing test-file lints left untouched to keep the freeze diff minimal) |

Functional coverage summary for Session G: every public vault/registry entry
point has direct tests; adversarial coverage includes the inflation attack
(unit + property, with and without `add_profit` as the vector), the
donation-assisted exit → slash write-off path, pause/exit invariants, rate-
limit window boundaries (86399/86400), draw-timestamp consistency, zero/
negative amounts on every money-moving function, and cross-contract
slash/reconcile accounting against the real registry.

## Known, documented properties (NOT bugs — do not re-report)

- **Self-declared draw asset** (DECISION F-2a): a malicious keeper can
  misdeclare; slashing is the backstop; the honest path is closed keeper-side
  (all-lot-assets check). Global pause hard-blocks regardless.
- **Fixed-window rate limit boundary**: up to ~2× `max_withdraw_per_24h` can
  leave across one window rollover instant (inherent to fixed windows —
  chosen by spec; review verdict: by design).
- **Slash-recovery accounting**: USDC recovered by a slash lands in the
  vault balance; when the write-down clamps at zero the recovery stays
  unaccounted (donation-like, conservative).
- **The live testnet pair (`CD33A7IG…`/`CDOGQY7N…`) predates this tag** —
  it runs the pre-Session-F interface. The audited code is deployed fresh
  (testnet re-deploy in Session H; mainnet per T3 plan). Audited code ==
  deployed code holds at those deployments, not at the legacy pair.
