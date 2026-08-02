# FACTS.md — Verified Blend Protocol Facts

Single source of truth for versions, contract IDs, and decoded shapes used by
Nectar Network's Blend integration. **Every entry must carry a date and a
source** (URL, repo-path@commit, command, or tx hash). Entries without a source
are invalid — do not add them.

Entry format: `claim — value — date — source`.

Status: SKELETON — populated by verification Gates 0.1–0.7 (see
VERIFICATION-REPORT.md once complete).

---

## Reference baselines

| Claim | Value | Date | Source |
|---|---|---|---|
| blend-contracts-v2 pinned commit | `ba22b487b2c5057a4ecc28b05b5193c28e4bd117` (authored 2025-08-14 11:26:04 -0400) | 2026-08-03 | `git rev-parse HEAD` / `git log -1 --format=%ci` in local clone of https://github.com/blend-capital/blend-contracts-v2 |
| blend-utils pinned commit | `b05242df30b6b6caf9d317646f754541824a5a8b` (authored 2025-12-18 08:22:46 -0500) | 2026-08-03 | `git rev-parse HEAD` / `git log -1 --format=%ci` in local clone of https://github.com/blend-capital/blend-utils |
| Clones live in `reference/` (gitignored) | pinned by hash above, not vendored | 2026-08-03 | `.gitignore` `reference/` entry |

## Request struct & RequestType integers

| Variant | Integer | Date | Source (file:line @ commit) |
|---|---|---|---|
| _to be filled by Gate 0.2_ | | | |

## Auction fill price curve

| Claim | Value | Date | Source (file:line @ commit) |
|---|---|---|---|
| _to be filled by Gate 0.3_ | | | |

## Auction asset flows

| Auction type | Lot asset(s) | Bid asset(s) | Filler provides | Filler receives | Backstop LP involved? | Source |
|---|---|---|---|---|---|---|
| _to be filled by Gate 0.4_ | | | | | | |

## Testnet addresses

### Pool public read interface (from pinned source)

Verified in `pool/src/contract.rs` @ `ba22b48` (blend-contracts-v2), 2026-08-03:

| Function | Signature | Source |
|---|---|---|
| `get_config` | `fn get_config(e: Env) -> PoolConfig` — `PoolConfig { oracle: Address, min_collateral: i128, bstop_rate: u32, status: u32, max_positions: u32 }` | `pool/src/contract.rs:81`, `pool/src/storage.rs:26-33` |
| `get_reserve_list` | `fn get_reserve_list(e: Env) -> Vec<Address>` | `pool/src/contract.rs:88` |
| `get_reserve` | `fn get_reserve(e: Env, asset: Address) -> Reserve` | `pool/src/contract.rs:94` |
| `get_positions` | `fn get_positions(e: Env, address: Address) -> Positions` | `pool/src/contract.rs:101` |
| `get_auction` | `fn get_auction(e: Env, auction_type: u32, user: Address) -> AuctionData` | `pool/src/contract.rs:295` |
| `submit` | `fn submit(e: Env, from: Address, spender: Address, to: Address, requests: Vec<Request>) -> Positions` | `pool/src/contract.rs:116-122` |
| (oracle getter) | none — the oracle address is a field of `PoolConfig`, read via `get_config` | `pool/src/storage.rs:27` |
| (backstop getter on pool) | none in the pool trait; backstop is a `__constructor` arg (`backstop_id`) | `pool/src/contract.rs:340-352` |
| backstop: `backstop_token` | `fn backstop_token(e: Env) -> Address` (on the backstop contract) | `backstop/src/contract.rs:76` |
| backstop: `pool_data` | `fn pool_data(e: Env, pool: Address) -> PoolBackstopData` | `backstop/src/contract.rs:73` |

### Canonical testnet deployment (live-read 2026-08-03)

All live reads: `stellar contract invoke --id <C…> --source-account GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W --network testnet --send=no -- <fn>` (simulation only, nothing sent).

| Claim | Value | Date | Source (command / URL) |
|---|---|---|---|
| Canonical testnet address list | `blend-utils/testnet.contracts.json` | 2026-08-03 | https://docs.blend.capital/mainnet-deployments.md ("All testnet contracts addresses can be found here …blend-utils/blob/main/testnet.contracts.json"); file read at blend-utils@`b05242d` |
| "Blend TestnetV2 pool" and "RegionalStarterPack Pool V2" are the SAME contract | `CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF` — there is one canonical V2 testnet pool, not two | 2026-08-03 | `testnet.contracts.json` key `TestnetV2` @ blend-utils@`b05242d`; on-chain instance storage `Name = "TestnetV2"` via `stellar contract read --id CCEBVDYM… --network testnet --durability persistent` |
| TestnetV2 pool on-chain name | `"TestnetV2"` (NOT "RegionalStarterPack") | 2026-08-03 | `stellar contract read --id CCEBVDYM… --network testnet --durability persistent` → `{key: {symbol: "Name"}, val: {string: "TestnetV2"}}` |
| TestnetV2 pool wasm sha256 | `a41fc53d6753b6c04eb15b021c55052366a4c8e0e21bc72700f461264ec1350e` = blend-utils `hashes.lendingPoolV2` (live pool runs blend-utils' shipped V2 wasm) | 2026-08-03 | `stellar contract fetch --id CCEBVDYM… --network testnet` + `shasum -a 256`; `testnet.contracts.json` `hashes.lendingPoolV2` |
| TestnetV2 reserve list (4 reserves, in index order) | `[CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC, CAZAQB3D7KSLSNOSQKYD2V4JP5V2Y3B4RDJZRLBFCCIXDCTE3WHSY3UE, CAP5AMC2OHNVREO66DFIN6DHJMPOBAJ2KCDDIMFBR7WWJH5RZBFM3UEI, CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU]` | 2026-08-03 | live `get_reserve_list` |
| Reserve 0: XLM (native SAC) | `CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC` — `symbol()="native"` | 2026-08-03 | live `symbol`/`name` |
| Reserve 1: wETH SAC | `CAZAQB3D7KSLSNOSQKYD2V4JP5V2Y3B4RDJZRLBFCCIXDCTE3WHSY3UE` — `wETH:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56` | 2026-08-03 | live `symbol`/`name` |
| Reserve 2: wBTC SAC | `CAP5AMC2OHNVREO66DFIN6DHJMPOBAJ2KCDDIMFBR7WWJH5RZBFM3UEI` — `wBTC:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56` | 2026-08-03 | live `symbol`/`name` |
| Reserve 3: pool USDC SAC (Blend testnet USDC) | `CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU` — `USDC:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56` | 2026-08-03 | live `symbol`/`name` |
| TestnetV2 oracle | `CAZOKR2Y5E2OSWSIBRVZMJ47RUTQPIGVWSAQ2UISGAVC46XKPGDG5PKI` — this is Blend's **mock oracle** (`oraclemock`), NOT Reflector: its on-chain wasm sha256 `66c0b87b5eb481be594175d59e66ec9a9ac8945be0fec4e09f6c28bf7a1708be` equals `testnet.contracts.json` `hashes.oraclemock` | 2026-08-03 | live `get_config` → `oracle`; `stellar contract fetch --id CAZOKR2Y… ` + `shasum -a 256`; blend-utils@`b05242d` `testnet.contracts.json` |
| TestnetV2 pool config (live) | `bstop_rate=1000000` (10% in 7-dec), `max_positions=8`, `min_collateral=0`, `status=0` | 2026-08-03 | live `get_config` |
| Backstop V2 | `CBDVWXT433PRVTUNM56C3JREF3HIZHRBA64NB2C3B2UNCKIS65ZYCLZA` — serves the TestnetV2 pool (live `pool_data(CCEBVDYM…)` returns `tokens=547206929909`, `shares=547206929909`, `usdc=323702229332`, `blnd=4417757006644`, `token_spot_price=29577680`, `q4w_pct=241068`) | 2026-08-03 | `testnet.contracts.json` `backstopV2`; live `pool_data` |
| Backstop token (comet BLND:USDC LP) | `CA5UTUUPHYL5K22UBRUVC37EARZUGYOSGK3IKIXG2JLCC5ZZLI4BDWDM` — `symbol()="CPAL"`, `name()="Comet Pool Token"` | 2026-08-03 | live `backstop_token()` on backstop; live `symbol`/`name` on token |
| BLND token (testnet) | `CB22KRA3YZVCNCQI64JQ5WE7UY2VAV7WFLK6A2JN3HEX56T2EDAFO7QF` — `BLND:GATALTGTWIOT6BUDBCZM3Q4OQ4BO2COLOAZ7IYSKPLC2PMSOPPGF5V56` | 2026-08-03 | `testnet.contracts.json` `BLND`; live `symbol`/`name` |
| Pool factory V2 (testnet) | `CDV6RX4CGPCOKGTBFS52V3LMWQGZN3LCQTXF5RVPOOCG4XVMHXQ4NTF6` | 2026-08-03 | `testnet.contracts.json` `poolFactoryV2` @ blend-utils@`b05242d` (not live-read) |

## Our testnet USDC

| Claim | Value | Date | Source |
|---|---|---|---|
| NectarVault settlement asset (Circle testnet USDC SAC) | `CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA` — `USDC:GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5` (Circle issuer, faucet.circle.com, not mintable by us) | 2026-08-03 | live `symbol`/`name` read; CLAUDE.md deployed-contracts section |
| **Our Circle USDC ≠ Blend TestnetV2 pool USDC** | The pool's USDC reserve is `CAQCFVLO…` (`USDC:GATALTGT…`, Blend's testnet issuer). Our vault settles in `CBIELTK6…` (`USDC:GBBD47IF…`, Circle). They are different assets; a keeper filling TestnetV2 auctions pays/receives Blend-USDC, not Circle-USDC. | 2026-08-03 | live `get_reserve_list` + `symbol`/`name` reads on both SACs |

## blend-utils pool deploy

| Claim | Value | Date | Source |
|---|---|---|---|
| _to be filled by Gate 0.6_ | | | |

## Decisions

| Decision | Rationale | Date | Source / evidence |
|---|---|---|---|
| _decisions recorded as they are made_ | | | |
