use soroban_sdk::{contracterror, contracttype, Address};

#[contracttype]
#[derive(Clone, Debug)]
pub struct Depositor {
    pub addr: Address,
    pub shares: i128,
    pub deposited_at: u64,
    pub last_deposit_time: u64,
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
}
