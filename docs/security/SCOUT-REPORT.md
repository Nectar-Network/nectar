# Nectar Network — Security Tooling & Static Analysis Report (audit-freeze-v1)

Companion to [AUDIT-SCOPE.md](./AUDIT-SCOPE.md), [THREAT-MODEL.md](./THREAT-MODEL.md)
and [DATAFLOW.md](./DATAFLOW.md). Prepared for the SCF Soroban Security Audit Bank.
**Date:** 2026-08-16 (Session G — supersedes the 2026-07-24 pre-freeze report).

## Tool provenance (verified this session, not assumed)

- **Tool:** `cargo-scout-audit` **0.3.16** (CoinFabrik Scout) — verified as the
  **latest published release** on crates.io (`max_version` 0.3.16, published
  2026-02-13) at run time.
- **Install/run procedure re-verified from Scout's own repo + docs**
  ([github.com/CoinFabrik/scout-audit](https://github.com/CoinFabrik/scout-audit),
  [Getting Started](https://coinfabrik.github.io/scout-audit/docs/intro)):
  `cargo install cargo-scout-audit`, then `cargo scout-audit` in the contract
  directory (workspace-aware; `--output-format md|json|…`).
- **Exact invocation (2026-08-16), against the working tree byte-identical to
  `audit-freeze-v1` for `contracts/`:**

  ```
  PATH="$HOME/.cargo/bin:$PATH"   # rustup shim MUST come first — see note
  cd contracts/keeper-registry && cargo scout-audit --output-format md --output-path <out>/keeper-registry.md
  cd contracts/nectar-vault    && cargo scout-audit --output-format md --output-path <out>/nectar-vault.md
  ```

  Both runs completed with status **Analyzed**. Raw tool output is committed
  verbatim at [`scout-raw/nectar-vault.md`](./scout-raw/nectar-vault.md) and
  [`scout-raw/keeper-registry.md`](./scout-raw/keeper-registry.md). The run used
  detector toolchain `nightly-2025-08-07` with `rustc-dev` + `llvm-tools`,
  auto-selected by Scout.

  *Local-host note (recorded for reproducibility):* on this machine a Homebrew
  `cargo` shadows the rustup shim; under it Scout fails with
  `Failed to build detector library`. Putting `~/.cargo/bin` first in `PATH`
  fixes it — this also explains the July report's "does not install reliably on
  mixed local toolchains" observation. The CI job (clean Ubuntu toolchain,
  `.github/workflows/ci.yml` `scout` job) remains the reference environment.

## Raw results at the frozen tag

| Crate | Status | Critical | Medium | Minor | Enhancement | Total |
|---|---|---|---|---|---|---|
| `nectar_vault` | Analyzed | 24 | 27 | 0 | 4 | 55 |
| `keeper_registry` | Analyzed | 3 | 30 | 0 | 5 | 38 |
| **Total** | | **27** | **57** | **0** | **9** | **93** |

By detector: `integer_overflow_or_underflow` 27 (all the Criticals) ·
`ineffective_extend_ttl` 46 · `unnecessary_admin_parameter` 9 ·
`dynamic_storage` 2 · `storage_change_events` 7 · `soroban_version` 2.

Delta vs the 2026-07-24 pre-freeze CI run (same classes, no new detector class):
vault +4 Critical / +4 Medium — exactly the Session F additions (`add_profit`
accounting, `reconcile_default` write-down, rate-limit window arithmetic, the
new pause entry points and their TTL extends). `front_running` (1 pre-freeze
hit) no longer fires.

## Disposition — every finding, by class

Scout's severity labels are detector-assigned and over-flag by design (every
unchecked i128 `+`/`-` is "Critical"). Each class below was triaged against the
frozen source finding-by-finding; the class verdicts were then adversarially
re-checked by independent reviewers instructed to construct concrete exploits
(see "Adversarial verification" below). Dispositions are
**FIXED / FALSE-POSITIVE / ACCEPTED** per the Audit Bank checklist.
**No finding required a fix — the freeze stands; no `audit-freeze-v2` was
needed.**

### 1. `integer_overflow_or_underflow` — 27 × Critical → FALSE-POSITIVE (class)

In Soroban, i128/u32 arithmetic **traps and reverts the transaction atomically**
— there is no wraparound path, so the worst reachable outcome is a
self-reverting no-op on an out-of-band argument. Site-by-site (vault 24,
registry 3, all at the tag):

| Sites (VLT/REG:line) | Expression | Why no reachable harm |
|---|---|---|
| VLT:22, 27 | share/asset conversion products | Bounded by `deposit_cap`-scale magnitudes; largest product ~1e28, ~10 orders below `i128::MAX`. |
| VLT:118, 125-126 | deposit accumulation | Cap-checked (VLT:89-91) before mutation. |
| VLT:196, 199 | rate-limit window sums | `usdc_out ≤ total_usdc` (clamped VLT:185); comparison precedes accumulation. |
| VLT:204, 210-211 | withdraw burn/decrements | Guarded: `shares ≤ depositor.shares` (VLT:149-151); `usdc_out ≤ total_usdc` (the **already-hardened** SCOUT-total_usdc-underflow clamp, VLT:184-185); `depositor.shares ≤ total_shares` by bookkeeping. |
| VLT:292 | `total_usdc − active_liq` | Can legitimately go negative after a donation-assisted exit; the ONLY use is the `amount > available` comparison, which then rejects every draw — correct fail-closed behavior, no state write. |
| VLT:323, 330 | cumulative draw + `active_liq` | Cap- and availability-checked (VLT:292-309) before commit. |
| VLT:405-409, 413 | return split | `repay_target = min(amount, drawn)` and `repay ≤ active_liq` make every subtraction non-negative by construction (VLT:396-404). |
| VLT:493-494 | `add_profit` accumulation | Backed by a real token transfer; trap-bounded. |
| VLT:653, 665-667 | reconcile write-down | `clear = min(outstanding, active_liq)`; the loss write-down is **clamped at zero** (VLT:666 — the freeze-review fix); `total_profit` may go negative **by design** (cumulative P&L, documented). A negative `loss` (recovery > outstanding) credits the surplus — intended. |
| REG:121 | `count + 1` (u32) | Each increment costs a `min_stake` bond; 2³² registrations is economically impossible. |
| REG:359, 367 | slash math | `slash_rate_bps ≤ 10_000` enforced (REG:29-31, 407-409) ⇒ `slash_amt ≤ stake` ⇒ `stake −= slash_amt ≥ 0`; product bounded ~1e15. |

History note kept for auditors: the July triage of this same class surfaced one
real latent edge (`SCOUT-total_usdc-underflow` — a persistable negative
`total_usdc` in the post-loss `S > U` regime). It was **FIXED pre-freeze**
(withdraw clamp VLT:184-185 + regression test), and the freeze review fixed its
sibling in `reconcile_default` (clamp VLT:666). Both fixes are inside the tag.
The class verdict is false-positive **because** those two edges are closed.

### 2. `unnecessary_admin_parameter` — 9 × Medium → FALSE-POSITIVE (class)

Flagged: vault `set_config`/`pause`/`unpause`/`set_global_pause`/`set_asset_pause`
(VLT:518, 538, 545, 563, 583), registry `set_vault`/`pause`/`unpause`/`set_config`
(REG:51, 250, 257, 404). Every one routes through the same pattern: **identity
check against the stored admin first, `require_auth` second** (VLT:711-722;
REG:427-438). No bypass is constructible: a wrong address fails the identity
check (`Unauthorized`); the right address without a signature fails
`require_auth`. Passing the admin explicitly is a deliberate style choice — it
makes multisig transaction construction explicit (the 2-of-3 admin account is
the signer; see `docs/evidence/f5-multisig.md`).

### 3. `ineffective_extend_ttl` — 46 × Medium → ACCEPTED

Every hit is our uniform `extend_ttl(1000, 1000)` (instance) /
`extend_ttl(&key, 535680, 535680)` (persistent) idiom: threshold == extension,
so the extend runs on every access. This is storage-**rent hygiene**, not a
security property: values sit within Soroban's TTL bounds, nothing can trap,
and the cost is a few wasted instructions per call when the TTL is already
fresh. Accepted at the freeze (an idiom change across 46 sites for a gas
micro-optimization is not audit remediation); noted as a post-audit cleanup
candidate.

### 4. `dynamic_storage` — 2 × Medium → ACCEPTED

REG:117 / REG:171 — the registry's `Vec<Address>` keeper list (push on
register, rebuild on deregister). Growth-griefing it toward entry-size limits
needs ~3,300 **staked** registrations (~330k USDC locked at current
`min_stake`), refundable only by individually deregistering — economically
absurd for a griefing outcome of "the list getter gets slow". No theft path.
Accepted; a paginated list is a possible future refactor.

### 5. `storage_change_events` — 7 × Enhancement → ACCEPTED

Vault `set_config`/`pause`/`unpause` (VLT:518, 538, 545) and registry
`set_vault`/`pause`/`unpause`/`set_config` (REG:51, 250, 257, 404) emit no
events. Deliberate asymmetry: the T3 liquidation flags DO emit on every flip
(`liq_pause`/`asset_pause`, VLT:573-574, 595-596) because keepers poll them;
the VLT-4 pause and config are low-frequency admin state readable via
`is_paused`/`get_config`. Accepted as an observability nicety; recorded in
THREAT-MODEL §3.5 and the DATAFLOW pause matrix so nobody mistakes the
un-evented flags for evented ones.

### 6. `soroban_version` — 2 × Enhancement → ACCEPTED

soroban-sdk 22.x is the pinned major line for this deployment and the freeze;
bumping the SDK mid-freeze would reopen the frozen artifacts for a
non-security-driven change. Revisit at the next natural upgrade window
(post-audit), per FREEZE-NOTE rules.

## Adversarial verification of this triage

Per this repo's standing practice (CORRECTION-REPORT method), the triage was
re-checked 2026-08-16 by independent adversarial review passes instructed to
construct concrete exploits against the frozen source rather than accept the
triage's reasoning. Scope and result: the two exploit-bearing FALSE-POSITIVE
classes were attacked directly — all 27 `integer_overflow_or_underflow` sites
(call-sequence construction for wraparound, state-corrupting traps, and
invariant violations, including config changes between register and slash) and
all 9 `unnecessary_admin_parameter` sites (bypass construction) — and both
verdicts were **upheld with zero counterexamples**. The four ACCEPTED classes
are hygiene items where no exploit claim is made; a separate consistency pass
checked this report's numbers against the raw tool output (and caught an
arithmetic slip in this section's totals, fixed before commit). (The July
edition of this exercise is what found the real `SCOUT-total_usdc-underflow`
edge — the loop has teeth.)

## Companion assurance (context for the audit)

| Check | Scope | Result at the tag |
|---|---|---|
| `cargo test` (workspace) | contracts | 113 passed / 0 failed (96 audit-scope: vault 69 incl. 4 proptest invariants × 2000 cases, registry 27) — re-verified green 2026-08-16 |
| Property invariants | nectar-vault | solvency, withdraw/deposit inverse, share monotonicity, inflation-attack-unprofitable (with and without `add_profit` as the vector) |
| `go test -race ./...` | keeper (out of audit scope) | all ok, 9 packages |
| `cargo clippy` | both contracts, prod code | no warnings (2 pre-existing test-file lints left to keep the freeze diff minimal) |
| `npm run build` | frontend | clean |

## Remediation plan

**No open findings requiring remediation.** All 93 raw findings are dispositioned
above: 36 false-positive (27 overflow-class + 9 admin-parameter), 57 accepted
with written reasons (46 TTL idiom, 2 dynamic storage, 7 event coverage, 2 SDK
version), 0 fixed-at-this-tag (the two historically-real edges in the overflow
class were fixed before the tag and are inside the frozen code).

Standing rule: if the external audit (or any future Scout run on remediation
commits) surfaces a finding that requires a code change, the fix lands as
clearly-labeled commits, the freeze re-tags as `audit-freeze-v2`, FREEZE-NOTE
is amended, and the full suites re-run — per the freeze contract in
[FREEZE-NOTE.md](../audit/FREEZE-NOTE.md).

Post-audit (non-blocking) cleanup candidates, in priority order:
1. Emit events on the remaining admin state changes (`storage_change_events`).
2. Rationalize the `extend_ttl` idiom (threshold < extension) to stop redundant
   writes.
3. Consider a paginated keeper list (`dynamic_storage`).
4. SDK bump at the next natural window.
