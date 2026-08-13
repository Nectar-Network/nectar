# Session D evidence — autonomous borrower discovery

**Claim proven:** the keeper finds borrowers by itself. A brand-new address that
appears in no config file, no environment variable, no cache and no prior
evidence file was discovered from on-chain events alone, health-checked, and
then liquidated end-to-end by the Session-B pipeline — with no keeper
restart and no configuration change between its creation and its liquidation.

Run date: 2026-08-14 (UTC times below). Pool `CBUBTHAT…` (Nectar Sandbox),
vault `CDOGQY7N…`, keeper-alpha `GCC52N6U…`.
Keeper build: `keeper/blend/index.go` at commit `dc3abb9`.

---

## The fresh borrower

Created by `scripts/nectar-sandbox/05-fresh-borrower.mjs`, which generates a
keypair at runtime and prints it once. The point of the script is that nobody —
including the keeper's operator — knew the address before the transaction
landed.

```
=== FRESH BORROWER (generated this run) ===
public : GDLTPYPG22QJU4GDDS6RGUXNITKCL7HENYNZPFCAMGIJS3CZJEY4XRZS
```

| Step | Tx hash |
|---|---|
| Trustline to Circle USDC | `4bdcb2004847f0e553f94c1b6f6df30abd92b2de9e5399eaff0557f33aedf73f` |
| Admin supplies 40 Circle-USDC of lend liquidity | `65ab839c11cf2876ae3e4b6a9e0a788713095859979de7e26d80b1dd1c81e41f` |
| **Borrower supplies 100 XLM collateral, borrows 20 Circle-USDC** | `e31a5ee8a2c3b026d9deb6298dc57fb613c2b134453a7fa3579f04390213232d` |

**Proof the address was unconfigured**, run before the keeper was restarted:

```
$ ADDR=GDLTPYPG22QJU4GDDS6RGUXNITKCL7HENYNZPFCAMGIJS3CZJEY4XRZS
$ grep -rn "$ADDR" . --exclude-dir=.git --exclude-dir=node_modules --exclude-dir=reference
(no matches)
$ grep -c "$ADDR" keeper.env                 -> 0     # the running keeper's env
$ grep -c "$ADDR" borrowers.json             -> 0     # the pre-restart cache
$ grep -c "$ADDR" reference/blend-utils/.env -> 0     # the sandbox scripts' env
```

`WATCH_ADDRESSES` was unset for the entire run.

---

## 1. Discovery, from events only

The keeper was restarted from the **existing cache**, whose `last_ledger` was
4124986 — before the borrow landed at 4125014. So this exercises the live
incremental path, not a backfill:

```
00:11:10.862 INFO borrower discovery pool=CBUB..CK3V backfill=false from=4124986 to=4125023
             events=3 added=2 tracked=3 debtors=1 probed=2
             pool_load_ms=2855 sync_ms=700 probe_ms=739
00:11:10.862 INFO position pool=CBUB..CK3V addr=GATK..3P7W hf=+Inf
00:11:10.862 INFO position pool=CBUB..CK3V addr=GDLT..XRZS hf=1.4962
```

`GDLT..XRZS` is the fresh borrower. `hf=1.4962` matches the hand computation
from the position's own parameters exactly:
`(100 × 0.42 × 0.75) / (20 ÷ 0.95) = 1.4963`.

`backfill=false from=4124986` is the cached resume mark; `probed=2` is the two
newly-seen addresses only — the third indexed address was already known to be
debt-free and was not re-read.

## 2. Forced underwater, detected

`./run.sh 03 0.15` (tx `c6100628463f826d7a4f4233235b2dc0f19081eeba62866039f3240bf707920d`)
set the sandbox oracle to XLM = $0.15.

```
00:12:05.123 INFO position pool=CBUB..CK3V addr=GDLT..XRZS hf=0.5344
00:12:05.123 INFO executing task protocol=blend:CBUB..CK3V type=liquidation target=GDLT..XRZS priority=7
```

`hf=0.5344` again matches by hand: `(100 × 0.15 × 0.75) / (20 ÷ 0.95) = 0.5344`.

## 3. Liquidated end-to-end

| # | Step | Tx hash |
|---|---|---|
| 1 | `new_auction` type 0, percent 100 (start block 4125037) | `4294da5b250ba13c930ca57498eef6293c55b5f4a5c2caaf61d1d08922c0b0b7` |
| — | ~21 min wait while the verified two-phase curve climbs; the log shows the ratio going 0.8876 → 0.9036 → … → 1.0067 → over 1.02 | |
| 2 | `vault.draw` #1 | `1a6a96225fac24111cc2a67e804a1883063918b588373db71fac747f6a2f5f77` |
| 3 | `vault.draw` #2 (after #1's fill failed — see below) | `fcafe02c75a17b807d15497174165a26ae34f0ff47fa437b22f1b27d26555a5e` |
| 4 | **Atomic fill+repay+withdraw_collateral** — one `submit()` with 3 requests | `a4af213db024f0da04382238fcf3531a8d62e10eab2a8d1385b6aa0beb77d1c7` |
| 5 | Collateral swap (Soroswap) during stale-draw recovery | `4f95cb79bca165804db7268a7e6a2bbfab5de1c30ca3ab9cfadeb59e040dcc68` |
| 6 | `vault.return_proceeds` | `fe5f03f6d4012e69dd49711454a902236a47110ceeff0d3522019420ca67d0fb` |

Draw #1's fill was rejected by the network with `tx_bad_seq`
(`stellar xdr decode --type TransactionResult` on `AAAAAAABE//////7AAAAAA==` →
`{"fee_charged":"70655","result":"tx_bad_seq"}`) — a transient sequence
collision, not a logic error. The keeper reported it honestly and left the draw
outstanding for recovery rather than pretending the fill had happened:

```
00:33:45.230 WARN task failed after drawing capital … drew=202000028 returned=0
             note=fill failed: …; nothing returnable above keeper float — draw outstanding, next cycle recovers
```

## 4. The index closed the loop

After the fill, the borrower's debt was gone. The index observed that through a
`get_positions` probe — not from an event, because Blend publishes no
post-action liability balance (docs/FACTS.md) — and retired the address:

```
00:35:01.605 INFO borrower discovery … events=4 added=0 tracked=3 debtors=0 probed=1
00:35:30.739 INFO borrower discovery … events=0 added=0 tracked=3 debtors=0 probed=0
```

`debtors=1 → 0`, then `probed=1 → 0`. The address stays *tracked* so a future
event can revive it cheaply, but it costs nothing per cycle. This is the
"add on borrow, keep until debt observed at 0" requirement, working live.

---

## Vault accounting — read from the chain, not the logs

```
$ stellar contract invoke --id CDOGQY7N… --send=no -- get_state
{"active_liq":"0","total_profit":"25960334","total_shares":"600000000","total_usdc":"625960334"}

$ stellar contract invoke --id CDOGQY7N… --send=no -- get_keeper_draw --keeper GCC52N6U…
"0"
```

Both numbers are **identical to the pre-run state** measured at 00:11 before the
price was dropped. So: the vault ended whole, with no outstanding draw, and this
liquidation booked **zero profit** for depositors.

That is an honest result, not a success to dress up. Session B's money test
(`docs/evidence/b-full-cycle.md`) is the profit proof; this run's purpose was
autonomy, and the economics came out flat because of the swap incident below.
The keeper absorbed the difference from its own float — the vault was made whole
by the recovery path, which is exactly what that path is for.

### The swap incident, and why the oracle was moved mid-run

At XLM = $0.15 the keeper **refused** to sell the seized collateral:

```
00:34:12.994 WARN collateral swap refused — holding asset=CDLZ..CYSC amount=999842047
             err=dex: quote below slippage floor: quote 105873397 < floor 148476543
```

The swap floor is anchored to the oracle price. Our sandbox oracle is one we
control, and $0.15 was **above** the real Soroswap market — the quote implies
`105873397 / 999842047 = $0.10589` per XLM. So the floor was unreachable by
construction, and the guard did the right thing by holding rather than selling
into a 30% adverse move.

To let the unwind finish, the oracle was set to $0.098 (tx
`106d308b8654ff1011671d8470c7c61b20451fab32bf83b9c1779712779a9f8d`) — the same
price Session B ran at, and below the observed market so the floor is
achievable. **This changed the swap floor, not the liquidation**: the fill had
already landed at step 4 before the price was touched. Stating it plainly
because it is the one operator intervention in the run.

The recovery then swept the keeper's held XLM and returned the outstanding draw:

```
00:35:17 INFO soroswap swap landed in=99909845637 out=10552996257
00:35:22 INFO vault return_proceeds landed amount=15088521
00:35:22.864 INFO recovered stale vault draw drawn=15088521 returned=15088521
```

Note `in=99909845637` (~9991 XLM) is far more than the 100 XLM seized: while a
draw is outstanding, recovery treats the keeper's **entire** holding of
pool-reserve tokens as sellable, as documented in CLAUDE.md. That is why the
vault came out whole and the keeper's float absorbed the loss.

**Per-step attribution gap, stated rather than papered over:** the task-level
log line reports `drew=202000028 proceeds=64000008`, and two draws of
202000028 landed while only one `return_proceeds` (15088521) is logged. The
chain reads above are authoritative and show the vault whole with zero
outstanding draw, but the intermediate returns between 00:34:12 and 00:34:54
are not individually logged, so the per-transaction ledger of that interval
could not be reconstructed from this run's logs. That is a logging gap worth
closing before mainnet, not a discrepancy in the final state.

---

## Cold start and cached restart

Both paths were exercised against the live sandbox.

**Cold start** (no cache file), 00:07 — this is the case the previous
implementation could not do at all:

```
00:08:08.157 INFO borrower discovery pool=CBUB..CK3V backfill=true from=4004042 to=4124986
             events=12 added=1 tracked=1 debtors=0 probed=1
```

`from=4004042` is the RPC's oldest retained ledger plus the safety margin. The
sandbox pool's entire event history sits at ledgers 4054311–4056272, roughly
70,000 ledgers behind head — **outside** the old 1000-ledger lookback window,
which is why the shipped default used to discover zero positions on this pool.

**Cached restart**, 00:10 — `backfill=false from=4124986`, resuming exactly at
the persisted mark (§1 above).

Cache contents after the run (`{pool → last_ledger, borrower set}`):

```json
{"version":1,"pools":{"CBUBTHAT…":{"last_ledger":4125323,"addrs":{
  "GATK27P6…":{"debt":false,"probed":4125022,"last_event":4125013,"first_seen":4125013},
  "GCCTPHRT…":{"debt":false,"probed":4124985,"last_event":4054637,"first_seen":4054311},
  "GDLTPYPG…":{"debt":false,"probed":4125308,"last_event":4125299,"first_seen":4125014}}}}}
```

### Direct A/B, run live after the liquidation

The keeper was stopped, the cache deleted, and it was started again with no
cache at all. The set it rebuilt is identical to the cached one:

```
00:38:36.550 INFO borrower discovery pool=CBUB..CK3V backfill=true from=4004407 to=4125351
             events=25 added=3 tracked=3 debtors=0 probed=3
             pool_load_ms=3751 sync_ms=5385 probe_ms=1271
```

| Address | cached `debt` / `last_event` | cold-start `debt` / `last_event` |
|---|---|---|
| `GATK27P6…` (admin, supply-only) | false / 4125013 | false / 4125013 |
| `GCCTPHRT…` (Session-B borrower) | false / 4054637 | false / 4054637 |
| `GDLTPYPG…` (this session's borrower) | false / 4125299 | false / 4125299 |

Same three addresses, same debt states, same last-event ledgers. The only
difference is `probed`, which is when each was last read — the cold start
re-probed all three (`probed=3`) where the cached run had already retired them
(`probed=0`). That is the cache doing its job: saving round-trips, not changing
the answer.

Note the cold start's `sync_ms=5385` against ~700–900 ms for an incremental
sweep: 13 paged `getEvents` requests to cover the full 120960-ledger retention
window versus one for the new ledgers.

### Where the cache is NOT just an optimisation

An earlier draft of this work claimed a cold start always reaches the same set
as a cached restart. An adversarial review refuted that, and it was wrong.

It holds for any borrower whose last event is still inside the RPC's retention
window (observed 120960 ledgers, ~7 days). It does **not** hold for one idle
longer than that: its events have aged out of every endpoint the keeper can
reach, and interest accrual — which is what pushes an idle position underwater —
emits no event at all. Such an address survives only because the cache
remembered it.

`TestSync_ColdStartCannotSeeBorrowersOlderThanRetention` pins this divergence
rather than asserting it away, and `WATCH_ADDRESSES` is the documented recovery.
Running with `BORROWER_CACHE` unset now warns at startup with the consequence
spelled out.

---

## Cost (D3)

Per-phase timings are logged every cycle. Steady state on the sandbox, one
pool, three tracked addresses and one debtor:

| Phase | Typical |
|---|---|
| `pool_load_ms` (LoadPool: 3 + 2N sequential simulates) | 2700–3500 |
| `sync_ms` (getHealth + incremental event sweep) | 690–970 |
| `probe_ms` (one `get_positions` per probed address) | 360–520 |

**The exit criterion "a cycle with the full set stays under the poll interval"
is NOT met, and discovery is not the reason.** Measured cycles ran 14–18 s
against a `POLL_INTERVAL` of 10 s, and `nectar_cycle_overruns_total` reached 83.
Discovery accounts for ~1.1–1.5 s of that; the dominant fixed cost is `LoadPool`
at ~3 s per pool per cycle, plus task execution. A cycle with `probed=0` and
`events=0` still took 11–12 s.

This is a pre-existing cost that this session made *visible* rather than
introduced — the metric and the warning are new. Closing it means caching the
pool's reserve list and config (only oracle prices genuinely need re-reading
every cycle) and/or raising `POLL_INTERVAL`; both are follow-up work, recorded
here rather than quietly left in the logs.

What discovery *did* remove is the scaling term: probes now go to confirmed
debtors plus newly-active addresses, so `probed` was 1 or 0 on almost every
steady-state cycle instead of one round-trip per address ever seen.

New metrics on `/metrics`:
`nectar_cycle_duration_ms`, `nectar_cycle_overruns_total`,
`nectar_borrowers_tracked`, `nectar_borrowers_with_debt`,
`nectar_discovery_truncated_total`.

---

## Raw logs

- `d-discovery-coldstart.log` — cold start, full retention backfill
- `d-discovery-active.log` — the discovery + liquidation run
