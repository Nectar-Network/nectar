#!/usr/bin/env bash
# redeploy-tranche1.sh — Redeploy KeeperRegistry + NectarVault after the
# Tranche-1 ABI changes:
#   - KeeperRegistry.record_execution  gains  response_time_ms: u64
#   - NectarVault.return_proceeds       gains  response_time_ms: u64
#   - KeeperRegistry.KeeperInfo        gains  total_response_time_ms,
#                                              response_count
#
# Existing testnet KeeperInfo entries serialized under the old schema cannot
# be read by the new contract code. This wrapper drops those keepers from
# the new registry (operators must re-register and re-stake) and saves the
# new contract IDs to .env.
#
# Usage:   ./scripts/redeploy-tranche1.sh
# Env:     ADMIN_SECRET, ADMIN_ADDRESS, USDC_CONTRACT (same as deploy.sh)

set -euo pipefail
SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(dirname "$SCRIPT_DIR")"

cat <<EOF
==> Tranche 1 redeploy
    This will deploy NEW KeeperRegistry + NectarVault WASMs to testnet.
    Pre-existing keeper registrations (and their stake balances) on the OLD
    registry will be orphaned. Operators must re-register against the new
    contract address printed below.
EOF

read -r -p "Continue? [y/N] " ack
case "${ack:-N}" in
  y|Y|yes|YES) ;;
  *) echo "Aborted."; exit 1 ;;
esac

cd "$ROOT_DIR"

echo "==> Building keeper-registry + nectar-vault wasms..."
cargo build --target wasm32-unknown-unknown --release \
  -p keeper-registry -p nectar-vault 2>&1 \
  | grep -E "Compiling|Finished|error" || true

# Delegate to deploy.sh for the actual deploy + init sequence. It writes new
# contract IDs to .env, which the keeper daemon and frontend pick up.
echo "==> Deploying via scripts/deploy.sh..."
"$SCRIPT_DIR/deploy.sh"

cat <<EOF

==> Tranche 1 redeploy complete.
    Next steps:
      1. Update frontend/.env.local with the NEXT_PUBLIC_* vars from .env.
      2. Restart the Railway keeper (or kill / restart locally) so it picks
         up the new VAULT_CONTRACT and REGISTRY_CONTRACT ids.
      3. Operators run scripts/register-keepers-testnet.sh to re-stake
         against the new registry.
EOF
