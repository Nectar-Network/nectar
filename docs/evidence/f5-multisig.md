# F5 — Classic 2-of-3 multisig admin, verified live on testnet (2026-08-16)

Session F, Task F5. Verifies — with transactions, not assumptions — that a
Soroban contract whose admin is a classic Stellar G-account enforces that
account's multisig thresholds on admin calls. Nothing here was asserted from
memory; every claim below has a tx hash or a captured error.

## Setup

| Item | Value |
|---|---|
| Multisig admin account M | `GA2EMXVAJJCB7LFEXFD2X367V3SMRHEVH2RRXNDX5ZNCK7Q5Q7CO66V7` (friendbot-funded) |
| Signer S2 | `GBV4GRI3HH4PLCRP2ADMNGA3552RQIZE4I76FN34TJBTXS7NVYDINCH7` (weight 1) |
| Signer S3 | `GB3BEBPLK4LJBEQYJMHLTKQ3SAWAROPS4ZZVW6HJQMFK3KFY62PL53H3` (weight 1) |
| Throwaway vault (Session F wasm) | `CC7KUIZ7RN7H4JDJP4KS5RC6DB5RCX5MPB3665GWUSXF6JOUIMGYT2NL` |
| Vault wasm hash | `a6914070fa13fa73f7f57cdd4d1056839938e407e25333c9dfd426aa7906f39f` (upload tx `e32c3099…`, deploy tx `851c6312…`) — the Session F contract build, so this also proves the new wasm deploys and constructs live |

The throwaway vault's `__constructor` admin arg was set to M. Deployed BEFORE
tightening thresholds (a fresh account has master weight 1 / thresholds 0, so
the single-signature deploy is valid).

### set_options transactions (in order — thresholds LAST, or the account
### locks itself out before the extra signers exist)

| Step | Tx hash |
|---|---|
| Add signer S2, weight 1 | `5a2762e5b96f150b76df144b25064c315497befe911e4dfa89d483051f2bbba5` |
| Add signer S3, weight 1 | `26c6b952f994ed6d7c6d2f601656289c173a18a1aff036424fc2e4d64fce6409` |
| master_weight=1, low=1, med=2, high=2 | `30fe077c397e9f13c07df9f1c902593896eba75b51deec2aff14e3bc2c3b4cde` |

Horizon confirms the final account state: 3 signers of weight 1,
`thresholds: {low: 1, med: 2, high: 2}`.

## The proof

All three probes call `set_global_pause` (admin-gated, `require_admin` →
`admin.require_auth()`) on the throwaway vault, with M as the **transaction
source**.

| Probe | Signatures | Result |
|---|---|---|
| A: pause, 1 sig (master only) | 1 | **REJECTED at submission: `TxFailed([OpBadAuth])`** — never landed, no state change (`is_global_liq_paused` still `false`). Simulation had passed; enforcement is the classic operation-auth check. |
| B: pause, 2 sigs (master + S2) | 2 | **SUCCESS** — tx `ac1da4ab72a3b803e5fce10e015f437ae4be2aa183d45e738ced4748036f0e28`; `is_global_liq_paused` → `true` |
| C: unpause, 2 sigs (S2 + S3 — master key NOT used) | 2 | **SUCCESS** — tx `f057a494bce99b5f0982f918b5bd6572d265dcdbdc3752112aa61d5d8a7621b6`; `is_global_liq_paused` → `false` |

Probe C matters: ANY two of the three signers authorize an admin op; the
master key is not privileged beyond its weight.

## Verified mechanism (scoped to what was tested)

- With the admin G-account as **tx source**, `require_auth(admin)` resolves
  via source-account credentials, and the classic transaction machinery
  enforces the account's **medium** threshold on the InvokeHostFunction
  operation. A signature set below the threshold fails classic op auth
  (`OpBadAuth`) **before any contract logic runs**.
- Threshold semantics observed: sum of signer weights (each 1) must reach
  med=2. 1 < 2 rejected; master+S2 = 2 accepted; S2+S3 = 2 accepted.
- NOT tested here: the admin authorizing as a NON-source auth entry
  (address credentials with per-entry signatures). Mainnet ops will use the
  source-account flow below, so the verified path is the operative one.

## Mainnet admin-op signing procedure (the team runbook)

1. Build unsigned: `stellar contract invoke --id <CONTRACT> --source-account <ADMIN_G> --network <net> --build-only -- <fn> --admin <ADMIN_G> <args…> > op.xdr`
2. Simulate/assemble (fills Soroban resources — **required**, a bare
   build-only envelope submits as `TxMalformed`): `stellar tx simulate op.xdr --source-account <ADMIN_G> --network <net> > op-assembled.xdr`
3. Signer 1: `stellar tx sign op-assembled.xdr --sign-with-key <key1> --network <net> > op-1.xdr`
4. Signer 2 (independently, air-gapped if desired — the XDR file is the
   handoff): `stellar tx sign op-1.xdr --sign-with-key <key2> --network <net> > op-2.xdr`
5. Submit: `stellar tx send op-2.xdr --network <net>`

CLI used: stellar 27.1.0. The throwaway vault and the three test keys are
disposable; nothing above touches the canonical testnet deployment.
