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

All entries verified 2026-08-03 against blend-contracts-v2 @ `ba22b48` (paths repo-relative).

`Request` struct (`pool/src/pool/actions.rs:12-18`):
```rust
#[derive(Clone)]
#[contracttype]
pub struct Request {
    pub request_type: u32,
    pub address: Address, // asset address or liquidatee
    pub amount: i128,
}
```

Complete `RequestType` enum — exactly 10 variants, `#[repr(u32)]`, no other fill/cancel variants exist (`pool/src/pool/actions.rs:21-34`):

| Variant | Integer | Dispatcher action | Source (file:line @ ba22b48) |
|---|---|---|---|
| `Supply` | 0 | `apply_supply`: mint bTokens (floor), spender→pool transfer; supply-cap enforced | `actions.rs:24` (enum), `:143-152` (dispatch), `:288-305` |
| `Withdraw` | 1 | `apply_withdraw`: burn bTokens (ceil, clamped to balance), pool→to transfer; no health check | `actions.rs:25`, `:153-163`, `:312-332` |
| `SupplyCollateral` | 2 | `apply_supply_collateral`: like Supply but counts as collateral | `actions.rs:26`, `:164-174`, `:339-356` |
| `WithdrawCollateral` | 3 | `apply_withdraw_collateral`: like Withdraw + triggers health check | `actions.rs:27`, `:175-185`, `:363-384` |
| `Borrow` | 4 | `apply_borrow`: mint dTokens (ceil), pool→to transfer, max-util check, health check | `actions.rs:28`, `:186-195`, `:391-408` |
| `Repay` | 5 | `apply_repay`: burn dTokens (floor); overpayment refunded | `actions.rs:29`, `:196-206`, `:415-441` |
| `FillUserLiquidationAuction` | 6 | `auctions::fill(…, 0, request.address=liquidatee, amount as u64 = fill %)` + health check | `actions.rs:30`, `:207-226` |
| `FillBadDebtAuction` | 7 | `auctions::fill(…, 1, request.address=backstop, amount as u64 = fill %)` + health check | `actions.rs:31`, `:227-247` |
| `FillInterestAuction` | 8 | `auctions::fill(…, 2, request.address=backstop, amount as u64 = fill %)`; the ONLY fill without a health check | `actions.rs:32`, `:248-266` |
| `DeleteLiquidationAuction` | 9 | deletes the SENDER's own user-liquidation auction; `request.address`/`amount` ignored | `actions.rs:33`, `:267-276` |

Additional verified behavior:

| Claim | Value | Date | Source |
|---|---|---|---|
| Invalid `request_type` handling | `RequestType::from_u32` panics `PoolError::BadRequest` (=1200); no `Result`/`TryFrom` path exists | 2026-08-03 | `pool/src/pool/actions.rs:41-55` (panic at `:53`); `pool/src/errors.rs:20` |
| Fill `amount` semantics | For request types 6/7/8, `Request.amount` is a fill PERCENTAGE cast `as u64`, must be 1..=100 (`scale_auction` panics `BadRequest` if `percent_filled > 100 \|\| == 0`) — not a token amount | 2026-08-03 | `pool/src/pool/actions.rs:214,235,255`; `pool/src/auctions/auction.rs:194-196` |
| Pre-dispatch checks | Per request: `require_nonnegative(amount)` (panics `NegativeAmountError`=8), then `pool.require_action_allowed(raw u32)` BEFORE `from_u32` | 2026-08-03 | `pool/src/pool/actions.rs:131-142`; `pool/src/validator.rs:12-15`; `pool/src/errors.rs:15` |
| Pool-status gating uses raw integers | status>1 blocks types 4,9 (Borrow, DeleteLiquidationAuction); status>3 blocks types 2,0 (SupplyCollateral, Supply); panics `InvalidPoolStatus`=1206 | 2026-08-03 | `pool/src/pool/pool.rs:75-82`; `pool/src/errors.rs:28` |
| Disabled-reserve gating | disabled reserve blocks types 0,2,4 with `ReserveDisabled`=1223 | 2026-08-03 | `pool/src/pool/reserve.rs:146-156`; `pool/src/errors.rs:53` |
| Health check enforcement | `validate_submit` runs HF check only `if check_health && from_state.has_liabilities()`; min HF 1_0000100 → `InvalidHf` | 2026-08-03 | `pool/src/pool/submit.rs:159-196` (check at `:188-191`) |
| `submit` guard | panics `BadRequest` if `from`, `spender`, or `to` is the pool contract itself | 2026-08-03 | `pool/src/pool/submit.rs:33-38` |
| Self-fill prohibited | `auctions::fill` panics `InvalidLiquidation` if auctioned user == filler | 2026-08-03 | `pool/src/auctions/auction.rs:148-150` |
| `FlashLoan` struct (same file) | `{ contract: Address, asset: Address, amount: i128 }`; flash-loan path gates as Borrow-class action | 2026-08-03 | `pool/src/pool/actions.rs:58-63`; `pool/src/pool/submit.rs:87-91` |

## Auction fill price curve

**Contradiction resolved (2026-08-03):** the curve is a **400-ledger two-phase Dutch auction**, NOT a "200-block decay" with simultaneous lot-up/bid-down. The prior CLAUDE.md note "lot scales 0%→100% over 200 blocks, bid scales 100%→0%" conflates two sequential phases. Decisive source: `scale_auction`, `pool/src/auctions/auction.rs:189-264` @ `ba22b48`:

```rust
let per_block_scalar: i128 = 0_0050000; // modifier moves 0.5% every block
let block_dif = i128(e.ledger().sequence() - auction_data.block);
if block_dif > 200 {
    // lot 100%, bid scaling down from 100% to 0%
    lot_modifier = SCALAR_7;
    if block_dif < 400 {
        bid_modifier = SCALAR_7 - (block_dif - 200) * per_block_scalar;
    } else {
        bid_modifier = 0;
    }
} else {
    // lot scaling from 0% to 100%, bid 100%
    lot_modifier = block_dif * per_block_scalar;
    bid_modifier = SCALAR_7;
}
```

With `t = block_dif = current_ledger − auction_data.block`:

| Claim | Value | Date | Source (file:line @ ba22b48) |
|---|---|---|---|
| Phase 1 (0 ≤ t ≤ 200) | `lot%(t) = t × 0.5%` (0→100%), `bid%(t) = 100%` | 2026-08-03 | `pool/src/auctions/auction.rs:222-225` |
| Fair point (lot=100% AND bid=100%) | exactly `t = 200` (200×0_0050000 = SCALAR_7; corroborated by unit test at seq 1200 vs block 1000) | 2026-08-03 | `auction.rs:212-226`; test `auction.rs:2618-2638` |
| Phase 2 (200 < t < 400) | `lot%(t) = 100%`, `bid%(t) = 100% − (t−200) × 0.5%` (100→0%) | 2026-08-03 | `auction.rs:214-219` |
| t ≥ 400 | `lot = 100%`, `bid = 0` — filler pays nothing, receives full lot; auction persists indefinitely (no auto-expiry) | 2026-08-03 | `auction.rs:217-221` |
| Total price-curve duration | 400 ledgers (~33 min at 5s ledgers); stale-DELETE allowed only after 500 blocks (`delete_stale_auction` panics if `auction.block + 500 > sequence`) | 2026-08-03 | `auction.rs:217`, `:103-106` |
| Auction start block | `AuctionData.block = e.ledger().sequence() + 1` at creation (auction begins the NEXT ledger); `AuctionData { bid: Map<Address,i128>, lot: Map<Address,i128>, block: u32 }` | 2026-08-03 | `auction.rs:55-57`; `user_liquidation_auction.rs:32,37`; `bad_debt_auction.rs:35`; `backstop_interest_auction.rs:38` |
| Fixed point + rounding | all modifiers 7-dec over `SCALAR_7 = 1_0000000`; bid amounts round UP twice (`fixed_mul_ceil` ×2), lot amounts round DOWN twice (`fixed_mul_floor` ×2) — always against the filler | 2026-08-03 | `auction.rs:229-257`; `pool/src/constants.rs:7` |
| Partial fills don't shift the curve | `percent_filled` (u64, whole percents 1..=100) scales amounts BEFORE the block modifier; the remainder is stored back with the ORIGINAL `block`, so the clock never resets; full fill deletes the auction; zero-amount scaled entries are dropped from the maps | 2026-08-03 | `auction.rs:194-196, 203-207, 229-243, 254-263` |
| Curve corroborated by unit test | t=0: bid 100%/lot empty; t=100: bid 100%/lot 50%; t=200: both 100%; t=300: bid 50%/lot 100%; t=400: bid empty/lot 100% | 2026-08-03 | test `test_scale_auction_100_fill_pct`, `auction.rs:2565-2680` |

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
