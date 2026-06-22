#![cfg(test)]

use soroban_sdk::{testutils::Address as _, token, Address, Env, IntoVal, Map};

use crate::{LiquidationLab, LiquidationLabClient};
use crate::types::{LabError, ReserveConfig, UserPositions};

fn setup() -> (Env, LiquidationLabClient<'static>, Address) {
    let env = Env::default();
    env.mock_all_auths();
    let contract_id = env.register(LiquidationLab, ());
    let client = LiquidationLabClient::new(&env, &contract_id);
    let admin = Address::generate(&env);
    client.initialize(&admin);
    (env, client, admin)
}

fn make_reserve(index: u32) -> ReserveConfig {
    ReserveConfig {
        index,
        c_factor: 7_500_000,
        l_factor: 11_000_000,
        b_rate: 10_000_000,
        d_rate: 10_000_000,
    }
}

#[test]
fn test_initialize() {
    let env = Env::default();
    env.mock_all_auths();
    let id = env.register(LiquidationLab, ());
    let client = LiquidationLabClient::new(&env, &id);
    let admin = Address::generate(&env);
    client.initialize(&admin);

    // Double init fails
    let res = client.try_initialize(&admin);
    assert_eq!(res.err(), Some(Ok(LabError::AlreadyInit)));
}

#[test]
fn test_add_reserve_and_get_list() {
    let (env, client, admin) = setup();
    let asset1 = Address::generate(&env);
    let asset2 = Address::generate(&env);

    client.admin_add_reserve(&admin, &asset1, &make_reserve(0));
    client.admin_add_reserve(&admin, &asset2, &make_reserve(1));

    let list = client.get_reserve_list();
    assert_eq!(list.len(), 2);
    assert_eq!(list.get(0).unwrap(), asset1);
    assert_eq!(list.get(1).unwrap(), asset2);
}

#[test]
fn test_set_and_get_reserve() {
    let (env, client, admin) = setup();
    let asset = Address::generate(&env);
    let config = make_reserve(0);

    client.admin_add_reserve(&admin, &asset, &config);
    let got = client.get_reserve(&asset);

    assert_eq!(got.index, 0);
    assert_eq!(got.c_factor, 7_500_000);
    assert_eq!(got.l_factor, 11_000_000);
    assert_eq!(got.b_rate, 10_000_000);
    assert_eq!(got.d_rate, 10_000_000);
}

#[test]
fn test_set_and_get_positions() {
    let (env, client, admin) = setup();
    let user = Address::generate(&env);

    let mut coll = Map::new(&env);
    coll.set(0u32, 50_000_000_000i128);
    let mut liab = Map::new(&env);
    liab.set(1u32, 8_500_000_000i128);

    let pos = UserPositions {
        collateral: coll,
        liabilities: liab,
    };
    client.admin_set_position(&admin, &user, &pos);

    let got = client.get_positions(&user);
    assert_eq!(got.collateral.get(0).unwrap(), 50_000_000_000i128);
    assert_eq!(got.liabilities.get(1).unwrap(), 8_500_000_000i128);
}

#[test]
fn test_get_positions_empty_user() {
    let (env, client, _admin) = setup();
    let unknown = Address::generate(&env);
    let pos = client.get_positions(&unknown);
    assert_eq!(pos.collateral.len(), 0);
    assert_eq!(pos.liabilities.len(), 0);
}

#[test]
fn test_create_auction() {
    let (env, client, admin) = setup();
    let asset0 = Address::generate(&env);
    let asset1 = Address::generate(&env);
    let user = Address::generate(&env);

    client.admin_add_reserve(&admin, &asset0, &make_reserve(0));
    client.admin_add_reserve(&admin, &asset1, &make_reserve(1));

    let mut coll = Map::new(&env);
    coll.set(0u32, 50_000_000_000i128);
    let mut liab = Map::new(&env);
    liab.set(1u32, 8_500_000_000i128);
    client.admin_set_position(&admin, &user, &UserPositions {
        collateral: coll,
        liabilities: liab,
    });

    // 50% = 500_000_000 (pct * 1e7)
    let auction = client.new_liquidation_auction(&user, &500_000_000u64);

    // lot should be 50% of collateral
    assert_eq!(auction.lot.get(asset0).unwrap(), 25_000_000_000i128);
    // bid should be 50% of liabilities
    assert_eq!(auction.bid.get(asset1).unwrap(), 4_250_000_000i128);
}

#[test]
fn test_auction_exists_error() {
    let (env, client, admin) = setup();
    let asset0 = Address::generate(&env);
    let user = Address::generate(&env);

    client.admin_add_reserve(&admin, &asset0, &make_reserve(0));

    let mut coll = Map::new(&env);
    coll.set(0u32, 10_000_000_000i128);
    client.admin_set_position(&admin, &user, &UserPositions {
        collateral: coll,
        liabilities: Map::new(&env),
    });

    client.new_liquidation_auction(&user, &500_000_000u64);

    // Second auction for same user should fail
    let res = client.try_new_liquidation_auction(&user, &500_000_000u64);
    assert_eq!(res.err(), Some(Ok(LabError::AuctionExists)));
}

#[test]
fn test_get_auction() {
    let (env, client, admin) = setup();
    let asset0 = Address::generate(&env);
    let user = Address::generate(&env);

    client.admin_add_reserve(&admin, &asset0, &make_reserve(0));
    let mut coll = Map::new(&env);
    coll.set(0u32, 10_000_000_000i128);
    client.admin_set_position(&admin, &user, &UserPositions {
        collateral: coll,
        liabilities: Map::new(&env),
    });

    client.new_liquidation_auction(&user, &500_000_000u64);

    let auction = client.get_auction(&0u64, &user);
    assert_eq!(auction.lot.get(asset0).unwrap(), 5_000_000_000i128);
}

#[test]
fn test_get_auction_not_found() {
    let (env, client, _admin) = setup();
    let user = Address::generate(&env);
    let res = client.try_get_auction(&0u64, &user);
    assert_eq!(res.err(), Some(Ok(LabError::AuctionNotFound)));
}

#[test]
fn test_submit_fills_auction() {
    let (env, client, admin) = setup();
    let lab_id = client.address.clone();

    // Real SAC tokens back the lot (collateral) and bid (debt) assets so the
    // fill moves real value, like a Blend auction.
    let xlm_sac = env.register_stellar_asset_contract_v2(Address::generate(&env));
    let usdc_sac = env.register_stellar_asset_contract_v2(Address::generate(&env));
    let asset0 = xlm_sac.address();
    let asset1 = usdc_sac.address();
    let xlm = token::TokenClient::new(&env, &asset0);
    let usdc = token::TokenClient::new(&env, &asset1);
    let xlm_admin = token::StellarAssetClient::new(&env, &asset0);
    let usdc_admin = token::StellarAssetClient::new(&env, &asset1);

    let user = Address::generate(&env);
    let keeper = Address::generate(&env);

    client.admin_add_reserve(&admin, &asset0, &make_reserve(0));
    client.admin_add_reserve(&admin, &asset1, &make_reserve(1));

    let mut coll = Map::new(&env);
    coll.set(0u32, 50_000_000_000i128);
    let mut liab = Map::new(&env);
    liab.set(1u32, 8_500_000_000i128);
    client.admin_set_position(&admin, &user, &UserPositions {
        collateral: coll,
        liabilities: liab,
    });

    client.new_liquidation_auction(&user, &500_000_000u64);
    // lot = 25_000_000_000 XLM, bid = 4_250_000_000 USDC

    // Fund the lab with the lot to pay out, and the keeper with the bid to pay.
    xlm_admin.mint(&lab_id, &25_000_000_000i128);
    usdc_admin.mint(&keeper, &4_250_000_000i128);

    // Build fill request: [{request_type: 6, address: user, amount: 0}]
    let mut req_map: Map<soroban_sdk::Symbol, soroban_sdk::Val> = Map::new(&env);
    req_map.set(
        soroban_sdk::Symbol::new(&env, "request_type"),
        6u64.into_val(&env),
    );
    req_map.set(
        soroban_sdk::Symbol::new(&env, "address"),
        user.into_val(&env),
    );
    req_map.set(
        soroban_sdk::Symbol::new(&env, "amount"),
        0u64.into_val(&env),
    );

    let mut requests: soroban_sdk::Vec<Map<soroban_sdk::Symbol, soroban_sdk::Val>> = soroban_sdk::Vec::new(&env);
    requests.push_back(req_map);

    let result = client.submit(&keeper, &keeper, &keeper, &requests);

    // Position should be cleared
    assert_eq!(result.collateral.len(), 0);
    assert_eq!(result.liabilities.len(), 0);

    // Tokens moved: keeper received the lot, lab received the bid.
    assert_eq!(xlm.balance(&keeper), 25_000_000_000i128);
    assert_eq!(usdc.balance(&lab_id), 4_250_000_000i128);

    // Auction should be gone
    let res = client.try_get_auction(&0u64, &user);
    assert_eq!(res.err(), Some(Ok(LabError::AuctionNotFound)));
}

#[test]
fn test_submit_already_filled() {
    let (env, client, _admin) = setup();
    let keeper = Address::generate(&env);
    let user = Address::generate(&env);

    // No auction exists — submit should fail
    let mut req_map: Map<soroban_sdk::Symbol, soroban_sdk::Val> = Map::new(&env);
    req_map.set(
        soroban_sdk::Symbol::new(&env, "request_type"),
        6u64.into_val(&env),
    );
    req_map.set(
        soroban_sdk::Symbol::new(&env, "address"),
        user.into_val(&env),
    );
    req_map.set(
        soroban_sdk::Symbol::new(&env, "amount"),
        0u64.into_val(&env),
    );

    let mut requests: soroban_sdk::Vec<Map<soroban_sdk::Symbol, soroban_sdk::Val>> = soroban_sdk::Vec::new(&env);
    requests.push_back(req_map);

    let res = client.try_submit(&keeper, &keeper, &keeper, &requests);
    assert_eq!(res.err(), Some(Ok(LabError::AuctionNotFound)));
}

#[test]
fn test_full_flow() {
    let (env, client, admin) = setup();
    let lab_id = client.address.clone();
    let xlm_sac = env.register_stellar_asset_contract_v2(Address::generate(&env));
    let usdc_sac = env.register_stellar_asset_contract_v2(Address::generate(&env));
    let xlm = xlm_sac.address();
    let usdc = usdc_sac.address();
    let xlm_tok = token::TokenClient::new(&env, &xlm);
    let xlm_admin = token::StellarAssetClient::new(&env, &xlm);
    let usdc_admin = token::StellarAssetClient::new(&env, &usdc);
    let borrower = Address::generate(&env);
    let keeper = Address::generate(&env);

    // 1. Add reserves
    client.admin_add_reserve(&admin, &xlm, &make_reserve(0));
    client.admin_add_reserve(&admin, &usdc, &make_reserve(1));

    // 2. Set position: 5000 XLM collateral, 850 USDC liability
    let mut coll = Map::new(&env);
    coll.set(0u32, 50_000_000_000i128);
    let mut liab = Map::new(&env);
    liab.set(1u32, 8_500_000_000i128);
    client.admin_set_position(&admin, &borrower, &UserPositions {
        collateral: coll,
        liabilities: liab,
    });

    // 3. Verify position
    let pos = client.get_positions(&borrower);
    assert_eq!(pos.collateral.get(0).unwrap(), 50_000_000_000i128);

    // 4. Create auction (50%)
    let auction = client.new_liquidation_auction(&borrower, &500_000_000u64);
    assert!(auction.lot.len() > 0);
    assert!(auction.bid.len() > 0);

    // Fund the lab with the lot and the keeper with the bid so the fill can
    // move real tokens.
    xlm_admin.mint(&lab_id, &25_000_000_000i128);
    usdc_admin.mint(&keeper, &4_250_000_000i128);

    // 5. Fill auction
    let mut req_map: Map<soroban_sdk::Symbol, soroban_sdk::Val> = Map::new(&env);
    req_map.set(soroban_sdk::Symbol::new(&env, "request_type"), 6u64.into_val(&env));
    req_map.set(soroban_sdk::Symbol::new(&env, "address"), borrower.into_val(&env));
    req_map.set(soroban_sdk::Symbol::new(&env, "amount"), 0u64.into_val(&env));

    let mut requests: soroban_sdk::Vec<Map<soroban_sdk::Symbol, soroban_sdk::Val>> = soroban_sdk::Vec::new(&env);
    requests.push_back(req_map);

    client.submit(&keeper, &keeper, &keeper, &requests);

    // 6. Auction gone, position cleared
    let res = client.try_get_auction(&0u64, &borrower);
    assert!(res.is_err());

    let cleared = client.get_positions(&borrower);
    assert_eq!(cleared.collateral.len(), 0);
    assert_eq!(cleared.liabilities.len(), 0);

    // Keeper received the seized collateral (the lot).
    assert_eq!(xlm_tok.balance(&keeper), 25_000_000_000i128);
}
