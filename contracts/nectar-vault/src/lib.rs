#![no_std]

mod types;

use soroban_sdk::{contract, contractimpl, token, vec, Address, Env, IntoVal, Symbol};
use types::{Depositor, VaultConfig, VaultError, VaultKey, VaultState};

#[contract]
pub struct NectarVault;

#[contractimpl]
impl NectarVault {
    pub fn initialize(
        env: Env,
        admin: Address,
        usdc_token: Address,
        registry: Address,
        config: VaultConfig,
    ) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        if env.storage().instance().has(&VaultKey::Admin) {
            return Err(VaultError::AlreadyInit);
        }
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
        Ok(())
    }

    /// Deposit USDC into the vault and receive shares proportional to current ratio.
    pub fn deposit(env: Env, user: Address, amount: i128) -> Result<i128, VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_init(&env)?;
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

        // Integer division floors toward zero — depositor gets at most the
        // proportional shares, never more, which protects existing depositors.
        let shares = if state.total_shares == 0 {
            amount
        } else {
            amount * state.total_shares / state.total_usdc
        };

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

        // Integer division floors toward zero — withdrawer gets at most the
        // proportional USDC, never more, which protects the pool. When the
        // depositor empties the vault (their shares == total_shares), this
        // formula naturally returns the full state.total_usdc.
        let usdc_out = shares * state.total_usdc / state.total_shares;

        let usdc: Address = env
            .storage()
            .instance()
            .get(&VaultKey::Usdc)
            .ok_or(VaultError::NotInit)?;
        token::Client::new(&env, &usdc).transfer(&env.current_contract_address(), &user, &usdc_out);

        depositor.shares -= shares;
        env.storage().persistent().set(&depositor_key, &depositor);
        env.storage()
            .persistent()
            .extend_ttl(&depositor_key, 535680, 535680);

        state.total_usdc -= usdc_out;
        state.total_shares -= shares;
        env.storage().instance().set(&VaultKey::State, &state);

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
        if state.total_shares == 0 {
            return (depositor.shares, 0);
        }
        let usdc_val = depositor.shares * state.total_usdc / state.total_shares;
        (depositor.shares, usdc_val)
    }

    /// Keeper draws capital for a liquidation. Verified against registry.
    pub fn draw(env: Env, keeper: Address, amount: i128) -> Result<(), VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        require_init(&env)?;
        keeper.require_auth();

        let cfg: VaultConfig = env
            .storage()
            .instance()
            .get(&VaultKey::VaultConfig)
            .ok_or(VaultError::NotInit)?;
        if cfg.max_draw_per_keeper > 0 && amount > cfg.max_draw_per_keeper {
            return Err(VaultError::DrawLimitExceeded);
        }

        let mut state: VaultState = env
            .storage()
            .instance()
            .get(&VaultKey::State)
            .ok_or(VaultError::NotInit)?;
        let available = state.total_usdc - state.active_liq;
        if amount > available {
            return Err(VaultError::InsufficientVault);
        }
        require_registered_keeper(&env, &keeper)?;

        let usdc: Address = env
            .storage()
            .instance()
            .get(&VaultKey::Usdc)
            .ok_or(VaultError::NotInit)?;
        token::Client::new(&env, &usdc).transfer(&env.current_contract_address(), &keeper, &amount);

        // Track per-keeper outstanding draw so return_proceeds can compute profit.
        let draw_key = VaultKey::KeeperDraw(keeper.clone());
        let prev: i128 = env.storage().persistent().get(&draw_key).unwrap_or(0);
        env.storage().persistent().set(&draw_key, &(prev + amount));
        env.storage()
            .persistent()
            .extend_ttl(&draw_key, 535680, 535680);

        state.active_liq += amount;
        env.storage().instance().set(&VaultKey::State, &state);

        // Notify the registry that this keeper now has an outstanding draw.
        if amount > 0 {
            registry_call(&env, "mark_draw", &keeper)?;
        }

        env.events()
            .publish((Symbol::new(&env, "draw"), keeper.clone()), amount);
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

        let usdc: Address = env
            .storage()
            .instance()
            .get(&VaultKey::Usdc)
            .ok_or(VaultError::NotInit)?;
        token::Client::new(&env, &usdc).transfer(&keeper, &env.current_contract_address(), &amount);

        let draw_key = VaultKey::KeeperDraw(keeper.clone());
        let drawn: i128 = env.storage().persistent().get(&draw_key).unwrap_or(0);

        let mut state: VaultState = env
            .storage()
            .instance()
            .get(&VaultKey::State)
            .ok_or(VaultError::NotInit)?;
        let repay = if amount < state.active_liq {
            amount
        } else {
            state.active_liq
        };
        let profit = if drawn > 0 && amount > drawn {
            amount - drawn
        } else if drawn == 0 {
            // No prior draw tracked — treat the whole amount as donated profit.
            amount
        } else {
            0
        };

        state.active_liq -= repay;
        state.total_usdc += profit;
        state.total_profit += profit;
        env.storage().instance().set(&VaultKey::State, &state);

        // Clear per-keeper draw record.
        if drawn > 0 {
            env.storage().persistent().remove(&draw_key);
            registry_call(&env, "clear_draw", &keeper)?;
            registry_record_execution(&env, &keeper, true, profit, response_time_ms)?;
        }

        env.events().publish(
            (Symbol::new(&env, "return"), keeper.clone()),
            (amount, profit),
        );
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

    pub fn get_depositor(env: Env, user: Address) -> Result<Depositor, VaultError> {
        env.storage().instance().extend_ttl(1000, 1000);
        env.storage()
            .persistent()
            .get(&VaultKey::Depositor(user))
            .ok_or(VaultError::NoShares)
    }
}

fn require_init(env: &Env) -> Result<(), VaultError> {
    if !env.storage().instance().has(&VaultKey::Admin) {
        return Err(VaultError::NotInit);
    }
    Ok(())
}

fn require_registered_keeper(env: &Env, keeper: &Address) -> Result<(), VaultError> {
    let registry: Address = env
        .storage()
        .instance()
        .get(&VaultKey::KeeperRegistry)
        .ok_or(VaultError::NotInit)?;
    let _: soroban_sdk::Val = env.invoke_contract(
        &registry,
        &Symbol::new(env, "get_keeper"),
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
