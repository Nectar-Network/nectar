#!/usr/bin/env bash
# run.sh — execute a nectar-sandbox script against the pinned blend-utils
# clone. Copies the .mjs files into reference/blend-utils/nectar/ so bare
# imports (@blend-capital/blend-sdk, @stellar/stellar-sdk) resolve against
# the clone's node_modules, then runs the requested step with network arg
# "testnet" (selects reference/blend-utils/testnet.contracts.json as the
# address book).
#
# Usage: ./run.sh 01 | 02 | 03 <xlm_price>
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
ROOT_DIR="$(cd "$SCRIPT_DIR/../.." && pwd)"
BU="$ROOT_DIR/reference/blend-utils"

[ -d "$BU/lib" ] || { echo "ERROR: $BU/lib missing — run 'npm i && npm run build' in reference/blend-utils" >&2; exit 1; }
[ -f "$BU/.env" ] || { echo "ERROR: $BU/.env missing — see scripts/nectar-sandbox/README.md" >&2; exit 1; }

STEP="${1:?usage: run.sh 01|02|03 [args...]}"
shift

mkdir -p "$BU/nectar"
cp "$SCRIPT_DIR"/*.mjs "$BU/nectar/"

case "$STEP" in
  01) exec node "$BU/nectar/01-deploy-sandbox.mjs" testnet "$@" ;;
  02) exec node "$BU/nectar/02-borrower.mjs" testnet "$@" ;;
  03) exec node "$BU/nectar/03-set-price.mjs" testnet "$@" ;;
  *) echo "unknown step: $STEP" >&2; exit 1 ;;
esac
