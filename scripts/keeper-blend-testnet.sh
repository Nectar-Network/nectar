#!/usr/bin/env bash
# keeper-blend-testnet.sh — run the local keeper binary against Blend's official
# Soroban testnet pool. Lets you verify the live-Blend integration path without
# touching the LiquidationLab mock.
#
# Pool ID source: https://github.com/blend-capital/blend-utils/blob/main/testnet.contracts.json
#   TestnetV2 = CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF
#
# What this exercises end-to-end:
#   - keeper reads Blend's reserve list + each reserve config
#   - keeper discovers users from pool events
#   - keeper loads each user's positions
#   - keeper computes health factors
#   - if any HF < 1.0 and the bid/lot ratio clears MIN_PROFIT, the keeper
#     attempts new_liquidation_auction + submit (Request{request_type: u32,
#     address, amount: i128} — the on-chain types Blend's pool expects)
#
# Prereqs:
#   - ./scripts/tranche-1-e2e.sh has been run (so .env and .env.wallets exist)
#   - go 1.22+ is installed
#
# Usage:
#   ./scripts/keeper-blend-testnet.sh           # foreground, Ctrl-C to stop
#   ./scripts/keeper-blend-testnet.sh --beta    # use keeper-beta keys
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

source "$ROOT_DIR/.env"
source "$ROOT_DIR/.env.wallets"

WHICH="${1:-alpha}"
case "$WHICH" in
  --alpha|alpha) KEEPER_SECRET="$KEEPER_A_SECRET"; KEEPER_NAME="keeper-alpha" ;;
  --beta|beta)   KEEPER_SECRET="$KEEPER_B_SECRET"; KEEPER_NAME="keeper-beta"  ;;
  *) echo "usage: $0 [alpha|beta]" >&2; exit 1 ;;
esac

export KEEPER_SECRET KEEPER_NAME
export REGISTRY_CONTRACT VAULT_CONTRACT
export BLEND_POOL="${BLEND_POOL:-CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF}"
export SOROBAN_RPC="${SOROBAN_RPC:-https://soroban-testnet.stellar.org:443}"
export HORIZON_URL="${HORIZON_URL:-https://horizon-testnet.stellar.org}"
export API_PORT="${API_PORT:-8090}"
export POLL_INTERVAL="${POLL_INTERVAL:-15}"
export MIN_PROFIT="${MIN_PROFIT:-1.02}"

echo "Running ${KEEPER_NAME} against Blend testnet pool"
echo "  BLEND_POOL=$BLEND_POOL"
echo "  REGISTRY=$REGISTRY_CONTRACT"
echo "  VAULT=$VAULT_CONTRACT"
echo "  API on http://localhost:${API_PORT}"
echo

cd "$ROOT_DIR/keeper" && go run .
