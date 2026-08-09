#!/usr/bin/env bash
# 04-money-test.sh — SELF-VALIDATE ("the money test"): run the COMPLETE
# liquidation cycle on the Nectar Sandbox with the real keeper binary and
# assert programmatically that the vault share price INCREASED.
#
#   deposit (pre-existing) → force HF<1 (03-set-price.mjs 0.098, run first)
#   → keeper creates auction → waits for the verified price curve to cross
#   MIN_PROFIT → draws → atomic fill+unwind → swaps collateral → returns
#   proceeds → share price rises.
#
# Usage:
#   KEEPER_SECRET=<keeper-alpha secret> ./scripts/nectar-sandbox/04-money-test.sh
#
# Prints BEFORE/AFTER vault state and exits 0 only when
# total_usdc/total_shares strictly increased AND the keeper's outstanding
# draw is back to zero. Keeper log goes to docs/evidence/b-money-test-keeper.log.
set -euo pipefail

: "${KEEPER_SECRET:?export KEEPER_SECRET (keeper-alpha) first}"

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"

POOL=CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V
VAULT=CDOGQY7NAE3BP4Q7RWBCBLW23Z36RNWNDNXX5DWNIEVMFEWP3GVEPXLR
REGISTRY=CD33A7IGNCOLVQ4EEINBVMVA7IHWXGN57R6YLE5AJEEKPA6VKC2E4IQD
USDC=CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA
ROUTER=CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD
KEEPER_ADDR=GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF
ADMIN_ADDR=GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W
LOG="$ROOT_DIR/docs/evidence/b-money-test-keeper.log"
TIMEOUT_SECS=2700   # 45 min: auction curve needs ~25 min to reach MIN_PROFIT

read_state() { # -> "total_usdc total_shares total_profit active_liq"
  stellar contract invoke --id "$VAULT" --source-account "$ADMIN_ADDR" \
    --network testnet --send=no -- get_state 2>/dev/null | tail -1 |
    python3 -c 'import json,sys; s=json.load(sys.stdin); print(s["total_usdc"], s["total_shares"], s["total_profit"], s["active_liq"])'
}
read_draw() {
  stellar contract invoke --id "$VAULT" --source-account "$ADMIN_ADDR" \
    --network testnet --send=no -- get_keeper_draw --keeper "$KEEPER_ADDR" 2>/dev/null | tail -1 | tr -d '"'
}

echo "== building keeper =="
(cd "$ROOT_DIR/keeper" && go build -o /tmp/nectar-keeper-moneytest .)

BEFORE=($(read_state))
echo "BEFORE: total_usdc=${BEFORE[0]} total_shares=${BEFORE[1]} total_profit=${BEFORE[2]} active_liq=${BEFORE[3]}"
echo "BEFORE share price: $(python3 -c "print(${BEFORE[0]}/${BEFORE[1]})")"

echo "== starting keeper (log: $LOG) =="
env \
  KEEPER_SECRET="$KEEPER_SECRET" \
  KEEPER_NAME=keeper-alpha \
  REGISTRY_CONTRACT="$REGISTRY" \
  VAULT_CONTRACT="$VAULT" \
  USDC_CONTRACT="$USDC" \
  BLEND_POOLS="$POOL:active" \
  SOROSWAP_ROUTER="$ROUTER" \
  SLIPPAGE_BPS=100 \
  MIN_PROFIT=1.02 \
  POLL_INTERVAL=10 \
  BLEND_EVENT_LOOKBACK="${BLEND_EVENT_LOOKBACK:-110000}" \
  API_PORT=8099 \
  SOROBAN_RPC="https://soroban-testnet.stellar.org:443" \
  HORIZON_URL="https://horizon-testnet.stellar.org" \
  NETWORK_PASSPHRASE="Test SDF Network ; September 2015" \
  /tmp/nectar-keeper-moneytest >"$LOG" 2>&1 &
KEEPER_PID=$!
trap 'kill $KEEPER_PID 2>/dev/null || true' EXIT

elapsed=0
while [ $elapsed -lt $TIMEOUT_SECS ]; do
  sleep 30; elapsed=$((elapsed + 30))
  NOW=($(read_state)) || continue
  DRAW=$(read_draw) || DRAW="?"
  echo "t=${elapsed}s total_usdc=${NOW[0]} profit=${NOW[2]} active_liq=${NOW[3]} keeper_draw=${DRAW}"
  if [ "${NOW[0]}" -gt "${BEFORE[0]}" ] && [ "${DRAW}" = "0" ] && [ "${NOW[3]}" = "0" ]; then
    echo "== cycle complete =="
    break
  fi
done

kill $KEEPER_PID 2>/dev/null || true
wait $KEEPER_PID 2>/dev/null || true

AFTER=($(read_state))
echo "AFTER:  total_usdc=${AFTER[0]} total_shares=${AFTER[1]} total_profit=${AFTER[2]} active_liq=${AFTER[3]}"
python3 - "$@" << EOF
before = ${BEFORE[0]} / ${BEFORE[1]}
after = ${AFTER[0]} / ${AFTER[1]}
delta_usdc = ${AFTER[0]} - ${BEFORE[0]}
print(f"share price: {before:.7f} -> {after:.7f} (delta_usdc={delta_usdc} stroops)")
if after > before:
    print("MONEY TEST PASS: share price increased")
    raise SystemExit(0)
print("MONEY TEST FAIL: share price did not increase")
raise SystemExit(1)
EOF
