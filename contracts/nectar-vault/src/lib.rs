#![no_std]

mod types;

use soroban_sdk::{contract, contractimpl, token, vec, Address, Env, IntoVal, Symbol};
use types::{Depositor, DrawInfo, VaultConfig, VaultError, VaultKey, VaultState};

// Virtual-offset share accounting (OpenZeppelin ERC-4626 inflation-attack
// mitigation, VLT-1). A constant phantom co-owner of VIRTUAL_OFFSET shares and
// VIRTUAL_OFFSET assets makes the first-depositor inflation attack economically
// irrational: to round a victim's deposit to zero an attacker must donate on the
// order of VIRTUAL_OFFSET x the victim's amount and cannot recover it (the
// phantom absorbs it on redeem). The symmetric offset leaves the first deposit
// exactly 1:1 and is negligible for a healthy pool. i128 traps on overflow.
const VIRTUAL_OFFSET: i128 = 1_000_000;

/// Fixed-window length for the per-address withdrawal rate limit (T3).
const SECONDS_PER_DAY: u64 = 86400;

/// Shares minted for `amount` USDC at the current ratio (floors, favors pool).
fn to_shares(amount: i128, total_shares: i128, total_usdc: i128) -> i128 {
    amount * (total_shares + VIRTUAL_OFFSET) / (total_usdc + VIRTUAL_OFFSET)
}

/// USDC redeemed for `shares` at the current ratio (floors, favors pool).
fn to_assets(shares: i128, total_shares: i128, total_usdc: i128) -> i128 {
    shares * (total_usdc + VIRTUAL_OFFSET) / (total_shares + VIRTUAL_OFFSET)
}

#[contract]
pub struct NectarVault;

#[contractimpl]
impl NectarVault {
    /// Constructor — runs atomically at deploy, so admin/token/registry/config
    /// are set in the same transaction that creates the contract. This closes
    /// the initialize front-run (NEW-init): there is no separate init tx for an
    /// attacker to race, and the deployer alone chooses the constructor args.
    pub fn __constructor(
        env: Env,
        admin: Address,
        usdc_token: Address,
        registry: Address,
        config: VaultConfig,
    ) {
        env.storage().instance().extend_ttl(1000, 1000);
        env.storage().instance().set(&VaultKey::Admin, &admin);
        env.storage().instance().set(&VaultKey::Usdc, &usdc_token);
        env.storage()
            .instance()
            .set(&VaultKey::KeeperRegistry, &registry);
        env.storage()
            .instance()
            .set(&VaultKey::VaultConfig, &config);
        env.storage().instance().set(
            &VaultKey::State,
            &VaultState {
                total_usdc: 0,
                total_shares: 0,
                total_profit: 0,
                active_liq: 0,
            },
        );
    }

    /// Deposit USDC into the vault and receive shares proportional to current ratio.
    pub fn deposit(env: Env, user: Address, amount: i128) -> Result<i128, VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_init(&env)?;
        require_not_paused(&env)?;
        user.require_auth();

        let usdc: Address = env
            .storage()
            .instance()
            .get(&VaultKey::Usdc)
            .ok_or(VaultError::NotInit)?;
        let cfg: VaultConfig = env
            .storage()
            .instance()
            .get(&VaultKey::VaultConfig)
            .ok_or(VaultError::NotInit)?;
        let mut state: VaultState = env
            .storage()
            .instance()
            .get(&VaultKey::State)
            .ok_or(VaultError::NotInit)?;

        if cfg.deposit_cap > 0 && state.total_usdc + amount > cfg.deposit_cap {
            return Err(VaultError::DepositCapExceeded);
        }

        // Virtual-offset conversion (VLT-1). Floors toward zero — the depositor
        // gets at most the proportional shares, never more.
        let shares = to_shares(amount, state.total_shares, state.total_usdc);
        // A deposit too small to mint any shares is rejected so no one can fund
        // the pool for nothing (belt-and-suspenders with the virtual offset).
        if shares <= 0 {
            return Err(VaultError::ZeroShares);
        }

        token::Client::new(&env, &usdc).transfer(&user, &env.current_contract_address(), &amount);

        let now = env.ledger().timestamp();
        let depositor_key = VaultKey::Depositor(user.clone());
        let mut depositor = env
            .storage()
            .persistent()
            .get::<VaultKey, Depositor>(&depositor_key)
            .unwrap_or(Depositor {
                addr: user.clone(),
                shares: 0,
                deposited_at: now,
                last_deposit_time: now,
                window_start: 0,
                withdrawn_in_window: 0,
            });
        depositor.shares += shares;
        depositor.last_deposit_time = now;
        env.storage().persistent().set(&depositor_key, &depositor);
        env.storage()
            .persistent()
            .extend_ttl(&depositor_key, 535680, 535680);

        state.total_usdc += amount;
        state.total_shares += shares;
        env.storage().instance().set(&VaultKey::State, &state);

        env.events().publish(
            (Symbol::new(&env, "deposit"), user.clone()),
            (amount, shares),
        );
        Ok(shares)
    }

    /// Burn shares and receive proportional USDC (including any accumulated profit).
    pub fn withdraw(env: Env, user: Address, shares: i128) -> Result<i128, VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_init(&env)?;
        user.require_auth();

        let depositor_key = VaultKey::Depositor(user.clone());
        let mut depositor: Depositor = env
            .storage()
            .persistent()
            .get(&depositor_key)
            .ok_or(VaultError::NoShares)?;

        if shares > depositor.shares {
            return Err(VaultError::InsufficientBalance);
        }

        let cfg: VaultConfig = env
            .storage()
            .instance()
            .get(&VaultKey::VaultConfig)
            .ok_or(VaultError::NotInit)?;
        let now = env.ledger().timestamp();
        if now.saturating_sub(depositor.last_deposit_time) < cfg.withdraw_cooldown {
            return Err(VaultError::WithdrawalCooldown);
        }

        let mut state: VaultState = env
            .storage()
            .instance()
            .get(&VaultKey::State)
            .ok_or(VaultError::NotInit)?;
        if state.total_shares == 0 {
            return Err(VaultError::InsufficientVault);
        }

        // Virtual-offset redemption (VLT-1). Floors toward zero — the withdrawer
        // gets at most the proportional USDC, never more, which protects the pool.
        //
        // Clamp to total_usdc (SCOUT-total_usdc-underflow): for total_shares <=
        // total_usdc the clamp is a proven no-op (to_assets(sh,S,U) <= U iff
        // S <= U). It only bites in the S > U regime reachable after a
        // reconcile_default loss write-off, where the +VIRTUAL_OFFSET numerator
        // can make to_assets exceed total_usdc by offset-scale dust. Since
        // total_usdc is a *signed* i128, the later `total_usdc -= usdc_out` would
        // NOT trap on going negative — combined with a permissionless raw-token
        // donation (which lets the over-payment transfer succeed) it would persist
        // a negative total_usdc. The clamp keeps the invariant total_usdc >= 0.
        let usdc_out =
            to_assets(shares, state.total_shares, state.total_usdc).min(state.total_usdc);

        // Per-address 24h rate limit (T3), applied to the USDC actually leaving.
        // Fixed-window accounting: (window_start, withdrawn_in_window), reset
        // once now - window_start >= 86400. Checked after the cooldown and
        // before any state commit; 0 disables the limit entirely.
        if cfg.max_withdraw_per_24h > 0 {
            if now.saturating_sub(depositor.window_start) >= SECONDS_PER_DAY {
                depositor.window_start = now;
                depositor.withdrawn_in_window = 0;
            }
            if depositor.withdrawn_in_window + usdc_out > cfg.max_withdraw_per_24h {
                return Err(VaultError::WithdrawalRateLimited);
            }
            depositor.withdrawn_in_window += usdc_out;
        }

        // Effects before interaction (CEI, VLT-6 defense-in-depth): commit the
        // share burn and state before moving tokens out.
        depositor.shares -= shares;
        env.storage().persistent().set(&depositor_key, &depositor);
        env.storage()
            .persistent()
            .extend_ttl(&depositor_key, 535680, 535680);

        state.total_usdc -= usdc_out;
        state.total_shares -= shares;
        env.storage().instance().set(&VaultKey::State, &state);

        let usdc: Address = env
            .storage()
            .instance()
            .get(&VaultKey::Usdc)
            .ok_or(VaultError::NotInit)?;
        token::Client::new(&env, &usdc).transfer(&env.current_contract_address(), &user, &usdc_out);

        env.events().publish(
            (Symbol::new(&env, "withdraw"), user.clone()),
            (shares, usdc_out),
        );
        Ok(usdc_out)
    }

    /// Returns depositor's shares and their current USDC value.
    pub fn balance(env: Env, user: Address) -> (i128, i128) {
        env.storage().instance().extend_ttl(1000, 1000);
        let depositor_key = VaultKey::Depositor(user);
        let depositor = env
            .storage()
            .persistent()
            .get::<VaultKey, Depositor>(&depositor_key);
        let depositor = match depositor {
            Some(d) => {
                env.storage()
                    .persistent()
                    .extend_ttl(&depositor_key, 535680, 535680);
                d
            }
            None => return (0, 0),
        };
        let state: VaultState = match env.storage().instance().get(&VaultKey::State) {
            Some(s) => s,
            None => return (0, 0),
        };
        let usdc_val = to_assets(depositor.shares, state.total_shares, state.total_usdc);
        (depositor.shares, usdc_val)
    }

    /// Keeper draws capital for a liquidation. Verified against registry.
    ///
    /// `asset` is the collateral asset this draw targets, declared by the
    /// keeper (DECISION F-2a): the vault checks the global liquidation pause
    /// and the per-asset pause on-chain, so honest keepers are hard-gated.
    /// Threat-model limitation: the declaration is not verifiable on-chain —
    /// a malicious keeper could misdeclare to route around a per-asset pause;
    /// staking/slashing is the backstop for that, and the GLOBAL pause blocks
    /// every draw regardless of declaration.
    pub fn draw(env: Env, keeper: Address, amount: i128, asset: Address) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_init(&env)?;
        require_not_paused(&env)?;
        require_liq_allowed(&env, &asset)?;
        keeper.require_auth();

        // Positive amounts only (freeze-review finding): a zero-amount draw
        // would re-stamp DrawInfo.since WITHOUT the matching registry
        // mark_draw (that call is amount-gated below), letting a keeper walk
        // the vault-side timestamp away from the registry's last_draw_time —
        // exactly the desync F4 exists to prevent. A negative amount would
        // shrink the recorded debt with only the SAC transfer trap as a
        // backstop. Both are rejected outright; draw(0) has no legitimate use
        // (the keeper client already refuses it).
        if amount <= 0 {
            return Err(VaultError::InvalidAmount);
        }

        let cfg: VaultConfig = env
            .storage()
            .instance()
            .get(&VaultKey::VaultConfig)
            .ok_or(VaultError::NotInit)?;

        let mut state: VaultState = env
            .storage()
            .instance()
            .get(&VaultKey::State)
            .ok_or(VaultError::NotInit)?;
        let available = state.total_usdc - state.active_liq;
        if amount > available {
            return Err(VaultError::InsufficientVault);
        }

        // The per-keeper cap bounds CUMULATIVE outstanding draw, not just this
        // call — otherwise a keeper could loop draw() up to the whole available
        // pool and blow past the intended exposure cap (NEW-cap, High).
        let draw_key = VaultKey::KeeperDraw(keeper.clone());
        let prev: i128 = env
            .storage()
            .persistent()
            .get::<VaultKey, DrawInfo>(&draw_key)
            .map(|d| d.amount)
            .unwrap_or(0);
        if cfg.max_draw_per_keeper > 0 && prev + amount > cfg.max_draw_per_keeper {
            return Err(VaultError::DrawLimitExceeded);
        }

        require_registered_keeper(&env, &keeper)?;

        // Effects before interaction (CEI, VLT-6): commit the outstanding draw
        // and active liability before transferring capital out. The record is
        // stamped with the draw's ledger timestamp (F4): a restarted keeper can
        // then distinguish its own stale draw's age from chain state alone, and
        // anyone can compute slash eligibility consistently from vault +
        // registry state (`since` mirrors the registry's last_draw_time — the
        // most recent draw, like mark_draw).
        env.storage().persistent().set(
            &draw_key,
            &DrawInfo {
                amount: prev + amount,
                since: env.ledger().timestamp(),
            },
        );
        env.storage()
            .persistent()
            .extend_ttl(&draw_key, 535680, 535680);
        state.active_liq += amount;
        env.storage().instance().set(&VaultKey::State, &state);

        let usdc: Address = env
            .storage()
            .instance()
            .get(&VaultKey::Usdc)
            .ok_or(VaultError::NotInit)?;
        token::Client::new(&env, &usdc).transfer(&env.current_contract_address(), &keeper, &amount);

        // Notify the registry that this keeper now has an outstanding draw.
        // Unconditional: amount > 0 is guaranteed above, so the vault-side
        // DrawInfo.since and the registry's last_draw_time always move
        // together (F4 consistency invariant).
        registry_call(&env, "mark_draw", &keeper)?;

        env.events()
            .publish((Symbol::new(&env, "draw"), keeper.clone()), (amount, asset));
        Ok(())
    }

    /// Keeper returns capital + profit after a successful liquidation.
    ///
    /// `response_time_ms` is the keeper-observed elapsed time from draw to
    /// fill+return — forwarded to the registry to build the per-keeper
    /// average response-time metric.
    pub fn return_proceeds(
        env: Env,
        keeper: Address,
        amount: i128,
        response_time_ms: u64,
    ) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_init(&env)?;
        keeper.require_auth();

        // Reject a return with no outstanding draw: only a keeper that has drawn
        // may return proceeds. This kills the anonymous donation-as-profit path
        // (VLT-2) — no one can inflate the share price by "returning" funds they
        // never drew.
        let draw_key = VaultKey::KeeperDraw(keeper.clone());
        let draw_rec: DrawInfo = env
            .storage()
            .persistent()
            .get(&draw_key)
            .unwrap_or(DrawInfo {
                amount: 0,
                since: 0,
            });
        let drawn: i128 = draw_rec.amount;
        if drawn <= 0 {
            return Err(VaultError::NoDraw);
        }

        let usdc: Address = env
            .storage()
            .instance()
            .get(&VaultKey::Usdc)
            .ok_or(VaultError::NotInit)?;
        token::Client::new(&env, &usdc).transfer(&keeper, &env.current_contract_address(), &amount);

        let mut state: VaultState = env
            .storage()
            .instance()
            .get(&VaultKey::State)
            .ok_or(VaultError::NotInit)?;
        // Principal repaid this call is capped at what THIS keeper owes, so a
        // profitable return never deducts another keeper's outstanding draw
        // from active_liq.
        let repay_target = if amount < drawn { amount } else { drawn };
        let repay = if repay_target < state.active_liq {
            repay_target
        } else {
            state.active_liq
        };
        let profit = amount - repay_target;

        state.active_liq -= repay;
        state.total_usdc += profit;
        state.total_profit += profit;
        env.storage().instance().set(&VaultKey::State, &state);

        if drawn > 0 {
            let remaining = drawn - repay_target;
            if remaining > 0 {
                // Partial repayment: the keeper still owes the remainder. Keep
                // the draw record (and the registry's active-draw mark) so the
                // shortfall stays slash-eligible — a 1-stroop return must not
                // settle a 10k draw. `since` is preserved: the outstanding
                // remainder dates from the draw that created it, so a partial
                // return cannot push out slash eligibility.
                env.storage().persistent().set(
                    &draw_key,
                    &DrawInfo {
                        amount: remaining,
                        since: draw_rec.since,
                    },
                );
                env.storage()
                    .persistent()
                    .extend_ttl(&draw_key, 535680, 535680);
            } else {
                // Fully settled: clear the draw and book the execution.
                env.storage().persistent().remove(&draw_key);
                registry_call(&env, "clear_draw", &keeper)?;
                registry_record_execution(&env, &keeper, true, profit, response_time_ms)?;
            }
        }

        env.events().publish(
            (Symbol::new(&env, "return"), keeper.clone()),
            (amount, profit),
        );
        Ok(())
    }

    /// Donated profit from a REGISTERED keeper (T3, DECISION F-1a). Credits
    /// float-funded profit — e.g. unwound bad-debt LP proceeds — to depositors
    /// without an outstanding draw: exactly the path the VLT-2 `NoDraw` guard
    /// (deliberately) blocks for anonymous callers in `return_proceeds`.
    ///
    /// Gate: the registry's `verify_keeper` (registered + active + stake >=
    /// min_stake, and the registry enforces min_stake > 0 in its constructor
    /// and set_config), so every donor is a bonded, slashable operator — not
    /// an anonymous donor. VLT-1 virtual-offset share math keeps donation-
    /// based share-price inflation unprofitable regardless.
    ///
    /// Effect: total_usdc and total_profit rise; NO shares are minted and
    /// draw accounting, cooldowns and rate-limit windows are untouched — the
    /// share price rises for existing holders only. Emits `profit_added`
    /// (NOT the `return` event) so donated profit stays separable from
    /// draw-cycle profit in accounting. Not pause-gated, like
    /// return_proceeds: money in is always safe.
    pub fn add_profit(env: Env, keeper: Address, amount: i128) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_init(&env)?;
        keeper.require_auth();

        if amount <= 0 {
            return Err(VaultError::InvalidAmount);
        }
        require_registered_keeper(&env, &keeper)?;

        let usdc: Address = env
            .storage()
            .instance()
            .get(&VaultKey::Usdc)
            .ok_or(VaultError::NotInit)?;
        token::Client::new(&env, &usdc).transfer(&keeper, &env.current_contract_address(), &amount);

        let mut state: VaultState = env
            .storage()
            .instance()
            .get(&VaultKey::State)
            .ok_or(VaultError::NotInit)?;
        state.total_usdc += amount;
        state.total_profit += amount;
        env.storage().instance().set(&VaultKey::State, &state);

        env.events()
            .publish((Symbol::new(&env, "profit_added"), keeper.clone()), amount);
        Ok(())
    }

    pub fn get_state(env: Env) -> Result<VaultState, VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        env.storage()
            .instance()
            .get(&VaultKey::State)
            .ok_or(VaultError::NotInit)
    }

    pub fn get_config(env: Env) -> Result<VaultConfig, VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        env.storage()
            .instance()
            .get(&VaultKey::VaultConfig)
            .ok_or(VaultError::NotInit)
    }

    pub fn set_config(env: Env, admin: Address, config: VaultConfig) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        let stored: Address = env
            .storage()
            .instance()
            .get(&VaultKey::Admin)
            .ok_or(VaultError::NotInit)?;
        if stored != admin {
            return Err(VaultError::Unauthorized);
        }
        admin.require_auth();
        env.storage()
            .instance()
            .set(&VaultKey::VaultConfig, &config);
        Ok(())
    }

    /// Emergency stop. Blocks deposit + draw (paths that increase exposure);
    /// withdraw and return_proceeds stay open so depositors can always exit and
    /// keepers can settle outstanding draws while paused (VLT-4).
    pub fn pause(env: Env, admin: Address) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_admin(&env, &admin)?;
        env.storage().instance().set(&VaultKey::Paused, &true);
        Ok(())
    }

    pub fn unpause(env: Env, admin: Address) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_admin(&env, &admin)?;
        env.storage().instance().remove(&VaultKey::Paused);
        Ok(())
    }

    pub fn is_paused(env: Env) -> bool {
        env.storage()
            .instance()
            .get::<VaultKey, bool>(&VaultKey::Paused)
            .unwrap_or(false)
    }

    /// Circuit breaker, global switch (T3, DECISION F-2a): pause/resume ALL
    /// liquidation draws. Blocks draw() only — deposit stays governed by the
    /// VLT-4 emergency pause, and withdraw is NEVER blocked by this flag, so
    /// depositors can always exit during a liquidation pause.
    pub fn set_global_pause(env: Env, admin: Address, paused: bool) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_admin(&env, &admin)?;
        if paused {
            env.storage()
                .instance()
                .set(&VaultKey::GlobalLiqPause, &true);
        } else {
            env.storage().instance().remove(&VaultKey::GlobalLiqPause);
        }
        env.events()
            .publish((Symbol::new(&env, "liq_pause"),), paused);
        Ok(())
    }

    /// Circuit breaker, per-asset switch (T3, DECISION F-2a): pause/resume
    /// draws that declare `asset` as their target collateral. Same scope rules
    /// as the global switch — draw() only, never depositor exit.
    pub fn set_asset_pause(
        env: Env,
        admin: Address,
        asset: Address,
        paused: bool,
    ) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_admin(&env, &admin)?;
        let key = VaultKey::AssetPaused(asset.clone());
        if paused {
            env.storage().instance().set(&key, &true);
        } else {
            env.storage().instance().remove(&key);
        }
        env.events()
            .publish((Symbol::new(&env, "asset_pause"), asset), paused);
        Ok(())
    }

    pub fn is_global_liq_paused(env: Env) -> bool {
        env.storage()
            .instance()
            .get::<VaultKey, bool>(&VaultKey::GlobalLiqPause)
            .unwrap_or(false)
    }

    pub fn is_asset_paused(env: Env, asset: Address) -> bool {
        env.storage()
            .instance()
            .get::<VaultKey, bool>(&VaultKey::AssetPaused(asset))
            .unwrap_or(false)
    }

    /// Registry-only. Called by `KeeperRegistry.slash` after a defaulted draw is
    /// slashed: writes the unrecovered principal off the pool, books the
    /// recovered slash amount (already transferred in by the registry), and
    /// clears the keeper's outstanding draw so vault and registry never drift
    /// (NEW-slash-reconcile). `recovered` is the slash amount credited to the
    /// vault; the pool absorbs `outstanding - recovered` as a loss.
    pub fn reconcile_default(
        env: Env,
        caller: Address,
        keeper: Address,
        recovered: i128,
    ) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_init(&env)?;
        require_registry(&env, &caller)?;

        let draw_key = VaultKey::KeeperDraw(keeper.clone());
        let outstanding: i128 = env
            .storage()
            .persistent()
            .get::<VaultKey, DrawInfo>(&draw_key)
            .map(|d| d.amount)
            .unwrap_or(0);
        if outstanding <= 0 {
            return Ok(());
        }

        let mut state: VaultState = env
            .storage()
            .instance()
            .get(&VaultKey::State)
            .ok_or(VaultError::NotInit)?;

        // The draw is resolved (as a loss): remove it from active liabilities.
        let clear = if outstanding < state.active_liq {
            outstanding
        } else {
            state.active_liq
        };
        state.active_liq -= clear;

        // Book the net loss: the pool loses the unrecovered principal and gains
        // the recovered slash amount. total_usdc counted the drawn capital as
        // owned; write it down by the loss so share value reflects real backing.
        //
        // Clamp at zero (freeze-review finding, sibling of the withdraw clamp
        // SCOUT-total_usdc-underflow): a donation-assisted full withdrawal
        // while capital is drawn can leave total_usdc < active_liq, and an
        // unclamped write-down would then persist a NEGATIVE total_usdc —
        // poisoning the share math for every later depositor. total_profit is
        // a cumulative P&L stat and may legitimately go negative.
        let loss = outstanding - recovered;
        state.total_usdc = (state.total_usdc - loss).max(0);
        state.total_profit -= loss;
        env.storage().instance().set(&VaultKey::State, &state);

        env.storage().persistent().remove(&draw_key);

        env.events().publish(
            (Symbol::new(&env, "write_off"), keeper.clone()),
            (outstanding, recovered),
        );
        Ok(())
    }

    pub fn get_depositor(env: Env, user: Address) -> Result<Depositor, VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        env.storage()
            .persistent()
            .get(&VaultKey::Depositor(user))
            .ok_or(VaultError::NoShares)
    }

    /// Outstanding draw for `keeper` as `(amount, since)` — the capital drawn
    /// but not yet returned, and the ledger timestamp of the most recent draw
    /// (`(0, 0)` if none). The amount lets an off-chain keeper cap any
    /// self-recovery return at what it owes; the timestamp (F4) lets a
    /// restarted keeper age its own stale draw from chain state alone, and
    /// lets anyone compute slash eligibility consistently from vault +
    /// registry state.
    pub fn get_keeper_draw(env: Env, keeper: Address) -> (i128, u64) {
        env.storage().instance().extend_ttl(1000, 1000);
        env.storage()
            .persistent()
            .get::<VaultKey, DrawInfo>(&VaultKey::KeeperDraw(keeper))
            .map(|d| (d.amount, d.since))
            .unwrap_or((0, 0))
    }
}

fn require_init(env: &Env) -> Result<(), VaultError> {
    if !env.storage().instance().has(&VaultKey::Admin) {
        return Err(VaultError::NotInit);
    }
    Ok(())
}

fn require_admin(env: &Env, admin: &Address) -> Result<(), VaultError> {
    let stored: Address = env
        .storage()
        .instance()
        .get(&VaultKey::Admin)
        .ok_or(VaultError::NotInit)?;
    if stored != *admin {
        return Err(VaultError::Unauthorized);
    }
    admin.require_auth();
    Ok(())
}

fn require_not_paused(env: &Env) -> Result<(), VaultError> {
    if env
        .storage()
        .instance()
        .get::<VaultKey, bool>(&VaultKey::Paused)
        .unwrap_or(false)
    {
        return Err(VaultError::Paused);
    }
    Ok(())
}

/// Circuit-breaker gate for draw() only (DECISION F-2a): global liquidation
/// pause first, then the per-asset flag for the keeper-declared asset.
fn require_liq_allowed(env: &Env, asset: &Address) -> Result<(), VaultError> {
    if env
        .storage()
        .instance()
        .get::<VaultKey, bool>(&VaultKey::GlobalLiqPause)
        .unwrap_or(false)
    {
        return Err(VaultError::LiquidationsPaused);
    }
    if env
        .storage()
        .instance()
        .get::<VaultKey, bool>(&VaultKey::AssetPaused(asset.clone()))
        .unwrap_or(false)
    {
        return Err(VaultError::AssetPaused);
    }
    Ok(())
}

fn require_registry(env: &Env, caller: &Address) -> Result<(), VaultError> {
    let stored: Address = env
        .storage()
        .instance()
        .get(&VaultKey::KeeperRegistry)
        .ok_or(VaultError::NotInit)?;
    if stored != *caller {
        return Err(VaultError::Unauthorized);
    }
    caller.require_auth();
    Ok(())
}

fn require_registered_keeper(env: &Env, keeper: &Address) -> Result<(), VaultError> {
    let registry: Address = env
        .storage()
        .instance()
        .get(&VaultKey::KeeperRegistry)
        .ok_or(VaultError::NotInit)?;
    // verify_keeper traps (reverting draw) if the keeper is unregistered OR
    // inactive — an explicit active check, not just record existence (VLT-3).
    let _: soroban_sdk::Val = env.invoke_contract(
        &registry,
        &Symbol::new(env, "verify_keeper"),
        soroban_sdk::vec![env, keeper.to_val()],
    );
    Ok(())
}

fn registry_call(env: &Env, fn_name: &str, keeper: &Address) -> Result<(), VaultError> {
    let registry: Address = env
        .storage()
        .instance()
        .get(&VaultKey::KeeperRegistry)
        .ok_or(VaultError::NotInit)?;
    let vault = env.current_contract_address();
    let _: soroban_sdk::Val = env.invoke_contract(
        &registry,
        &Symbol::new(env, fn_name),
        vec![env, vault.into_val(env), keeper.into_val(env)],
    );
    Ok(())
}

fn registry_record_execution(
    env: &Env,
    keeper: &Address,
    success: bool,
    profit: i128,
    response_time_ms: u64,
) -> Result<(), VaultError> {
    let registry: Address = env
        .storage()
        .instance()
        .get(&VaultKey::KeeperRegistry)
        .ok_or(VaultError::NotInit)?;
    let vault = env.current_contract_address();
    let _: soroban_sdk::Val = env.invoke_contract(
        &registry,
        &Symbol::new(env, "record_execution"),
        vec![
            env,
            vault.into_val(env),
            keeper.into_val(env),
            success.into_val(env),
            profit.into_val(env),
            response_time_ms.into_val(env),
        ],
    );
    Ok(())
}

#[cfg(test)]
mod test;

#[cfg(test)]
mod prop_test;
