# Nectar Network — Security Documentation

Prepared for the SCF Soroban Security Audit Bank and the Tranche 3 mainnet
launch. **Current as of tag `audit-freeze-v1`** (2026-08-16); the contracts are
frozen — no edits past the tag except audit remediation.

| Document | What it covers |
|---|---|
| [AUDIT-SCOPE.md](./AUDIT-SCOPE.md) | In-scope contracts (registry + vault) at the frozen tag, out-of-scope components, trust assumptions, and the explicit scope decisions (self-declared draw asset, ORA-1 deferral, rate-limit design). |
| [THREAT-MODEL.md](./THREAT-MODEL.md) | STRIDE threat model at the frozen tag: system overview, trust boundaries, per-component STRIDE (incl. the T3 surfaces: `add_profit`, pause-flag abuse, rate-limit sybil, self-declared asset, multisig key loss), threat table with frozen-source cites, residual risks. |
| [DATAFLOW.md](./DATAFLOW.md) | Zone map + 8 annotated flows: deposit, withdraw (cooldown + rate limit), the live-proven liquidation cycle, bad-debt float path, `add_profit`, registration/slashing, pause propagation, testnet faucet. |
| [SCOUT-REPORT.md](./SCOUT-REPORT.md) | Security tooling results (cargo-scout-audit at the frozen tag, tests, property invariants, clippy) and the finding-by-finding disposition + remediation plan. |

Companion documents outside this directory:
[`docs/audit/FREEZE-NOTE.md`](../audit/FREEZE-NOTE.md) (the freeze contract:
scope, wasm hashes, suite status, LOC, known properties),
[`docs/audit/INTAKE-DRAFT.md`](../audit/INTAKE-DRAFT.md) (Audit Bank intake
answers), [`docs/audit/READINESS-CHECK.md`](../audit/READINESS-CHECK.md)
(checklist verification), and
[`docs/CORRECTION-REPORT.md`](../CORRECTION-REPORT.md) (the adversarial
self-verification arc: 17 corrected claims + remaining limitations).

## Scope

Two Soroban contracts — `KeeperRegistry` and `NectarVault`, **1,071 functional
LOC** (tokei, excl. tests) at `audit-freeze-v1` — plus their interaction with
the off-chain Go keeper and external protocols (Blend, Soroswap, oracle).

## Security posture at the freeze

- Every previously-identified High/Medium contract finding is **fixed at the
  tag** (VLT-1..4, VLT-6, NEW-cap/reconcile/drain/init, SCOUT-underflow, and the
  9 confirmed findings of the 21-agent pre-freeze review); VLT-5 is closed by a
  live-verified 2-of-3 admin multisig.
- Remaining open items are tracked honestly: ORA-1 (oracle circuit breaker,
  Tranche-3 module driving the frozen pause flags) and the accepted/documented
  properties listed in AUDIT-SCOPE.md and THREAT-MODEL.md §6.
- 113 tests green at the tag (96 audit-scope, incl. 4×2000-case property
  invariants); `go test -race` clean; suite status re-verified 2026-08-16.
