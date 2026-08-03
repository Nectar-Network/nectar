package dex

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/soroban"
)

// ErrOffParity is returned when a stable-to-stable conversion quote deviates
// from 1:1 by more than the configured slippage bound. Both legs are USD
// stablecoins, so par is the only defensible reference price — a route that
// cannot deliver near-par is treated as no viable route, and no capital moves.
var ErrOffParity = errors.New("conversion route off parity")

// QuoteConvertIn returns how much of `from` is required to obtain exactly
// amountOut of `to` on a direct Soroswap route, rejecting quotes beyond the
// par bound: required <= amountOut * (1 + slippageBps/10000). Both legs must
// be USD stablecoins — that is what makes par the reference; for arbitrary
// pairs use QuoteIn and anchor against an oracle value instead.
func (s *SwapClient) QuoteConvertIn(from, to string, amountOut int64) (int64, error) {
	required, err := s.QuoteIn(from, to, amountOut)
	if err != nil {
		return 0, err
	}
	if maxIn := maxInForParity(amountOut, s.cfg.SlippageBps); required > maxIn {
		return 0, fmt.Errorf("%w: need %d of vault USDC for %d of pool USDC (par bound %d)",
			ErrOffParity, required, amountOut, maxIn)
	}
	return required, nil
}

// maxInForParity is the most `from` we will pay for amountOut of a same-USD
// stable: par plus the slippage allowance.
func maxInForParity(amountOut int64, slippageBps int) int64 {
	return amountOut + amountOut*int64(slippageBps)/10000
}

// ConvertExactOut swaps at most maxIn of `from` for exactly amountOut of `to`
// via swap_tokens_for_exact_tokens, measuring the real received balance delta.
// Like soroswapSwap, it is never auto-retried (a post-send-ambiguous failure
// could double-spend); a transient failure surfaces to the caller.
//
// ABI verified against the live router spec: swap_tokens_for_exact_tokens(
// amount_out: i128, amount_in_max: i128, path: Vec<Address>, to: Address,
// deadline: u64) -> Vec<i128>.
func (s *SwapClient) ConvertExactOut(kp *keypair.Full, from, to string, amountOut, maxIn int64) (*SwapResult, error) {
	if s.cfg.SoroswapRouter == "" {
		return nil, ErrNoRoute
	}
	if amountOut <= 0 || maxIn <= 0 {
		return nil, fmt.Errorf("non-positive amounts out=%d maxIn=%d", amountOut, maxIn)
	}
	pathVal, err := addressVec([]string{from, to})
	if err != nil {
		return nil, err
	}
	toVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return nil, err
	}
	before, err := TokenBalance(s.rpc, s.cfg.Passphrase, to, kp.Address())
	if err != nil {
		return nil, err
	}
	deadline := uint64(s.now() + s.cfg.DeadlineSecs)
	tx, err := s.rpc.Invoke(s.cfg.HorizonURL, kp, s.cfg.Passphrase, s.cfg.SoroswapRouter,
		"swap_tokens_for_exact_tokens",
		soroban.ScvI128(amountOut), soroban.ScvI128(maxIn), pathVal, toVal, soroban.ScvU64(deadline))
	if err != nil {
		return nil, fmt.Errorf("swap_tokens_for_exact_tokens: %w", err)
	}
	after, err := TokenBalance(s.rpc, s.cfg.Passphrase, to, kp.Address())
	if err != nil {
		return nil, fmt.Errorf("conversion landed but balance read failed: %w", err)
	}
	got := after - before
	if got <= 0 {
		return nil, fmt.Errorf("conversion sent but %s balance did not increase", to[:4])
	}
	slog.Info("exact-out conversion landed", "out", got, "maxIn", maxIn, "tx", tx.Hash)
	return &SwapResult{
		InputToken:   from,
		InputAmount:  maxIn, // upper bound; router refunds unspent input
		OutputAmount: got,
		Route:        "soroswap",
		TxHash:       tx.Hash,
	}, nil
}
