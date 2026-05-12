use soroban_sdk::{contracterror, contracttype, Address, String};

#[contracttype]
#[derive(Clone, Debug)]
pub struct KeeperInfo {
    pub addr: Address,
    pub name: String,
    pub stake: i128,
    pub registered_at: u64,
    pub active: bool,
    pub total_executions: u64,
    pub successful_fills: u64,
    pub total_profit: i128,
    pub last_draw_time: u64,
    pub has_active_draw: bool,
    pub total_response_time_ms: u64,
    pub response_count: u64,
}

#[contracttype]
#[derive(Clone, Debug)]
pub struct RegistryConfig {
    pub min_stake: i128,
    pub slash_timeout: u64,
    pub slash_rate_bps: u32,
    pub usdc_token: Address,
}

#[contracttype]
pub enum DataKey {
    Admin,
    KeeperCount,
    Keeper(Address),
    KeeperList,
    Paused,
    Config,
    VaultAddr,
}

#[contracterror]
#[derive(Clone, Debug, PartialEq)]
pub enum Error {
    AlreadyInit = 1,
    NotInit = 2,
    AlreadyRegistered = 3,
    NotRegistered = 4,
    Unauthorized = 5,
    Paused = 6,
    InsufficientStake = 7,
    ActiveDraw = 8,
    SlashTimeout = 9,
}
