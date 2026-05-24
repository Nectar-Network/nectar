#![no_std]

mod types;
pub use types::{DataKey, Error, KeeperInfo, RegistryConfig};

#[cfg(test)]
mod test;

use soroban_sdk::{contract, contractimpl, token, vec, Address, Env, String, Symbol, Vec};

#[contract]
pub struct KeeperRegistry;

#[contractimpl]
impl KeeperRegistry {
    pub fn initialize(
        env: Env,
        admin: Address,
        config: RegistryConfig,
        vault: Address,
    ) -> Result<(), Error> {
        let store = env.storage().instance();
        if store.has(&DataKey::Admin) {
            return Err(Error::AlreadyInit);
        }
        store.set(&DataKey::Admin, &admin);
        store.set(&DataKey::KeeperCount, &0u32);
        store.set(&DataKey::Config, &config);
        store.set(&DataKey::VaultAddr, &vault);
        store.extend_ttl(1000, 1000);
        Ok(())
    }

    pub fn register(env: Env, operator: Address, name: String) -> Result<(), Error> {
        env.storage().instance().extend_ttl(1000, 1000);

        let store = env.storage().instance();
        if !store.has(&DataKey::Admin) {
            return Err(Error::NotInit);
        }
        if store
            .get::<DataKey, bool>(&DataKey::Paused)
            .unwrap_or(false)
        {
            return Err(Error::Paused);
        }

        operator.require_auth();

        let pstore = env.storage().persistent();
        if pstore.has(&DataKey::Keeper(operator.clone())) {
            return Err(Error::AlreadyRegistered);
        }

        let cfg: RegistryConfig = store.get(&DataKey::Config).ok_or(Error::NotInit)?;
        if cfg.min_stake <= 0 {
            return Err(Error::InsufficientStake);
        }

        // Pull stake from operator into the registry contract.
        token::Client::new(&env, &cfg.usdc_token).transfer(
            &operator,
            &env.current_contract_address(),
            &cfg.min_stake,
        );

        let info = KeeperInfo {
            addr: operator.clone(),
            name: name.clone(),
            stake: cfg.min_stake,
            registered_at: env.ledger().timestamp(),
            active: true,
            total_executions: 0,
            successful_fills: 0,
            total_profit: 0,
            last_draw_time: 0,
            has_active_draw: false,
            total_response_time_ms: 0,
            response_count: 0,
        };

        pstore.set(&DataKey::Keeper(operator.clone()), &info);
        pstore.extend_ttl(&DataKey::Keeper(operator.clone()), 535680, 535680);

        let mut list: Vec<Address> = pstore
            .get(&DataKey::KeeperList)
            .unwrap_or_else(|| vec![&env]);
        list.push_back(operator.clone());
        pstore.set(&DataKey::KeeperList, &list);
        pstore.extend_ttl(&DataKey::KeeperList, 535680, 535680);

        let count: u32 = store.get(&DataKey::KeeperCount).unwrap_or(0);
        store.set(&DataKey::KeeperCount, &(count + 1));

        env.events().publish(
            (Symbol::new(&env, "registered"), operator.clone()),
            (name, cfg.min_stake, env.ledger().timestamp()),
        );

        Ok(())
    }

    pub fn deregister(env: Env, operator: Address) -> Result<(), Error> {
        env.storage().instance().extend_ttl(1000, 1000);

        let store = env.storage().instance();
        if !store.has(&DataKey::Admin) {
            return Err(Error::NotInit);
        }

        operator.require_auth();

        let pstore = env.storage().persistent();
        let info: KeeperInfo = pstore
            .get(&DataKey::Keeper(operator.clone()))
            .ok_or(Error::NotRegistered)?;

        if info.has_active_draw {
            return Err(Error::ActiveDraw);
        }

        // Refund the stake before removing the keeper record.
        if info.stake > 0 {
            let cfg: RegistryConfig = store.get(&DataKey::Config).ok_or(Error::NotInit)?;
            token::Client::new(&env, &cfg.usdc_token).transfer(
                &env.current_contract_address(),
                &operator,
                &info.stake,
            );
        }

        pstore.remove(&DataKey::Keeper(operator.clone()));

        let list: Vec<Address> = pstore
            .get(&DataKey::KeeperList)
            .unwrap_or_else(|| vec![&env]);
        let mut updated: Vec<Address> = vec![&env];
        for a in list.iter() {
            if a != operator {
                updated.push_back(a);
            }
        }
        pstore.set(&DataKey::KeeperList, &updated);
        pstore.extend_ttl(&DataKey::KeeperList, 535680, 535680);

        let count: u32 = store.get(&DataKey::KeeperCount).unwrap_or(1);
        store.set(&DataKey::KeeperCount, &count.saturating_sub(1));

        env.events().publish(
            (Symbol::new(&env, "deregistered"), operator.clone()),
            (info.stake, env.ledger().timestamp()),
        );

        Ok(())
    }

    pub fn get_keeper(env: Env, operator: Address) -> Result<KeeperInfo, Error> {
        env.storage().instance().extend_ttl(1000, 1000);
        env.storage()
            .persistent()
            .get(&DataKey::Keeper(operator))
            .ok_or(Error::NotRegistered)
    }

    pub fn avg_response_time_ms(env: Env, operator: Address) -> Result<u64, Error> {
        env.storage().instance().extend_ttl(1000, 1000);
        let info: KeeperInfo = env
            .storage()
            .persistent()
            .get(&DataKey::Keeper(operator))
            .ok_or(Error::NotRegistered)?;
        if info.response_count == 0 {
            Ok(0)
        } else {
            Ok(info.total_response_time_ms / info.response_count)
        }
    }

    pub fn get_keepers(env: Env) -> Vec<Address> {
        env.storage().instance().extend_ttl(1000, 1000);
        env.storage()
            .persistent()
            .get(&DataKey::KeeperList)
            .unwrap_or_else(|| vec![&env])
    }

    pub fn keeper_count(env: Env) -> u32 {
        env.storage().instance().extend_ttl(1000, 1000);
        env.storage()
            .instance()
            .get(&DataKey::KeeperCount)
            .unwrap_or(0)
    }

    pub fn pause(env: Env, admin: Address) -> Result<(), Error> {
        env.storage().instance().extend_ttl(1000, 1000);
        Self::require_admin(&env, &admin)?;
        env.storage().instance().set(&DataKey::Paused, &true);
        Ok(())
    }

    pub fn unpause(env: Env, admin: Address) -> Result<(), Error> {
        env.storage().instance().extend_ttl(1000, 1000);
        Self::require_admin(&env, &admin)?;
        env.storage().instance().remove(&DataKey::Paused);
        Ok(())
    }

    pub fn mark_draw(env: Env, caller: Address, keeper: Address) -> Result<(), Error> {
        env.storage().instance().extend_ttl(1000, 1000);
        Self::require_vault(&env, &caller)?;

        let pstore = env.storage().persistent();
        let mut info: KeeperInfo = pstore
            .get(&DataKey::Keeper(keeper.clone()))
            .ok_or(Error::NotRegistered)?;

        info.has_active_draw = true;
        info.last_draw_time = env.ledger().timestamp();
        pstore.set(&DataKey::Keeper(keeper.clone()), &info);
        pstore.extend_ttl(&DataKey::Keeper(keeper.clone()), 535680, 535680);

        env.events().publish(
            (Symbol::new(&env, "draw_marked"), keeper),
            info.last_draw_time,
        );
        Ok(())
    }

    pub fn clear_draw(env: Env, caller: Address, keeper: Address) -> Result<(), Error> {
        env.storage().instance().extend_ttl(1000, 1000);
        Self::require_vault(&env, &caller)?;

        let pstore = env.storage().persistent();
        let mut info: KeeperInfo = pstore
            .get(&DataKey::Keeper(keeper.clone()))
            .ok_or(Error::NotRegistered)?;

        info.has_active_draw = false;
        pstore.set(&DataKey::Keeper(keeper.clone()), &info);
        pstore.extend_ttl(&DataKey::Keeper(keeper.clone()), 535680, 535680);

        env.events()
            .publish((Symbol::new(&env, "draw_cleared"), keeper), ());
        Ok(())
    }

    pub fn record_execution(
        env: Env,
        caller: Address,
        keeper: Address,
        success: bool,
        profit: i128,
        response_time_ms: u64,
    ) -> Result<(), Error> {
        env.storage().instance().extend_ttl(1000, 1000);
        Self::require_vault(&env, &caller)?;

        let pstore = env.storage().persistent();
        let mut info: KeeperInfo = pstore
            .get(&DataKey::Keeper(keeper.clone()))
            .ok_or(Error::NotRegistered)?;

        info.total_executions = info.total_executions.saturating_add(1);
        if success {
            info.successful_fills = info.successful_fills.saturating_add(1);
            info.total_profit = info.total_profit.saturating_add(profit);
            info.total_response_time_ms = info
                .total_response_time_ms
                .saturating_add(response_time_ms);
            info.response_count = info.response_count.saturating_add(1);
        }

        pstore.set(&DataKey::Keeper(keeper.clone()), &info);
        pstore.extend_ttl(&DataKey::Keeper(keeper.clone()), 535680, 535680);

        env.events().publish(
            (Symbol::new(&env, "execution"), keeper),
            (success, profit, info.total_executions, response_time_ms),
        );
        Ok(())
    }

    pub fn slash(env: Env, keeper: Address) -> Result<i128, Error> {
        env.storage().instance().extend_ttl(1000, 1000);

        let store = env.storage().instance();
        let cfg: RegistryConfig = store.get(&DataKey::Config).ok_or(Error::NotInit)?;
        let vault: Address = store.get(&DataKey::VaultAddr).ok_or(Error::NotInit)?;

        let pstore = env.storage().persistent();
        let mut info: KeeperInfo = pstore
            .get(&DataKey::Keeper(keeper.clone()))
            .ok_or(Error::NotRegistered)?;

        if !info.has_active_draw {
            return Err(Error::SlashTimeout);
        }

        let now = env.ledger().timestamp();
        if now.saturating_sub(info.last_draw_time) <= cfg.slash_timeout {
            return Err(Error::SlashTimeout);
        }

        let slash_amt: i128 = info.stake * (cfg.slash_rate_bps as i128) / 10_000;

        if slash_amt > 0 {
            token::Client::new(&env, &cfg.usdc_token).transfer(
                &env.current_contract_address(),
                &vault,
                &slash_amt,
            );
            info.stake -= slash_amt;
        }
        info.has_active_draw = false;

        pstore.set(&DataKey::Keeper(keeper.clone()), &info);
        pstore.extend_ttl(&DataKey::Keeper(keeper.clone()), 535680, 535680);

        env.events().publish(
            (Symbol::new(&env, "slashed"), keeper),
            (slash_amt, info.stake),
        );

        Ok(slash_amt)
    }

    pub fn set_config(env: Env, admin: Address, config: RegistryConfig) -> Result<(), Error> {
        env.storage().instance().extend_ttl(1000, 1000);
        Self::require_admin(&env, &admin)?;
        env.storage().instance().set(&DataKey::Config, &config);
        Ok(())
    }

    pub fn get_config(env: Env) -> Result<RegistryConfig, Error> {
        env.storage().instance().extend_ttl(1000, 1000);
        env.storage()
            .instance()
            .get(&DataKey::Config)
            .ok_or(Error::NotInit)
    }

    fn require_admin(env: &Env, caller: &Address) -> Result<(), Error> {
        let stored: Address = env
            .storage()
            .instance()
            .get(&DataKey::Admin)
            .ok_or(Error::NotInit)?;
        if stored != *caller {
            return Err(Error::Unauthorized);
        }
        caller.require_auth();
        Ok(())
    }

    fn require_vault(env: &Env, caller: &Address) -> Result<(), Error> {
        let stored: Address = env
            .storage()
            .instance()
            .get(&DataKey::VaultAddr)
            .ok_or(Error::NotInit)?;
        if stored != *caller {
            return Err(Error::Unauthorized);
        }
        caller.require_auth();
        Ok(())
    }
}
