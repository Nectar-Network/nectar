package dex

import (
	"fmt"
	"log/slog"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// swapViaSoroswap quotes then executes a from→to swap on the Soroswap router,
// returning the real output received (balance delta of `to`). The second
// return reports whether the swap transaction was (or may have been)
// broadcast — once true, the caller must not try another venue: the input may
// already be sold even though this call errored.
func (s *SwapClient) swapViaSoroswap(kp *keypair.Full, from, to string, amount, refValueOut int64) (*SwapResult, bool, error) {
	path := []string{from, to}

	expectedOut, err := s.soroswapQuote(amount, path)
	if err != nil {
		return nil, false, err
	}
	if expectedOut <= 0 {
		return nil, false, fmt.Errorf("empty quote")
	}
	if belowFloor(expectedOut, refValueOut, s.cfg.SlippageBps) {
		return nil, false, fmt.Errorf("%w: quote %d < floor %d", ErrSlippageExceeded,
			expectedOut, minOutForSlippage(refValueOut, s.cfg.SlippageBps))
	}

	minOut := minOutForSlippage(expectedOut, s.cfg.SlippageBps)

	before, err := TokenBalance(s.rpc, s.cfg.Passphrase, to, kp.Address())
	if err != nil {
		return nil, false, err
	}

	hash, err := s.soroswapSwap(kp, amount, minOut, path)
	if err != nil {
		// Post-send-ambiguous failures mean the tx may still land — report sent.
		return nil, soroban.IsTxStatusUnknown(err), err
	}

	after, err := TokenBalance(s.rpc, s.cfg.Passphrase, to, kp.Address())
	if err != nil {
		return nil, true, fmt.Errorf("swap landed but post-swap balance read failed: %w", err)
	}
	got := after - before
	if got <= 0 {
		return nil, true, fmt.Errorf("swap sent but output balance did not increase")
	}

	ref := refValueOut
	if ref <= 0 {
		ref = expectedOut
	}
	slog.Info("soroswap swap landed", "in", amount, "out", got, "tx", hash)
	return &SwapResult{
		InputToken:   from,
		InputAmount:  amount,
		OutputAmount: got,
		Slippage:     slippageFraction(ref, got),
		Route:        "soroswap",
		TxHash:       hash,
	}, true, nil
}

// soroswapQuote calls router_get_amounts_out (read-only) and returns the final
// (output-token) element of the returned Vec<i128>. ABI verified against
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

// QuoteIn returns how much of `from` the Soroswap router requires to obtain
// exactly amountOut of `to` on a direct route. No price bound is applied here
// — callers anchor the result against an oracle reference (or par, for
// stable-to-stable: see QuoteConvertIn) before moving capital.
//
// ABI verified against the live router spec (docs/evidence/a2-route-checks.json):
// router_get_amounts_in(amount_out: i128, path: Vec<Address>) -> Vec<i128>,
// first element = required input.
func (s *SwapClient) QuoteIn(from, to string, amountOut int64) (int64, error) {
	if s.cfg.SoroswapRouter == "" {
		return 0, ErrNoRoute
	}
	if amountOut <= 0 {
		return 0, fmt.Errorf("non-positive amount %d", amountOut)
	}
	pathVal, err := addressVec([]string{from, to})
	if err != nil {
		return 0, err
	}
	sim, err := s.rpc.SimulateRead(s.cfg.Passphrase, s.cfg.SoroswapRouter,
		"router_get_amounts_in", soroban.ScvI128(amountOut), pathVal)
	if err != nil {
		return 0, fmt.Errorf("router_get_amounts_in: %w", err)
	}
	if sim.Error != "" {
		return 0, fmt.Errorf("router_get_amounts_in: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return 0, fmt.Errorf("router_get_amounts_in: no result")
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return 0, err
	}
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil || len(**val.Vec) == 0 {
		return 0, fmt.Errorf("router_get_amounts_in: malformed result")
	}
	required := scI128((**val.Vec)[0])
	if required <= 0 {
		return 0, fmt.Errorf("router_get_amounts_in: empty quote")
	}
	return required, nil
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
	// post-send timeout could sell the input twice (at a second price). A
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
