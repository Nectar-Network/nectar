package blend

import (
	"fmt"
	"math"
	"math/big"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

type Position struct {
	Address     string
	Collateral  map[uint32]*big.Int // reserve index -> b_token balance
	Liabilities map[uint32]*big.Int // reserve index -> d_token balance
	HF          float64
}

// GetPositions discovers users from pool events and loads their positions.
func GetPositions(rpc *soroban.Client, passphrase, poolAddr string, startLedger int64) ([]Position, error) {
	events, err := rpc.GetEvents(startLedger, poolAddr)
	if err != nil {
		return nil, fmt.Errorf("get events: %w", err)
	}

	seen := make(map[string]struct{})
	for _, ev := range events {
		if len(ev.Topic) < 2 {
			continue
		}
		var val xdr.ScVal
		if err := xdr.SafeUnmarshalBase64(ev.Topic[1], &val); err != nil {
			continue
		}
		if val.Type == xdr.ScValTypeScvAddress && val.Address != nil {
			addr, err := soroban.ParseAddress(*val.Address)
			if err == nil {
				seen[addr] = struct{}{}
			}
		}
	}

	positions := make([]Position, 0, len(seen))
	for addr := range seen {
		pos, err := loadPosition(rpc, passphrase, poolAddr, addr)
		if err != nil {
			continue
		}
		positions = append(positions, *pos)
	}
	return positions, nil
}

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
		return &Position{Address: user, Collateral: make(map[uint32]*big.Int), Liabilities: make(map[uint32]*big.Int)}, nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil, err
	}
	return parsePositions(val, user), nil
}

func parsePositions(val xdr.ScVal, user string) *Position {
	pos := &Position{
		Address:     user,
		Collateral:  make(map[uint32]*big.Int),
		Liabilities: make(map[uint32]*big.Int),
	}
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return pos
	}
	for _, entry := range **val.Map {
		key := scSymbol(entry.Key)
		switch key {
		case "collateral", "supply":
			if entry.Val.Type == xdr.ScValTypeScvMap && entry.Val.Map != nil && *entry.Val.Map != nil {
				for _, e := range **entry.Val.Map {
					idx := scU32(e.Key)
					if amt := scI128(e.Val); amt != nil {
						pos.Collateral[idx] = amt
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
	return pos
}

// EstimateCapital returns an upper-bound USD estimate of the capital a
// keeper needs to liquidate `pct`% of the position's debt. The result is
// expressed in 7-decimal stroops (USDC). Returns 0 if the position has no
// liabilities or pct <= 0.
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
		totalUSD += (f / scalar) * (r.DRate / scalar) * r.OraclePrice
	}
	scaled := totalUSD * float64(pct) / 100.0
	return int64(scaled * scalar)
}

// CalcHealthFactor computes HF = Σ(collateral*price*cFactor) / Σ(liability*price/lFactor).
func CalcHealthFactor(pos Position, pool *PoolState) float64 {
	// build index -> reserve map
	indexMap := make(map[uint32]*Reserve)
	for _, r := range pool.Reserves {
		indexMap[r.Index] = r
	}

	var effColl, effLiab float64
	for idx, bAmt := range pos.Collateral {
		r, ok := indexMap[idx]
		if !ok {
			continue
		}
		f, _ := new(big.Float).SetInt(bAmt).Float64()
		effColl += (f / scalar) * (r.BRate / scalar) * r.OraclePrice * r.CollateralFactor
	}
	for idx, dAmt := range pos.Liabilities {
		r, ok := indexMap[idx]
		if !ok {
			continue
		}
		f, _ := new(big.Float).SetInt(dAmt).Float64()
		effLiab += (f / scalar) * (r.DRate / scalar) * r.OraclePrice / r.LiabilityFactor
	}

	if effLiab == 0 {
		return math.Inf(1)
	}
	return effColl / effLiab
}
