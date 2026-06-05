// Package dex converts non-USDC liquidation collateral into USDC so it can be
// returned to the vault, closing the liquidation loop: fill auction → receive
// collateral → swap to USDC → return proceeds. Swaps route through Soroswap
// (primary) with a Phoenix pool fallback. Output is measured by the keeper's
// USDC balance delta, never synthesized, so reported proceeds are always real.
package dex

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// Sentinel errors callers can branch on.
var (
	// ErrNoRoute means no configured DEX could complete the swap.
	ErrNoRoute = errors.New("dex: no swap route available")
	// ErrSlippageExceeded means the best quote was worse than the oracle-anchored
	// floor; the keeper refuses to sell collateral that cheaply on any venue.
	ErrSlippageExceeded = errors.New("dex: quote below slippage floor")
	// ErrUSDCNotConfigured means the USDC token address is missing from config.
	ErrUSDCNotConfigured = errors.New("dex: USDC address not configured")
)

// Config holds the DEX endpoints and swap policy. Empty router fields disable
// that venue. SlippageBps caps acceptable slippage (100 = 1%).
type Config struct {
	HorizonURL     string
	Passphrase     string
	UsdcAddr       string
	SoroswapRouter string
	PhoenixRouter  string // Phoenix XYK pool (pair) contract for the collateral/USDC pair
	SlippageBps    int
	DeadlineSecs   int64 // swap deadline buffer in seconds (default 60)
}

// SwapClient executes collateral→USDC swaps against configured DEXs.
type SwapClient struct {
	rpc *soroban.Client
	cfg Config
	now func() int64 // injectable clock for deadlines (overridable in tests)
}

// SwapResult describes a completed swap.
type SwapResult struct {
	InputToken   string
	InputAmount  int64
	OutputAmount int64   // USDC actually received (balance delta)
	Slippage     float64 // realized slippage vs the reference value, [0,1]
	Route        string  // "soroswap", "phoenix", or "none"
	TxHash       string
}

// NewSwapClient builds a SwapClient. A zero/negative DeadlineSecs defaults to 60.
func NewSwapClient(rpc *soroban.Client, cfg Config) *SwapClient {
	if cfg.DeadlineSecs <= 0 {
		cfg.DeadlineSecs = 60
	}
	if cfg.SlippageBps < 0 {
		cfg.SlippageBps = 0
	}
	return &SwapClient{rpc: rpc, cfg: cfg, now: func() int64 { return time.Now().Unix() }}
}

// SwapToUSDC converts amount of tokenAddr into USDC, trying Soroswap first and
// Phoenix second. refValueUSDC is the oracle-implied fair USDC value of the
// input (7-decimal stroops); when > 0 it anchors the slippage check so a
// manipulated pool quote cannot pass. Pass 0 to rely only on the DEX quote's
// on-chain amount_out_min. Returns ErrSlippageExceeded (without falling back)
// when the price is worse than the floor, or ErrNoRoute when every venue fails.
func (s *SwapClient) SwapToUSDC(kp *keypair.Full, tokenAddr string, amount, refValueUSDC int64) (*SwapResult, error) {
	if amount <= 0 {
		return nil, fmt.Errorf("swap: amount must be > 0, got %d", amount)
	}
	if s.cfg.UsdcAddr == "" {
		return nil, ErrUSDCNotConfigured
	}
	if tokenAddr == s.cfg.UsdcAddr {
		// Already USDC — no swap needed.
		return &SwapResult{InputToken: tokenAddr, InputAmount: amount, OutputAmount: amount, Route: "none"}, nil
	}

	var attempts []string

	if s.cfg.SoroswapRouter != "" {
		res, err := s.swapViaSoroswap(kp, tokenAddr, amount, refValueUSDC)
		switch {
		case err == nil:
			return res, nil
		case errors.Is(err, ErrSlippageExceeded):
			// A bad price is a global decision: don't dump on another venue either.
			return nil, err
		default:
			attempts = append(attempts, "soroswap: "+err.Error())
		}
	}

	if s.cfg.PhoenixRouter != "" {
		res, err := s.swapViaPhoenix(kp, tokenAddr, amount, refValueUSDC)
		if err == nil {
			return res, nil
		}
		attempts = append(attempts, "phoenix: "+err.Error())
	}

	if len(attempts) == 0 {
		return nil, ErrNoRoute
	}
	return nil, fmt.Errorf("%w (%s)", ErrNoRoute, strings.Join(attempts, "; "))
}

// minOutForSlippage returns the minimum acceptable output for a quoted amount
// given a slippage tolerance in basis points (100 = 1%).
func minOutForSlippage(quotedOut int64, slippageBps int) int64 {
	if quotedOut <= 0 {
		return 0
	}
	if slippageBps < 0 {
		slippageBps = 0
	}
	if slippageBps > 10000 {
		slippageBps = 10000
	}
	return quotedOut * int64(10000-slippageBps) / 10000
}

// belowFloor reports whether a quote is worse than the slippage floor derived
// from an oracle reference value. A non-positive reference disables the check.
func belowFloor(quotedOut, refValueUSDC int64, slippageBps int) bool {
	if refValueUSDC <= 0 {
		return false
	}
	return quotedOut < minOutForSlippage(refValueUSDC, slippageBps)
}

// slippageFraction is the realized shortfall of got vs ref, clamped to [0,1].
func slippageFraction(ref, got int64) float64 {
	if ref <= 0 || got >= ref {
		return 0
	}
	return float64(ref-got) / float64(ref)
}

// TokenBalance reads a SAC/token contract balance for owner (7-decimal stroops).
func TokenBalance(rpc *soroban.Client, passphrase, tokenAddr, owner string) (int64, error) {
	addrVal, err := soroban.ScvAddress(owner)
	if err != nil {
		return 0, err
	}
	sim, err := rpc.SimulateRead(passphrase, tokenAddr, "balance", addrVal)
	if err != nil {
		return 0, fmt.Errorf("token balance: %w", err)
	}
	if sim.Error != "" {
		return 0, fmt.Errorf("token balance: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return 0, nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return 0, err
	}
	return scI128(val), nil
}

// addressVec builds a Vec<Address> ScVal from string addresses.
func addressVec(addrs []string) (xdr.ScVal, error) {
	vals := make([]xdr.ScVal, 0, len(addrs))
	for _, a := range addrs {
		v, err := soroban.ScvAddress(a)
		if err != nil {
			return xdr.ScVal{}, err
		}
		vals = append(vals, v)
	}
	return soroban.ScvVec(vals...), nil
}

// scI128 decodes a single i128 ScVal to int64 (low 64 bits). 7-decimal amounts
// in this protocol stay well within int64.
func scI128(val xdr.ScVal) int64 {
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return 0
	}
	return int64(val.I128.Lo)
}
