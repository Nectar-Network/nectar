package dex

import (
	"fmt"

	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/soroban"
)

// swapViaPhoenix executes a token→USDC swap on a Phoenix XYK pool (fallback),
// returning the real USDC received (balance delta). Phoenix has no public
// testnet deployment and ships multiple swap ABIs, so it is gated behind the
// PhoenixRouter config (set to the XYK pool/pair contract for the
// collateral/USDC pair) and used only when Soroswap is unavailable.
//
// Unlike the Soroswap path there is no pre-trade quote here, so the oracle
// reference is the ONLY slippage anchor: without a positive refValueUSDC the
// swap would execute with no minimum at all on exactly the venue used when
// things are already degraded. Refuse instead. The second return mirrors
// swapViaSoroswap's sent semantics.
func (s *SwapClient) swapViaPhoenix(kp *keypair.Full, tokenAddr string, amount, refValueUSDC int64) (*SwapResult, bool, error) {
	if refValueUSDC <= 0 {
		return nil, false, fmt.Errorf("phoenix: no oracle reference value — refusing swap without a slippage floor")
	}
	minOut := minOutForSlippage(refValueUSDC, s.cfg.SlippageBps)
	if minOut <= 0 {
		return nil, false, fmt.Errorf("phoenix: slippage floor computed as 0 — refusing unprotected swap")
	}

	before, err := TokenBalance(s.rpc, s.cfg.Passphrase, s.cfg.UsdcAddr, kp.Address())
	if err != nil {
		return nil, false, err
	}

	hash, err := s.phoenixSwap(kp, s.cfg.PhoenixRouter, tokenAddr, amount, minOut)
	if err != nil {
		return nil, soroban.IsTxStatusUnknown(err), err
	}

	after, err := TokenBalance(s.rpc, s.cfg.Passphrase, s.cfg.UsdcAddr, kp.Address())
	if err != nil {
		return nil, true, fmt.Errorf("swap landed but post-swap balance read failed: %w", err)
	}
	got := after - before
	if got <= 0 {
		return nil, true, fmt.Errorf("swap sent but USDC balance did not increase")
	}

	return &SwapResult{
		InputToken:   tokenAddr,
		InputAmount:  amount,
		OutputAmount: got,
		Slippage:     slippageFraction(refValueUSDC, got),
		Route:        "phoenix",
		TxHash:       hash,
	}, true, nil
}

// phoenixSwap executes a Phoenix XYK pool swap and returns the tx hash. ABI per
// phoenix-contracts main (contracts/pool/src/contract.rs):
//
//	swap(sender: Address, offer_asset: Address, offer_amount: i128,
//	     ask_asset_min_amount: Option<i128>, max_spread_bps: Option<i64>,
//	     deadline: Option<u64>, max_allowed_fee_bps: Option<i64>) -> i128
//
// Option::None encodes as ScVoid; Option::Some(x) as the value itself. We set
// ask_asset_min_amount as the deterministic min-received guard and leave the
// spread/fee caps unset. NOTE: Phoenix ships more than one swap ABI version;
// verify the deployed contract's interface before relying on this in production.
func (s *SwapClient) phoenixSwap(kp *keypair.Full, poolAddr, offerAsset string, amount, minOut int64) (string, error) {
	senderVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return "", err
	}
	offerVal, err := soroban.ScvAddress(offerAsset)
	if err != nil {
		return "", err
	}
	askMin := soroban.ScvVoid()
	if minOut > 0 {
		askMin = soroban.ScvI128(minOut)
	}
	deadline := soroban.ScvU64(uint64(s.now() + s.cfg.DeadlineSecs))

	// Not auto-retried — see soroswapSwap; a non-idempotent swap must never be
	// re-broadcast on a post-send timeout.
	tx, err := s.rpc.Invoke(s.cfg.HorizonURL, kp, s.cfg.Passphrase, poolAddr,
		"swap",
		senderVal,               // sender
		offerVal,                // offer_asset
		soroban.ScvI128(amount), // offer_amount
		askMin,                  // ask_asset_min_amount: Option<i128>
		soroban.ScvVoid(),       // max_spread_bps: Option<i64> = None
		deadline,                // deadline: Option<u64> = Some
		soroban.ScvVoid(),       // max_allowed_fee_bps: Option<i64> = None
	)
	if err != nil {
		return "", fmt.Errorf("phoenix swap: %w", err)
	}
	return tx.Hash, nil
}
