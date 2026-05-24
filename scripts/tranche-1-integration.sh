#!/usr/bin/env bash
# Tranche 1 end-to-end integration runbook.
#
# Walks through every checklist item from the Tranche 1 spec against testnet:
#   1. Deploy updated KeeperRegistry + NectarVault with new config
#   2. Mint USDC + register a keeper with stake
#   3. Deposit (cap-enforced), pre-cooldown withdraw should fail
#   4. After cooldown, withdraw works; share price = 1.00 baseline
#   5. Keeper draws (mark_draw fires), returns proceeds (clear_draw fires)
#   6. Verify share price increased after profitable return
#   7. Attempt over-cap deposit / over-limit draw — both rejected
#
# Pause-after-each-step style so you can inspect on stellar.expert between
# operations and capture screenshots for the demo. Override RUN=auto to
# skip the prompts.

set -euo pipefail

: "${ADMIN_SECRET:?ADMIN_SECRET required}"
: "${ADMIN_ADDRESS:?ADMIN_ADDRESS required}"
: "${USDC_CONTRACT:?USDC_CONTRACT required}"
: "${REGISTRY_CONTRACT:?REGISTRY_CONTRACT required (run deploy.sh first)}"
: "${VAULT_CONTRACT:?VAULT_CONTRACT required (run deploy.sh first)}"
: "${KEEPER_SECRET:?KEEPER_SECRET required — operator key}"
: "${KEEPER_ADDRESS:?KEEPER_ADDRESS required}"
: "${DEPOSITOR_SECRET:?DEPOSITOR_SECRET required — vault depositor}"
: "${DEPOSITOR_ADDRESS:?DEPOSITOR_ADDRESS required}"

RPC_URL="${SOROBAN_RPC:-https://soroban-testnet.stellar.org:443}"
PASSPHRASE="${NETWORK_PASSPHRASE:-Test SDF Network ; September 2015}"
RUN="${RUN:-interactive}"

EXPLORER="https://stellar.expert/explorer/testnet"

pause() {
  if [[ "$RUN" != "auto" ]]; then
    read -r -p "→ press enter to continue ($1)..." _
  fi
}

invoke() {
  local id="$1" src="$2"; shift 2
  stellar contract invoke \
    --id "$id" --source "$src" \
    --rpc-url "$RPC_URL" --network-passphrase "$PASSPHRASE" \
    "$@"
}

step() { echo; echo "═══ $* ═══"; }

step "0. Sanity: read both configs"
echo "Registry config:"
invoke "$REGISTRY_CONTRACT" "$ADMIN_SECRET" -- get_config
echo "Vault config:"
invoke "$VAULT_CONTRACT" "$ADMIN_SECRET" -- get_config
echo "Vault state:"
invoke "$VAULT_CONTRACT" "$ADMIN_SECRET" -- get_state
pause "step 0"

step "1. Mint USDC to depositor + keeper"
# Assumes USDC SAC where ADMIN is the asset issuer. If you're using Circle
# USDC on testnet you'll need a faucet or transfer instead.
invoke "$USDC_CONTRACT" "$ADMIN_SECRET" -- mint --to "$DEPOSITOR_ADDRESS" --amount 1000000000000
invoke "$USDC_CONTRACT" "$ADMIN_SECRET" -- mint --to "$KEEPER_ADDRESS"    --amount 100000000000
echo "Depositor balance:"
invoke "$USDC_CONTRACT" "$ADMIN_SECRET" -- balance --id "$DEPOSITOR_ADDRESS"
echo "Keeper balance:"
invoke "$USDC_CONTRACT" "$ADMIN_SECRET" -- balance --id "$KEEPER_ADDRESS"
pause "step 1"

step "2. Register keeper (transfers stake to registry)"
# Idempotent — testnet-setup.sh already registered both keepers, so this is
# a no-op on a fresh-bootstrap run. We probe get_keeper first.
if invoke "$REGISTRY_CONTRACT" "$ADMIN_SECRET" -- get_keeper --operator "$KEEPER_ADDRESS" >/dev/null 2>&1; then
  echo "  already registered — skipping register call."
else
  invoke "$REGISTRY_CONTRACT" "$KEEPER_SECRET" -- register \
    --operator "$KEEPER_ADDRESS" --name "keeper-demo"
fi
echo "Keeper info:"
invoke "$REGISTRY_CONTRACT" "$ADMIN_SECRET" -- get_keeper --operator "$KEEPER_ADDRESS"
echo "Keeper USDC after stake (should drop by min_stake):"
invoke "$USDC_CONTRACT" "$ADMIN_SECRET" -- balance --id "$KEEPER_ADDRESS"
pause "step 2"

step "3. Depositor deposits 1000 USDC"
invoke "$VAULT_CONTRACT" "$DEPOSITOR_SECRET" -- deposit \
  --user "$DEPOSITOR_ADDRESS" --amount 100000000000
echo "Vault state:"
invoke "$VAULT_CONTRACT" "$ADMIN_SECRET" -- get_state
echo "Depositor record (note last_deposit_time — cooldown anchor):"
invoke "$VAULT_CONTRACT" "$ADMIN_SECRET" -- get_depositor --user "$DEPOSITOR_ADDRESS"
pause "step 3"

step "4. Withdraw before cooldown — must fail with WithdrawalCooldown (#9)"
set +e
invoke "$VAULT_CONTRACT" "$DEPOSITOR_SECRET" -- withdraw \
  --user "$DEPOSITOR_ADDRESS" --shares 10000000000
set -e
echo "↑ if WithdrawalCooldown shown, the cooldown gate is working."
pause "step 4"

step "5. Over-cap deposit attempt — must fail with DepositCapExceeded (#8)"
set +e
invoke "$VAULT_CONTRACT" "$DEPOSITOR_SECRET" -- deposit \
  --user "$DEPOSITOR_ADDRESS" --amount 9999999900000000  # huge
set -e
pause "step 5"

step "6. Keeper draws \$500 — vault calls registry.mark_draw"
invoke "$VAULT_CONTRACT" "$KEEPER_SECRET" -- draw \
  --keeper "$KEEPER_ADDRESS" --amount 50000000000
echo "Vault state (active_liq should be 500_0000000):"
invoke "$VAULT_CONTRACT" "$ADMIN_SECRET" -- get_state
echo "Keeper info (has_active_draw should be TRUE):"
invoke "$REGISTRY_CONTRACT" "$ADMIN_SECRET" -- get_keeper --operator "$KEEPER_ADDRESS"
pause "step 6"

step "7. Over-limit draw — must fail with DrawLimitExceeded (#10)"
set +e
invoke "$VAULT_CONTRACT" "$KEEPER_SECRET" -- draw \
  --keeper "$KEEPER_ADDRESS" --amount 99999900000000
set -e
pause "step 7"

step "8. Keeper returns 510 (10 USDC profit) — registry.clear_draw + record_execution"
invoke "$VAULT_CONTRACT" "$KEEPER_SECRET" -- return_proceeds \
  --keeper "$KEEPER_ADDRESS" --amount 51000000000 --response_time_ms 175
echo "Vault state (total_profit > 0, active_liq = 0):"
invoke "$VAULT_CONTRACT" "$ADMIN_SECRET" -- get_state
echo "Keeper info (has_active_draw=FALSE, total_executions=1, total_profit=10_0000000,"
echo "             response_count=1, total_response_time_ms=175):"
invoke "$REGISTRY_CONTRACT" "$ADMIN_SECRET" -- get_keeper --operator "$KEEPER_ADDRESS"
echo "avg_response_time_ms (expect 175):"
invoke "$REGISTRY_CONTRACT" "$ADMIN_SECRET" -- avg_response_time_ms --operator "$KEEPER_ADDRESS"
pause "step 8"

step "9. Verify share price increased: depositor's USDC value > 1000"
invoke "$VAULT_CONTRACT" "$ADMIN_SECRET" -- balance --user "$DEPOSITOR_ADDRESS" || \
  invoke "$VAULT_CONTRACT" "$ADMIN_SECRET" -- balance --address "$DEPOSITOR_ADDRESS"
echo "Expected: shares=1000_0000000, usdc_value≈1010_0000000 (1.01x share price)."
pause "step 9"

step "10. Wait for cooldown then withdraw — share price uplift realized"
echo "Sleeping ${WITHDRAW_COOLDOWN:-3600}s for the cooldown window."
echo "(Skip with: WITHDRAW_COOLDOWN=0 in deploy.sh, or kill + manually wait.)"
sleep "${WITHDRAW_COOLDOWN_SLEEP:-${WITHDRAW_COOLDOWN:-3600}}"
invoke "$VAULT_CONTRACT" "$DEPOSITOR_SECRET" -- withdraw \
  --user "$DEPOSITOR_ADDRESS" --shares 100000000000
echo "Final depositor USDC balance (should be original + 10 profit):"
invoke "$USDC_CONTRACT" "$ADMIN_SECRET" -- balance --id "$DEPOSITOR_ADDRESS"

echo
echo "Integration test complete."
echo "Inspect each tx on $EXPLORER/account/$KEEPER_ADDRESS"
