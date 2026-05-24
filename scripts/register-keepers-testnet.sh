#!/usr/bin/env bash
# register-keepers-testnet.sh — Register keeper-alpha and keeper-beta in the KeeperRegistry
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
ENV_FILE="$ROOT_DIR/.env"
ENV_WALLETS="$ROOT_DIR/.env.wallets"

source "$ENV_WALLETS"
source "$ENV_FILE"

: "${REGISTRY_CONTRACT:?REGISTRY_CONTRACT not set in .env}"
: "${USDC_CONTRACT:?USDC_CONTRACT not set in .env}"

RPC_URL="${SOROBAN_RPC:-https://soroban-testnet.stellar.org:443}"
PASSPHRASE="${NETWORK_PASSPHRASE:-Test SDF Network ; September 2015}"

# Keepers stake MIN_STAKE USDC on register(). Mint 2× margin so they can
# also pay tx fees / participate in liquidations later from the same key.
MIN_STAKE="${MIN_STAKE:-1000000000}"               # 100 USDC (7-decimal stroops)
KEEPER_MINT_AMOUNT=$((MIN_STAKE * 2))

mint_usdc() {
  local to="$1"
  echo -n "Minting ${KEEPER_MINT_AMOUNT} USDC stroops → $to ... "
  stellar contract invoke \
    --id "$USDC_CONTRACT" \
    --source "$ADMIN_SECRET" \
    --rpc-url "$RPC_URL" \
    --network-passphrase "$PASSPHRASE" \
    -- mint \
      --to "$to" \
      --amount "$KEEPER_MINT_AMOUNT" \
    >/dev/null
  echo "OK"
}

register_keeper() {
  local secret="$1"
  local address="$2"
  local name="$3"
  echo -n "Registering $name ($address)... "
  stellar contract invoke \
    --id "$REGISTRY_CONTRACT" \
    --source "$secret" \
    --rpc-url "$RPC_URL" \
    --network-passphrase "$PASSPHRASE" \
    -- register \
      --operator "$address" \
      --name "$name"
  echo "OK"
}

# Mint stake → register. Both keepers in sequence.
mint_usdc "$KEEPER_A_ADDRESS"
register_keeper "$KEEPER_A_SECRET" "$KEEPER_A_ADDRESS" "keeper-alpha"
mint_usdc "$KEEPER_B_ADDRESS"
register_keeper "$KEEPER_B_SECRET" "$KEEPER_B_ADDRESS" "keeper-beta"

echo ""
echo "Keeper count:"
stellar contract invoke \
  --id "$REGISTRY_CONTRACT" \
  --source "$ADMIN_SECRET" \
  --rpc-url "$RPC_URL" \
  --network-passphrase "$PASSPHRASE" \
  -- keeper_count
