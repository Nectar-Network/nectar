# Nectar Network — Audit Scope

Prepared for the SCF Audit Bank engagement. Pairs with
[THREAT-MODEL.md](./THREAT-MODEL.md), [DATAFLOW.md](./DATAFLOW.md), and
[SCOUT-REPORT.md](./SCOUT-REPORT.md).

## In scope — the two on-chain Soroban contracts

| Contract | Path | LOC (excl. tests) |
|---|---|---|
| **KeeperRegistry** | `contracts/keeper-registry/` | 420 |
| **NectarVault** | `contracts/nectar-vault/` | 513 |

Total auditable functional LOC: **~933** (Rust, soroban-sdk 22). These hold all
value and all privileged logic: USDC deposits/shares, keeper stake, capital
draw/return, slashing, and the cross-contract link between them.

The audited commit is tagged **`v0.3.0-audit`** on `main` (frozen). The
security-hardening remediations (VLT‑1..6, NEW‑cap/reconcile/drain, atomic
`__constructor`) are all included in this tag.

## Out of scope

| Component | Why excluded |
|---|---|
| `contracts/liquidation-lab/` | Test harness — an ABI-compatible Blend-auction simulator used only in E2E tests; never deployed as production. |
| `contracts/mock-token/` | Test-only mock SAC. Production uses **Circle USDC** (a Stellar-native SAC, not our code). |
| `keeper/` (Go daemon) | Off-chain; holds no user funds. It cannot bypass any on-chain check — the contracts are the source of truth. A daemon compromise is bounded to that operator's stake. |
| `frontend/` | Read + Freighter-signed transaction builder; holds no keys and no privileged authority. |
| Blend, Soroswap, Reflector | External protocols in their own trust domains. The keeper mediates; the vault never trusts their output directly. |

## Trust assumptions
- **USDC SAC is non-reentrant / well-behaved** (Circle USDC on mainnet, its testnet counterpart on testnet). The vault accounts value via internal state (`total_usdc`), not a token-balance read, so it does not depend on the token for accounting integrity — but the `draw` transfer assumes a non-reentrant token (VLT‑6, documented).
- **Admin key is trusted** for configuration and pause. See VLT‑5 below for the mainnet hardening.

## Two open findings — explicit scope decisions

Both are documented in the threat model; **neither is a defect in the in-scope
contract code**, and neither blocks auditing the current contracts.

### VLT‑5 — admin key is a single signer (mitigated at deployment, not in code)
Every privileged contract action already gates on `admin.require_auth()` and an
identity check. Multisig is therefore a **deployment/account-level** control, not
a contract change: on mainnet the admin will be a **Stellar multisig account
(e.g. 2-of-3 signers with a high threshold)**, which satisfies `require_auth`
without touching the contracts. **Decision: out of contract scope; addressed by
the mainnet deploy configuration (Tranche 3).** Auditors are asked to confirm the
`require_auth`/identity gating is correct and complete; the signer policy is set
at the account level.

### ORA‑1 — oracle circuit breaker (a Tranche‑3 module, not yet built)
The cross-referencing oracle circuit breaker (auto-pause on Reflector deviation)
is a **Tranche‑3 deliverable** — new on-chain logic that does not exist in the
current contracts, so it is **out of the current audit scope**. Interim
mitigation is the keeper's off-chain oracle-anchored slippage floor + on-chain
`amount_out_min` on swaps. **Decision: deferred to Tranche 3; will be audited when
built.**

## Assurance evidence in this tag
- **73 tests** across the two in-scope contracts (nectar-vault 47, keeper-registry 26),
  including a real cross-contract draw→return cycle, slash→`reconcile_default`, and a
  post-loss withdraw-underflow regression; plus 17 in the test-harness crates — all
  passing under `cargo test --locked`.
- **4 property-based invariants** (`contracts/nectar-vault/src/prop_test.rs`,
  2000 cases each, counted in the 47) proving: deposit→withdraw never profits
  (solvency), the inverse, share monotonicity, and the first-depositor inflation
  attack is unprofitable.
- **cargo-scout-audit** run on a clean CI runner (artifact `scout-report`); every
  finding class triaged and adversarially re-checked in
  [SCOUT-REPORT.md](./SCOUT-REPORT.md) — all false-positive or accepted, with one
  latent accounting edge (`SCOUT-total_usdc-underflow`) hardened via a `withdraw`
  clamp + regression test.
- **`go test -race`** clean on the keeper; a **contract coverage** summary runs in CI.
- Both contracts build to deployable `wasm32v1-none` and are **live on testnet**.
