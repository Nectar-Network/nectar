#[cfg(test)]
#[allow(clippy::inconsistent_digit_grouping)] // project convention: <usdc>_<7-decimal-stroops>
mod tests {
    use soroban_sdk::{
        contract, contractimpl,
        testutils::{Address as _, Events as _, Ledger, LedgerInfo},
        token, Address, Env, Symbol, TryFromVal, Val, Vec,
    };

    use crate::{
        types::{VaultConfig, VaultError},
        NectarVault, NectarVaultClient,
    };

    const COOLDOWN: u64 = 600; // 10 minutes
    const MAX_DRAW: i128 = 500_0000000; // 500 USDC
    const NO_CAP: i128 = 0;

    // Mock registry: all functions are no-op stubs that succeed.
    #[contract]
    pub struct MockRegistry;
    #[contractimpl]
    impl MockRegistry {
        pub fn get_keeper(_env: Env, operator: Address) -> Address {
            operator
        }
        pub fn mark_draw(_env: Env, _caller: Address, _keeper: Address) {}
        pub fn clear_draw(_env: Env, _caller: Address, _keeper: Address) {}
        pub fn record_execution(
            _env: Env,
            _caller: Address,
            _keeper: Address,
            _success: bool,
            _profit: i128,
            _response_time_ms: u64,
        ) {
        }
    }

    fn setup_token(env: &Env, admin: &Address) -> Address {
        let token_id = env
            .register_stellar_asset_contract_v2(admin.clone())
            .address();
        // Mint 100M USDC so individual tests can pull arbitrary amounts.
        token::StellarAssetClient::new(env, &token_id).mint(admin, &100_000_000_0000000);
        token_id
    }

    fn default_config() -> VaultConfig {
        VaultConfig {
            deposit_cap: NO_CAP,
            withdraw_cooldown: 0,
            max_draw_per_keeper: 0,
        }
    }

    fn setup<'a>(env: &'a Env) -> (NectarVaultClient<'a>, Address, Address, Address) {
        setup_with_config(env, default_config())
    }

    fn setup_with_config<'a>(
        env: &'a Env,
        cfg: VaultConfig,
    ) -> (NectarVaultClient<'a>, Address, Address, Address) {
        let admin = Address::generate(env);
        let usdc = setup_token(env, &admin);
        let registry_id = env.register(MockRegistry, ());
        let vault_id = env.register(NectarVault, ());
        let client = NectarVaultClient::new(env, &vault_id);
        client.initialize(&admin, &usdc, &registry_id, &cfg);
        (client, admin, usdc, vault_id)
    }

    fn set_time(env: &Env, ts: u64, seq: u32) {
        env.ledger().set(LedgerInfo {
            timestamp: ts,
            protocol_version: 22,
            sequence_number: seq,
            network_id: Default::default(),
            base_reserve: 10,
            min_temp_entry_ttl: 16,
            min_persistent_entry_ttl: 4096,
            max_entry_ttl: 6_312_000,
        });
    }

    // ── Existing tests (preserved) ─────────────────────────────────────────

    #[test]
    fn test_deposit_and_balance() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &100_0000000);
        client.deposit(&user, &100_0000000);

        let (shares, usdc_val) = client.balance(&user);
        assert_eq!(shares, 100_0000000);
        assert_eq!(usdc_val, 100_0000000);
    }

    #[test]
    fn test_withdraw_full() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &100_0000000);
        let shares = client.deposit(&user, &100_0000000);
        let usdc_back = client.withdraw(&user, &shares);
        assert_eq!(usdc_back, 100_0000000);
        let (s, _) = client.balance(&user);
        assert_eq!(s, 0);
    }

    #[test]
    fn test_full_cycle_with_profit() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &200_0000000);

        client.deposit(&user, &1000_0000000);
        client.draw(&keeper, &500_0000000);
        client.return_proceeds(&keeper, &510_0000000, &120u64);

        let state = client.get_state();
        assert_eq!(state.total_usdc, 1010_0000000);
        assert_eq!(state.total_profit, 10_0000000);

        let (shares, _) = client.balance(&user);
        let out = client.withdraw(&user, &shares);
        assert_eq!(out, 1010_0000000);
    }

    #[test]
    fn test_withdraw_more_than_owned_fails() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &100_0000000);
        let shares = client.deposit(&user, &100_0000000);
        let result = client.try_withdraw(&user, &(shares + 1));
        assert_eq!(result, Err(Ok(VaultError::InsufficientBalance)));
    }

    #[test]
    fn test_draw_more_than_available_fails() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &100_0000000);
        client.deposit(&user, &100_0000000);
        let keeper = Address::generate(&env);
        let result = client.try_draw(&keeper, &200_0000000);
        assert_eq!(result, Err(Ok(VaultError::InsufficientVault)));
    }

    #[test]
    fn test_multiple_depositors_proportional_shares() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let alice = Address::generate(&env);
        let bob = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &alice, &200_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &bob, &100_0000000);

        client.deposit(&alice, &200_0000000);
        client.deposit(&bob, &100_0000000);

        let (alice_shares, _) = client.balance(&alice);
        let (bob_shares, _) = client.balance(&bob);
        assert_eq!(alice_shares, bob_shares * 2);
    }

    #[test]
    fn test_double_init_fails() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, vault_id) = setup(&env);
        let dummy_reg = Address::generate(&env);
        let result = client.try_initialize(&admin, &usdc, &dummy_reg, &default_config());
        assert_eq!(result, Err(Ok(VaultError::AlreadyInit)));
        let _ = vault_id;
    }

    #[test]
    fn test_withdraw_with_zero_shares_fails() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &100_0000000);
        client.deposit(&user, &100_0000000);

        let usdc_back = client.withdraw(&user, &0);
        assert_eq!(usdc_back, 0);
        let (s, _) = client.balance(&user);
        assert_eq!(s, 100_0000000);
    }

    #[test]
    fn test_withdraw_more_than_available_fails() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &100_0000000);
        let shares = client.deposit(&user, &100_0000000);

        let result = client.try_withdraw(&user, &(shares + 1));
        assert_eq!(result, Err(Ok(VaultError::InsufficientBalance)));
    }

    #[test]
    fn test_partial_return_reduces_active_liq() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &500_0000000);

        client.deposit(&user, &1000_0000000);
        client.draw(&keeper, &500_0000000);

        client.return_proceeds(&keeper, &400_0000000, &120u64);

        let state = client.get_state();
        assert_eq!(state.active_liq, 100_0000000);
        assert_eq!(state.total_profit, 0);
    }

    #[test]
    fn test_draw_zero_fails_or_noop() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &100_0000000);
        client.deposit(&user, &100_0000000);

        client.draw(&keeper, &0);
        let state = client.get_state();
        assert_eq!(state.active_liq, 0);
    }

    #[test]
    fn test_return_without_draw_no_panic() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &50_0000000);

        client.deposit(&user, &1000_0000000);

        client.return_proceeds(&keeper, &50_0000000, &120u64);
        let state = client.get_state();
        assert_eq!(state.total_profit, 50_0000000);
        assert_eq!(state.total_usdc, 1050_0000000);
    }

    #[test]
    fn test_multiple_draws_and_returns() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &2000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &500_0000000);

        client.deposit(&user, &2000_0000000);

        client.draw(&keeper, &300_0000000);
        client.return_proceeds(&keeper, &310_0000000, &120u64);

        client.draw(&keeper, &200_0000000);
        client.return_proceeds(&keeper, &215_0000000, &120u64);

        let state = client.get_state();
        assert_eq!(state.active_liq, 0);
        assert_eq!(state.total_profit, 25_0000000);
        assert_eq!(state.total_usdc, 2025_0000000);
    }

    #[test]
    fn test_share_rounding_bounded() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let a = Address::generate(&env);
        let b = Address::generate(&env);
        let c = Address::generate(&env);

        token::Client::new(&env, &usdc).transfer(&admin, &a, &100_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &b, &100_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &c, &100_0000000);

        client.deposit(&a, &100_0000000);
        client.deposit(&b, &100_0000000);
        client.deposit(&c, &100_0000000);

        let state = client.get_state();
        assert_eq!(state.total_usdc, 300_0000000);
        assert_eq!(state.total_shares, 300_0000000);

        let (shares_a, _) = client.balance(&a);
        let (shares_b, _) = client.balance(&b);
        let (shares_c, _) = client.balance(&c);

        let out_a = client.withdraw(&a, &shares_a);
        let out_b = client.withdraw(&b, &shares_b);
        let out_c = client.withdraw(&c, &shares_c);

        let total_out = out_a + out_b + out_c;
        assert!(
            total_out >= 300_0000000 - 3,
            "too much rounding dust: {}",
            300_0000000 - total_out
        );
        assert!(total_out <= 300_0000000);
    }

    #[test]
    fn test_draw_event_emitted() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &200_0000000);

        client.deposit(&user, &1000_0000000);
        client.draw(&keeper, &100_0000000);

        let events = env.events().all();
        let has_draw = events
            .iter()
            .any(|(_, topics, _): (Address, Vec<Val>, Val)| {
                if let Some(val) = topics.first() {
                    if let Ok(s) = Symbol::try_from_val(&env, &val) {
                        return s == Symbol::new(&env, "draw");
                    }
                }
                false
            });
        assert!(has_draw, "draw event not emitted");
    }

    #[test]
    fn test_return_event_emitted() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &200_0000000);

        client.deposit(&user, &1000_0000000);
        client.draw(&keeper, &100_0000000);
        client.return_proceeds(&keeper, &110_0000000, &120u64);

        let events = env.events().all();
        let has_return = events
            .iter()
            .any(|(_, topics, _): (Address, Vec<Val>, Val)| {
                if let Some(val) = topics.first() {
                    if let Ok(s) = Symbol::try_from_val(&env, &val) {
                        return s == Symbol::new(&env, "return");
                    }
                }
                false
            });
        assert!(has_return, "return event not emitted");
    }

    // ── Tranche 1 deliverable 2: caps, cooldown, draw limit, share math ────

    #[test]
    fn test_deposit_within_cap() {
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: 500_0000000,
            withdraw_cooldown: 0,
            max_draw_per_keeper: 0,
        };
        let (client, admin, usdc, _) = setup_with_config(&env, cfg);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &500_0000000);
        client.deposit(&user, &400_0000000);

        let state = client.get_state();
        assert_eq!(state.total_usdc, 400_0000000);
    }

    #[test]
    fn test_deposit_at_exact_cap() {
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: 500_0000000,
            withdraw_cooldown: 0,
            max_draw_per_keeper: 0,
        };
        let (client, admin, usdc, _) = setup_with_config(&env, cfg);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &500_0000000);
        client.deposit(&user, &500_0000000);

        let state = client.get_state();
        assert_eq!(state.total_usdc, 500_0000000);
    }

    #[test]
    fn test_deposit_exceeds_cap() {
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: 500_0000000,
            withdraw_cooldown: 0,
            max_draw_per_keeper: 0,
        };
        let (client, admin, usdc, _) = setup_with_config(&env, cfg);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &600_0000000);
        let result = client.try_deposit(&user, &500_0000001);
        assert_eq!(result, Err(Ok(VaultError::DepositCapExceeded)));
    }

    #[test]
    fn test_deposit_cap_with_existing_balance() {
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: 500_0000000,
            withdraw_cooldown: 0,
            max_draw_per_keeper: 0,
        };
        let (client, admin, usdc, _) = setup_with_config(&env, cfg);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &600_0000000);
        client.deposit(&user, &300_0000000);
        // Second deposit would bring total to 501 — over cap.
        let result = client.try_deposit(&user, &201_0000000);
        assert_eq!(result, Err(Ok(VaultError::DepositCapExceeded)));
    }

    #[test]
    fn test_withdraw_before_cooldown() {
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: 0,
            withdraw_cooldown: COOLDOWN,
            max_draw_per_keeper: 0,
        };
        let (client, admin, usdc, _) = setup_with_config(&env, cfg);

        let t0 = 1_000_000;
        set_time(&env, t0, 1);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &100_0000000);
        let shares = client.deposit(&user, &100_0000000);

        // Try to withdraw 10 seconds later — still within cooldown.
        set_time(&env, t0 + 10, 2);
        let result = client.try_withdraw(&user, &shares);
        assert_eq!(result, Err(Ok(VaultError::WithdrawalCooldown)));
    }

    #[test]
    fn test_withdraw_after_cooldown() {
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: 0,
            withdraw_cooldown: COOLDOWN,
            max_draw_per_keeper: 0,
        };
        let (client, admin, usdc, _) = setup_with_config(&env, cfg);

        let t0 = 1_000_000;
        set_time(&env, t0, 1);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &100_0000000);
        let shares = client.deposit(&user, &100_0000000);

        set_time(&env, t0 + COOLDOWN, 2);
        let out = client.withdraw(&user, &shares);
        assert_eq!(out, 100_0000000);
    }

    #[test]
    fn test_cooldown_resets_on_new_deposit() {
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: 0,
            withdraw_cooldown: COOLDOWN,
            max_draw_per_keeper: 0,
        };
        let (client, admin, usdc, _) = setup_with_config(&env, cfg);

        let t0 = 1_000_000;
        set_time(&env, t0, 1);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &200_0000000);
        client.deposit(&user, &100_0000000);

        // Wait past first cooldown.
        set_time(&env, t0 + COOLDOWN + 10, 2);
        // Second deposit resets the timer.
        client.deposit(&user, &100_0000000);

        // Try to withdraw immediately — must fail because cooldown restarted.
        let result = client.try_withdraw(&user, &10_0000000);
        assert_eq!(result, Err(Ok(VaultError::WithdrawalCooldown)));
    }

    #[test]
    fn test_draw_within_limit() {
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: 0,
            withdraw_cooldown: 0,
            max_draw_per_keeper: MAX_DRAW,
        };
        let (client, admin, usdc, _) = setup_with_config(&env, cfg);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        client.deposit(&user, &1000_0000000);

        client.draw(&keeper, &MAX_DRAW);
        let state = client.get_state();
        assert_eq!(state.active_liq, MAX_DRAW);
    }

    #[test]
    fn test_draw_exceeds_limit() {
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: 0,
            withdraw_cooldown: 0,
            max_draw_per_keeper: MAX_DRAW,
        };
        let (client, admin, usdc, _) = setup_with_config(&env, cfg);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        client.deposit(&user, &1000_0000000);

        let result = client.try_draw(&keeper, &(MAX_DRAW + 1));
        assert_eq!(result, Err(Ok(VaultError::DrawLimitExceeded)));
    }

    #[test]
    fn test_share_math_first_deposit() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1234_5678901);
        let shares = client.deposit(&user, &1234_5678901);
        // First deposit: shares == amount (1:1).
        assert_eq!(shares, 1234_5678901);
    }

    #[test]
    fn test_share_math_with_profit() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user_a = Address::generate(&env);
        let user_b = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user_a, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &user_b, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &200_0000000);

        client.deposit(&user_a, &1000_0000000);
        client.draw(&keeper, &500_0000000);
        // Return with 100 USDC profit.
        client.return_proceeds(&keeper, &600_0000000, &120u64);

        // Total: 1100 USDC, 1000 shares. Share price = 1.1.
        // user_b deposits 1000 → gets 1000 * 1000 / 1100 = 909_0909090 shares.
        let b_shares = client.deposit(&user_b, &1000_0000000);
        assert_eq!(b_shares, 1000_0000000 * 1000_0000000 / 1100_0000000);

        let (a_shares, a_val) = client.balance(&user_a);
        // user_a owns 1000 of (1000+909) shares; total_usdc = 2100.
        // a_val = 1000 * 2100 / 1909.0909 ≈ 1099.99 — close to 1100.
        assert_eq!(a_shares, 1000_0000000);
        assert!((1099_0000000..=1100_0000000).contains(&a_val));
    }

    #[test]
    fn test_share_math_tiny_amounts() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &10);

        // 1 stroop deposit (smallest unit, 0.0000001 USDC).
        let shares = client.deposit(&user, &1);
        assert_eq!(shares, 1);

        // Withdraw 1 stroop.
        let out = client.withdraw(&user, &1);
        assert_eq!(out, 1);
    }

    #[test]
    fn test_share_math_large_amounts() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let big: i128 = 10_000_000_0000000; // 10M USDC
        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &big);
        let shares = client.deposit(&user, &big);
        assert_eq!(shares, big);

        let (s, v) = client.balance(&user);
        assert_eq!(s, big);
        assert_eq!(v, big);

        let out = client.withdraw(&user, &shares);
        assert_eq!(out, big);
    }

    #[test]
    fn test_multiple_depositors_proportional_with_profit() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let a = Address::generate(&env);
        let b = Address::generate(&env);
        let c = Address::generate(&env);
        let keeper = Address::generate(&env);

        // a:b:c = 1:2:3 → share split 1/6, 2/6, 3/6.
        token::Client::new(&env, &usdc).transfer(&admin, &a, &100_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &b, &200_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &c, &300_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &500_0000000);

        client.deposit(&a, &100_0000000);
        client.deposit(&b, &200_0000000);
        client.deposit(&c, &300_0000000);

        // Total = 600. Keeper draws 200, returns 260 → 60 profit.
        client.draw(&keeper, &200_0000000);
        client.return_proceeds(&keeper, &260_0000000, &120u64);

        // Total now 660 USDC across 600 shares. Share price = 1.1.
        let (_, a_val) = client.balance(&a);
        let (_, b_val) = client.balance(&b);
        let (_, c_val) = client.balance(&c);
        assert_eq!(a_val, 110_0000000);
        assert_eq!(b_val, 220_0000000);
        assert_eq!(c_val, 330_0000000);
    }

    #[test]
    fn test_get_config_returns_set_values() {
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: 1234_0000000,
            withdraw_cooldown: 999,
            max_draw_per_keeper: 567_0000000,
        };
        let (client, _, _, _) = setup_with_config(&env, cfg.clone());
        let got = client.get_config();
        assert_eq!(got.deposit_cap, cfg.deposit_cap);
        assert_eq!(got.withdraw_cooldown, cfg.withdraw_cooldown);
        assert_eq!(got.max_draw_per_keeper, cfg.max_draw_per_keeper);
    }

    #[test]
    fn test_set_config_admin_only() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, _, _) = setup(&env);

        let new_cfg = VaultConfig {
            deposit_cap: 9999_0000000,
            withdraw_cooldown: 1234,
            max_draw_per_keeper: 100_0000000,
        };
        client.set_config(&admin, &new_cfg);

        let got = client.get_config();
        assert_eq!(got.deposit_cap, 9999_0000000);
        assert_eq!(got.withdraw_cooldown, 1234);
    }

    #[test]
    fn test_set_config_unauthorized() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, _, _, _) = setup(&env);

        // The admin-mismatch check returns Unauthorized before require_auth runs,
        // so even with auths mocked, an intruder cannot mutate config.
        let intruder = Address::generate(&env);
        let new_cfg = default_config();
        let result = client.try_set_config(&intruder, &new_cfg);
        assert_eq!(result, Err(Ok(VaultError::Unauthorized)));
    }

    // ── Cross-contract integration with the real KeeperRegistry ──────────

    #[test]
    fn test_real_registry_full_cycle() {
        use keeper_registry::{KeeperRegistry, KeeperRegistryClient, RegistryConfig};
        use soroban_sdk::String as SorString;

        let env = Env::default();
        env.mock_all_auths();

        let admin = Address::generate(&env);
        let usdc_admin = Address::generate(&env);
        let usdc = env
            .register_stellar_asset_contract_v2(usdc_admin.clone())
            .address();
        let usdc_admin_client = token::StellarAssetClient::new(&env, &usdc);

        // Deploy both contracts so we know their addresses up front.
        let registry_id = env.register(KeeperRegistry, ());
        let vault_id = env.register(NectarVault, ());

        let registry = KeeperRegistryClient::new(&env, &registry_id);
        let vault = NectarVaultClient::new(&env, &vault_id);

        let reg_cfg = RegistryConfig {
            min_stake: 100_0000000,
            slash_timeout: 3600,
            slash_rate_bps: 1000,
            usdc_token: usdc.clone(),
        };
        registry.initialize(&admin, &reg_cfg, &vault_id);

        let vault_cfg = VaultConfig {
            deposit_cap: 0,
            withdraw_cooldown: 0,
            max_draw_per_keeper: 1000_0000000,
        };
        vault.initialize(&admin, &usdc, &registry_id, &vault_cfg);

        // Mint USDC to keeper for stake; register.
        let keeper = Address::generate(&env);
        usdc_admin_client.mint(&keeper, &200_0000000);
        registry.register(&keeper, &SorString::from_str(&env, "k1"));

        let info = registry.get_keeper(&keeper);
        assert_eq!(info.stake, 100_0000000);

        // Mint USDC to depositor; deposit.
        let depositor = Address::generate(&env);
        usdc_admin_client.mint(&depositor, &1000_0000000);
        vault.deposit(&depositor, &1000_0000000);

        // Keeper draws 500 — vault calls registry.mark_draw.
        vault.draw(&keeper, &500_0000000);
        let info = registry.get_keeper(&keeper);
        assert!(info.has_active_draw);

        // Keeper repays 510 (10 profit) — vault calls clear_draw + record_execution.
        // Mint extra so keeper can pay back.
        usdc_admin_client.mint(&keeper, &10_0000000);
        vault.return_proceeds(&keeper, &510_0000000, &175u64);

        let info = registry.get_keeper(&keeper);
        assert!(!info.has_active_draw);
        assert_eq!(info.total_executions, 1);
        assert_eq!(info.successful_fills, 1);
        assert_eq!(info.total_profit, 10_0000000);
        assert_eq!(info.response_count, 1);
        assert_eq!(info.total_response_time_ms, 175);
        assert_eq!(registry.avg_response_time_ms(&keeper), 175);

        let state = vault.get_state();
        assert_eq!(state.active_liq, 0);
        assert_eq!(state.total_profit, 10_0000000);
        assert_eq!(state.total_usdc, 1010_0000000);
    }
}
