use soroban_sdk::{contracterror, contracttype, Address};

#[contracttype]
#[derive(Clone, Debug)]
pub struct Depositor {
    pub addr: Address,
    pub shares: i128,
    pub deposited_at: u64,
    pub last_deposit_time: u64,
    // Fixed-window withdrawal rate limiting (T3): USDC withdrawn since
    // window_start; the window resets once now - window_start >= 86400.
    // Only maintained while VaultConfig.max_withdraw_per_24h > 0.
    pub window_start: u64,
    pub withdrawn_in_window: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct VaultState {
    pub total_usdc: i128,
    pub total_shares: i128,
    pub total_profit: i128,
    pub active_liq: i128,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct VaultConfig {
    pub deposit_cap: i128,
    pub withdraw_cooldown: u64,
    pub max_draw_per_keeper: i128,
    // Per-address 24h withdrawal cap in USDC stroops (T3 rate limiting);
    // 0 disables the limit.
    pub max_withdraw_per_24h: i128,
}

// Per-keeper outstanding draw record (T3, F4). `since` is the ledger timestamp
// of the MOST RECENT draw — mirroring the registry's last_draw_time — so slash
// eligibility can be computed consistently from vault + registry state, and a
// restarted keeper can age its own stale draw from chain state alone.
#[contracttype]
#[derive(Clone, Debug)]
pub struct DrawInfo {
    pub amount: i128,
    pub since: u64,
}

#[contracttype]
pub enum VaultKey {
    Admin,
    Usdc,
    State,
    Depositor(Address),
    KeeperRegistry,
    VaultConfig,
    KeeperDraw(Address),
    Paused,
    // Circuit-breaker flags (T3, DECISION F-2a). Distinct from Paused (VLT-4):
    // these gate draw() only — depositor entry/exit is never governed by them,
    // so depositors can always withdraw during a liquidation pause.
    GlobalLiqPause,
    AssetPaused(Address),
}

#[contracterror]
#[derive(Clone, Debug, PartialEq)]
pub enum VaultError {
    AlreadyInit = 1,
    NotInit = 2,
    InsufficientBalance = 3,
    InsufficientVault = 4,
    Unauthorized = 5,
    NoShares = 6,
    DepositCapExceeded = 8,
    WithdrawalCooldown = 9,
    DrawLimitExceeded = 10,
    Paused = 11,
    // A deposit so small (relative to the virtual-offset share price) that it
    // would mint zero shares. Rejected so a depositor can never fund the pool
    // for nothing (share-inflation defense, VLT-1).
    ZeroShares = 12,
    // return_proceeds called with no outstanding draw for the keeper. Blocks the
    // anonymous donation-as-profit path (VLT-2).
    NoDraw = 13,
    // draw() while the admin has ALL liquidations paused (global circuit
    // breaker). Deposit stays governed by the VLT-4 Paused flag alone, and
    // withdraw is never blocked by a liquidation pause.
    LiquidationsPaused = 14,
    // draw() declared a collateral asset the admin has individually paused.
    AssetPaused = 15,
    // withdraw() would exceed the per-address 24h cap
    // (VaultConfig.max_withdraw_per_24h) inside the current fixed window.
    WithdrawalRateLimited = 16,
    // add_profit() with a non-positive amount.
    InvalidAmount = 17,
}
