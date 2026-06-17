package dex

import (
	"fmt"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// swapViaSoroswap quotes then executes a token→USDC swap on the Soroswap
// router, returning the real USDC received (balance delta). The second return
// reports whether the swap transaction was (or may have been) broadcast — once
// true, the caller must not try another venue: the collateral may already be
// sold even though this call errored.
func (s *SwapClient) swapViaSoroswap(kp *keypair.Full, tokenAddr string, amount, refValueUSDC int64) (*SwapResult, bool, error) {
	path := []string{tokenAddr, s.cfg.UsdcAddr}

	expectedOut, err := s.soroswapQuote(amount, path)
	if err != nil {
		return nil, false, err
	}
	if expectedOut <= 0 {
		return nil, false, fmt.Errorf("empty quote")
	}
	if belowFloor(expectedOut, refValueUSDC, s.cfg.SlippageBps) {
		return nil, false, fmt.Errorf("%w: quote %d < floor %d", ErrSlippageExceeded,
			expectedOut, minOutForSlippage(refValueUSDC, s.cfg.SlippageBps))
	}

	minOut := minOutForSlippage(expectedOut, s.cfg.SlippageBps)

	before, err := TokenBalance(s.rpc, s.cfg.Passphrase, s.cfg.UsdcAddr, kp.Address())
	if err != nil {
		return nil, false, err
	}

	hash, err := s.soroswapSwap(kp, amount, minOut, path)
	if err != nil {
		// Post-send-ambiguous failures mean the tx may still land — report sent.
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

	ref := refValueUSDC
	if ref <= 0 {
		ref = expectedOut
	}
	return &SwapResult{
		InputToken:   tokenAddr,
		InputAmount:  amount,
		OutputAmount: got,
		Slippage:     slippageFraction(ref, got),
		Route:        "soroswap",
		TxHash:       hash,
	}, true, nil
}

// soroswapQuote calls router_get_amounts_out (read-only) and returns the final
// (USDC) element of the returned Vec<i128>. ABI verified against
// soroswap/core router lib.rs: router_get_amounts_out(amount_in: i128,
// path: Vec<Address>) -> Vec<i128>.
func (s *SwapClient) soroswapQuote(amount int64, path []string) (int64, error) {
	pathVal, err := addressVec(path)
	if err != nil {
		return 0, err
	}
	sim, err := s.rpc.SimulateRead(s.cfg.Passphrase, s.cfg.SoroswapRouter,
		"router_get_amounts_out", soroban.ScvI128(amount), pathVal)
	if err != nil {
		return 0, fmt.Errorf("router_get_amounts_out: %w", err)
	}
	if sim.Error != "" {
		return 0, fmt.Errorf("router_get_amounts_out: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return 0, fmt.Errorf("router_get_amounts_out: no result")
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return 0, err
	}
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		return 0, fmt.Errorf("router_get_amounts_out: result is not a vec")
	}
	vec := **val.Vec
	if len(vec) == 0 {
		return 0, fmt.Errorf("router_get_amounts_out: empty vec")
	}
	return scI128(vec[len(vec)-1]), nil
}

// soroswapSwap executes swap_exact_tokens_for_tokens and returns the tx hash.
// ABI verified against soroswap/core router lib.rs (exact arg order):
// amount_in i128, amount_out_min i128, path Vec<Address>, to Address,
// deadline u64.
func (s *SwapClient) soroswapSwap(kp *keypair.Full, amount, minOut int64, path []string) (string, error) {
	pathVal, err := addressVec(path)
	if err != nil {
		return "", err
	}
	toVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return "", err
	}
	deadline := uint64(s.now() + s.cfg.DeadlineSecs)
	// Swaps are NOT auto-retried: re-broadcasting a non-idempotent swap after a
	// post-send timeout could sell the collateral twice (at a second price). A
	// transient failure is simply retried on the next keeper cycle. The on-chain
	// amount_out_min still bounds execution-time slippage.
	tx, err := s.rpc.Invoke(s.cfg.HorizonURL, kp, s.cfg.Passphrase, s.cfg.SoroswapRouter,
		"swap_exact_tokens_for_tokens",
		soroban.ScvI128(amount), soroban.ScvI128(minOut), pathVal, toVal, soroban.ScvU64(deadline))
	if err != nil {
		return "", fmt.Errorf("swap_exact_tokens_for_tokens: %w", err)
	}
	return tx.Hash, nil
}
