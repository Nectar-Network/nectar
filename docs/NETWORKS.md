# Nectar Network — Networks & Circle USDC

Nectar runs the **same codebase** on two networks. The frontend is network-aware
(`NEXT_PUBLIC_NETWORK` = `testnet` | `mainnet` drives RPC / Horizon / passphrase /
wallet-kit in `frontend/lib/stellar.ts`); the contracts take `usdc_token` at
construction, so each network is a separate deployment.

| | Testnet | Mainnet |
|---|---|---|
| Site | `testnet.nectar.monster` | `nectarnetwork.fun` |
| Docs | `docs.nectar.monster` | `docs.nectarnetwork.fun` |
| `NEXT_PUBLIC_NETWORK` | `testnet` | `mainnet` |
| Passphrase | `Test SDF Network ; September 2015` | `Public Global Stellar Network ; September 2015` |
| Soroban RPC | `https://soroban-testnet.stellar.org` | *(set `NEXT_PUBLIC_SOROBAN_RPC` — a provider, e.g. Validation Cloud / Blockdaemon)* |
| Horizon | `https://horizon-testnet.stellar.org` | `https://horizon.stellar.org` |
| Explorer | `stellar.expert/explorer/testnet` | `stellar.expert/explorer/public` |

## Circle USDC (the production asset)

Nectar uses **Circle USDC** (not the mock SAC) on both networks. The Stellar
Asset Contract (SAC) IDs — computed from Circle's issuers with
`stellar contract id asset --asset USDC:<issuer>`:

| Network | Circle USDC issuer | **USDC SAC (contract id)** |
|---|---|---|
| Testnet | `GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5` | `CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA` |
| Mainnet | `GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN` | `CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75` |

> **Testnet Circle USDC** is obtained from Circle's testnet faucet
> (faucet.circle.com) — not admin-minted. **Mainnet USDC is real value.**

## Deploying against Circle USDC

`USDC_CONTRACT` in `scripts/deploy.sh` is the only thing that changes — set it to
the SAC above for the target network, then deploy (the contracts now initialize
atomically via `__constructor`, then link with `set_vault`):

```bash
# testnet, Circle USDC
USDC_CONTRACT=CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA ./scripts/deploy.sh
# mainnet, Circle USDC (post-audit)
USDC_CONTRACT=CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75 ./scripts/deploy.sh
```

## Frontend env per domain (Vercel)

Set on each deployment; the network vars pick the chain, the contract vars pick
the deployment on it.

**testnet.nectar.monster**
```
NEXT_PUBLIC_NETWORK=testnet
NEXT_PUBLIC_USDC_CONTRACT=<testnet vault's usdc — Circle CBIELTK6… once redeployed>
NEXT_PUBLIC_VAULT_CONTRACT=<testnet vault>
NEXT_PUBLIC_REGISTRY_CONTRACT=<testnet registry>
NEXT_PUBLIC_API_URL=https://keeper-gamma-production.up.railway.app
```

**nectarnetwork.fun (mainnet — post-audit)**
```
NEXT_PUBLIC_NETWORK=mainnet
NEXT_PUBLIC_SOROBAN_RPC=<mainnet Soroban RPC provider>
NEXT_PUBLIC_USDC_CONTRACT=CCW67TSZV3SSS2HXMBQ5JFGCKJNXKZM7UQUWUZPUTHXSTZLEO7SJMI75
NEXT_PUBLIC_VAULT_CONTRACT=<mainnet vault>
NEXT_PUBLIC_REGISTRY_CONTRACT=<mainnet registry>
NEXT_PUBLIC_API_URL=<mainnet keeper API>
```

## Status / sequencing
- ✅ Frontend network-aware (`stellar.ts`); Circle USDC SAC IDs confirmed; deploy script uses `__constructor` + `set_vault`.
- ⏳ **Decision:** move the **testnet** deployment from the mock SAC to Circle testnet USDC (a redeploy — resets the current mock-USDC demo TVL; testers then use Circle's faucet).
- ⏳ **Mainnet** deploy with Circle mainnet USDC is **post-audit** (Tranche 3).
- ⏳ Follow-up: make the 9 `stellar.expert/explorer/testnet` links in the dashboard components network-aware (correct on testnet today; matters at mainnet launch).
