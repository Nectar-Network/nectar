# Nectar Network — Security Tooling & Static Analysis Report

Companion to [AUDIT-SCOPE.md](./AUDIT-SCOPE.md), [THREAT-MODEL.md](./THREAT-MODEL.md)
and [DATAFLOW.md](./DATAFLOW.md). Prepared for the SCF Audit Bank. **Date:** 2026-07-24.

## Summary

| Tool | Scope | Result |
|---|---|---|
| `cargo test --locked` | Both contracts | ✅ **73/73 pass** (nectar-vault 47, keeper-registry 26) + 17 test-harness (LiquidationLab 12, mock-token 5) |
| `proptest` invariants | nectar-vault | ✅ **4 properties × 2000 cases** (solvency + share-price) — included in the 47 |
| `go test -race ./...` | Keeper daemon | ✅ pass (unit + integration + stress, race-clean) |
| `cargo clippy --all-targets` | Both contracts | ✅ 0 errors; 2 low-severity style warnings (duplicated attribute, manual range-contains) |
| **`cargo-scout-audit`** (Soroban linter) | Both contracts | ✅ **ran on CI** — 23 "Critical" + 53 "Medium" raw; **every class triaged** below (all false-positive or accepted, one latent edge hardened). Raw output: CI artifact `scout-report`. |
| Manual STRIDE review | Both contracts | Findings in THREAT-MODEL.md — all High/Medium remediated |

## Test suite

`cargo test --locked` (native, soroban-sdk 22 testutils, `mock_all_auths`):
- **nectar-vault: 47** — 43 unit/integration (deposit/withdraw share math, 7-decimal
  rounding, deposit caps, cooldown, per-keeper draw caps, partial-return
  slash-eligibility, real cross-contract slash→`reconcile_default`, and the
  post-loss withdraw-underflow regression) + **4 property invariants**.
- **keeper-registry: 26** — registration/staking, dedup, slashing (timeout + rate +
  deactivation), draw marking, execution recording, admin/vault auth gates, pause.
- **test harness (out of audit scope): 17** — LiquidationLab 12, mock-token 5.

### Property-based invariants (`nectar-vault/src/prop_test.rs`)
2000 random cases each, over wide 7-decimal ranges:
1. **Solvency** — deposit→withdraw of exactly the minted shares never returns more than deposited (rounding always favors the pool).
2. **Inverse** — withdraw→re-deposit never yields more shares.
3. **Monotonicity** — shares minted are non-decreasing in the deposit amount.
4. **Inflation-attack unprofitable (VLT-1)** — the first-depositor donation attack always loses money (`attacker_out ≤ attacker_in`).

`go test -race ./...` on the keeper passes under the race detector (client/XDR encoding, adapter loop, DEX slippage, retry/tx-safety), hermetic against httptest mocks.

## `cargo-scout-audit` — ran on CI, fully triaged

The SDF-recommended Soroban linter now runs in CI on a clean Ubuntu toolchain
(it does not install reliably on a mixed local Homebrew/rustup host). Raw counts:

| Crate | Critical | Medium | Minor | Enhancement |
|---|---|---|---|---|
| `nectar_vault` | 20 | 23 | 0 | 4 |
| `keeper_registry` | 3 | 30 | 0 | 5 |

Scout's severity labels are detector-assigned and **over-flag heavily** (e.g. it
marks every unchecked i128 `+`/`-` "Critical"). Each finding **class** was triaged
against the real code and then **adversarially re-checked by an independent
skeptic** instructed to construct a concrete exploit; verdicts below. **No class
was a must-fix vulnerability**; one latent accounting edge was hardened anyway.

| Detector (class) | Count | Severity (Scout) | Verdict | Disposition |
|---|---|---|---|---|
| `integer_overflow_or_underflow` | 23 | Critical | **False positive** | i128 **traps and reverts atomically** in Soroban (no wraparound); every accounting op is bounded by `deposit_cap`/`max_draw` or explicitly guarded (`shares ≤ depositor.shares`, `repay = min(amount,drawn)`, `slash_rate_bps ≤ 10000`, saturating counters). The largest product (share math) is ~1e28, ~10 orders below `i128::MAX`. No within-caps input reaches a trap; the worst case is a self-reverting no-op on an out-of-band argument. **One latent edge hardened — see below.** |
| `unnecessary_admin_parameter` | 7 | Medium | **False positive** | `pause`/`unpause`/`set_config`/`set_vault` in **both** contracts check `stored_admin != caller → Unauthorized` (via `require_admin`) **before** `require_auth`. No bypass is constructible: a wrong address fails the identity check; the real address fails `require_auth` (no signature). |
| `ineffective_extend_ttl` | 43 | Medium | **False positive** | `extend_ttl` values (instance 1000, persistent 535680) sit within Soroban's min/max-TTL bounds, so no trap or refresh-DoS. Storage-rent hygiene only; not a funds/security issue. |
| `dynamic_storage` | 2 | Medium | **Accepted risk** | Registry keeps a `Vec<Address>` keeper list. Bricking it via `Vec` growth needs ~3,300 distinct staked registrations (~330,000 USDC of stake locked) — economically absurd griefing, no theft. Documented; a paginated list is a possible future refactor. |
| `front_running` | 1 | Medium | **False positive** | Detector wants a min-out on a token transfer. The flagged transfer is not a DEX swap; keepers are verified + draw-capped and the virtual-offset share ratio is not mempool-manipulable for profit. |
| `storage_change_events` | 7 | Enhancement | **Accepted (enhancement)** | Emitting events on admin/config change is nice-to-have observability; gates nothing. |
| `soroban_version` | 2 | Enhancement | **Accepted (enhancement)** | soroban-sdk 22.x is the current major line for this deployment. |

### Latent edge found by the triage and **hardened**: `SCOUT-total_usdc-underflow`
Not flagged as a distinct Scout finding, but surfaced while adversarially checking
the overflow class. After a `reconcile_default` loss write-off the vault can enter
the **`total_shares > total_usdc`** regime; there `to_assets(shares)` (which adds
`+VIRTUAL_OFFSET` to the numerator) can exceed `total_usdc` by offset-scale dust.
Because `total_usdc` is a **signed** `i128`, `total_usdc -= usdc_out` does **not**
trap on going negative — and a permissionless raw-token **donation** (which
inflates the vault's liquid balance) can let the over-payment transfer succeed and
**persist a negative `total_usdc`**. Impact is dust and non-profitable (the donor
loses more than is extracted; later depositors recover it), so it was **not a
freeze-blocker** — but it is a real invariant violation, so it is **fixed**:
`withdraw` now clamps `usdc_out = to_assets(...).min(total_usdc)`. The clamp is a
**proven no-op** for `total_shares ≤ total_usdc` (`to_assets(sh,S,U) ≤ U ⟺ S ≤ U`)
and keeps `total_usdc ≥ 0`. Covered by
`test_withdraw_after_loss_cannot_underflow_total_usdc` (fails without the clamp:
`total_usdc` reaches `-389961`; passes with it).

## Findings & remediation status (from THREAT-MODEL.md)

Findings were adversarially re-verified by independent review passes, then
remediated; the implemented fixes were adversarially reviewed again (which
surfaced NEW‑drain and the underflow edge, both fixed below). **All 73 in-scope
contract tests pass** and both contracts build to deployable `wasm32v1-none`.

| ID | Finding | Severity | Status |
|---|---|---|---|
| VLT‑1 | Share-inflation / first-depositor attack | **High** | ✅ **Fixed** — symmetric virtual-offset share math (`VIRTUAL_OFFSET=1_000_000` in `to_shares`/`to_assets`) + reject-zero-share deposits. 5 regression tests + a 2000-case property invariant. |
| VLT‑2 | `return_proceeds` books arbitrary donations as profit | Medium | ✅ **Fixed** — rejects `drawn == 0` (`NoDraw`); only a keeper with an outstanding draw can settle. |
| VLT‑3 | Keeper verification discards result, no `active`/stake check | Medium | ✅ **Fixed** — `draw` → `registry.verify_keeper` asserts `active == true` **and** `stake ≥ min_stake`. |
| VLT‑4 | No emergency pause on the vault | Medium | ✅ **Fixed** — admin `pause`/`unpause` gating `deposit`+`draw` (withdraw + return stay open). |
| VLT‑5 | Single admin key (no multisig) | Medium | **Accepted / deployment-level** — mainnet admin is a Stellar multisig account; no contract change. See [AUDIT-SCOPE.md](./AUDIT-SCOPE.md). |
| VLT‑6 | CEI ordering in `draw` | Low | ✅ **Fixed** — effects committed before the token transfer. |
| NEW‑cap | Per-keeper draw cap was per-call, not cumulative | **High** | ✅ **Fixed** — `draw` enforces `outstanding + amount ≤ max_draw_per_keeper`. |
| NEW‑reconcile | Slash didn't reconcile the vault (registry/vault drift) | **High** | ✅ **Fixed** — `slash` cross-calls `vault.reconcile_default` (atomic) to write off the defaulted draw. |
| NEW‑drain | Slashed keeper could re-draw the full cap each window | **High** | ✅ **Fixed** (fix-review) — `slash` **deactivates** the keeper (`active=false`); must re-register to draw. |
| NEW‑init | `initialize` front-run (attacker self-seizes admin) | Medium | ✅ **Fixed** — atomic soroban-sdk 22 `__constructor` (no separate init tx to race); registry↔vault circular ref resolved by one-time admin-gated `set_vault`. |
| SCOUT‑underflow | `total_usdc` can persist negative in the post-loss S>U regime + donation | Low | ✅ **Fixed** — `withdraw` clamps `usdc_out` to `total_usdc`; regression test added. |
| REG‑1 | Permissionless `slash` | Info | By design & safe (timeout + active-draw gated); `slash_rate_bps ≤ 10000` validated. |
| ORA‑1 | Oracle manipulation / circuit breaker | Med/High | **Out of current scope** — a Tranche-3 module not yet built; interim mitigation is the keeper's oracle-anchored `amount_out_min`. See [AUDIT-SCOPE.md](./AUDIT-SCOPE.md). |
| DEX‑1 | Swap slippage / sandwich | Low/Med | Mitigated (oracle-anchored min-out). |
| INT‑1 | Integer overflow in share math | Low | Reviewed — safe (i128 traps on overflow; products stay < i128::MAX at realistic caps). Corroborated by the Scout triage above. |

**Known design limitations (documented, not bugs):** `withdraw` sizes payout from
`total_usdc` (which counts drawn-but-unreturned capital), so a withdrawal can
revert on insufficient *liquid* balance while utilization is high — a liveness
limitation, never fund loss. Every deposit resets the withdraw cooldown on the
depositor's whole balance (minor, self-inflicted).

**Remaining for mainnet (Tranche 3, out of current audit scope):** VLT‑5 (admin
multisig, account-level) and ORA‑1 (oracle circuit breaker module). All contract
code findings are remediated; the audit will validate the fixes.
