#!/usr/bin/env bash
set -euo pipefail

: "${ADMIN_SECRET:?ADMIN_SECRET required}"
: "${ADMIN_ADDRESS:?ADMIN_ADDRESS required}"
: "${USDC_CONTRACT:?USDC_CONTRACT required — testnet USDC token address}"

# Tranche 1 config knobs (override via env)
MIN_STAKE="${MIN_STAKE:-1000000000}"               # 100 USDC, 7 decimals
SLASH_TIMEOUT="${SLASH_TIMEOUT:-3600}"             # 1 h
SLASH_RATE_BPS="${SLASH_RATE_BPS:-1000}"           # 10 %
DEPOSIT_CAP="${DEPOSIT_CAP:-50000000000000}"       # $5,000,000 USDC; 0 = unlimited
WITHDRAW_COOLDOWN="${WITHDRAW_COOLDOWN:-3600}"     # 1 h
MAX_DRAW_PER_KEEPER="${MAX_DRAW_PER_KEEPER:-100000000000}"  # $10,000 USDC
MAX_WITHDRAW_PER_24H="${MAX_WITHDRAW_PER_24H:-0}"  # per-address 24h cap; 0 = disabled (set explicitly for mainnet)

RPC_URL="${SOROBAN_RPC:-https://soroban-testnet.stellar.org:443}"
PASSPHRASE="${NETWORK_PASSPHRASE:-Test SDF Network ; September 2015}"

REPO_ROOT="$(cd "$(dirname "$0")/.." && pwd)"
ENV_FILE="$REPO_ROOT/.env"

update_env() {
  local key="$1" val="$2"
  if grep -q "^${key}=" "$ENV_FILE" 2>/dev/null; then
    sed -i.bak "s|^${key}=.*|${key}=${val}|" "$ENV_FILE" && rm -f "$ENV_FILE.bak"
  else
    echo "${key}=${val}" >> "$ENV_FILE"
  fi
}

invoke() {
  stellar contract invoke \
    --id "$1" \
    --source "$ADMIN_SECRET" \
    --rpc-url "$RPC_URL" \
    --network-passphrase "$PASSPHRASE" \
    "${@:2}"
}

# ── 1. Build both contracts ───────────────────────────────────────────────────
echo "Building contracts..."
cd "$REPO_ROOT"
cargo build --target wasm32-unknown-unknown --release \
  2>&1 | grep -E "Compiling|Finished|error" || true

REGISTRY_WASM="target/wasm32-unknown-unknown/release/keeper_registry.wasm"
VAULT_WASM="target/wasm32-unknown-unknown/release/nectar_vault.wasm"

REGISTRY_OPT="${REGISTRY_WASM%.wasm}.optimized.wasm"
VAULT_OPT="${VAULT_WASM%.wasm}.optimized.wasm"

if [[ "${SKIP_OPTIMIZE:-0}" == "1" ]]; then
  echo "Skipping wasm-opt (SKIP_OPTIMIZE=1) — using raw release wasms."
  cp "$REGISTRY_WASM" "$REGISTRY_OPT"
  cp "$VAULT_WASM" "$VAULT_OPT"
else
  echo "Optimizing WASMs..."
  stellar contract optimize --wasm "$REGISTRY_WASM"
  stellar contract optimize --wasm "$VAULT_WASM"
fi

# ── 2. Deploy KeeperRegistry — admin + config set atomically in the deploy tx
#      via the contract constructor (closes the init front-run, NEW-init). The
#      registry's vault is linked afterwards (they reference each other).
echo "Deploying KeeperRegistry..."
REGISTRY_ID=$(stellar contract deploy \
  --wasm "$REGISTRY_OPT" \
  --source "$ADMIN_SECRET" \
  --rpc-url "$RPC_URL" \
  --network-passphrase "$PASSPHRASE" \
  -- \
  --admin "$ADMIN_ADDRESS" \
  --config "{\"min_stake\":\"$MIN_STAKE\",\"slash_timeout\":$SLASH_TIMEOUT,\"slash_rate_bps\":$SLASH_RATE_BPS,\"usdc_token\":\"$USDC_CONTRACT\"}")
echo "KeeperRegistry: $REGISTRY_ID"

# ── 3. Deploy NectarVault — admin + usdc + registry + config via constructor.
echo "Deploying NectarVault..."
VAULT_ID=$(stellar contract deploy \
  --wasm "$VAULT_OPT" \
  --source "$ADMIN_SECRET" \
  --rpc-url "$RPC_URL" \
  --network-passphrase "$PASSPHRASE" \
  -- \
  --admin "$ADMIN_ADDRESS" \
  --usdc_token "$USDC_CONTRACT" \
  --registry "$REGISTRY_ID" \
  --config "{\"deposit_cap\":\"$DEPOSIT_CAP\",\"withdraw_cooldown\":$WITHDRAW_COOLDOWN,\"max_draw_per_keeper\":\"$MAX_DRAW_PER_KEEPER\",\"max_withdraw_per_24h\":\"$MAX_WITHDRAW_PER_24H\"}")
echo "NectarVault:    $VAULT_ID"

# ── 4. Link the vault into the registry (one-time, admin-gated set_vault) ─────
echo "Linking vault to registry (set_vault)..."
invoke "$REGISTRY_ID" -- set_vault \
  --admin "$ADMIN_ADDRESS" \
  --vault "$VAULT_ID"

update_env "REGISTRY_CONTRACT" "$REGISTRY_ID"
update_env "VAULT_CONTRACT" "$VAULT_ID"

cat <<EOF

Done.
  REGISTRY_CONTRACT=$REGISTRY_ID
  VAULT_CONTRACT=$VAULT_ID

Frontend env vars (paste into frontend/.env.local before deploying):
  NEXT_PUBLIC_REGISTRY_CONTRACT=$REGISTRY_ID
  NEXT_PUBLIC_VAULT_CONTRACT=$VAULT_ID

Registry config: min_stake=$MIN_STAKE slash_timeout=${SLASH_TIMEOUT}s slash_rate_bps=$SLASH_RATE_BPS
Vault config:    deposit_cap=$DEPOSIT_CAP cooldown=${WITHDRAW_COOLDOWN}s max_draw=$MAX_DRAW_PER_KEEPER max_withdraw_per_24h=$MAX_WITHDRAW_PER_24H

Saved to $ENV_FILE
EOF
