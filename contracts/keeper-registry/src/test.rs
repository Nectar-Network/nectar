#[cfg(test)]
mod tests {
    use crate::{Error, KeeperRegistry, KeeperRegistryClient, RegistryConfig};
    use soroban_sdk::{
        testutils::{Address as _, Ledger, LedgerInfo},
        token, Address, Env, String,
    };

    const MIN_STAKE: i128 = 100_0000000; // 100 USDC
    const SLASH_TIMEOUT: u64 = 3600; // 1 hour
    const SLASH_RATE_BPS: u32 = 1000; // 10%
    const MINT_AMT: i128 = 1000_0000000; // 1000 USDC

    struct Setup<'a> {
        env: Env,
        client: KeeperRegistryClient<'a>,
        contract_id: Address,
        admin: Address,
        vault: Address,
        usdc: Address,
        usdc_client: token::Client<'a>,
    }

    fn setup<'a>() -> Setup<'a> {
        let env = Env::default();
        let contract_id = env.register(KeeperRegistry, ());
        let client = KeeperRegistryClient::new(&env, &contract_id);

        let admin = Address::generate(&env);
        let vault = Address::generate(&env);
        let usdc_admin = Address::generate(&env);
        let usdc_sac = env.register_stellar_asset_contract_v2(usdc_admin.clone());
        let usdc = usdc_sac.address();

        env.mock_all_auths();

        let cfg = RegistryConfig {
            min_stake: MIN_STAKE,
            slash_timeout: SLASH_TIMEOUT,
            slash_rate_bps: SLASH_RATE_BPS,
            usdc_token: usdc.clone(),
        };
        client.initialize(&admin, &cfg, &vault);

        let usdc_client = token::Client::new(&env, &usdc);

        Setup {
            env,
            client,
            contract_id,
            admin,
            vault,
            usdc,
            usdc_client,
        }
    }

    fn mint(s: &Setup, to: &Address, amt: i128) {
        let mint_client = token::StellarAssetClient::new(&s.env, &s.usdc);
        mint_client.mint(to, &amt);
    }

    fn make_op(s: &Setup) -> Address {
        let op = Address::generate(&s.env);
        mint(s, &op, MINT_AMT);
        op
    }

    // ---------- existing behaviour preserved ----------

    #[test]
    fn test_initialize() {
        let s = setup();
        assert_eq!(s.client.keeper_count(), 0);
        let cfg = s.client.get_config();
        assert_eq!(cfg.min_stake, MIN_STAKE);
        assert_eq!(cfg.slash_timeout, SLASH_TIMEOUT);
        assert_eq!(cfg.slash_rate_bps, SLASH_RATE_BPS);
        assert_eq!(cfg.usdc_token, s.usdc);
    }

    #[test]
    fn test_register_and_get() {
        let s = setup();
        let op = make_op(&s);
        s.client
            .register(&op, &String::from_str(&s.env, "keeper-alpha"));

        let info = s.client.get_keeper(&op);
        assert_eq!(info.name, String::from_str(&s.env, "keeper-alpha"));
        assert!(info.active);
        assert_eq!(info.addr, op);
        assert_eq!(info.stake, MIN_STAKE);
        assert_eq!(info.total_executions, 0);
        assert_eq!(info.successful_fills, 0);
        assert_eq!(info.total_profit, 0);
        assert!(!info.has_active_draw);
        assert_eq!(s.client.keeper_count(), 1);
    }

    #[test]
    fn test_deregister() {
        let s = setup();
        let op = make_op(&s);

        s.client
            .register(&op, &String::from_str(&s.env, "keeper-alpha"));
        assert_eq!(s.client.keeper_count(), 1);

        s.client.deregister(&op);
        assert_eq!(s.client.keeper_count(), 0);
        assert_eq!(s.client.get_keepers().len(), 0);
    }

    #[test]
    fn test_double_register_fails() {
        let s = setup();
        let op = make_op(&s);

        s.client
            .register(&op, &String::from_str(&s.env, "keeper-alpha"));
        let result = s
            .client
            .try_register(&op, &String::from_str(&s.env, "keeper-alpha"));
        assert_eq!(result, Err(Ok(Error::AlreadyRegistered)));
    }

    #[test]
    fn test_deregister_unregistered_fails() {
        let s = setup();
        let op = Address::generate(&s.env);
        let result = s.client.try_deregister(&op);
        assert_eq!(result, Err(Ok(Error::NotRegistered)));
    }

    #[test]
    fn test_pause_blocks_register() {
        let s = setup();
        let op = make_op(&s);
        s.client.pause(&s.admin);
        let result = s
            .client
            .try_register(&op, &String::from_str(&s.env, "test"));
        assert_eq!(result, Err(Ok(Error::Paused)));
    }

    #[test]
    fn test_unpause_allows_register() {
        let s = setup();
        let op = make_op(&s);
        s.client.pause(&s.admin);
        s.client.unpause(&s.admin);
        s.client
            .register(&op, &String::from_str(&s.env, "keeper-alpha"));
        assert_eq!(s.client.keeper_count(), 1);
    }

    #[test]
    fn test_get_keepers_list() {
        let s = setup();
        let op_a = make_op(&s);
        let op_b = make_op(&s);

        s.client
            .register(&op_a, &String::from_str(&s.env, "keeper-alpha"));
        s.client
            .register(&op_b, &String::from_str(&s.env, "keeper-beta"));

        let keepers = s.client.get_keepers();
        assert_eq!(keepers.len(), 2);

        s.client.deregister(&op_a);
        let keepers = s.client.get_keepers();
        assert_eq!(keepers.len(), 1);
        assert_eq!(keepers.get(0).unwrap(), op_b);
    }

    #[test]
    fn test_unauthorized_pause() {
        let env = Env::default();
        let contract_id = env.register(KeeperRegistry, ());
        let client = KeeperRegistryClient::new(&env, &contract_id);

        let admin = Address::generate(&env);
        let intruder = Address::generate(&env);
        let vault = Address::generate(&env);
        let usdc_admin = Address::generate(&env);
        let usdc = env.register_stellar_asset_contract_v2(usdc_admin).address();

        let cfg = RegistryConfig {
            min_stake: MIN_STAKE,
            slash_timeout: SLASH_TIMEOUT,
            slash_rate_bps: SLASH_RATE_BPS,
            usdc_token: usdc,
        };
        client.initialize(&admin, &cfg, &vault);
        // No mock_all_auths — admin mismatch should short-circuit before require_auth.
        let result = client.try_pause(&intruder);
        assert_eq!(result, Err(Ok(Error::Unauthorized)));
    }

    #[test]
    fn test_double_init_fails() {
        let s = setup();
        let admin2 = Address::generate(&s.env);
        let cfg = s.client.get_config();
        let result = s.client.try_initialize(&admin2, &cfg, &s.vault);
        assert_eq!(result, Err(Ok(Error::AlreadyInit)));
    }

    #[test]
    fn test_keeper_count_after_deregister() {
        let s = setup();
        let op_a = make_op(&s);
        let op_b = make_op(&s);
        let op_c = make_op(&s);

        s.client.register(&op_a, &String::from_str(&s.env, "alpha"));
        s.client.register(&op_b, &String::from_str(&s.env, "beta"));
        s.client.register(&op_c, &String::from_str(&s.env, "gamma"));
        assert_eq!(s.client.keeper_count(), 3);

        s.client.deregister(&op_b);
        assert_eq!(s.client.keeper_count(), 2);

        s.client.deregister(&op_a);
        assert_eq!(s.client.keeper_count(), 1);

        s.client.deregister(&op_c);
        assert_eq!(s.client.keeper_count(), 0);
    }

    #[test]
    fn test_name_persisted_correctly() {
        let s = setup();
        let op = make_op(&s);

        let name = String::from_str(&s.env, "my-keeper-node-01");
        s.client.register(&op, &name);

        let info = s.client.get_keeper(&op);
        assert_eq!(info.name, name);
        assert_eq!(info.addr, op);
        assert!(info.active);
    }

    // ---------- staking / performance / slashing ----------

    #[test]
    fn test_register_with_stake() {
        let s = setup();
        let op = make_op(&s);

        let bal_before = s.usdc_client.balance(&op);
        let registry_before = s.usdc_client.balance(&s.contract_id);

        s.client.register(&op, &String::from_str(&s.env, "alpha"));

        let bal_after = s.usdc_client.balance(&op);
        let registry_after = s.usdc_client.balance(&s.contract_id);

        assert_eq!(bal_before - bal_after, MIN_STAKE);
        assert_eq!(registry_after - registry_before, MIN_STAKE);

        let info = s.client.get_keeper(&op);
        assert_eq!(info.stake, MIN_STAKE);
    }

    #[test]
    fn test_register_insufficient_stake() {
        let s = setup();
        let op = Address::generate(&s.env);
        // Mint less than min_stake.
        mint(&s, &op, MIN_STAKE - 1);

        let result = s
            .client
            .try_register(&op, &String::from_str(&s.env, "alpha"));
        // Token transfer panics on insufficient balance — captured as Err(Err(...)).
        assert!(result.is_err());
        let r = s.client.try_get_keeper(&op);
        assert!(matches!(r, Err(Ok(Error::NotRegistered))));
    }

    #[test]
    fn test_register_zero_min_stake_rejected() {
        let env = Env::default();
        let contract_id = env.register(KeeperRegistry, ());
        let client = KeeperRegistryClient::new(&env, &contract_id);

        let admin = Address::generate(&env);
        let vault = Address::generate(&env);
        let usdc_admin = Address::generate(&env);
        let usdc = env.register_stellar_asset_contract_v2(usdc_admin).address();
        let cfg = RegistryConfig {
            min_stake: 0,
            slash_timeout: SLASH_TIMEOUT,
            slash_rate_bps: SLASH_RATE_BPS,
            usdc_token: usdc,
        };
        client.initialize(&admin, &cfg, &vault);
        env.mock_all_auths();

        let op = Address::generate(&env);
        let result = client.try_register(&op, &String::from_str(&env, "alpha"));
        assert_eq!(result, Err(Ok(Error::InsufficientStake)));
    }

    #[test]
    fn test_deregister_returns_stake() {
        let s = setup();
        let op = make_op(&s);

        s.client.register(&op, &String::from_str(&s.env, "alpha"));
        let bal_after_reg = s.usdc_client.balance(&op);

        s.client.deregister(&op);
        let bal_after_dereg = s.usdc_client.balance(&op);

        assert_eq!(bal_after_dereg - bal_after_reg, MIN_STAKE);
        assert_eq!(s.usdc_client.balance(&s.contract_id), 0);
    }

    #[test]
    fn test_deregister_with_active_draw_fails() {
        let s = setup();
        let op = make_op(&s);
        s.client.register(&op, &String::from_str(&s.env, "alpha"));

        s.client.mark_draw(&s.vault, &op);

        let result = s.client.try_deregister(&op);
        assert_eq!(result, Err(Ok(Error::ActiveDraw)));
    }

    #[test]
    fn test_mark_draw_sets_state() {
        let s = setup();
        let op = make_op(&s);
        s.client.register(&op, &String::from_str(&s.env, "alpha"));

        let now: u64 = 1_000_000;
        s.env.ledger().set(LedgerInfo {
            timestamp: now,
            protocol_version: 22,
            sequence_number: 1,
            network_id: Default::default(),
            base_reserve: 10,
            min_temp_entry_ttl: 16,
            min_persistent_entry_ttl: 4096,
            max_entry_ttl: 6_312_000,
        });

        s.client.mark_draw(&s.vault, &op);

        let info = s.client.get_keeper(&op);
        assert!(info.has_active_draw);
        assert_eq!(info.last_draw_time, now);
    }

    #[test]
    fn test_clear_draw_resets_state() {
        let s = setup();
        let op = make_op(&s);
        s.client.register(&op, &String::from_str(&s.env, "alpha"));
        s.client.mark_draw(&s.vault, &op);
        assert!(s.client.get_keeper(&op).has_active_draw);

        s.client.clear_draw(&s.vault, &op);
        assert!(!s.client.get_keeper(&op).has_active_draw);
    }

    #[test]
    fn test_record_execution_updates_counters() {
        let s = setup();
        let op = make_op(&s);
        s.client.register(&op, &String::from_str(&s.env, "alpha"));

        s.client
            .record_execution(&s.vault, &op, &true, &50_0000000, &150u64);
        s.client
            .record_execution(&s.vault, &op, &true, &30_0000000, &250u64);
        s.client
            .record_execution(&s.vault, &op, &false, &0i128, &9999u64);

        let info = s.client.get_keeper(&op);
        assert_eq!(info.total_executions, 3);
        assert_eq!(info.successful_fills, 2);
        assert_eq!(info.total_profit, 80_0000000);
        // Failures don't contribute to response-time stats.
        assert_eq!(info.response_count, 2);
        assert_eq!(info.total_response_time_ms, 400);
    }

    #[test]
    fn test_avg_response_time_computed() {
        let s = setup();
        let op = make_op(&s);
        s.client.register(&op, &String::from_str(&s.env, "alpha"));

        // No executions yet — avg should be 0.
        assert_eq!(s.client.avg_response_time_ms(&op), 0);

        s.client
            .record_execution(&s.vault, &op, &true, &10_0000000, &100u64);
        s.client
            .record_execution(&s.vault, &op, &true, &10_0000000, &200u64);
        assert_eq!(s.client.avg_response_time_ms(&op), 150);

        // Failure shouldn't move the average.
        s.client
            .record_execution(&s.vault, &op, &false, &0i128, &9999u64);
        assert_eq!(s.client.avg_response_time_ms(&op), 150);
    }

    #[test]
    fn test_slash_after_timeout() {
        let s = setup();
        let op = make_op(&s);
        s.client.register(&op, &String::from_str(&s.env, "alpha"));

        let t0: u64 = 1_000_000;
        s.env.ledger().set(LedgerInfo {
            timestamp: t0,
            protocol_version: 22,
            sequence_number: 1,
            network_id: Default::default(),
            base_reserve: 10,
            min_temp_entry_ttl: 16,
            min_persistent_entry_ttl: 4096,
            max_entry_ttl: 6_312_000,
        });
        s.client.mark_draw(&s.vault, &op);

        // Advance past timeout.
        s.env.ledger().set(LedgerInfo {
            timestamp: t0 + SLASH_TIMEOUT + 1,
            protocol_version: 22,
            sequence_number: 2,
            network_id: Default::default(),
            base_reserve: 10,
            min_temp_entry_ttl: 16,
            min_persistent_entry_ttl: 4096,
            max_entry_ttl: 6_312_000,
        });

        let vault_before = s.usdc_client.balance(&s.vault);
        let slashed = s.client.slash(&op);

        let expected: i128 = MIN_STAKE * (SLASH_RATE_BPS as i128) / 10_000;
        assert_eq!(slashed, expected);

        let info = s.client.get_keeper(&op);
        assert_eq!(info.stake, MIN_STAKE - expected);
        assert!(!info.has_active_draw);
        assert_eq!(s.usdc_client.balance(&s.vault) - vault_before, expected);
    }

    #[test]
    fn test_slash_before_timeout_fails() {
        let s = setup();
        let op = make_op(&s);
        s.client.register(&op, &String::from_str(&s.env, "alpha"));

        let t0: u64 = 1_000_000;
        s.env.ledger().set(LedgerInfo {
            timestamp: t0,
            protocol_version: 22,
            sequence_number: 1,
            network_id: Default::default(),
            base_reserve: 10,
            min_temp_entry_ttl: 16,
            min_persistent_entry_ttl: 4096,
            max_entry_ttl: 6_312_000,
        });
        s.client.mark_draw(&s.vault, &op);

        // Still within timeout window.
        s.env.ledger().set(LedgerInfo {
            timestamp: t0 + SLASH_TIMEOUT - 1,
            protocol_version: 22,
            sequence_number: 2,
            network_id: Default::default(),
            base_reserve: 10,
            min_temp_entry_ttl: 16,
            min_persistent_entry_ttl: 4096,
            max_entry_ttl: 6_312_000,
        });

        let result = s.client.try_slash(&op);
        assert_eq!(result, Err(Ok(Error::SlashTimeout)));
    }

    #[test]
    fn test_slash_without_active_draw_fails() {
        let s = setup();
        let op = make_op(&s);
        s.client.register(&op, &String::from_str(&s.env, "alpha"));

        let result = s.client.try_slash(&op);
        assert_eq!(result, Err(Ok(Error::SlashTimeout)));
    }

    #[test]
    fn test_unauthorized_mark_draw() {
        let s = setup();
        let op = make_op(&s);
        s.client.register(&op, &String::from_str(&s.env, "alpha"));

        let intruder = Address::generate(&s.env);
        let result = s.client.try_mark_draw(&intruder, &op);
        assert_eq!(result, Err(Ok(Error::Unauthorized)));
    }

    #[test]
    fn test_set_config_admin_only() {
        let s = setup();
        let new_cfg = RegistryConfig {
            min_stake: 200_0000000,
            slash_timeout: 7200,
            slash_rate_bps: 2000,
            usdc_token: s.usdc.clone(),
        };
        s.client.set_config(&s.admin, &new_cfg);

        let cfg = s.client.get_config();
        assert_eq!(cfg.min_stake, 200_0000000);
        assert_eq!(cfg.slash_timeout, 7200);
        assert_eq!(cfg.slash_rate_bps, 2000);
    }
}
