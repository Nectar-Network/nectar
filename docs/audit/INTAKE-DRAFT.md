# Audit Bank Intake — Draft Answers (audit-freeze-v1)

**Prepared:** 2026-08-16 (Session G). Copy-paste answers for the Soroban Security
Audit Bank application form (the form itself arrives by email from the Stellar
Community Fund — [handbook](https://stellar.gitbook.io/scf-handbook/supporting-programs/audit-bank)).
Items marked `⟨USER⟩` need the submitter's own details. Every number below is
sourced from repo evidence at tag `audit-freeze-v1`; nothing is projected.

---

## Project selector (SCF project name)

**Nectar Network** — SCF #42 Build Award ($75K), approved March 2026.

## Contact details

| Field | Value |
|---|---|
| Primary email | `kunaldrall29@gmail.com` ⟨USER: confirm⟩ |
| Additional emails | ⟨USER: Daksh, Priya⟩ |
| Telegram handle(s) | ⟨USER⟩ |

## Type of project

**Financial Protocol** (pooled liquidation vault — priority category under the
[official rules](https://stellar.gitbook.io/scf-handbook/supporting-programs/audit-bank/official-rules)).

## Project description (2–3 sentences)

> Nectar Network is a pooled liquidation protocol for Soroban DeFi on Stellar:
> depositors pool Circle USDC in a vault, and bonded keeper operators draw that
> capital to fill Blend Protocol liquidation auctions, with measured profits
> returned to the pool as depositor yield. Two contracts are in scope — the
> NectarVault (share accounting with inflation-resistant virtual-offset math,
> capital draws, deposit caps, withdrawal cooldown + rate limiting, emergency
> pause and global/per-asset liquidation circuit-breaker flags) and the
> KeeperRegistry (USDC-staked operator registration, performance tracking, and
> permissionless timeout-gated slashing with atomic vault reconciliation).

## Traction (testnet reality — all evidence-backed, nothing projected)

- **Full liquidation cycle proven live on testnet with real value:** auction
  create → capital draw → ONE atomic fill+repay+withdraw → DEX swap → proceeds
  returned; vault share price rose 1.0000000 → 1.0432672 (+2.5960334 USDC profit
  on a 60 USDC pool from one liquidation), asserted programmatically, every step
  tx-hashed (`docs/evidence/b-full-cycle.md`, 2026-08-09).
- **Autonomous discovery proven:** a borrower generated at runtime — present in no
  config or cache — was discovered from chain events, health-checked, and
  liquidated end-to-end with no restart (`docs/evidence/d-discovery.md`, 2026-08-14).
- **Bad-debt auction fill proven live** (float-funded, atomic fill+repay;
  `docs/evidence/c-bad-debt.md`); the mainnet LP-unwind venue verified by live
  simulation (`docs/evidence/c-lp-unwind.md`).
- **Operators:** 1 registered keeper (keeper-alpha, 100 USDC stake) — the
  operator that produced the live full-cycle evidence above (2026-08-09,
  pre-freeze build); 2 more keeper identities queued pending testnet-USDC faucet
  funds. Note: the currently-deployed testnet pair predates the frozen
  interface, so the daemon at HEAD resumes operating against it after the
  Session H redeploy (FREEZE-NOTE "known properties"). (Honest count — this is
  a pre-mainnet pilot, not public traction.)
- **Test suite at the frozen tag:** 113 workspace tests passing — 96 audit-scope
  (incl. 4 property invariants × 2000 cases); keeper `go test -race` clean across
  9 packages; 2-of-3 admin multisig enforcement verified live on-chain
  (`docs/evidence/f5-multisig.md`).

## URLs

| Field | Value |
|---|---|
| App | https://testnet.nectar.monster (testnet — serves the deployment the traction claims describe); https://nectarnetwork.fun is the mainnet domain, post-audit (docs/NETWORKS.md) |
| Repository | https://github.com/Nectar-Network/nectar (canonical — matches the SCF award record, docs-site and README; `main` + `tranche-3` + tag `audit-freeze-v1` pushed 2026-08-16) |
| Docs | https://docs.nectar.monster |
| Keeper SDK | https://github.com/Nectar-Network/keeper-sdk |

## Lines of code (audit scope)

**1,071 functional LOC** (tokei 14.0.0, code lines excl. comments/blanks):
NectarVault 672, KeeperRegistry 399. Raw lines incl. comments: 1,438.
Frozen at tag `audit-freeze-v1` (commit `dbf0e5cd0a0c…`); optimized wasm hashes
recorded in [`FREEZE-NOTE.md`](./FREEZE-NOTE.md).

## Anticipated audit readiness date

**2026-08-16** — the code is frozen and all readiness artifacts are complete as
of the freeze date (see [`READINESS-CHECK.md`](./READINESS-CHECK.md)); SDF's own
guidance favors early submission, and the readiness review takes up to 4 weeks
regardless.

## Tests written and executed?

**Yes.** 113 passing at the tag (96 in audit scope: vault 69 incl. 4 property
suites × 2000 cases, registry 27); cross-contract integration tests run the real
draw→return and slash→reconcile cycles against the actual registry contract;
keeper `go test -race ./...` all ok; frontend build clean. Re-verified green
2026-08-16.

## STRIDE threat model attached?

**Yes** — [`docs/security/THREAT-MODEL.md`](../security/THREAT-MODEL.md), built on
the [Stellar threat-modeling guidance](https://developers.stellar.org/docs/build/security-docs/threat-modeling)
(4-question structure, STRIDE per component, threat table with frozen-source
cites, residual risks). Dataflow diagrams:
[`docs/security/DATAFLOW.md`](../security/DATAFLOW.md).

## Security tooling used

- **cargo-scout-audit 0.3.16** (CoinFabrik Scout — the current crates.io release,
  published 2026-02-13), run against both frozen contracts; full finding-by-finding
  triage + remediation plan in
  [`docs/security/SCOUT-REPORT.md`](../security/SCOUT-REPORT.md).
- cargo test (incl. proptest invariants), cargo clippy, `go test -race`,
  cargo-llvm-cov (CI coverage job).

## Audit firm preference

**Flexible** — we understand firm selection is at SDF's sole discretion. If a
preference is considered, ours is informed by published Soroban/Stellar audit
history (verified 2026-08-16 against [stellar.org/audit-bank/projects](https://stellar.org/audit-bank/projects)
and the firms' public reports):

1. **Code4rena or Certora** — they audited **Blend v2 itself**
   ([Feb–Mar 2025 audit + formal verification](https://code4rena.com/audits/2025-02-blend-v2-audit-certora-formal-verification)).
   Nectar's entire capital cycle runs against Blend v2 auctions, so reviewers who
   know Blend's auction/backstop mechanics can audit our integration assumptions,
   not just our code. Certora additionally audited Spectra (Soroban, May 2026).
2. **Halborn** (4 Audit Bank engagements incl. Peridot lending) or
   **Runtime Verification** (OctoLend, EquitX) — the most repeated Soroban DeFi
   practice among the approved firms.

## Additional notes (previous security practices)

> Attach/link: [`docs/CORRECTION-REPORT.md`](../CORRECTION-REPORT.md).
>
> Before requesting this audit we ran an adversarial self-verification arc
> against our own claims: every protocol-mechanic assumption was re-derived from
> pinned Blend source and live chain state (93/93 claims verified), and the
> resulting **17-item corrected-claims ledger** — including what we got wrong and
> the live-evidence fixes — is published in CORRECTION-REPORT.md. The freeze
> itself was preceded by a 21-agent adversarial review (16 findings: 9 confirmed,
> all fixed pre-tag; 7 refuted). Known limitations are documented as limitations,
> not omitted: the same report lists the residual risks we expect auditors to
> probe. Admin is a 2-of-3 multisig whose enforcement we verified live on-chain
> before freezing (`docs/evidence/f5-multisig.md`).
