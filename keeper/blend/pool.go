package blend

import (
	"fmt"
	"math/big"
	"time"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// PoolState is a read-only snapshot of one Blend pool: its reserve list with
// rates and risk params, plus oracle metadata. All facts are loaded live from
// chain — nothing is defaulted. Shapes verified against blend-contracts-v2
// @ ba22b48 (pool/src/storage.rs, pool/src/pool/reserve.rs) — see docs/FACTS.md.
type PoolState struct {
	Reserves       map[string]*Reserve // asset address -> reserve
	OracleAddr     string              // PoolConfig.oracle
	OracleDecimals uint32              // oracle decimals() — price scaling
	Status         uint32              // PoolConfig.status (0=Active .. 6=Setup)
	MaxPositions   uint32              // PoolConfig.max_positions
	Backstop       string              // backstop address, when discoverable
}

// maxPriceAge mirrors the pool's own staleness rule: load_price panics
// InvalidPrice when price_data.timestamp + 24h < now, or price <= 0
// (blend-contracts-v2 pool/src/pool/pool.rs:119-131 @ ba22b48). A price the
// pool would reject must not drive our decisions either.
const maxPriceAge = 24 * 60 * 60

// Reserve carries one reserve's config, rates and oracle price.
//
// Semantics (per blend-contracts-v2 @ ba22b48):
//   - CollateralFactor / LiabilityFactor: ReserveConfig.c_factor / l_factor,
//     u32 with 7 decimals, stored here as plain fractions (0.95 = 95 %).
//   - BRate / DRate: ReserveData.b_rate / d_rate, i128 with 12 decimals
//     (SCALAR_12), stored here as plain multipliers (1.0034 …). bToken and
//     dToken balances multiply by these to reach underlying amounts.
//   - OraclePrice: lastprice(Asset::Stellar(asset)).price / 10^OracleDecimals,
//     i.e. USD per whole token. 0 means "no price available" and consumers
//     must treat the reserve as unpriced, never as price 0.30 or 1.
type Reserve struct {
	Asset            string
	Index            uint32
	Decimals         uint32 // underlying + bToken/dToken decimals
	Enabled          bool
	CollateralFactor float64
	LiabilityFactor  float64
	BRate            float64
	DRate            float64
	BSupply          *big.Int // bToken supply, underlying decimals
	DSupply          *big.Int // dToken supply, underlying decimals
	OraclePrice      float64
}

const scalar = 1e7

// scalar12 converts ReserveData.b_rate/d_rate (12-dec fixed point) to floats.
const scalar12 = 1e12

// TokenScalar returns 10^Decimals as a float, the divisor from raw token
// amounts to whole tokens.
func (r *Reserve) TokenScalar() float64 {
	s := 1.0
	for i := uint32(0); i < r.Decimals; i++ {
		s *= 10
	}
	return s
}

// LoadPool queries a Blend pool contract for its config, reserves and live
// oracle prices. Reserves that fail to load or parse are skipped (the returned
// PoolState only ever contains fully-parsed reserves); a pool whose config or
// reserve list cannot be read errors out.
func LoadPool(rpc *soroban.Client, passphrase, poolAddr string) (*PoolState, error) {
	ps := &PoolState{Reserves: make(map[string]*Reserve)}

	// get_config -> PoolConfig { oracle, min_collateral, bstop_rate, status, max_positions }
	cfgSim, err := rpc.SimulateRead(passphrase, poolAddr, "get_config")
	if err != nil {
		return nil, fmt.Errorf("get_config: %w", err)
	}
	if cfgSim.Error != "" {
		return nil, fmt.Errorf("get_config sim: %s", cfgSim.Error)
	}
	if len(cfgSim.Results) > 0 {
		var cfgVal xdr.ScVal
		if err := xdr.SafeUnmarshalBase64(cfgSim.Results[0].XDR, &cfgVal); err != nil {
			return nil, fmt.Errorf("get_config decode: %w", err)
		}
		parsePoolConfig(cfgVal, ps)
	}

	// oracle decimals() — needed to scale every price
	if ps.OracleAddr != "" {
		decSim, err := rpc.SimulateRead(passphrase, ps.OracleAddr, "decimals")
		if err == nil && decSim.Error == "" && len(decSim.Results) > 0 {
			var decVal xdr.ScVal
			if xdr.SafeUnmarshalBase64(decSim.Results[0].XDR, &decVal) == nil {
				ps.OracleDecimals = scU32(decVal)
			}
		}
	}

	// get_reserve_list
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
		res, err := parseReserve(resVal, assetAddr)
		if err != nil {
			continue
		}
		res.OraclePrice = loadOraclePrice(rpc, passphrase, ps, assetAddr, nowUnix())
		ps.Reserves[assetAddr] = res
	}
	return ps, nil
}

// nowUnix is the clock used for oracle staleness checks; a variable so tests
// can pin it.
var nowUnix = func() int64 { return time.Now().Unix() }

// loadOraclePrice fetches lastprice(Asset::Stellar(asset)) from the pool's
// oracle and returns USD per whole token, or 0 when unavailable, non-positive,
// or staler than the pool's own 24h window.
func loadOraclePrice(rpc *soroban.Client, passphrase string, ps *PoolState, assetAddr string, now int64) float64 {
	if ps.OracleAddr == "" || ps.OracleDecimals == 0 {
		return 0
	}
	assetArg, err := oracleAssetArg(assetAddr)
	if err != nil {
		return 0
	}
	sim, err := rpc.SimulateRead(passphrase, ps.OracleAddr, "lastprice", assetArg)
	if err != nil || sim.Error != "" || len(sim.Results) == 0 {
		return 0
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return 0
	}
	raw, ts := parsePriceData(val)
	if raw == nil || raw.Sign() <= 0 {
		return 0 // absent or non-positive: the pool would panic InvalidPrice
	}
	if ts > 0 && now > 0 && ts+maxPriceAge < now {
		return 0 // stale beyond the pool's 24h window
	}
	price, _ := new(big.Float).SetInt(raw).Float64()
	div := 1.0
	for i := uint32(0); i < ps.OracleDecimals; i++ {
		div *= 10
	}
	return price / div
}

// oracleAssetArg encodes the SEP-40 Asset::Stellar(address) enum variant:
// Vec[Symbol("Stellar"), Address].
func oracleAssetArg(assetAddr string) (xdr.ScVal, error) {
	addrVal, err := soroban.ScvAddress(assetAddr)
	if err != nil {
		return xdr.ScVal{}, err
	}
	return soroban.ScvVec(soroban.ScvSymbol("Stellar"), addrVal), nil
}

// parsePriceData decodes Option<PriceData { price: i128, timestamp: u64 }>,
// returning the raw price and its timestamp (0 when absent).
func parsePriceData(val xdr.ScVal) (*big.Int, int64) {
	if val.Type == xdr.ScValTypeScvVoid {
		return nil, 0
	}
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return nil, 0
	}
	var price *big.Int
	var ts int64
	for _, e := range **val.Map {
		switch scSymbol(e.Key) {
		case "price":
			price = scI128(e.Val)
		case "timestamp":
			switch e.Val.Type {
			case xdr.ScValTypeScvU64:
				if e.Val.U64 != nil {
					ts = int64(*e.Val.U64)
				}
			case xdr.ScValTypeScvU32:
				if e.Val.U32 != nil {
					ts = int64(*e.Val.U32)
				}
			}
		}
	}
	return price, ts
}

// parsePoolConfig decodes PoolConfig { oracle, min_collateral, bstop_rate,
// status, max_positions } into ps.
func parsePoolConfig(val xdr.ScVal, ps *PoolState) {
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return
	}
	for _, e := range **val.Map {
		switch scSymbol(e.Key) {
		case "oracle":
			if e.Val.Type == xdr.ScValTypeScvAddress && e.Val.Address != nil {
				if addr, err := soroban.ParseAddress(*e.Val.Address); err == nil {
					ps.OracleAddr = addr
				}
			}
		case "status":
			ps.Status = scU32(e.Val)
		case "max_positions":
			ps.MaxPositions = scU32(e.Val)
		}
	}
}

// parseReserve decodes the nested Reserve { asset, config: ReserveConfig,
// data: ReserveData, scalar } returned by get_reserve. Structure verified at
// blend-contracts-v2 pool/src/pool/reserve.rs:16-21 and pool/src/storage.rs
// (ReserveConfig :45-59, ReserveData :71-79) @ ba22b48. Errors instead of
// fabricating defaults: a reserve we cannot parse must never reach HF or
// profitability math.
func parseReserve(val xdr.ScVal, asset string) (*Reserve, error) {
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return nil, fmt.Errorf("reserve %s: not a map", asset)
	}
	res := &Reserve{Asset: asset}
	var haveConfig, haveData bool
	for _, e := range **val.Map {
		switch scSymbol(e.Key) {
		case "config":
			if parseReserveConfig(e.Val, res) {
				haveConfig = true
			}
		case "data":
			if parseReserveData(e.Val, res) {
				haveData = true
			}
		}
	}
	if !haveConfig || !haveData {
		return nil, fmt.Errorf("reserve %s: missing config/data submaps", asset)
	}
	return res, nil
}

func parseReserveConfig(val xdr.ScVal, res *Reserve) bool {
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return false
	}
	seen := 0
	for _, e := range **val.Map {
		switch scSymbol(e.Key) {
		case "index":
			res.Index = scU32(e.Val)
			seen++
		case "decimals":
			res.Decimals = scU32(e.Val)
			seen++
		case "c_factor":
			res.CollateralFactor = float64(scU32(e.Val)) / scalar
			seen++
		case "l_factor":
			res.LiabilityFactor = float64(scU32(e.Val)) / scalar
			seen++
		case "enabled":
			if e.Val.Type == xdr.ScValTypeScvBool && e.Val.B != nil {
				res.Enabled = bool(*e.Val.B)
			}
		}
	}
	return seen >= 4
}

func parseReserveData(val xdr.ScVal, res *Reserve) bool {
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return false
	}
	seen := 0
	for _, e := range **val.Map {
		switch scSymbol(e.Key) {
		case "b_rate":
			if v := scI128(e.Val); v != nil {
				f, _ := new(big.Float).SetInt(v).Float64()
				res.BRate = f / scalar12
				seen++
			}
		case "d_rate":
			if v := scI128(e.Val); v != nil {
				f, _ := new(big.Float).SetInt(v).Float64()
				res.DRate = f / scalar12
				seen++
			}
		case "b_supply":
			res.BSupply = scI128(e.Val)
		case "d_supply":
			res.DSupply = scI128(e.Val)
		}
	}
	return seen == 2
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

// ReserveAssets returns just the pool's reserve address list — a single cheap
// read used at startup to verify a pool settles the asset the vault holds,
// without the per-reserve config/oracle round-trips LoadPool performs.
func ReserveAssets(rpc *soroban.Client, passphrase, poolAddr string) ([]string, error) {
	sim, err := rpc.SimulateRead(passphrase, poolAddr, "get_reserve_list")
	if err != nil {
		return nil, fmt.Errorf("reserve list: %w", err)
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("reserve list sim: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return nil, fmt.Errorf("reserve list: empty result")
	}
	var listVal xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &listVal); err != nil {
		return nil, err
	}
	return parseVec(listVal), nil
}
