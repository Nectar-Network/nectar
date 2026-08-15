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
        // Mirrors the real registry's verify_keeper: succeeds (void) for any
        // operator so draw()'s registration/active gate passes in unit tests.
        pub fn verify_keeper(_env: Env, _operator: Address) {}
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
        let vault_id = env.register(
            NectarVault,
            (
                admin.clone(),
                usdc.clone(),
                registry_id.clone(),
                cfg.clone(),
            ),
        );
        let client = NectarVaultClient::new(env, &vault_id);
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
        client.draw(&keeper, &500_0000000, &usdc);
        client.return_proceeds(&keeper, &510_0000000, &120u64);

        let state = client.get_state();
        assert_eq!(state.total_usdc, 1010_0000000);
        assert_eq!(state.total_profit, 10_0000000);

        let (shares, _) = client.balance(&user);
        let out = client.withdraw(&user, &shares);
        // Virtual-offset seed dust (< ~0.01 USDC) stays permanently in the pool;
        // the sole depositor recovers principal + nearly all profit, never more.
        assert!(out <= 1010_0000000 && out >= 1009_0000000);
    }

    #[test]
    fn test_get_keeper_draw_tracks_and_clears() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &200_0000000);

        client.deposit(&user, &1000_0000000);
        // no outstanding draw initially
        assert_eq!(client.get_keeper_draw(&keeper), 0);
        // a draw is tracked under the keeper
        client.draw(&keeper, &500_0000000, &usdc);
        assert_eq!(client.get_keeper_draw(&keeper), 500_0000000);
        // returning proceeds clears the per-keeper draw
        client.return_proceeds(&keeper, &510_0000000, &120u64);
        assert_eq!(client.get_keeper_draw(&keeper), 0);
    }

    #[test]
    fn test_partial_return_keeps_remaining_draw_slashable() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &200_0000000);

        client.deposit(&user, &1000_0000000);
        client.draw(&keeper, &500_0000000, &usdc);

        // A partial return must NOT settle the draw: a 1-stroop return cannot
        // clear a 500 USDC debt. The remainder stays owed (and slash-eligible).
        client.return_proceeds(&keeper, &200_0000000, &0u64);
        assert_eq!(client.get_keeper_draw(&keeper), 300_0000000);
        let state = client.get_state();
        assert_eq!(state.active_liq, 300_0000000);
        assert_eq!(state.total_profit, 0);
        assert_eq!(state.total_usdc, 1000_0000000);

        // Settling the remainder (plus profit) clears the draw and books the
        // profit exactly once: 200 + 310 returned vs 500 drawn = 10 profit.
        client.return_proceeds(&keeper, &310_0000000, &120u64);
        assert_eq!(client.get_keeper_draw(&keeper), 0);
        let state = client.get_state();
        assert_eq!(state.active_liq, 0);
        assert_eq!(state.total_profit, 10_0000000);
        assert_eq!(state.total_usdc, 1010_0000000);
    }

    #[test]
    fn test_profitable_return_scoped_to_own_draw() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper_a = Address::generate(&env);
        let keeper_b = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper_a, &100_0000000);

        client.deposit(&user, &1000_0000000);
        client.draw(&keeper_a, &100_0000000, &usdc);
        client.draw(&keeper_b, &100_0000000, &usdc);

        // A returns 150 (100 principal + 50 profit). Only A's principal may be
        // repaid against active_liq — B's outstanding 100 must remain tracked.
        client.return_proceeds(&keeper_a, &150_0000000, &120u64);

        assert_eq!(client.get_keeper_draw(&keeper_a), 0);
        assert_eq!(client.get_keeper_draw(&keeper_b), 100_0000000);
        let state = client.get_state();
        assert_eq!(state.active_liq, 100_0000000);
        assert_eq!(state.total_profit, 50_0000000);
        assert_eq!(state.total_usdc, 1050_0000000);
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
        let result = client.try_draw(&keeper, &200_0000000, &usdc);
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
    fn test_constructor_initializes_atomically() {
        // Init now happens in the deploy (constructor), so there is no separate
        // initialize tx an attacker could front-run to seize admin (NEW-init).
        // Confirm the constructor set the config and zeroed the vault state.
        let env = Env::default();
        env.mock_all_auths();
        let (client, _admin, _usdc, _vault_id) = setup(&env);
        assert_eq!(
            client.get_config().deposit_cap,
            default_config().deposit_cap
        );
        let state = client.get_state();
        assert_eq!(state.total_usdc, 0);
        assert_eq!(state.total_shares, 0);
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
        client.draw(&keeper, &500_0000000, &usdc);

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

        client.draw(&keeper, &0, &usdc);
        let state = client.get_state();
        assert_eq!(state.active_liq, 0);
    }

    #[test]
    fn test_return_without_draw_rejected() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &50_0000000);

        client.deposit(&user, &1000_0000000);

        // VLT-2: a return with no outstanding draw is rejected — there is no
        // anonymous donation-as-profit path. State is unchanged.
        let res = client.try_return_proceeds(&keeper, &50_0000000, &120u64);
        assert_eq!(res, Err(Ok(VaultError::NoDraw)));
        let state = client.get_state();
        assert_eq!(state.total_profit, 0);
        assert_eq!(state.total_usdc, 1000_0000000);
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

        client.draw(&keeper, &300_0000000, &usdc);
        client.return_proceeds(&keeper, &310_0000000, &120u64);

        client.draw(&keeper, &200_0000000, &usdc);
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
        client.draw(&keeper, &100_0000000, &usdc);

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
        client.draw(&keeper, &100_0000000, &usdc);
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

        client.draw(&keeper, &MAX_DRAW, &usdc);
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

        let result = client.try_draw(&keeper, &(MAX_DRAW + 1), &usdc);
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
        client.draw(&keeper, &500_0000000, &usdc);
        // Return with 100 USDC profit.
        client.return_proceeds(&keeper, &600_0000000, &120u64);

        // Total: 1100 USDC, 1000 shares. Share price = 1.1.
        // user_b deposits 1000 → ~909 shares (1000*1000/1100), within offset dust.
        let b_shares = client.deposit(&user_b, &1000_0000000);
        assert!((909_0000000..=909_5000000).contains(&b_shares));

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
        client.draw(&keeper, &200_0000000, &usdc);
        client.return_proceeds(&keeper, &260_0000000, &120u64);

        // Total now 660 USDC across 600 shares. Share price = 1.1.
        let (_, a_val) = client.balance(&a);
        let (_, b_val) = client.balance(&b);
        let (_, c_val) = client.balance(&c);
        // Proportional to within virtual-offset dust (< ~0.01 USDC), never over.
        assert!((109_9000000..=110_0000000).contains(&a_val));
        assert!((219_9000000..=220_0000000).contains(&b_val));
        assert!((329_9000000..=330_0000000).contains(&c_val));
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

    // ── Security regression tests (audit hardening) ──────────────────────

    #[test]
    fn test_inflation_attack_unprofitable() {
        // VLT-1: the virtual offset defeats the first-depositor inflation attack.
        // An attacker who seeds 1 stroop and donates a large "profit" cannot zero
        // the victim's deposit — the victim keeps ~all their value, and the
        // attacker's single share is worth a tiny fraction of what they donated.
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);
        let attacker = Address::generate(&env);
        let victim = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &attacker, &6000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &victim, &1000_0000000);

        client.deposit(&attacker, &1); // 1 share
        client.draw(&attacker, &1, &usdc); // outstanding draw so the return is allowed
        client.return_proceeds(&attacker, &5000_0000000, &1u64); // donate ~5000 to inflate

        // Victim deposits 1000 and receives shares worth ~their deposit.
        client.deposit(&victim, &1000_0000000);
        let (_, v_val) = client.balance(&victim);
        assert!(v_val >= 990_0000000, "victim value {}", v_val);

        // The attacker's single share is worth < 1 USDC after donating 5000 —
        // the attack is a catastrophic net loss.
        let (_, a_val) = client.balance(&attacker);
        assert!(a_val < 1_0000000, "attacker value {}", a_val);
    }

    #[test]
    fn test_zero_share_deposit_rejected() {
        // VLT-1 backstop: a deposit too small to mint a share is rejected.
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);
        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        token::Client::new(&env, &usdc).transfer(&admin, &keeper, &100_0000000);
        client.deposit(&user, &1000_0000000);
        // Raise the price above 1 via a legit profitable return.
        client.draw(&keeper, &1, &usdc);
        client.return_proceeds(&keeper, &100_0000000, &1u64);

        let dust = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &dust, &10);
        let res = client.try_deposit(&dust, &1); // 1 stroop mints 0 shares
        assert_eq!(res, Err(Ok(VaultError::ZeroShares)));
    }

    #[test]
    fn test_cumulative_draw_cap() {
        // NEW-cap: the per-keeper cap bounds CUMULATIVE outstanding draw, not one
        // call — a keeper cannot loop draw() past the cap.
        let env = Env::default();
        env.mock_all_auths();
        let cfg = VaultConfig {
            deposit_cap: NO_CAP,
            withdraw_cooldown: 0,
            max_draw_per_keeper: 500_0000000,
        };
        let (client, admin, usdc, _) = setup_with_config(&env, cfg);
        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        client.deposit(&user, &1000_0000000);

        client.draw(&keeper, &300_0000000, &usdc); // cumulative 300 <= 500 ok
        let res = client.try_draw(&keeper, &300_0000000, &usdc); // would be 600 > 500
        assert_eq!(res, Err(Ok(VaultError::DrawLimitExceeded)));
        client.draw(&keeper, &200_0000000, &usdc); // cumulative exactly 500 ok
        assert_eq!(client.get_keeper_draw(&keeper), 500_0000000);
    }

    #[test]
    fn test_pause_blocks_entry_allows_exit() {
        // VLT-4: pause blocks deposit + draw; withdraw + return_proceeds stay open
        // so depositors can exit and keepers can settle during an incident.
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);
        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        client.deposit(&user, &1000_0000000);
        client.draw(&keeper, &100_0000000, &usdc); // outstanding draw before pause

        client.pause(&admin);
        assert!(client.is_paused());
        assert_eq!(
            client.try_deposit(&user, &100_0000000),
            Err(Ok(VaultError::Paused))
        );
        assert_eq!(
            client.try_draw(&keeper, &100_0000000, &usdc),
            Err(Ok(VaultError::Paused))
        );

        // Keeper settles the outstanding draw and the depositor exits — both work.
        client.return_proceeds(&keeper, &100_0000000, &1u64);
        let (shares, _) = client.balance(&user);
        assert!(client.withdraw(&user, &shares) > 0);

        client.unpause(&admin);
        assert!(!client.is_paused());
    }

    // ── Tranche 3: circuit-breaker pause plumbing (DECISION F-2a) ─────────

    #[test]
    fn test_global_liq_pause_blocks_draw_and_unpause_restores() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);
        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        client.deposit(&user, &1000_0000000);

        assert!(!client.is_global_liq_paused());
        client.set_global_pause(&admin, &true);
        assert!(client.is_global_liq_paused());
        assert_eq!(
            client.try_draw(&keeper, &100_0000000, &usdc),
            Err(Ok(VaultError::LiquidationsPaused))
        );

        client.set_global_pause(&admin, &false);
        assert!(!client.is_global_liq_paused());
        client.draw(&keeper, &100_0000000, &usdc);
        assert_eq!(client.get_state().active_liq, 100_0000000);
    }

    #[test]
    fn test_asset_pause_blocks_only_declared_asset() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);
        let user = Address::generate(&env);
        let keeper = Address::generate(&env);
        let xlm = Address::generate(&env); // a second collateral asset
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        client.deposit(&user, &1000_0000000);

        assert!(!client.is_asset_paused(&xlm));
        client.set_asset_pause(&admin, &xlm, &true);
        assert!(client.is_asset_paused(&xlm));
        assert!(!client.is_asset_paused(&usdc));

        // A draw declaring the paused asset is blocked; another asset passes.
        assert_eq!(
            client.try_draw(&keeper, &100_0000000, &xlm),
            Err(Ok(VaultError::AssetPaused))
        );
        client.draw(&keeper, &100_0000000, &usdc);

        client.set_asset_pause(&admin, &xlm, &false);
        assert!(!client.is_asset_paused(&xlm));
        client.draw(&keeper, &100_0000000, &xlm);
        assert_eq!(client.get_state().active_liq, 200_0000000);
    }

    #[test]
    fn test_withdraw_and_deposit_work_during_liq_pause() {
        // INVARIANT (DECISION F-2a): the liquidation pause is NOT the VLT-4
        // emergency pause — depositors must always be able to exit (and enter)
        // while liquidations are paused. Only draw() is gated.
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);
        let user = Address::generate(&env);
        token::Client::new(&env, &usdc).transfer(&admin, &user, &1000_0000000);
        client.deposit(&user, &500_0000000);

        client.set_global_pause(&admin, &true);
        client.set_asset_pause(&admin, &usdc, &true);

        // Deposit still governed by the (unset) VLT-4 pause alone.
        client.deposit(&user, &500_0000000);
        // Withdraw must work while every liquidation switch is thrown.
        let (shares, _) = client.balance(&user);
        let out = client.withdraw(&user, &shares);
        assert_eq!(out, 1000_0000000);
    }

    #[test]
    fn test_liq_pause_non_admin_rejected() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, _, usdc, _) = setup(&env);
        let intruder = Address::generate(&env);
        assert_eq!(
            client.try_set_global_pause(&intruder, &true),
            Err(Ok(VaultError::Unauthorized))
        );
        assert_eq!(
            client.try_set_asset_pause(&intruder, &usdc, &true),
            Err(Ok(VaultError::Unauthorized))
        );
        assert!(!client.is_global_liq_paused());
        assert!(!client.is_asset_paused(&usdc));
    }

    #[test]
    fn test_liq_pause_events_emitted_on_every_flip() {
        let env = Env::default();
        env.mock_all_auths();
        let (client, admin, usdc, _) = setup(&env);

        let count_topic = |name: &str| {
            env.events()
                .all()
                .iter()
                .filter(|(_, topics, _): &(Address, Vec<Val>, Val)| {
                    topics
                        .first()
                        .and_then(|val| Symbol::try_from_val(&env, &val).ok())
                        .is_some_and(|s| s == Symbol::new(&env, name))
                })
                .count()
        };

        // NOTE: the soroban test env only retains events of the LAST invocation,
        // so each flip is asserted immediately after its call.
        client.set_global_pause(&admin, &true);
        assert_eq!(count_topic("liq_pause"), 1);
        client.set_global_pause(&admin, &false);
        assert_eq!(count_topic("liq_pause"), 1);
        client.set_asset_pause(&admin, &usdc, &true);
        assert_eq!(count_topic("asset_pause"), 1);
        client.set_asset_pause(&admin, &usdc, &false);
        assert_eq!(count_topic("asset_pause"), 1);
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

        // Deploy the registry first (its constructor needs no vault), then the
        // vault (whose constructor references the registry), then link them via
        // the one-time admin-gated set_vault — breaking the circular reference.
        let reg_cfg = RegistryConfig {
            min_stake: 100_0000000,
            slash_timeout: 3600,
            slash_rate_bps: 1000,
            usdc_token: usdc.clone(),
        };
        let registry_id = env.register(KeeperRegistry, (admin.clone(), reg_cfg.clone()));

        let vault_cfg = VaultConfig {
            deposit_cap: 0,
            withdraw_cooldown: 0,
            max_draw_per_keeper: 1000_0000000,
        };
        let vault_id = env.register(
            NectarVault,
            (
                admin.clone(),
                usdc.clone(),
                registry_id.clone(),
                vault_cfg.clone(),
            ),
        );

        let registry = KeeperRegistryClient::new(&env, &registry_id);
        let vault = NectarVaultClient::new(&env, &vault_id);
        registry.set_vault(&admin, &vault_id);

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
        vault.draw(&keeper, &500_0000000, &usdc);
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

    #[test]
    fn test_slash_reconciles_vault_accounting() {
        // NEW-slash-reconcile: when a keeper defaults and is slashed, the registry
        // cross-calls the vault to write off the defaulted draw and book the
        // recovery, so the two contracts never drift and the vault stays solvent
        // (token balance == total_usdc - active_liq).
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

        let reg_cfg = RegistryConfig {
            min_stake: 100_0000000,
            slash_timeout: 3600,
            slash_rate_bps: 1000, // 10%
            usdc_token: usdc.clone(),
        };
        let registry_id = env.register(KeeperRegistry, (admin.clone(), reg_cfg.clone()));
        let vault_cfg = VaultConfig {
            deposit_cap: 0,
            withdraw_cooldown: 0,
            max_draw_per_keeper: 1000_0000000,
        };
        let vault_id = env.register(
            NectarVault,
            (
                admin.clone(),
                usdc.clone(),
                registry_id.clone(),
                vault_cfg.clone(),
            ),
        );
        let registry = KeeperRegistryClient::new(&env, &registry_id);
        let vault = NectarVaultClient::new(&env, &vault_id);
        registry.set_vault(&admin, &vault_id);

        let keeper = Address::generate(&env);
        usdc_admin_client.mint(&keeper, &100_0000000);
        registry.register(&keeper, &SorString::from_str(&env, "k1"));
        let depositor = Address::generate(&env);
        usdc_admin_client.mint(&depositor, &1000_0000000);
        vault.deposit(&depositor, &1000_0000000);

        // Keeper draws 400 and absconds (never returns).
        vault.draw(&keeper, &400_0000000, &usdc);
        assert_eq!(vault.get_keeper_draw(&keeper), 400_0000000);
        assert_eq!(vault.get_state().active_liq, 400_0000000);

        // After the timeout, slash: sends 10% of the 100 stake (10) to the vault
        // AND reconciles the vault's books.
        set_time(&env, 4000, 100);
        let slashed = registry.slash(&keeper);
        assert_eq!(slashed, 10_0000000);

        // Vault reconciled: draw cleared, active_liq zeroed, total_usdc written
        // down by the net loss (400 lost − 10 recovered → 1000 − 390 = 610).
        assert_eq!(vault.get_keeper_draw(&keeper), 0);
        let state = vault.get_state();
        assert_eq!(state.active_liq, 0);
        assert_eq!(state.total_usdc, 610_0000000);
        // Solvency: real token balance backs total_usdc − active_liq.
        assert_eq!(
            token::Client::new(&env, &usdc).balance(&vault_id),
            610_0000000
        );
        // Registry cleared the active draw (keeper may now deregister).
        assert!(!registry.get_keeper(&keeper).has_active_draw);
    }

    #[test]
    fn test_withdraw_after_loss_cannot_underflow_total_usdc() {
        // SCOUT-total_usdc-underflow (surfaced by the audit-prep Scout triage):
        // after a reconcile_default loss write-off the vault enters the S > U
        // regime (total_shares > total_usdc). There to_assets(shares) can exceed
        // total_usdc by virtual-offset dust; because total_usdc is a *signed*
        // i128 the `total_usdc -= usdc_out` would NOT trap on going negative, and
        // a permissionless raw-token donation lets the over-payment transfer
        // succeed and persist a negative total_usdc. The withdraw clamp keeps
        // total_usdc >= 0 and caps the payout at the pool's accounted USDC.
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

        let reg_cfg = RegistryConfig {
            min_stake: 100_0000000,
            slash_timeout: 3600,
            slash_rate_bps: 1000, // 10%
            usdc_token: usdc.clone(),
        };
        let registry_id = env.register(KeeperRegistry, (admin.clone(), reg_cfg.clone()));
        let vault_cfg = VaultConfig {
            deposit_cap: 0,
            withdraw_cooldown: 0,
            max_draw_per_keeper: 1000_0000000,
        };
        let vault_id = env.register(
            NectarVault,
            (
                admin.clone(),
                usdc.clone(),
                registry_id.clone(),
                vault_cfg.clone(),
            ),
        );
        let registry = KeeperRegistryClient::new(&env, &registry_id);
        let vault = NectarVaultClient::new(&env, &vault_id);
        registry.set_vault(&admin, &vault_id);

        let keeper = Address::generate(&env);
        usdc_admin_client.mint(&keeper, &100_0000000);
        registry.register(&keeper, &SorString::from_str(&env, "k1"));
        let depositor = Address::generate(&env);
        usdc_admin_client.mint(&depositor, &1000_0000000);
        vault.deposit(&depositor, &1000_0000000);

        // Keeper draws 400 and absconds; slash writes off the loss → S > U.
        vault.draw(&keeper, &400_0000000, &usdc);
        set_time(&env, 4000, 100);
        registry.slash(&keeper);
        let state = vault.get_state();
        assert_eq!(state.total_usdc, 610_0000000); // U = 610
        assert!(state.total_shares > state.total_usdc); // S > U regime confirmed

        // Permissionless raw-token donation: inflate the vault's liquid balance so
        // an over-payment transfer would otherwise succeed. Not credited to books.
        usdc_admin_client.mint(&vault_id, &5_0000000);

        // Full withdrawal by the sole depositor: without the clamp this drives
        // total_usdc negative; with it, payout is capped at the accounted 610.
        let (shares, _) = vault.balance(&depositor);
        let out = vault.withdraw(&depositor, &shares);

        let state = vault.get_state();
        assert!(
            state.total_usdc >= 0,
            "total_usdc underflowed to {}",
            state.total_usdc
        );
        assert!(out <= 610_0000000, "withdrew {} > accounted 610 USDC", out);
        // The donation is NOT extractable by the withdrawer — it stays in the
        // contract (recoverable by later depositors), never over-paid out.
        assert!(token::Client::new(&env, &usdc).balance(&vault_id) >= 5_0000000);
    }
}
