# A4 EVIDENCE — Nectar Sandbox: self-owned Blend V2 testnet environment (2026-08-03)

Deployed via `scripts/nectar-sandbox/` (blend-utils lib @ `b05242d`, the same
code that deployed canonical TestnetV2 — FACTS.md Gate 0.6). 49 transactions
total for the environment deploy; every hash below is on
https://stellar.expert/explorer/testnet/tx/<hash>.

## Why a full stack (verified constraint)

`execute_set_pool_status` requires the backstop threshold
(`calc_pool_backstop_threshold >= SCALAR_7`, i.e. BLND^4×USDC ≥ 100k^5) for
status 0 — **no admin bypass** (blend-contracts-v2 `pool/src/pool/status.rs:81-90`
@ ba22b48). The canonical backstop's LP is comet BLND:USDC and testnet BLND has
no faucet (FACTS.md), so a canonical-factory pool could never be activated by
us. The sandbox therefore deploys its own emitter/backstop/factory/comet with
admin-issued BLND + USDC for backstop economics, while the POOL's reserves are
the REAL assets: Circle testnet USDC (the vault's settlement asset) + native
XLM. The oracle is blend-utils' `oraclemock` wasm (same wasm as canonical
testnet's oracle) with admin-settable prices — the insolvency lever.

## Deployed addresses (live-verified via get_config / get_reserve_list)

| Contract | Address |
|---|---|
| **Pool "Nectar Sandbox"** | `CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V` |
| Mock oracle (ours) | `CAPLUDWOVS6IEXZ35MLNDLXDYEDZMGFCK6QU4JBUAYANITAV3ZH2I27N` |
| Backstop | `CCT4FMLHPJVYBC6SCAOFIRSYU74ZBU36ADREQUQEFCN5C5MRG26S6PTH` |
| Emitter | `CAHQB47PPSKPIJ7MRQI72LAONXPELHNLEBZUMOKX7AES4LVSRVABTUZ2` |
| Pool factory | `CDYY5FJ6ZGO4AEAN6S6NGASAB6UMKDFHSRIZI6CHKRFV7LY26SU6V7BN` |
| Comet (BLND:USDC 80/20 LP) | `CAMYNQY4L6BM45HJ6KJMNAX7SK474QHAFJWQBBAKCGV7XP7VJGZ7RUFD` |
| Sandbox BLND (`BLND:GATK27P6…`) | `CB627JMA3N4NZWQCUOKRPZWDHYS656DOMQEUUMFQRWANP7H363UXK3PW` |
| Backstop USDC (`USDC:GATK27P6…`, testnet-only) | `CAKGVZ343VNJGVNL77S5FDBBFOYZLBA4FFORNW5CXQA3YAKCH2NBVGZ6` |
| Reserve 0: Circle USDC (real vault asset) | `CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA` |
| Reserve 1: native XLM SAC | `CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC` |

Live `get_config` after deploy: `{bstop_rate:1000000, max_positions:4,
min_collateral:0, oracle:CAPLUDWO…, status:0}` — **status 0 (Active) reached
through a genuinely-funded backstop** (50,001 comet LP deposited, threshold
met), not an admin override, because none exists.

Reserve risk params (both 7-dec): Circle-USDC idx0 c=0.95 l=0.95 util=0.80
max_util=0.99 r_base=0.001 r1=0.004 r2=0.02 r3=0.05 reactivity=100;
XLM idx1 c=0.75 l=0.75 util=0.60 max_util=0.90 r_base=0.0005 r1=0.005 r2=0.05
r3=0.15 reactivity=500. Oracle: decimals 7, resolution 300, assets
[Circle-USDC, XLM], initial prices [$1.00, $0.42].

## Deploy transactions (grouped; full ordered log: session capture of ./run.sh 01)

- Admin-issued SACs (BLND, USDC): `3b3cbd7c…`, `d7d97812…`, `e5bb74ca…`, `81a323e4…`
- Comet factory + comet: `313c952f…`, `a2600f23…`, `47e1f08a…`, `bf8b3245…`, `c6689a56…`, `009df4e9…`, `ca2eb682…`, `b7773ded…`, `74dc765e…`
- Mock oracle (install/deploy/setData/setPriceStable): `64ea4304…`, `966a774c…`, `2455efec…`, `f7f3ebc2…`, `d76c52dc…`, `a2b90601…`
- Emitter + backstop + pool factory: `83df4f57…`, `82eed8d5…`, `79107fcb…`, `da41bdde…`, `1ba9d927…`, `85e8e90a…`, `fef3f485…`, `54f0cec5…`, `d2f7ba50…`, `cf3b18e1…`, `08210d52…`, `3071d8c1…`, `10fdbd09…`, `19565799…`, `c4b78b5a…`
- Pool "Nectar Sandbox" deploy: `0bacf5bf…`, `e124838e…`
- Reserves (queue+set ×2): `cb7ca0ef…`, `8b6a26e2…`, `c4f4ea9a…`, `1ee890fb…`
- Backstop seed (whale trustlines, issuer mints 500,100 BLND + 12,501 USDC,
  comet join 50,001 LP, backstop deposit, setStatus(0), addReward):
  `1f980ae9…`, `aaa4bf64…`, `18fb067f…`, `2888ba70…`, `710e3ea9…`,
  `c0f112d4…`, `23580b1c…`, `c39bb92f…`
- BLND admin → emitter: `4458605e…`

## Borrower + forced insolvency (every step live)

Borrower: `GCCTPHRTKMZMQEOH26GZGD3VQUYALUZZDWRP37ZC3QBDOXQHWQ2MNOIJ`

| Step | Tx hash |
|---|---|
| Borrower Circle-USDC trustline | `1bfc71bcff7791eb28d793482600169fb6f0dbcec2f88a0026146affd1ef1e99` |
| Admin supplies 40 Circle-USDC liquidity | `b96250dedd2767517e239307d8b09951100d3979936a0624065cd64d7ab65b85` |
| Borrower: SupplyCollateral 100 XLM + Borrow 20 Circle-USDC (one submit) | `0adf363404e271317e074df1edb2363d2580f27fcf18cfbe9d8caace89f3fc59` |
| **Price crash**: oracle XLM $0.42 → $0.15 | `f33b1835d55fe07db85a24e19b381d7bad26c3bf3956dba135845db452b17478` |
| Price restore: XLM back to $0.42 (sandbox healthy at rest) | `a568a996fd9847a91978fdc134a3a507234949f6836ec48fed585535a82609a2` |

On-chain position (live `get_positions(borrower)`):
`{"collateral":{"1":"1000000000"},"liabilities":{"0":"200000000"},"supply":{}}`
= 100 XLM collateral (idx 1), 20 USDC debt (idx 0).

## Keeper detection (the point of the exercise)

Keeper run with `BLEND_POOLS=CCEBVDYM…:monitor:CAQCFVLO…,CBUBTHATT…:monitor`
(monitor mode: detection only, zero capital movement) while XLM=$0.15:

```
01:49:17.937 [] INFO pool scan pool=CBUB..CK3V mode=monitor status=0 reserves=2 oracle_decimals=7 positions=2
01:49:17.937 [] INFO reserve price pool=CBUB..CK3V asset=CBIE..DAMA usd=1.0000000
01:49:17.937 [] INFO reserve price pool=CBUB..CK3V asset=CDLZ..CYSC usd=0.1500000
01:49:17.937 [] INFO position pool=CBUB..CK3V addr=GATK..3P7W hf=+Inf
01:49:17.937 [] INFO position pool=CBUB..CK3V addr=GCCT..NOIJ hf=0.5344
```

`hf=0.5344` matches the hand computation from on-chain params exactly:
(100 × 0.15 × 0.75) / (20 × 1.0 ÷ 0.95) = 11.25 / 21.0526 = 0.5344. The
admin's supply-only position correctly reports +Inf.

**On-demand lever**: `scripts/nectar-sandbox/run.sh 03 0.15` makes the
position liquidatable at any time; `… 03 0.42` restores it. Current resting
state: XLM=$0.42, borrower HF ≈ 1.50, keeper sees both pools.

## Keeper bugs found and fixed by this live exercise

1. **Position discovery read the wrong topic**: Blend puts the user at
   topic[2] for supply/borrow/repay events (`["supply", asset, from]` —
   events.rs @ ba22b48); the keeper only parsed topic[1] and so could never
   discover plain borrowers. Both topic positions are now scanned.
2. **getEvents pagination**: Soroban RPC scans a bounded ledger segment per
   request and returns an empty page + `cursor` when events lie in a later
   segment (observed live: latest−17000 start → empty first page while 8
   events existed). The keeper treated one page as the full answer; it now
   follows the cursor until the RPC's latest ledger. After the fix, TestnetV2
   discovery went from 1 to 11 live positions in the same window.

## Post-review re-verification (after the 15 review fixes)

The fixes touched HF math, position discovery, and task emission, so the
detection was re-run end-to-end on the same live pools (price crashed to
$0.15 again, then restored — restore tx `f1f65047…`):

```
10:28:46 INFO pool scan pool=CCEB..4HGF mode=monitor status=0 reserves=4 oracle_decimals=7 positions=7
10:28:46 WARN pool scan note pool=CCEB..4HGF note=cross-USDC pool: monitored only, execution disabled
         (FACTS.md 'USDC asset bridging'); 1-USDC route quote: conversion route off parity:
         need 10661879 of vault USDC for 10000000 of pool USDC (par bound 10100000)
10:28:51 INFO pool scan pool=CBUB..CK3V mode=monitor status=0 reserves=2 oracle_decimals=7 positions=2
10:28:51 INFO position pool=CBUB..CK3V addr=GATK..3P7W hf=+Inf
10:28:51 INFO position pool=CBUB..CK3V addr=GCCT..NOIJ hf=0.5344
```

Same HF (0.5344) for the sandbox borrower, and the cross-USDC pool now states
its monitor-only status with a live measurement of why the route is unusable
(1.066 vault USDC required per 1.0 pool USDC, against a 1.01 par bound). The
TestnetV2 position count differs from the earlier run (7 vs 11) because
unpriceable positions are now excluded from the HF list instead of being
assigned an invented health factor.

Result: **PASS** — Nectar Sandbox deployed with a real backstop-funded Active
pool, real Circle-USDC/XLM reserves, our own oracle; a genuine borrower goes
underwater on demand and the multi-pool keeper detects it (HF < 1) alongside
the official TestnetV2 pool.
