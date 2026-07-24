# Nectar Network — Security Documentation

Prepared for the SCF Audit Bank and the Tranche 3 mainnet launch.

| Document | What it covers |
|---|---|
| [AUDIT-SCOPE.md](./AUDIT-SCOPE.md) | What the audit covers: in-scope contracts (registry + vault), out-of-scope components, trust assumptions, the frozen tag, and the explicit VLT‑5 / ORA‑1 scope decisions. |
| [THREAT-MODEL.md](./THREAT-MODEL.md) | STRIDE threat model grounded in the contract code: system overview, trust boundaries, per-component STRIDE analysis, findings table, and residual risks. |
| [DATAFLOW.md](./DATAFLOW.md) | Five annotated data-flow diagrams (deposit, liquidation, withdrawal, registration, slashing) with trust boundaries and data entities. |
| [SCOUT-REPORT.md](./SCOUT-REPORT.md) | Security tooling + static analysis results (tests, property invariants, clippy, Scout) and the finding-by-finding remediation plan. |

## Scope
Two Soroban contracts — `KeeperRegistry` and `NectarVault` (~933 functional LOC, excl. tests) —
plus their interaction with the off-chain Go keeper and external protocols (Blend, Soroswap, Reflector).

## Key findings (see THREAT-MODEL.md for detail)
- **VLT‑1 (High):** share-inflation / first-depositor vector — remediate before mainnet.
- **VLT‑2 (Medium):** `return_proceeds` not gated to registered keepers.
- **VLT‑3/4/5 (Medium):** implicit keeper verification, no vault pause, single admin key → Tranche 3 hardening.

All are open items for the audit engagement; VLT‑1/VLT‑2 will be fixed (with regression tests) before mainnet.
