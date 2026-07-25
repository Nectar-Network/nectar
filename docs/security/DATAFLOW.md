# Nectar Network — Data Flow & Trust Boundaries

Companion to [THREAT-MODEL.md](./THREAT-MODEL.md). Each flow shows where value and data cross
trust boundaries and where validation happens. Line references: `contracts/nectar-vault/src/lib.rs`
(VLT) and `contracts/keeper-registry/src/lib.rs` (REG).

**Legend:** solid arrow = call/transfer · dashed boundary = trust boundary · 🔒 = signature/auth
required · ✅ = on-chain validation.

---

## 1. Deposit flow

```mermaid
sequenceDiagram
    autonumber
    participant U as User (off-chain 🔒)
    participant F as Frontend
    participant V as NectarVault (on-chain)
    participant T as USDC SAC

    U->>F: connect Freighter, enter amount
    F->>V: deposit(user, amount)  🔒 user.require_auth()
    V->>V: ✅ require_init; ✅ deposit_cap check (VLT:66)
    V->>V: shares = amount·total_shares/total_usdc (floor, VLT:72-76)
    V->>T: transfer(user → vault, amount)  🔒 (VLT:78)
    V->>V: persist Depositor{shares, last_deposit_time}; total_usdc/shares += (VLT:92-101)
    V-->>F: emit "deposit"(amount, shares)
```
**Boundary:** user → vault. **Validates:** auth, deposit cap, share math. **Note:** first deposit
mints 1:1 with no dead-shares floor (see THREAT-MODEL VLT‑1).

---

## 2. Liquidation flow (the core cycle)

```mermaid
sequenceDiagram
    autonumber
    participant K as Keeper daemon (off-chain 🔒)
    participant V as NectarVault
    participant R as KeeperRegistry
    participant B as Blend pool (external)
    participant S as Soroswap (external)

    K->>K: detect HF<1, evaluate profitability (oracle-anchored)
    K->>V: draw(keeper, amount)  🔒 keeper.require_auth()
    V->>V: ✅ max_draw_per_keeper; ✅ amount ≤ total_usdc−active_liq (VLT:215,224)
    V->>R: (cross-contract) require_registered_keeper → get_keeper (VLT:403-415)
    V->>V: transfer(vault → keeper, amount); KeeperDraw += ; active_liq += (VLT:235-246)
    V->>R: mark_draw(vault, keeper)  🔒 require_vault (REG:209)
    K->>B: fill liquidation auction → receive collateral
    K->>S: swap collateral → USDC (min_out, oracle floor)
    K->>V: return_proceeds(keeper, amount, response_ms)  🔒 keeper.require_auth()
    V->>V: transfer(keeper → vault, amount); compute repay/profit (VLT:278-315)
    alt fully settled
        V->>R: clear_draw + record_execution(success, profit) 🔒 require_vault
    else partial
        V->>V: keep remaining draw owed (slash-eligible) (VLT:317-334)
    end
    V-->>K: emit "return"(amount, profit)
```
**Boundaries:** keeper → vault (draw/return), vault → registry (mark/clear/record), keeper →
external Blend/Soroswap (separate trust zones). **Validates:** draw limits, registry membership,
per-keeper draw accounting. **Notes:** `require_registered_keeper` discards its result (VLT‑3);
`return_proceeds` isn't gated to registered keepers (VLT‑2); CEI ordering in `draw` (VLT‑6).

---

## 3. Withdrawal flow

```mermaid
sequenceDiagram
    autonumber
    participant U as User 🔒
    participant V as NectarVault
    participant T as USDC SAC

    U->>V: withdraw(user, shares)  🔒 user.require_auth()
    V->>V: ✅ shares ≤ depositor.shares (VLT:123)
    V->>V: ✅ now − last_deposit_time ≥ withdraw_cooldown (VLT:133)
    V->>V: usdc_out = shares·total_usdc/total_shares (floor, VLT:150)
    V->>T: transfer(vault → user, usdc_out)  🔒 (VLT:157)
    V->>V: depositor.shares -= ; total_usdc/shares -= (VLT:159-167)
    V-->>U: emit "withdraw"(shares, usdc_out)
```
**Boundary:** vault → user. **Validates:** ownership, cooldown (anti deposit-and-withdraw
arbitrage), share math. Withdrawer receives current share value incl. accrued profit.

---

## 4. Keeper registration & staking

```mermaid
sequenceDiagram
    autonumber
    participant O as Operator 🔒
    participant R as KeeperRegistry
    participant T as USDC SAC

    O->>R: register(operator, name)  🔒 operator.require_auth()
    R->>R: ✅ not Paused; ✅ not already registered; ✅ min_stake>0 (REG:41-58)
    R->>T: transfer(operator → registry, min_stake)  🔒 (REG:61)
    R->>R: store KeeperInfo{stake, active, counters}; KeeperList += ; count += (REG:82-93)
    R-->>O: emit "registered"(name, stake, ts)
    Note over O,R: deregister refunds stake, blocked while has_active_draw (REG:118)
```
**Boundary:** operator → registry. **Validates:** pause flag, uniqueness, positive stake.
Stake is the accountability bond (slashable).

---

## 5. Slashing flow

```mermaid
sequenceDiagram
    autonumber
    participant A as Anyone (permissionless)
    participant R as KeeperRegistry
    participant V as NectarVault (recipient)
    participant T as USDC SAC

    A->>R: slash(keeper)
    R->>R: ✅ has_active_draw; ✅ now − last_draw_time > slash_timeout (REG:296-303)
    R->>R: slash_amt = stake · slash_rate_bps / 10_000 (REG:305)
    R->>T: transfer(registry → vault, slash_amt)  (REG:308)
    R->>R: stake -= slash_amt; has_active_draw = false (REG:313-315)
    R-->>V: compensation credited to the vault
    R-->>A: emit "slashed"(slash_amt, remaining_stake)
```
**Boundary:** anyone → registry → vault. **Validates:** active draw + timeout elapsed.
Permissionless by design (anyone can enforce), self-limiting (one slash per draw). See
THREAT-MODEL REG‑1.

---

## Data entities crossing boundaries

| Entity | Where it lives | Crosses boundary at |
|---|---|---|
| USDC amount | USDC SAC balances | deposit, withdraw, draw, return, register, slash |
| Shares | Vault persistent `Depositor` | deposit (mint), withdraw (burn) |
| `active_liq` / `KeeperDraw` | Vault instance/persistent | draw (+), return (−) |
| Stake | Registry persistent `KeeperInfo` | register (+), deregister (refund), slash (−) |
| `has_active_draw` / `last_draw_time` | Registry `KeeperInfo` | mark_draw (set), clear_draw/slash (unset) |
| Performance counters | Registry `KeeperInfo` | record_execution |
| Position / auction data | Blend pool (external) | read by keeper only — never enters the vault |

## External trust zones
Blend, Soroswap, and Reflector are **outside** the Nectar trust boundary. The keeper is the only
component that talks to them, and it can never make the vault trust their output directly — the
vault only ever sees a keeper-signed `return_proceeds` with real USDC. Oracle risk is bounded by
the Tranche 3 Reflector circuit breaker (THREAT-MODEL ORA‑1).
