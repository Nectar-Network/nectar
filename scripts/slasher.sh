#!/usr/bin/env bash
# slasher.sh — standalone slasher cron for the Nectar KeeperRegistry.
#
# Scans every registered keeper, and for any keeper whose `has_active_draw=true`
# and `last_draw_time + slash_timeout < now`, invokes `slash(operator)`.
#
# The contract requires no admin rights — any account with XLM can call slash().
# Run this as a cron job alongside (or instead of) the in-keeper SLASH_SCAN_EVERY
# sweep, if you want a third party watching the registry.
#
# Usage:
#   SLASHER_SECRET=Sxxx... ./scripts/slasher.sh                   # one-shot
#   LOOP=1 INTERVAL=300 ./scripts/slasher.sh                     # 5-min loop
#
# Env (required):
#   SLASHER_SECRET     S... key of the slasher account (only needs XLM for fees)
#   REGISTRY_CONTRACT  KeeperRegistry contract id
#
# Env (optional):
#   SOROBAN_RPC        default: https://soroban-testnet.stellar.org:443
#   NETWORK_PASSPHRASE default: "Test SDF Network ; September 2015"
#   LOOP               1 → run forever with INTERVAL seconds between sweeps
#   INTERVAL           sweep cadence in seconds when LOOP=1 (default 300)

set -euo pipefail

: "${SLASHER_SECRET:?SLASHER_SECRET required}"
: "${REGISTRY_CONTRACT:?REGISTRY_CONTRACT required}"

RPC_URL="${SOROBAN_RPC:-https://soroban-testnet.stellar.org:443}"
PASSPHRASE="${NETWORK_PASSPHRASE:-Test SDF Network ; September 2015}"
INTERVAL="${INTERVAL:-300}"

need() {
  command -v "$1" >/dev/null 2>&1 || { echo "ERROR: '$1' not in PATH" >&2; exit 1; }
}
need stellar
need jq

view() {
  stellar contract invoke \
    --id "$REGISTRY_CONTRACT" --source "$SLASHER_SECRET" \
    --rpc-url "$RPC_URL" --network-passphrase "$PASSPHRASE" \
    --send no \
    -- "$@" 2>/dev/null
}

invoke() {
  stellar contract invoke \
    --id "$REGISTRY_CONTRACT" --source "$SLASHER_SECRET" \
    --rpc-url "$RPC_URL" --network-passphrase "$PASSPHRASE" \
    -- "$@"
}

sweep_once() {
  local now_ts
  now_ts=$(date +%s)
  local timeout
  timeout=$(view get_config | jq -r '.slash_timeout')
  if [[ -z "$timeout" || "$timeout" == "null" ]]; then
    echo "ERR: cannot read slash_timeout from registry config" >&2
    return 1
  fi

  local keepers
  keepers=$(view get_keepers | jq -r '.[]')
  if [[ -z "$keepers" ]]; then
    echo "[$(date -u +%FT%TZ)] no registered keepers"
    return 0
  fi

  local checked=0 candidates=0 slashed=0
  while IFS= read -r k; do
    [[ -z "$k" ]] && continue
    checked=$((checked + 1))

    local info has_draw last_draw
    info=$(view get_keeper --operator "$k") || continue
    has_draw=$(echo "$info" | jq -r '.has_active_draw // false')
    last_draw=$(echo "$info" | jq -r '.last_draw_time // 0')

    if [[ "$has_draw" != "true" ]]; then continue; fi
    local elapsed=$((now_ts - last_draw))
    if [[ $elapsed -le $timeout ]]; then continue; fi

    candidates=$((candidates + 1))
    echo "[$(date -u +%FT%TZ)] slashing $k (drawn $elapsed s ago, timeout $timeout)"
    if invoke slash --keeper "$k"; then
      slashed=$((slashed + 1))
    else
      echo "  ↑ slash() reverted; will retry next sweep"
    fi
  done <<<"$keepers"

  echo "[$(date -u +%FT%TZ)] sweep done: checked=$checked candidates=$candidates slashed=$slashed"
}

if [[ "${LOOP:-0}" == "1" ]]; then
  trap 'echo "shutdown"; exit 0' INT TERM
  while true; do
    if ! sweep_once; then
      echo "sweep error — sleeping ${INTERVAL}s and retrying"
    fi
    sleep "$INTERVAL"
  done
else
  sweep_once
fi
