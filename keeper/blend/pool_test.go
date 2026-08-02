package blend

import (
	"math/big"
	"testing"

	"github.com/stellar/go/strkey"
	"github.com/stellar/go/xdr"
)

// --- ScVal fixture builders -------------------------------------------------

func symVal(s string) xdr.ScVal {
	sym := xdr.ScSymbol(s)
	return xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym}
}

func u32Val(v uint32) xdr.ScVal {
	u := xdr.Uint32(v)
	return xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &u}
}

func boolVal(b bool) xdr.ScVal {
	return xdr.ScVal{Type: xdr.ScValTypeScvBool, B: &b}
}

func i128Val(v int64) xdr.ScVal {
	var hi xdr.Int64
	if v < 0 {
		hi = -1
	}
	parts := xdr.Int128Parts{Hi: hi, Lo: xdr.Uint64(uint64(v))}
	return xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &parts}
}

func mapVal(entries ...xdr.ScMapEntry) xdr.ScVal {
	m := xdr.ScMap(entries)
	mp := &m
	return xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &mp}
}

func entry(key string, val xdr.ScVal) xdr.ScMapEntry {
	return xdr.ScMapEntry{Key: symVal(key), Val: val}
}

func addrVal(t *testing.T, contractID string) xdr.ScVal {
	t.Helper()
	raw, err := strkey.Decode(strkey.VersionByteContract, contractID)
	if err != nil {
		t.Fatalf("decode %s: %v", contractID, err)
	}
	var h xdr.ContractId
	copy(h[:], raw)
	addr := xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract, ContractId: &h}
	return xdr.ScVal{Type: xdr.ScValTypeScvAddress, Address: &addr}
}

const testAsset = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
const testOracle = "CAZOKR2Y5E2OSWSIBRVZMJ47RUTQPIGVWSAQ2UISGAVC46XKPGDG5PKI"

// nestedReserveVal mirrors the real get_reserve return: Reserve { asset,
// config: ReserveConfig, data: ReserveData, scalar } per blend-contracts-v2
// pool/src/pool/reserve.rs:16-21 @ ba22b48.
func nestedReserveVal(t *testing.T) xdr.ScVal {
	config := mapVal(
		entry("c_factor", u32Val(9_500_000)), // 0.95
		entry("decimals", u32Val(7)),
		entry("enabled", boolVal(true)),
		entry("index", u32Val(2)),
		entry("l_factor", u32Val(9_000_000)), // 0.90
		entry("max_util", u32Val(9_900_000)),
	)
	data := mapVal(
		entry("b_rate", i128Val(1_003_400_000_000)), // 1.0034 in 12-dec
		entry("b_supply", i128Val(5_000_0000000)),
		entry("d_rate", i128Val(1_010_000_000_000)), // 1.01 in 12-dec
		entry("d_supply", i128Val(2_000_0000000)),
	)
	return mapVal(
		entry("asset", addrVal(t, testAsset)),
		entry("config", config),
		entry("data", data),
		entry("scalar", i128Val(10_000_000)),
	)
}

// --- parseReserve -----------------------------------------------------------

func TestParseReserve_NestedStructure(t *testing.T) {
	res, err := parseReserve(nestedReserveVal(t), testAsset)
	if err != nil {
		t.Fatalf("parseReserve: %v", err)
	}
	if res.Index != 2 {
		t.Errorf("index: got %d want 2", res.Index)
	}
	if res.Decimals != 7 {
		t.Errorf("decimals: got %d want 7", res.Decimals)
	}
	if !res.Enabled {
		t.Error("enabled: got false want true")
	}
	if res.CollateralFactor != 0.95 {
		t.Errorf("c_factor: got %v want 0.95", res.CollateralFactor)
	}
	if res.LiabilityFactor != 0.90 {
		t.Errorf("l_factor: got %v want 0.90", res.LiabilityFactor)
	}
	if res.BRate < 1.0033 || res.BRate > 1.0035 {
		t.Errorf("b_rate: got %v want ~1.0034 (12-dec scaled)", res.BRate)
	}
	if res.DRate < 1.0099 || res.DRate > 1.0101 {
		t.Errorf("d_rate: got %v want ~1.01", res.DRate)
	}
	if res.BSupply == nil || res.BSupply.Cmp(big.NewInt(5_000_0000000)) != 0 {
		t.Errorf("b_supply: got %v", res.BSupply)
	}
}

func TestParseReserve_FlatMapRejected(t *testing.T) {
	// The pre-A1 parser expected a flat map; the real contract never returns
	// one. A flat map must now error, not silently produce defaults.
	flat := mapVal(
		entry("index", u32Val(1)),
		entry("c_factor", u32Val(7_500_000)),
	)
	if _, err := parseReserve(flat, testAsset); err == nil {
		t.Fatal("flat reserve map must be rejected")
	}
}

func TestParseReserve_NonMapRejected(t *testing.T) {
	if _, err := parseReserve(u32Val(7), testAsset); err == nil {
		t.Fatal("non-map value must be rejected")
	}
}

// --- parsePriceData / oracleAssetArg ---------------------------------------

func TestParsePriceData_SomeAndNone(t *testing.T) {
	some := mapVal(
		entry("price", i128Val(4_200_000)),
		entry("timestamp", u32Val(1)),
	)
	got := parsePriceData(some)
	if got == nil || got.Cmp(big.NewInt(4_200_000)) != 0 {
		t.Errorf("Some(PriceData): got %v want 4200000", got)
	}
	if parsePriceData(xdr.ScVal{Type: xdr.ScValTypeScvVoid}) != nil {
		t.Error("None must decode to nil")
	}
}

func TestOracleAssetArg_StellarVariant(t *testing.T) {
	val, err := oracleAssetArg(testAsset)
	if err != nil {
		t.Fatalf("oracleAssetArg: %v", err)
	}
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		t.Fatal("want vec")
	}
	vec := **val.Vec
	if len(vec) != 2 {
		t.Fatalf("want 2 elements, got %d", len(vec))
	}
	if scSymbol(vec[0]) != "Stellar" {
		t.Errorf("variant tag: got %q want Stellar", scSymbol(vec[0]))
	}
	if vec[1].Type != xdr.ScValTypeScvAddress {
		t.Error("second element must be the asset address")
	}
}

// --- parsePoolConfig --------------------------------------------------------

func TestParsePoolConfig(t *testing.T) {
	ps := &PoolState{}
	cfg := mapVal(
		entry("bstop_rate", u32Val(1_000_000)),
		entry("max_positions", u32Val(8)),
		entry("min_collateral", i128Val(0)),
		entry("oracle", addrVal(t, testOracle)),
		entry("status", u32Val(6)),
	)
	parsePoolConfig(cfg, ps)
	if ps.OracleAddr != testOracle {
		t.Errorf("oracle: got %s", ps.OracleAddr)
	}
	if ps.Status != 6 {
		t.Errorf("status: got %d want 6", ps.Status)
	}
	if ps.MaxPositions != 8 {
		t.Errorf("max_positions: got %d want 8", ps.MaxPositions)
	}
}

// --- decimal handling in HF math -------------------------------------------

func TestCalcHealthFactor_MixedDecimals(t *testing.T) {
	// A 6-decimal collateral asset and a 7-decimal debt asset must not skew
	// the HF: 100 tokens @ $2, c=0.5 → $100 effective collateral;
	// 50 tokens @ $1, l=1.0 → $50 effective debt; HF=2.
	sixDec := &Reserve{Asset: "SIX", Index: 0, Decimals: 6, CollateralFactor: 0.5, LiabilityFactor: 1.0, BRate: 1.0, DRate: 1.0, OraclePrice: 2.0}
	sevenDec := &Reserve{Asset: "SEVEN", Index: 1, Decimals: 7, CollateralFactor: 0.9, LiabilityFactor: 1.0, BRate: 1.0, DRate: 1.0, OraclePrice: 1.0}
	pool := &PoolState{Reserves: map[string]*Reserve{"SIX": sixDec, "SEVEN": sevenDec}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(100_000000)}, // 100 six-dec tokens
		Liabilities: map[uint32]*big.Int{1: big.NewInt(50_0000000)}, // 50 seven-dec tokens
	}
	hf := CalcHealthFactor(pos, pool)
	if hf < 1.99 || hf > 2.01 {
		t.Errorf("HF: got %v want ~2.0", hf)
	}
}

func TestCalcHealthFactor_BRateScalesCollateral(t *testing.T) {
	// b_rate 1.10 lifts 100 bTokens to 110 underlying: HF = 110×0.5 / 50 = 1.1
	r0 := &Reserve{Asset: "A", Index: 0, Decimals: 7, CollateralFactor: 0.5, LiabilityFactor: 1.0, BRate: 1.10, DRate: 1.0, OraclePrice: 1.0}
	r1 := &Reserve{Asset: "B", Index: 1, Decimals: 7, CollateralFactor: 0.9, LiabilityFactor: 1.0, BRate: 1.0, DRate: 1.0, OraclePrice: 1.0}
	pool := &PoolState{Reserves: map[string]*Reserve{"A": r0, "B": r1}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(100_0000000)},
		Liabilities: map[uint32]*big.Int{1: big.NewInt(50_0000000)},
	}
	hf := CalcHealthFactor(pos, pool)
	if hf < 1.099 || hf > 1.101 {
		t.Errorf("HF: got %v want ~1.1", hf)
	}
}

func TestCalcHealthFactor_UnpricedReserveContributesNothing(t *testing.T) {
	// OraclePrice 0 (no price) must zero that leg, never default to a price.
	r0 := &Reserve{Asset: "A", Index: 0, Decimals: 7, CollateralFactor: 0.5, LiabilityFactor: 1.0, BRate: 1.0, DRate: 1.0, OraclePrice: 0}
	r1 := &Reserve{Asset: "B", Index: 1, Decimals: 7, CollateralFactor: 0.9, LiabilityFactor: 1.0, BRate: 1.0, DRate: 1.0, OraclePrice: 1.0}
	pool := &PoolState{Reserves: map[string]*Reserve{"A": r0, "B": r1}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(100_0000000)},
		Liabilities: map[uint32]*big.Int{1: big.NewInt(50_0000000)},
	}
	hf := CalcHealthFactor(pos, pool)
	if hf != 0 {
		t.Errorf("HF with unpriced collateral: got %v want 0", hf)
	}
}
