package blend

import (
	"math"
	"testing"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

func scvAddressForTest(addr string) (xdr.ScVal, error) { return soroban.ScvAddress(addr) }

// Helpers to build xdr.ScVal values by hand for parseReserve testing.

func scvU32(v uint32) xdr.ScVal {
	u := xdr.Uint32(v)
	return xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &u}
}

func scvI128(hi int64, lo uint64) xdr.ScVal {
	return xdr.ScVal{
		Type: xdr.ScValTypeScvI128,
		I128: &xdr.Int128Parts{Hi: xdr.Int64(hi), Lo: xdr.Uint64(lo)},
	}
}

func scvSym(s string) xdr.ScVal {
	sym := xdr.ScSymbol(s)
	return xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym}
}

func scvMap(pairs ...xdr.ScMapEntry) xdr.ScVal {
	m := xdr.ScMap(pairs)
	mp := &m
	return xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &mp}
}

func pair(k, v xdr.ScVal) xdr.ScMapEntry { return xdr.ScMapEntry{Key: k, Val: v} }

// TestParseReserve_RealBlendShape locks in the parser against Blend v2's
// nested Reserve struct: top-level "asset" + "config" sub-map + "data" sub-map.
// Raw c_factor=9000000 → 0.90; raw b_rate at 1e12 scale → BRate/scalar ≈ 1.0×.
func TestParseReserve_RealBlendShape(t *testing.T) {
	cfg := scvMap(
		pair(scvSym("c_factor"), scvU32(9000000)),
		pair(scvSym("l_factor"), scvU32(9000000)),
		pair(scvSym("index"), scvU32(3)),
	)
	data := scvMap(
		// b_rate = 1_287_642_165_386 ≈ 1.2876 at 1e12 scale → stored / scalar ≈ 1.2876.
		pair(scvSym("b_rate"), scvI128(0, 1287642165386)),
		pair(scvSym("d_rate"), scvI128(0, 1447146158391)),
	)
	root := scvMap(
		pair(scvSym("config"), cfg),
		pair(scvSym("data"), data),
	)

	res := parseReserve(root, "ASSET")

	if res.Index != 3 {
		t.Errorf("Index: got %d want 3", res.Index)
	}
	if math.Abs(res.CollateralFactor-0.9) > 1e-9 {
		t.Errorf("c-factor: got %f want 0.9", res.CollateralFactor)
	}
	if math.Abs(res.LiabilityFactor-0.9) > 1e-9 {
		t.Errorf("l-factor: got %f want 0.9", res.LiabilityFactor)
	}
	// BRate/scalar should land near 1.2876 — i.e. BRate ≈ 12876421.
	mult := res.BRate / scalar
	if math.Abs(mult-1.2876) > 0.001 {
		t.Errorf("BRate multiplier: got %f want ~1.2876", mult)
	}
	mult = res.DRate / scalar
	if math.Abs(mult-1.4471) > 0.001 {
		t.Errorf("DRate multiplier: got %f want ~1.4471", mult)
	}
	if res.OraclePrice != 0 {
		t.Errorf("parseReserve must leave OraclePrice=0 (oracle pass fills it), got %f", res.OraclePrice)
	}
}

// TestParseReserve_FlatLabShape preserves backward compat with LiquidationLab's
// flat reserve layout — c_factor/l_factor/index/b_rate/d_rate at the top level.
func TestParseReserve_FlatLabShape(t *testing.T) {
	root := scvMap(
		pair(scvSym("index"), scvU32(0)),
		pair(scvSym("c_factor"), scvU32(8000000)),
		pair(scvSym("l_factor"), scvU32(11000000)),
		// Lab uses scalar=1e7 for rates → no normalization needed; raw==1e7 → BRate==scalar = 1.0×.
		pair(scvSym("b_rate"), scvI128(0, 10000000)),
		pair(scvSym("d_rate"), scvI128(0, 10000000)),
	)
	res := parseReserve(root, "ASSET")
	if res.Index != 0 {
		t.Errorf("Index: got %d want 0", res.Index)
	}
	if math.Abs(res.CollateralFactor-0.8) > 1e-9 {
		t.Errorf("c-factor: got %f want 0.8", res.CollateralFactor)
	}
	if math.Abs(res.LiabilityFactor-1.1) > 1e-9 {
		t.Errorf("l-factor: got %f want 1.1", res.LiabilityFactor)
	}
	mult := res.BRate / scalar
	if math.Abs(mult-1.0) > 1e-6 {
		t.Errorf("BRate multiplier: got %f want 1.0", mult)
	}
}

func TestPriceFor_MissingAsset(t *testing.T) {
	p := &PoolState{Reserves: map[string]*Reserve{
		"A": {OraclePrice: 1.0},
	}}
	if _, ok := p.PriceFor("MISSING"); ok {
		t.Fatal("missing asset must report ok=false")
	}
	if px, ok := p.PriceFor("A"); !ok || px != 1.0 {
		t.Fatalf("present asset: got px=%f ok=%v", px, ok)
	}
}

func TestPriceFor_ZeroPriceTreatedAsUnknown(t *testing.T) {
	p := &PoolState{Reserves: map[string]*Reserve{
		"A": {OraclePrice: 0},
	}}
	if _, ok := p.PriceFor("A"); ok {
		t.Fatal("zero price must report ok=false (oracle didn't answer)")
	}
}

func TestHasPricesFor_RequiresAll(t *testing.T) {
	p := &PoolState{Reserves: map[string]*Reserve{
		"A": {OraclePrice: 1.0},
		"B": {OraclePrice: 0},
	}}
	if p.HasPricesFor("A", "B") {
		t.Fatal("HasPricesFor must fail when any asset price is missing")
	}
	if !p.HasPricesFor("A") {
		t.Fatal("HasPricesFor must pass when all asked-for assets have prices")
	}
}

func TestParseConfigOracle_Found(t *testing.T) {
	contractID := "CAZOKR2Y5E2OSWSIBRVZMJ47RUTQPIGVWSAQ2UISGAVC46XKPGDG5PKI"
	addrVal, err := scvAddressForTest(contractID)
	if err != nil {
		t.Fatalf("address build: %v", err)
	}
	root := scvMap(
		pair(scvSym("bstop_rate"), scvU32(1000000)),
		pair(scvSym("oracle"), addrVal),
		pair(scvSym("status"), scvU32(0)),
	)
	got := parseConfigOracle(root)
	if got != contractID {
		t.Fatalf("oracle: got %q want %q", got, contractID)
	}
}

func TestParseConfigOracle_NotFound(t *testing.T) {
	root := scvMap(
		pair(scvSym("bstop_rate"), scvU32(1000000)),
	)
	if got := parseConfigOracle(root); got != "" {
		t.Fatalf("missing oracle key should return empty, got %q", got)
	}
}
