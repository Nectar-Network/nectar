#!/usr/bin/env bash
# tranche-1-e2e.sh — One-shot end-to-end testnet validation for Tranche 1.
#
# What it does (in order):
#   1. Generate fresh wallets (admin + 2 keepers + 15 users) via gen-wallets.sh
#   2. Fund all wallets via Friendbot (10 000 XLM each)
#   3. Deploy MockUSDC, KeeperRegistry, NectarVault to testnet
#   4. Register keeper-alpha + keeper-beta (each stakes 100 USDC)
#   5. Mint USDC to a depositor + a keeper
#   6. Walk the full deposit → draw → return_proceeds → withdraw cycle
#   7. Assert the NEW Tranche 1 surface:
#        - avg_response_time_ms (KeeperRegistry getter)
#        - response_count, total_response_time_ms on KeeperInfo
#        - retry survival on transient errors (covered by Go tests, listed for
#          completeness — actual transient injection requires a faulty RPC)
#
# Usage:
#   cd nectar/
#   ./scripts/tranche-1-e2e.sh             # interactive (pauses between steps)
#   RUN=auto ./scripts/tranche-1-e2e.sh    # non-interactive
#   WITHDRAW_COOLDOWN=10 ./scripts/tranche-1-e2e.sh   # short cooldown for demo
#
# Requirements on your machine:
#   - stellar CLI  (https://developers.stellar.org/docs/tools/developer-tools)
#   - node or python3 with stellar-sdk (for wallet generation)
#   - curl
#   - Rust toolchain with wasm32-unknown-unknown target
#   - jq (for assertion parsing)

set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"
ENV_FILE="$ROOT_DIR/.env"
ENV_WALLETS="$ROOT_DIR/.env.wallets"

# Short cooldown by default so the demo doesn't sleep an hour.
export DEPOSIT_CAP="${DEPOSIT_CAP:-100000000000000}"        # 10M USDC cap
export WITHDRAW_COOLDOWN="${WITHDRAW_COOLDOWN:-30}"          # 30 s
export MIN_STAKE="${MIN_STAKE:-1000000000}"                  # 100 USDC
export SLASH_TIMEOUT="${SLASH_TIMEOUT:-3600}"
export SLASH_RATE_BPS="${SLASH_RATE_BPS:-1000}"
export MAX_DRAW_PER_KEEPER="${MAX_DRAW_PER_KEEPER:-100000000000}"

PASS=0; FAIL=0
ok()   { echo "  ✓ $1"; PASS=$((PASS+1)); }
fail() { echo "  ✗ $1"; FAIL=$((FAIL+1)); }
step() { echo; echo "═══ $* ═══"; }

# ── Preflight ────────────────────────────────────────────────────────────────
step "Preflight"
for bin in stellar curl jq cargo; do
  if ! command -v "$bin" >/dev/null 2>&1; then
    echo "ERROR: '$bin' not found in PATH" >&2
    exit 1
  fi
done
echo "stellar CLI: $(stellar --version | head -1)"
echo "cargo:       $(cargo --version)"

# ── Bootstrap (wallets + funding + contract deploys + keeper register) ───────
step "Bootstrap testnet (wallets → fund → deploy → register)"
"$SCRIPT_DIR/testnet-setup.sh"

# Reload env emitted by testnet-setup.sh.
source "$ENV_WALLETS"
source "$ENV_FILE"

: "${USDC_CONTRACT:?USDC_CONTRACT missing after bootstrap}"
: "${REGISTRY_CONTRACT:?REGISTRY_CONTRACT missing after bootstrap}"
: "${VAULT_CONTRACT:?VAULT_CONTRACT missing after bootstrap}"

# ── Run the full integration runbook against the live testnet ────────────────
step "Run tranche-1-integration.sh against live contracts"
# Use user-01 as the depositor (already minted in seed-vault.sh).
export ADMIN_SECRET ADMIN_ADDRESS USDC_CONTRACT REGISTRY_CONTRACT VAULT_CONTRACT
export KEEPER_SECRET="$KEEPER_A_SECRET"
export KEEPER_ADDRESS="$KEEPER_A_ADDRESS"
export DEPOSITOR_SECRET="$USER_01_SECRET"
export DEPOSITOR_ADDRESS="$USER_01_ADDRESS"
# Cooldown sleep matches the live config so we don't oversleep.
export WITHDRAW_COOLDOWN_SLEEP="$WITHDRAW_COOLDOWN"
RUN="${RUN:-auto}" "$SCRIPT_DIR/tranche-1-integration.sh"

# ── Explicit assertions on the Tranche 1 surface ─────────────────────────────
step "Assertions: Tranche 1 deliverable surface"

RPC_URL="${SOROBAN_RPC:-https://soroban-testnet.stellar.org:443}"
PASSPHRASE="${NETWORK_PASSPHRASE:-Test SDF Network ; September 2015}"

call_view() {
  stellar contract invoke \
    --id "$1" --source "$ADMIN_SECRET" \
    --rpc-url "$RPC_URL" --network-passphrase "$PASSPHRASE" \
    --send no \
    -- "${@:2}" 2>/dev/null
}

# 1. avg_response_time_ms returns 175 after the one fill from step 8.
echo "→ KeeperRegistry.avg_response_time_ms"
AVG=$(call_view "$REGISTRY_CONTRACT" avg_response_time_ms --operator "$KEEPER_ADDRESS" | tr -d '"')
if [[ "$AVG" == "175" ]]; then
  ok "avg_response_time_ms == 175"
else
  fail "avg_response_time_ms got '$AVG', want 175"
fi

# 2. KeeperInfo carries response_count == 1, total_response_time_ms == 175.
echo "→ KeeperRegistry.get_keeper"
INFO=$(call_view "$REGISTRY_CONTRACT" get_keeper --operator "$KEEPER_ADDRESS")
RC=$(echo "$INFO" | jq -r '.response_count // empty')
TRT=$(echo "$INFO" | jq -r '.total_response_time_ms // empty')
TE=$(echo "$INFO" | jq -r '.total_executions // empty')
SF=$(echo "$INFO" | jq -r '.successful_fills // empty')

[[ "$RC"  == "1"   ]] && ok "response_count == 1"                   || fail "response_count='$RC'"
[[ "$TRT" == "175" ]] && ok "total_response_time_ms == 175"         || fail "total_response_time_ms='$TRT'"
[[ "$TE"  == "1"   ]] && ok "total_executions == 1"                 || fail "total_executions='$TE'"
[[ "$SF"  == "1"   ]] && ok "successful_fills == 1"                 || fail "successful_fills='$SF'"

# 3. Vault state — share price uplift visible.
echo "→ NectarVault.get_state"
STATE=$(call_view "$VAULT_CONTRACT" get_state)
PROFIT=$(echo "$STATE" | jq -r '.total_profit // empty' | tr -d '"')
if [[ -n "$PROFIT" ]] && [[ "$PROFIT" -ge 100000000 ]]; then
  ok "vault total_profit >= 10 USDC (got $PROFIT stroops)"
else
  fail "vault total_profit too low: '$PROFIT'"
fi

# 4. Retry-path tests already covered by Go unit tests; surface them here.
echo "→ Retry classifier (covered by Go tests)"
(cd "$ROOT_DIR/keeper" && go test -run 'TestFillAuction_Retries|TestFillAuction_DoesNotRetry|TestRegister_DoesNotRetry' ./blend/ -count=1 -timeout 30s) \
  && ok "blend retry tests pass" \
  || fail "blend retry tests failed"

# ── Summary ──────────────────────────────────────────────────────────────────
echo
echo "════════════════════════════════════════════════════════════════"
echo "  Tranche 1 E2E: $PASS passed, $FAIL failed"
echo "════════════════════════════════════════════════════════════════"
echo
echo "Contract addresses (saved to $ENV_FILE):"
echo "  USDC:           $USDC_CONTRACT"
echo "  KeeperRegistry: $REGISTRY_CONTRACT"
echo "  NectarVault:    $VAULT_CONTRACT"
echo
echo "Inspect on Stellar Expert:"
echo "  https://stellar.expert/explorer/testnet/contract/$REGISTRY_CONTRACT"
echo "  https://stellar.expert/explorer/testnet/contract/$VAULT_CONTRACT"
echo "  https://stellar.expert/explorer/testnet/account/$KEEPER_A_ADDRESS"

[[ $FAIL -eq 0 ]] || exit 1
