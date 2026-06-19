#!/usr/bin/env bash
# railway-keeper-env.sh — set all public env vars on the linked Railway service.
#
# Usage:
#   cd keeper && railway link            # pick the right service first
#   ../scripts/railway-keeper-env.sh keeper-alpha
#   ../scripts/railway-keeper-env.sh keeper-beta
#
# This script does NOT set KEEPER_SECRET — set that in the Railway dashboard
# (Variables tab → New Variable → tick "Mark as secret"). Keys end up in
# shell history if set via CLI.

set -euo pipefail

NAME="${1:-keeper-alpha}"
case "$NAME" in
  keeper-alpha|keeper-beta|keeper-gamma) ;;
  *)
    echo "name must be keeper-alpha, keeper-beta or keeper-gamma" >&2
    exit 1
    ;;
esac

# Tranche 1 hardened contracts (testnet, redeployed 2026-05-24) — current.
# Source of truth: wallets.md. Earlier (May-13) addresses are deprecated.
REGISTRY=CDT257SL2IYDZJIDXEVKI67MYLCKE73JY6WGUTGZOEFXJHG26FJHJDRB
VAULT=CDZR6VDCPQFOFFKKZ2KMVB67Z54LI5OY73NHBFVI6DR6RE6TL7NN7345
USDC=CD34YC6FFI2KIE2U4ZPCGQIRPH7UPG5YY2QBYNP25ATSFOQSG73J4VBW
# Soroswap testnet router — enables collateral->USDC conversion after fills.
SOROSWAP_ROUTER=CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD

ARGS=(
  --set "KEEPER_NAME=${NAME}"
  --set "REGISTRY_CONTRACT=${REGISTRY}"
  --set "VAULT_CONTRACT=${VAULT}"
  --set "USDC_CONTRACT=${USDC}"
  --set "SOROSWAP_ROUTER=${SOROSWAP_ROUTER}"
  --set "SLIPPAGE_BPS=100"
  --set "SOROBAN_RPC=https://soroban-testnet.stellar.org:443"
  --set "HORIZON_URL=https://horizon-testnet.stellar.org"
  --set "POLL_INTERVAL=10"
  --set "MIN_PROFIT=1.02"
  --set "API_PORT=8080"
)

# Blend testnet pool — only set when non-empty (Railway CLI rejects "KEY=").
# Set BLEND_POOL=... in the environment before running, or set it later via
# `railway variables --set BLEND_POOL=C...`. Without it, the keeper runs in
# vault-monitor-only mode (no liquidation cycle, but the API still serves).
if [[ -n "${BLEND_POOL:-}" ]]; then
  ARGS+=(--set "BLEND_POOL=${BLEND_POOL}")
fi

railway variables "${ARGS[@]}"

echo
echo "Public env vars set for ${NAME}."
echo "Now set KEEPER_SECRET in the Railway dashboard (Variables → New Variable → mark as secret)."
echo
case "$NAME" in
  keeper-alpha) echo "  KEEPER_SECRET → alpha's secret from deploy/keepers.json"  ;;
  keeper-beta)  echo "  KEEPER_SECRET → beta's secret from deploy/keepers.json"   ;;
  keeper-gamma) echo "  KEEPER_SECRET → gamma's secret from deploy/keepers.json"  ;;
esac
