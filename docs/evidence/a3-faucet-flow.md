# A3 EVIDENCE — trustline + faucet + deposit, live on testnet (2026-08-03)

## What our USDC actually is (verified, not assumed)

On-chain reads against the vault's settlement token
`CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA`:

- `symbol()` = `"USDC"`, `decimals()` = 7
- `name()` = `"USDC:GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"`

The `CODE:ISSUER` name is definitive: this is a **Stellar Asset Contract
wrapping the classic asset** `USDC:GBBD47IF…` — Circle's testnet USDC. So:

1. **Trustlines apply** — the SAC enforces classic semantics; a wallet cannot
   receive USDC without a trustline. The trustline UI path is required.
2. **We cannot mint it** — the issuer is Circle (upstream source:
   faucet.circle.com, browser-only). The "faucet" is therefore an **admin
   treasury payment**, not a mint. Treasury (admin
   `GATK27P6…`) held exactly **90.0000000 USDC** at session start (live
   Horizon read), so the spec's 1000-USDC claim is impossible to fund:
   `FAUCET_AMOUNT` stays 1000 USDC by default per spec, and the deployed
   testnet instance is configured to **25 USDC per claim** (≈3 claims of
   headroom after reserving USDC for the A4 sandbox pool). The UI reads the
   amount and live treasury balance from `/api/faucet/info`, so the button
   never overstates what the faucet can pay.
3. The legacy in-repo `contracts/mock-token` (pure Soroban token with admin
   `mint`) and `USDC:GD5FMUS…` (our old mock issuer, wallets.md) belong to
   the DEPRECATED pre-tranche-3 deployments and are not part of this flow.

## Live end-to-end run (fresh wallet, every step on-chain)

Fresh wallet: `GCDCPWBS5PN2QS344JZI2LSBXAYW572IZRLNBRHR5Y4LXLTKZWFH3363`
Keeper faucet config: `FAUCET_AMOUNT=250000000` (25 USDC),
`FAUCET_COOLDOWN_SECS=3600`, treasury = admin `GATK27P6…`.

| # | Step | Result | Tx hash / proof |
|---|------|--------|-----------------|
| 1 | State detect: account | Horizon `GET /accounts/…` → **404** → `no_account` | (status code) |
| 2 | Friendbot fund | account created, 10,000 XLM | `5951ef9cc2b968b44c8b3debcfd300e0c0555c7c6ab1ac450402780a27a481d4` |
| 3 | State detect: trustline | balances = XLM only → `no_trustline` | (Horizon read) |
| 4 | Add trustline (changeTrust `USDC:GBBD47IF…`, signed by the fresh wallet — same op the UI builds) | USDC balance line appears at `0.0000000` | `a6061dc38c0014c228a3962c6c7e45d666f9f8fd30b8c19361eb0d81511eb652` |
| 5 | `GET /api/faucet/info` | `{enabled:true, amount:"250000000", cooldownSecs:3600, asset:{USDC, GBBD47IF…, CBIELTK6…}, treasuryBalance:"900000000"}` — asset derived from on-chain `name()` | (API response) |
| 6 | `POST /api/faucet` claim | 25 USDC paid; wallet balance `25.0000000` | `d88ac3e4802a9034599abbe359e21bb6c327078aba29c9a001e99fb126e1a32d` |
| 7 | Second claim (rate limit) | **429** `{"error":"cooldown","retryAfterSecs":3596}` | (API response) |
| 8 | Claim for a wallet WITHOUT trustline | **409** `{"error":"no_trustline"}` — checked before any payment | (API response) |
| 9 | Vault deposit 10 USDC (signed by fresh wallet) | vault `deposit` event `[100000000, 100000000]`; `balance(user)` → `["100000000","100000000"]` (10.0 shares); wallet USDC `15.0000000` | `e9c3a17fea0a4737934c0e85b9750beb6ea6c219956927802d5db1676d6a9bbd` |

All hashes on https://stellar.expert/explorer/testnet/tx/<hash>.

## Frontend

The vault page implements the same flow interactively (WALLET FUNDING
panel): trustline state detection via Horizon (`no_account` → Friendbot
button, `no_trustline` → "Add USDC Trustline" building the changeTrust op
signed by the connected wallet, `ok` → live balance + "Get N USDC" faucet
button with N from `/api/faucet/info`), and the deposit submit is disabled
at zero USDC balance with a pointer to the faucet. The USDC balance
read now matches code AND issuer (previously any-issuer "USDC" matched).
`npm run build` green. The CLI steps above exercise the identical
operations (changeTrust op, faucet POST, vault deposit invoke) against the
same contracts the UI targets.

Result: **PASS** — no-trustline state detected → trustline added → faucet
paid 25 USDC → balance shown → deposit works; faucet is cooldown- and
trustline-guarded and cannot pay more than the treasury holds.
