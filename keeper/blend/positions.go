package blend

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// Position mirrors blend-contracts-v2 `Positions { liabilities, collateral,
// supply }` (pool/src/pool/user.rs:11-15 @ ba22b48). `supply` is NON-collateral
// lending and is deliberately kept separate: it does not back the user's debt
// (health_factor.rs only reads `collateral`) and it is not part of a
// liquidation lot, so folding it into Collateral would both overstate HF
// inputs and put non-auctionable reserves into new_auction's lot vector.
type Position struct {
	Address     string
	Collateral  map[uint32]*big.Int // reserve index -> b_token balance backing debt
	Supply      map[uint32]*big.Int // reserve index -> b_token balance, non-collateral
	Liabilities map[uint32]*big.Int // reserve index -> d_token balance
	HF          float64
}

// ProbeResult is one get_positions read. Err distinguishes "this address holds
// nothing" (Pos set, all maps empty) from "we could not find out" (Err set) —
// the caller must not record a failed read as a debt-free position, or a
// transient RPC error would quietly retire a real borrower from the index.
type ProbeResult struct {
	Address string
	Pos     *Position
	Err     error
}

// ProbePositions reads positions for an explicit address list.
//
// Discovery is deliberately NOT done here any more: which addresses are worth
// asking about is the BorrowerIndex's job (index.go), because that answer has
// to accumulate across cycles and survive restarts. This function only does the
// part that must be re-read from chain every time — the position itself, which
// no event payload can tell us (docs/FACTS.md: `repay` publishes deltas only).
//
// Reads are sequential and each costs a simulateTransaction round-trip, so the
// length of addrs is the cycle's dominant cost. Keep it to addresses the index
// says are worth it.
func ProbePositions(rpc *soroban.Client, passphrase, poolAddr string, addrs []string) []ProbeResult {
	out := make([]ProbeResult, 0, len(addrs))
	for _, addr := range addrs {
		pos, err := loadPosition(rpc, passphrase, poolAddr, addr)
		out = append(out, ProbeResult{Address: addr, Pos: pos, Err: err})
	}
	return out
}

// Empty reports whether a probed position holds nothing at all on this pool.
func (p *Position) Empty() bool {
	return len(p.Collateral) == 0 && len(p.Supply) == 0 && len(p.Liabilities) == 0
}

// HasDebt reports whether the position carries liabilities. This is the
// authoritative answer to "is this still a borrower" — events cannot supply it.
func (p *Position) HasDebt() bool { return len(p.Liabilities) > 0 }

func loadPosition(rpc *soroban.Client, passphrase, poolAddr, user string) (*Position, error) {
	userVal, err := soroban.ScvAddress(user)
	if err != nil {
		return nil, err
	}
	sim, err := rpc.SimulateRead(passphrase, poolAddr, "get_positions", userVal)
	if err != nil {
		return nil, err
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("get_positions: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		// NOT a debt-free answer. A simulate that returns no result at all is a
		// read we could not complete, and reporting it as "holds nothing" is
		// how a live borrower gets retired from the index: the index would
		// record Debt=false, and an idle underwater position emits no further
		// events to revive it, so it would never be probed again.
		return nil, fmt.Errorf("get_positions: simulate returned no results")
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil, err
	}
	return parsePositions(val, user)
}

func newPosition(user string) *Position {
	return &Position{
		Address:     user,
		Collateral:  make(map[uint32]*big.Int),
		Supply:      make(map[uint32]*big.Int),
		Liabilities: make(map[uint32]*big.Int),
	}
}

// parsePositions decodes the pool's Positions struct. A value that is not the
// expected map is an unreadable answer, not an empty position — same reasoning
// as the no-results case above.
func parsePositions(val xdr.ScVal, user string) (*Position, error) {
	pos := newPosition(user)
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return nil, fmt.Errorf("get_positions: expected ScvMap, got %v", val.Type)
	}
	for _, entry := range **val.Map {
		key := scSymbol(entry.Key)
		switch key {
		case "collateral":
			if entry.Val.Type == xdr.ScValTypeScvMap && entry.Val.Map != nil && *entry.Val.Map != nil {
				for _, e := range **entry.Val.Map {
					idx := scU32(e.Key)
					if amt := scI128(e.Val); amt != nil {
						pos.Collateral[idx] = amt
					}
				}
			}
		case "supply":
			if entry.Val.Type == xdr.ScValTypeScvMap && entry.Val.Map != nil && *entry.Val.Map != nil {
				for _, e := range **entry.Val.Map {
					idx := scU32(e.Key)
					if amt := scI128(e.Val); amt != nil {
						pos.Supply[idx] = amt
					}
				}
			}
		case "liabilities":
			if entry.Val.Type == xdr.ScValTypeScvMap && entry.Val.Map != nil && *entry.Val.Map != nil {
				for _, e := range **entry.Val.Map {
					idx := scU32(e.Key)
					if amt := scI128(e.Val); amt != nil {
						pos.Liabilities[idx] = amt
					}
				}
			}
		}
	}
	return pos, nil
}

// EstimateCapital returns an upper-bound USD estimate of the capital a
// keeper needs to liquidate `pct`% of the position's debt. The result is
// expressed in 7-decimal stroops (USDC). Returns 0 if the position has no
// liabilities or pct <= 0. Unpriced reserves (OraclePrice 0) contribute
// nothing rather than a fabricated value.
func EstimateCapital(pos Position, pool *PoolState, pct int64) int64 {
	if pct <= 0 {
		return 0
	}
	if pct > 100 {
		pct = 100
	}
	indexMap := make(map[uint32]*Reserve)
	for _, r := range pool.Reserves {
		indexMap[r.Index] = r
	}
	var totalUSD float64
	for idx, dAmt := range pos.Liabilities {
		r, ok := indexMap[idx]
		if !ok {
			continue
		}
		f, _ := new(big.Float).SetInt(dAmt).Float64()
		// dTokens -> underlying (DRate, 12-dec multiplier) -> whole tokens -> USD
		totalUSD += (f / r.TokenScalar()) * r.DRate * r.OraclePrice
	}
	scaled := totalUSD * float64(pct) / 100.0
	return int64(scaled * scalar)
}

// ErrUnpricedReserve reports that a position touches a reserve with no usable
// oracle price, so its health factor cannot be computed.
var ErrUnpricedReserve = errors.New("position touches an unpriced reserve")

// CalcHealthFactor mirrors blend-contracts-v2 PositionData::calculate_from_positions
// (pool/src/pool/health_factor.rs:27-77 @ ba22b48):
//
//	collateral_base = Σ b_tokens × b_rate × c_factor × price
//	liability_base  = Σ d_tokens × d_rate ÷ l_factor × price
//	HF = collateral_base / liability_base
//
// b_rate/d_rate convert b/d tokens to underlying (12-dec fixed point on-chain,
// plain multipliers here); token amounts are scaled by each reserve's own
// decimals. Only `collateral` backs debt — `supply` is excluded, as on-chain.
//
// Deprecated for decision-making: prefer HealthFactor, which reports when a
// leg could not be priced. This wrapper keeps the old signature (0 on error)
// for display paths.
func CalcHealthFactor(pos Position, pool *PoolState) float64 {
	hf, err := HealthFactor(pos, pool)
	if err != nil {
		return 0
	}
	return hf
}

// HealthFactor computes the health factor and returns ErrUnpricedReserve when
// any leg of the position references a reserve the pool did not price or does
// not list. Silently skipping such a leg would invent a health factor: an
// unpriced collateral reserve reads as HF≈0 (false liquidation signal, and the
// on-chain new_auction would panic InvalidPrice), while an unpriced liability
// reads as HF=+Inf (a real liquidation missed).
func HealthFactor(pos Position, pool *PoolState) (float64, error) {
	// build index -> reserve map
	indexMap := make(map[uint32]*Reserve)
	for _, r := range pool.Reserves {
		indexMap[r.Index] = r
	}

	var effColl, effLiab float64
	for idx, bAmt := range pos.Collateral {
		r, ok := indexMap[idx]
		if !ok || r.OraclePrice <= 0 {
			return 0, fmt.Errorf("%w: collateral reserve index %d", ErrUnpricedReserve, idx)
		}
		f, _ := new(big.Float).SetInt(bAmt).Float64()
		effColl += (f / r.TokenScalar()) * r.BRate * r.OraclePrice * r.CollateralFactor
	}
	for idx, dAmt := range pos.Liabilities {
		r, ok := indexMap[idx]
		if !ok || r.OraclePrice <= 0 || r.LiabilityFactor == 0 {
			return 0, fmt.Errorf("%w: liability reserve index %d", ErrUnpricedReserve, idx)
		}
		f, _ := new(big.Float).SetInt(dAmt).Float64()
		effLiab += (f / r.TokenScalar()) * r.DRate * r.OraclePrice / r.LiabilityFactor
	}

	if effLiab == 0 {
		return math.Inf(1), nil
	}
	return effColl / effLiab, nil
}
