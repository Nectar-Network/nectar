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

| ID | Finding | Severity | Status |
|---|---|---|---|
| VLT‑1 | Share-inflation / first-depositor attack | **High** | Open — fix before mainnet (virtual-shares offset + dead-shares + min first deposit) |
| VLT‑2 | `return_proceeds` not gated to registered keepers | Medium | Open — gate to keeper with `KeeperDraw > 0` |
| VLT‑3 | `require_registered_keeper` discards result, no `active` check | Medium | Open — bind result, assert active |
| VLT‑4 | No emergency pause on the vault | Medium | Open — Tranche 3 hardening |
| VLT‑5 | Single admin key (no multisig) | Medium | Open — Tranche 3 (2-of-3 multisig + timelock) |
| VLT‑6 | CEI ordering in `draw` | Low | Mitigated (trusted SAC) — reorder for defense in depth |
| REG‑1 | Permissionless `slash` | Info | By design & safe (timeout + active-draw gated) |
| ORA‑1 | Oracle manipulation | Med/High | Tranche 3 circuit breaker |
| DEX‑1 | Swap slippage / sandwich | Low/Med | Mitigated (oracle-anchored min-out) |
| INT‑1 | Integer overflow in share math | Low | Mitigated (i128 + pool-favoring floors) |

**Remediation plan:** VLT‑1 and VLT‑2 are logic fixes we will land (with regression tests) **before** the mainnet deployment, independent of audit timing. VLT‑3/4/5 fold into the Tranche 3 "Security Hardening" deliverable. The audit will validate the fixes.
