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
func (s *SwapClient) swapViaPhoenix(kp *keypair.Full, tokenAddr string, amount, refValueUSDC int64) (*SwapResult, error) {
	minOut := int64(0)
	if refValueUSDC > 0 {
		minOut = minOutForSlippage(refValueUSDC, s.cfg.SlippageBps)
	}

	before, err := TokenBalance(s.rpc, s.cfg.Passphrase, s.cfg.UsdcAddr, kp.Address())
	if err != nil {
		return nil, err
	}

	hash, err := s.phoenixSwap(kp, s.cfg.PhoenixRouter, tokenAddr, amount, minOut)
	if err != nil {
		return nil, err
	}

	after, err := TokenBalance(s.rpc, s.cfg.Passphrase, s.cfg.UsdcAddr, kp.Address())
	if err != nil {
		return nil, err
	}
	got := after - before
	if got <= 0 {
		return nil, fmt.Errorf("swap sent but USDC balance did not increase")
	}

	ref := refValueUSDC
	if ref <= 0 {
		ref = got
	}
	return &SwapResult{
		InputToken:   tokenAddr,
		InputAmount:  amount,
		OutputAmount: got,
		Slippage:     slippageFraction(ref, got),
		Route:        "phoenix",
		TxHash:       hash,
	}, nil
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
