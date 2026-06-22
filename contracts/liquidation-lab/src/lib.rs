#![no_std]

mod types;

#[cfg(test)]
mod test;

use soroban_sdk::{contract, contractimpl, token, Address, Env, Map, Symbol, TryFromVal, Val, Vec};
use types::{AuctionData, DataKey, LabError, ReserveConfig, UserPositions};

#[contract]
pub struct LiquidationLab;

#[contractimpl]
impl LiquidationLab {
    // ── Admin Functions ──────────────────────────────────────────

    pub fn initialize(env: Env, admin: Address) -> Result<(), LabError> {
        if env.storage().instance().has(&DataKey::Admin) {
            return Err(LabError::AlreadyInit);
        }
        env.storage().instance().set(&DataKey::Admin, &admin);
        let empty: Vec<Address> = Vec::new(&env);
        env.storage().instance().set(&DataKey::ReserveList, &empty);
        env.storage().instance().extend_ttl(535680, 535680);
        Ok(())
    }

    pub fn admin_add_reserve(
        env: Env,
        admin: Address,
        asset: Address,
        config: ReserveConfig,
    ) -> Result<(), LabError> {
        admin.require_auth();
        Self::require_admin(&env, &admin)?;

        let mut list: Vec<Address> = env
            .storage()
            .instance()
            .get(&DataKey::ReserveList)
            .unwrap_or(Vec::new(&env));
        list.push_back(asset.clone());
        env.storage().instance().set(&DataKey::ReserveList, &list);
        env.storage()
            .persistent()
            .set(&DataKey::Reserve(asset), &config);
        Ok(())
    }

    pub fn admin_set_reserve(
        env: Env,
        admin: Address,
        asset: Address,
        config: ReserveConfig,
    ) -> Result<(), LabError> {
        admin.require_auth();
        Self::require_admin(&env, &admin)?;
        env.storage()
            .persistent()
            .set(&DataKey::Reserve(asset), &config);
        Ok(())
    }

    pub fn admin_set_position(
        env: Env,
        admin: Address,
        user: Address,
        positions: UserPositions,
    ) -> Result<(), LabError> {
        admin.require_auth();
        Self::require_admin(&env, &admin)?;
        env.storage()
            .persistent()
            .set(&DataKey::Position(user.clone()), &positions);
        env.storage()
            .persistent()
            .extend_ttl(&DataKey::Position(user.clone()), 535680, 535680);

        // Emit event so the Go keeper discovers this user via event scanning
        env.events()
            .publish((Symbol::new(&env, "set_position"), user), 0i128);
        Ok(())
    }

    pub fn admin_clear_auction(
        env: Env,
        admin: Address,
        user: Address,
    ) -> Result<(), LabError> {
        admin.require_auth();
        Self::require_admin(&env, &admin)?;
        env.storage()
            .persistent()
            .remove(&DataKey::Auction(user));
        Ok(())
    }

    // ── Blend-Compatible Functions (called by Go keeper) ─────────

    pub fn get_reserve_list(env: Env) -> Vec<Address> {
        env.storage()
            .instance()
            .get(&DataKey::ReserveList)
            .unwrap_or(Vec::new(&env))
    }

    pub fn get_reserve(env: Env, asset: Address) -> ReserveConfig {
        env.storage()
            .persistent()
            .get(&DataKey::Reserve(asset))
            .expect("reserve not found")
    }

    pub fn get_positions(env: Env, address: Address) -> UserPositions {
        env.storage()
            .persistent()
            .get(&DataKey::Position(address))
            .unwrap_or(UserPositions {
                collateral: Map::new(&env),
                liabilities: Map::new(&env),
            })
    }

    /// Creates a liquidation auction for a user.
    /// pct is scaled by 1e7 from the keeper (e.g., 50% = 500_000_000).
    /// We treat pct / 1_000_000_000 as the fraction.
    pub fn new_liquidation_auction(
        env: Env,
        user: Address,
        pct: u64,
    ) -> Result<AuctionData, LabError> {
        let key = DataKey::Auction(user.clone());
        if env.storage().persistent().has(&key) {
            return Err(LabError::AuctionExists);
        }

        let positions: UserPositions = env
            .storage()
            .persistent()
            .get(&DataKey::Position(user.clone()))
            .ok_or(LabError::PositionNotFound)?;

        let reserve_list: Vec<Address> = env
            .storage()
            .instance()
            .get(&DataKey::ReserveList)
            .unwrap_or(Vec::new(&env));

        let denom: i128 = 1_000_000_000;
        let pct_i128 = pct as i128;

        let mut lot: Map<Address, i128> = Map::new(&env);
        for i in 0..reserve_list.len() {
            let idx = i as u32;
            if let Some(amt) = positions.collateral.get(idx) {
                let lot_amt = amt * pct_i128 / denom;
                if lot_amt > 0 {
                    lot.set(reserve_list.get(i).unwrap(), lot_amt);
                }
            }
        }

        let mut bid: Map<Address, i128> = Map::new(&env);
        for i in 0..reserve_list.len() {
            let idx = i as u32;
            if let Some(amt) = positions.liabilities.get(idx) {
                let bid_amt = amt * pct_i128 / denom;
                if bid_amt > 0 {
                    bid.set(reserve_list.get(i).unwrap(), bid_amt);
                }
            }
        }

        // Set block to 150 ledgers ago so the Dutch auction is already in
        // the profitable zone (elapsed=150 → lotPct=75%, bidPct=25% → ratio=3.0)
        let block = env.ledger().sequence().saturating_sub(150);
        let auction = AuctionData {
            lot,
            bid,
            block,
        };

        env.storage().persistent().set(&key, &auction);
        env.storage().persistent().extend_ttl(&key, 535680, 535680);

        env.events().publish(
            (Symbol::new(&env, "new_auction"), user),
            auction.block,
        );

        Ok(auction)
    }

    pub fn get_auction(
        env: Env,
        _auction_type: u64,
        user: Address,
    ) -> Result<AuctionData, LabError> {
        env.storage()
            .persistent()
            .get(&DataKey::Auction(user))
            .ok_or(LabError::AuctionNotFound)
    }

    /// Fills an auction. The Go keeper sends request_type=6 (FillUserLiquidationAuction).
    /// We parse the requests to find the user, remove the auction, and clear the position.
    ///
    /// The keeper sends requests as Vec<Map<Symbol, Val>> where each map has:
    /// {request_type: U64(6), address: Address(user), amount: U64(0)}
    pub fn submit(
        env: Env,
        from: Address,
        _spender: Address,
        _to: Address,
        requests: Vec<Map<Symbol, Val>>,
    ) -> Result<UserPositions, LabError> {
        from.require_auth();

        // Parse requests to find the user being liquidated
        let mut liquidated_user: Option<Address> = None;

        for i in 0..requests.len() {
            let req_map = requests.get(i).unwrap();
            if let Some(addr_val) = req_map.get(Symbol::new(&env, "address")) {
                if let Ok(addr) = Address::try_from_val(&env, &addr_val) {
                    liquidated_user = Some(addr);
                }
            }
        }

        let user = liquidated_user.ok_or(LabError::AuctionNotFound)?;
        let auction_key = DataKey::Auction(user.clone());

        let auction: AuctionData = env
            .storage()
            .persistent()
            .get(&auction_key)
            .ok_or(LabError::AuctionNotFound)?;

        // Move real tokens like a Blend auction would: the filler pays the bid
        // assets to the lab and receives the lot (seized collateral). The lab
        // must hold the lot assets (pre-funded by the admin). This makes the
        // harness exercise the keeper's full draw -> fill -> swap -> return
        // value cycle end-to-end, not just the auction-lifecycle plumbing.
        let lab = env.current_contract_address();
        for asset in auction.bid.keys() {
            let amt = auction.bid.get(asset.clone()).unwrap_or(0);
            if amt > 0 {
                token::Client::new(&env, &asset).transfer(&from, &lab, &amt);
            }
        }
        for asset in auction.lot.keys() {
            let amt = auction.lot.get(asset.clone()).unwrap_or(0);
            if amt > 0 {
                token::Client::new(&env, &asset).transfer(&lab, &from, &amt);
            }
        }

        // Remove auction (it's been filled)
        env.storage().persistent().remove(&auction_key);

        // Clear the user's position (liquidated)
        let empty = UserPositions {
            collateral: Map::new(&env),
            liabilities: Map::new(&env),
        };
        env.storage()
            .persistent()
            .set(&DataKey::Position(user.clone()), &empty);

        env.events().publish(
            (Symbol::new(&env, "fill_auction"), user.clone(), from),
            0i128,
        );

        Ok(empty)
    }

    // ── Internal ─────────────────────────────────────────────────

    fn require_admin(env: &Env, caller: &Address) -> Result<(), LabError> {
        let admin: Address = env
            .storage()
            .instance()
            .get(&DataKey::Admin)
            .ok_or(LabError::NotInit)?;
        if *caller != admin {
            return Err(LabError::Unauthorized);
        }
        Ok(())
    }
}
