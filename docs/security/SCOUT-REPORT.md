# Nectar Network — Security Tooling & Static Analysis Report

Companion to [THREAT-MODEL.md](./THREAT-MODEL.md) and [DATAFLOW.md](./DATAFLOW.md).
Prepared for the SCF Audit Bank. **Date:** 2026-06-22.

## Summary

| Tool | Scope | Result |
|---|---|---|
| `cargo test` | Both contracts | ✅ **63/63 pass** (nectar-vault 26, keeper-registry 37) + LiquidationLab 12 |
| `go test -race ./...` | Keeper daemon | ✅ pass (unit + integration + stress, race-clean) |
| `cargo clippy --all-targets` | Both contracts | ✅ compiles clean; **1 low-severity style warning per crate**, 0 errors, 0 security lints |
| `cargo-scout-audit` (Soroban linter) | Both contracts | ⚠️ **could not be run in the local dev environment** — see note below; deferred to CI + the audit firm |
| Manual STRIDE review | Both contracts | **12 findings** documented in THREAT-MODEL.md (5 open for pre-mainnet remediation) |

## Test suite

`cargo test` (native, soroban-sdk testutils, `mock_all_auths`):
- **nectar-vault:** 26 tests — deposit/withdraw share math, 7-decimal rounding edge cases, deposit caps, withdrawal cooldown, draw limits, per-keeper draw accounting, partial-return slash-eligibility.
- **keeper-registry:** 37 tests — registration/staking, dedup, slashing (timeout + rate), draw marking, execution recording, admin/vault auth gates, pause.
- **LiquidationLab (test harness):** 12 tests — auction lifecycle + real token transfers on fill.

`go test -race ./...` on the keeper: passes under the race detector (client/XDR encoding, adapter loop, DEX slippage, retry/tx-safety), hermetic against httptest mocks.

## Static analysis — `cargo clippy`

Both contracts compile under clippy with **only a single low-severity style warning per crate** and **no errors**. No `clippy::correctness`, `clippy::suspicious`, or security-relevant lints fire. The remaining warning is stylistic (does not affect behavior) and will be cleared in the Tranche 3 hardening pass.

## `cargo-scout-audit` (Soroban security scanner) — not run locally

We attempted the SDF-recommended Soroban linter (`cargo install cargo-scout-audit`, v0.3.16). It **failed to compile in this local development environment** — a dependency build failure tied to a mixed Homebrew/rustup Rust toolchain on the build host, not to our contract code. Rather than ship an unreliable local run, we are deferring the Scout scan to:

1. **CI** — a clean Linux container in GitHub Actions (consistent rustup toolchain), where Scout installs reliably; the report will be committed here and linked.
2. **The audit engagement** — the whitelisted firm runs Scout (and their own tooling) as part of the review.

In the interim, the **manual STRIDE review** (THREAT-MODEL.md) is the substantive security analysis — it goes deeper than an automated linter (e.g. the share-inflation vector VLT‑1 and the `return_proceeds` gating gap VLT‑2 are logic/economic issues a lint would not catch).

## Findings & remediation status (from THREAT-MODEL.md)

Findings were adversarially re-verified by an independent review pass, then remediated; the
implemented fixes were adversarially reviewed again (which surfaced NEW‑drain, fixed below). All
80 contract tests pass and both contracts compile to deployable `wasm32v1-none`.

| ID | Finding | Severity | Status |
|---|---|---|---|
| VLT‑1 | Share-inflation / first-depositor attack | **High** | ✅ **Fixed** — symmetric virtual-offset share math (`VIRTUAL_OFFSET=1_000_000` in `to_shares`/`to_assets`) + reject-zero-share deposits. (Also structurally hard: `total_usdc` is internal, so a bare token donation can't move price.) 5 regression tests. |
| VLT‑2 | `return_proceeds` books arbitrary donations as profit | Medium | ✅ **Fixed** — `return_proceeds` rejects `drawn == 0` (`NoDraw`); only a keeper with an outstanding draw can settle. |
| VLT‑3 | Keeper verification discards result, no `active`/stake check | Medium | ✅ **Fixed** — `draw` → `registry.verify_keeper` which asserts `active == true` **and** `stake ≥ min_stake`. |
| VLT‑4 | No emergency pause on the vault | Medium | ✅ **Fixed** — admin `pause`/`unpause` gating `deposit`+`draw` (withdraw + return stay open). |
| VLT‑5 | Single admin key (no multisig) | Medium | Open — Tranche 3 (2-of-3 admin multisig, account-level). |
| VLT‑6 | CEI ordering in `draw` | Low | ✅ **Fixed** — effects committed before the token transfer. |
| NEW‑cap | Per-keeper draw cap was per-call, not cumulative | **High** | ✅ **Fixed** — `draw` enforces `outstanding + amount ≤ max_draw_per_keeper`. |
| NEW‑reconcile | Slash didn't reconcile the vault (registry/vault drift) | **High** | ✅ **Fixed** — `slash` cross-calls `vault.reconcile_default` (atomic) to write off the defaulted draw. |
| NEW‑drain | Slashed keeper could re-draw the full cap each window | **High** | ✅ **Fixed** (surfaced by fix-review) — `slash` now **deactivates** the keeper (`active=false`); it must re-register to draw. |
| NEW‑init | `initialize` front-run (attacker self-seizes admin) | Medium | ⚠️ **Partial** — `admin.require_auth()` added (blocks assigning a non-consenting admin). Full fix = an atomic soroban-sdk 22 `__constructor`; **scheduled for the mainnet redeploy** (Tranche 3), since the window is deploy-time only and current testnet contracts are already safely initialized. |
| REG‑1 | Permissionless `slash` | Info | By design & safe (timeout + active-draw gated); `slash_rate_bps ≤ 10000` now validated. |
| ORA‑1 | Oracle manipulation | Med/High | Tranche 3 circuit breaker. |
| DEX‑1 | Swap slippage / sandwich | Low/Med | Mitigated (oracle-anchored min-out). |
| INT‑1 | Integer overflow in share math | Low | Reviewed — safe (i128 traps on overflow; products stay < i128::MAX at realistic caps). |

**Known design limitations (documented, not bugs):** `withdraw` sizes payout from `total_usdc`
(which counts drawn-but-unreturned capital), so a withdrawal can revert on insufficient *liquid*
balance while utilization is high — a liveness limitation, never fund loss. Every deposit resets
the withdraw cooldown on the depositor's whole balance (minor, self-inflicted).

**Remaining before mainnet:** VLT‑5 (admin multisig), NEW‑init (`__constructor`), and ORA‑1
(oracle circuit breaker) are the Tranche 3 "Mainnet Deployment" + "Security Hardening" items. The
audit will validate all fixes.
