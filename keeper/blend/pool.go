package blend

import (
	"fmt"
	"math/big"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

type PoolState struct {
	Reserves    map[string]*Reserve // asset address -> reserve
	OracleAddr  string              // pool's oracle contract (Blend get_config.oracle)
	OracleDec   uint32              // oracle price decimals (from oracle.decimals())
	OracleStale bool                // true if any reserve fell back to a stub price
}

type Reserve struct {
	Asset            string
	Index            uint32
	CollateralFactor float64
	LiabilityFactor  float64
	BRate            float64 // exchange-rate multiplier × scalar (1.0× ⇒ scalar)
	DRate            float64 // exchange-rate multiplier × scalar (1.0× ⇒ scalar)
	OraclePrice      float64 // USD price; sourced from pool oracle. 0 means "unknown".
	PriceTimestamp   uint64  // oracle's reported sample timestamp
}

const (
	scalar = 1e7
	// rateScalar is Blend's universal RATE_SCALAR (1e12). The keeper stores
	// rates so that BRate/scalar = real multiplier, so on parse we divide the
	// raw rate by (rateScalar/scalar) = 1e5. A fresh pool with no accrual
	// returns raw_rate=1e12 which then maps to BRate=scalar (multiplier 1.0).
	rateScalar       = 1e12
	rateNormDivisor  = rateScalar / scalar // 1e5
)

// LoadPool queries a Blend pool contract for reserve configuration and pulls
// live USD prices from the pool's configured oracle. A reserve without a fresh
// oracle reading retains OraclePrice=0 and PoolState.OracleStale is set — the
// caller can decide whether to skip profitability checks for that asset.
func LoadPool(rpc *soroban.Client, passphrase, poolAddr string) (*PoolState, error) {
	ps := &PoolState{Reserves: make(map[string]*Reserve)}

	// 1. Read pool config to find the oracle address (Blend v2: get_config.oracle).
	if cfgSim, err := rpc.SimulateRead(passphrase, poolAddr, "get_config"); err == nil && cfgSim.Error == "" && len(cfgSim.Results) > 0 {
		var cfgVal xdr.ScVal
		if err := xdr.SafeUnmarshalBase64(cfgSim.Results[0].XDR, &cfgVal); err == nil {
			ps.OracleAddr = parseConfigOracle(cfgVal)
		}
	}

	// 2. Pull oracle decimals once (Reflector/Blend oracles expose .decimals() → u32).
	ps.OracleDec = 7 // sane default if oracle doesn't answer
	if ps.OracleAddr != "" {
		if decSim, err := rpc.SimulateRead(passphrase, ps.OracleAddr, "decimals"); err == nil && decSim.Error == "" && len(decSim.Results) > 0 {
			var v xdr.ScVal
			if err := xdr.SafeUnmarshalBase64(decSim.Results[0].XDR, &v); err == nil {
				if v.Type == xdr.ScValTypeScvU32 && v.U32 != nil {
					ps.OracleDec = uint32(*v.U32)
				}
			}
		}
	}

	// 3. Reserve list.
	sim, err := rpc.SimulateRead(passphrase, poolAddr, "get_reserve_list")
	if err != nil {
		return nil, fmt.Errorf("reserve list: %w", err)
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("reserve list sim: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return ps, nil
	}
	var listVal xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &listVal); err != nil {
		return nil, err
	}
	assets := parseVec(listVal)

	priceDivisor := float64(1)
	for i := uint32(0); i < ps.OracleDec; i++ {
		priceDivisor *= 10
	}

	// 4. For each asset: load reserve config, then ask the oracle for a price.
	for _, assetAddr := range assets {
		addrVal, err := soroban.ScvAddress(assetAddr)
		if err != nil {
			continue
		}
		resSim, err := rpc.SimulateRead(passphrase, poolAddr, "get_reserve", addrVal)
		if err != nil || resSim.Error != "" {
			continue
		}
		if len(resSim.Results) == 0 {
			continue
		}
		var resVal xdr.ScVal
		if err := xdr.SafeUnmarshalBase64(resSim.Results[0].XDR, &resVal); err != nil {
			continue
		}
		res := parseReserve(resVal, assetAddr)

		// Price lookup: Reflector-style oracle.lastprice(Asset::Stellar(addr)).
		// Missing/zero price leaves OraclePrice=0 and flags the pool as stale.
		if ps.OracleAddr != "" {
			price, ts, ok := lookupPrice(rpc, passphrase, ps.OracleAddr, assetAddr, priceDivisor)
			if ok {
				res.OraclePrice = price
				res.PriceTimestamp = ts
			} else {
				ps.OracleStale = true
			}
		} else {
			ps.OracleStale = true
		}

		ps.Reserves[assetAddr] = res
	}
	return ps, nil
}

// lookupPrice asks the oracle for the asset's last price using the Blend/Reflector
// enum encoding Asset::Stellar(Address). Returns (price_in_usd, sample_ts, ok).
func lookupPrice(rpc *soroban.Client, passphrase, oracle, asset string, divisor float64) (float64, uint64, bool) {
	assetVal, err := assetStellar(asset)
	if err != nil {
		return 0, 0, false
	}
	sim, err := rpc.SimulateRead(passphrase, oracle, "lastprice", assetVal)
	if err != nil || sim == nil || sim.Error != "" || len(sim.Results) == 0 {
		return 0, 0, false
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return 0, 0, false
	}
	// Oracle returns Option<PriceData{price: i128, timestamp: u64}>; absent =>
	// ScvVoid. We accept ScvMap directly or unwrap a single-element Vec/Option.
	mapVal := val
	if mapVal.Type == xdr.ScValTypeScvVec && mapVal.Vec != nil && *mapVal.Vec != nil && len(**mapVal.Vec) == 1 {
		mapVal = (**mapVal.Vec)[0]
	}
	if mapVal.Type != xdr.ScValTypeScvMap || mapVal.Map == nil || *mapVal.Map == nil {
		return 0, 0, false
	}
	var priceRaw *big.Int
	var ts uint64
	for _, e := range **mapVal.Map {
		if e.Key.Type != xdr.ScValTypeScvSymbol || e.Key.Sym == nil {
			continue
		}
		switch string(*e.Key.Sym) {
		case "price":
			if p := scI128(e.Val); p != nil {
				priceRaw = p
			}
		case "timestamp":
			if e.Val.Type == xdr.ScValTypeScvU64 && e.Val.U64 != nil {
				ts = uint64(*e.Val.U64)
			}
		}
	}
	if priceRaw == nil || priceRaw.Sign() <= 0 || divisor == 0 {
		return 0, 0, false
	}
	pf, _ := new(big.Float).SetInt(priceRaw).Float64()
	return pf / divisor, ts, true
}

// assetStellar builds the Asset::Stellar(Address) enum variant: ScVec[Symbol("Stellar"), Address].
func assetStellar(addr string) (xdr.ScVal, error) {
	a, err := soroban.ScvAddress(addr)
	if err != nil {
		return xdr.ScVal{}, err
	}
	sym := soroban.ScvSymbol("Stellar")
	vec := xdr.ScVec{sym, a}
	vp := &vec
	return xdr.ScVal{Type: xdr.ScValTypeScvVec, Vec: &vp}, nil
}

// parseConfigOracle extracts the `oracle` address from a Blend pool's get_config map.
func parseConfigOracle(val xdr.ScVal) string {
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return ""
	}
	for _, e := range **val.Map {
		if e.Key.Type != xdr.ScValTypeScvSymbol || e.Key.Sym == nil {
			continue
		}
		if string(*e.Key.Sym) != "oracle" {
			continue
		}
		if e.Val.Type == xdr.ScValTypeScvAddress && e.Val.Address != nil {
			a, err := soroban.ParseAddress(*e.Val.Address)
			if err == nil {
				return a
			}
		}
	}
	return ""
}

// parseReserve reads a Blend Reserve struct. Real Blend nests sub-fields under
// "config" (c_factor, l_factor, index) and "data" (b_rate, d_rate) and uses
// 1e12-scale rates. LiquidationLab keeps fields flat and uses 1e7-scale rates.
// We detect which shape we're in (the presence of "config" or "data" sub-maps
// is the signal) and normalize rates so that stored / scalar = real multiplier
// in both cases.
//
// OraclePrice is filled in afterwards by LoadPool from the pool's oracle —
// this function leaves it at 0 ("unknown").
func parseReserve(val xdr.ScVal, asset string) *Reserve {
	res := &Reserve{
		Asset:            asset,
		CollateralFactor: 0.75,
		LiabilityFactor:  1.1,
		BRate:            scalar, // multiplier 1.0 = no interest accrued
		DRate:            scalar,
		OraclePrice:      0, // filled in by LoadPool's oracle pass
	}
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return res
	}

	nested := false
	for _, e := range **val.Map {
		switch scSymbol(e.Key) {
		case "config", "data":
			nested = true
		}
	}

	for _, e := range **val.Map {
		k := scSymbol(e.Key)
		switch k {
		case "asset":
			// Blend's reserve carries the asset address inline. We already
			// know it from the caller, but keep this branch so the field
			// doesn't silently fall into the default no-op.
		case "config":
			applyReserveConfig(res, e.Val)
		case "data":
			applyReserveData(res, e.Val, nested)
		// LiquidationLab's flat shape (no "config"/"data" wrappers):
		case "index":
			if !nested {
				res.Index = scU32(e.Val)
			}
		case "c_factor":
			if !nested {
				res.CollateralFactor = scU32AsFactor(e.Val)
			}
		case "l_factor":
			if !nested {
				res.LiabilityFactor = scU32AsFactor(e.Val)
			}
		case "b_rate":
			if !nested {
				res.BRate = scI128AsRate(e.Val, false)
			}
		case "d_rate":
			if !nested {
				res.DRate = scI128AsRate(e.Val, false)
			}
		}
	}
	return res
}

func applyReserveConfig(res *Reserve, val xdr.ScVal) {
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return
	}
	for _, e := range **val.Map {
		switch scSymbol(e.Key) {
		case "index":
			res.Index = scU32(e.Val)
		case "c_factor":
			res.CollateralFactor = scU32AsFactor(e.Val)
		case "l_factor":
			res.LiabilityFactor = scU32AsFactor(e.Val)
		}
	}
}

func applyReserveData(res *Reserve, val xdr.ScVal, isBlendScale bool) {
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return
	}
	for _, e := range **val.Map {
		switch scSymbol(e.Key) {
		case "b_rate":
			res.BRate = scI128AsRate(e.Val, isBlendScale)
		case "d_rate":
			res.DRate = scI128AsRate(e.Val, isBlendScale)
		}
	}
}

func scU32AsFactor(val xdr.ScVal) float64 {
	if val.Type != xdr.ScValTypeScvU32 || val.U32 == nil {
		return 0
	}
	return float64(*val.U32) / scalar
}

// scI128AsRate normalizes a rate i128 to the keeper's stored convention where
// `rate / scalar = real multiplier`. Real Blend rates are at 1e12 scale, so we
// divide by rateNormDivisor (1e5). LiquidationLab rates are already at scalar
// (1e7) and pass through unchanged.
func scI128AsRate(val xdr.ScVal, isBlendScale bool) float64 {
	v := scI128(val)
	if v == nil {
		return scalar // safe default = 1.0×
	}
	f, _ := new(big.Float).SetInt(v).Float64()
	if f <= 0 {
		return scalar
	}
	if isBlendScale {
		return f / rateNormDivisor
	}
	return f
}

// PriceFor returns (priceUSD, true) when the pool has a fresh oracle price for
// the asset. (0, false) means either the asset isn't in the pool or the oracle
// didn't return a usable price — callers should refuse to fill in that case.
func (p *PoolState) PriceFor(asset string) (float64, bool) {
	if p == nil {
		return 0, false
	}
	r, ok := p.Reserves[asset]
	if !ok || r == nil || r.OraclePrice <= 0 {
		return 0, false
	}
	return r.OraclePrice, true
}

// HasPricesFor returns true iff every asset in the slice has a non-zero oracle
// price. Use it to gate any fill that depends on profitability math.
func (p *PoolState) HasPricesFor(assets ...string) bool {
	for _, a := range assets {
		if _, ok := p.PriceFor(a); !ok {
			return false
		}
	}
	return true
}

func parseVec(val xdr.ScVal) []string {
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		return nil
	}
	out := make([]string, 0)
	for _, item := range **val.Vec {
		if item.Type == xdr.ScValTypeScvAddress && item.Address != nil {
			addr, err := soroban.ParseAddress(*item.Address)
			if err == nil {
				out = append(out, addr)
			}
		}
	}
	return out
}

func scSymbol(val xdr.ScVal) string {
	if val.Type == xdr.ScValTypeScvSymbol && val.Sym != nil {
		return string(*val.Sym)
	}
	return ""
}

func scU32(val xdr.ScVal) uint32 {
	if val.Type == xdr.ScValTypeScvU32 && val.U32 != nil {
		return uint32(*val.U32)
	}
	return 0
}

func scI128(val xdr.ScVal) *big.Int {
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return nil
	}
	hi := new(big.Int).SetInt64(int64(val.I128.Hi))
	lo := new(big.Int).SetUint64(uint64(val.I128.Lo))
	result := new(big.Int).Lsh(hi, 64)
	result.Add(result, lo)
	return result
}
