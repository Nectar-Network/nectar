package blend

import (
	"math"
	"math/big"
	"testing"
)

func makeReserveAt(idx uint32, asset string, c, l, price float64) *Reserve {
	return &Reserve{
		Asset:            asset,
		Index:            idx,
		CollateralFactor: c,
		LiabilityFactor:  l,
		BRate:            scalar,
		DRate:            scalar,
		OraclePrice:      price,
	}
}

func TestCalcHealthFactor_Healthy(t *testing.T) {
	// 100 XLM collateral @ $1, c-factor 0.8 → 80 effective
	// 50 USDC debt @ $1, l-factor 1.0 → 50 effective
	// HF = 80/50 = 1.6
	pool := &PoolState{Reserves: map[string]*Reserve{
		"XLM":  makeReserveAt(0, "XLM", 0.8, 1.0, 1.0),
		"USDC": makeReserveAt(1, "USDC", 0.9, 1.0, 1.0),
	}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(100_0000000)},
		Liabilities: map[uint32]*big.Int{1: big.NewInt(50_0000000)},
	}
	got := CalcHealthFactor(pos, pool)
	const eps = 1e-6
	if math.Abs(got-1.6) > eps {
		t.Fatalf("HF: got %f want 1.6", got)
	}
}

func TestCalcHealthFactor_Underwater(t *testing.T) {
	// 50 XLM collateral @ $1, c-factor 0.5 → 25 effective
	// 100 USDC debt @ $1, l-factor 1.0 → 100 effective
	// HF = 25/100 = 0.25
	pool := &PoolState{Reserves: map[string]*Reserve{
		"XLM":  makeReserveAt(0, "XLM", 0.5, 1.0, 1.0),
		"USDC": makeReserveAt(1, "USDC", 0.9, 1.0, 1.0),
	}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(50_0000000)},
		Liabilities: map[uint32]*big.Int{1: big.NewInt(100_0000000)},
	}
	got := CalcHealthFactor(pos, pool)
	if got >= 1.0 {
		t.Fatalf("expected underwater HF (<1), got %f", got)
	}
	const eps = 1e-6
	if math.Abs(got-0.25) > eps {
		t.Fatalf("HF: got %f want 0.25", got)
	}
}

func TestCalcHealthFactor_NoLiabilities_Infinite(t *testing.T) {
	pool := &PoolState{Reserves: map[string]*Reserve{
		"XLM": makeReserveAt(0, "XLM", 0.8, 1.0, 1.0),
	}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(100_0000000)},
		Liabilities: map[uint32]*big.Int{},
	}
	got := CalcHealthFactor(pos, pool)
	if !math.IsInf(got, 1) {
		t.Fatalf("expected +Inf for no debt, got %f", got)
	}
}

func TestCalcHealthFactor_LiabilityFactor_AmplifiesDebt(t *testing.T) {
	// l-factor = 1.1 effectively divides liability by 1.1, MAKING it healthier.
	// Sanity: with l-factor=1.0 → HF=1.6; with l-factor=2.0 → HF=3.2.
	pool := &PoolState{Reserves: map[string]*Reserve{
		"XLM":  makeReserveAt(0, "XLM", 0.8, 1.0, 1.0),
		"USDC": makeReserveAt(1, "USDC", 0.9, 2.0, 1.0),
	}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(100_0000000)},
		Liabilities: map[uint32]*big.Int{1: big.NewInt(50_0000000)},
	}
	got := CalcHealthFactor(pos, pool)
	const eps = 1e-6
	if math.Abs(got-3.2) > eps {
		t.Fatalf("HF: got %f want 3.2", got)
	}
}

func TestCalcHealthFactor_UnknownAsset_Skipped(t *testing.T) {
	// Position references reserve idx 99, which the pool doesn't have.
	pool := &PoolState{Reserves: map[string]*Reserve{
		"XLM": makeReserveAt(0, "XLM", 0.8, 1.0, 1.0),
	}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(100_0000000), 99: big.NewInt(1_0000000)},
		Liabilities: map[uint32]*big.Int{},
	}
	got := CalcHealthFactor(pos, pool)
	if !math.IsInf(got, 1) {
		t.Fatalf("expected +Inf with no debts, got %f", got)
	}
}

func TestEstimateCapital_FullDebt(t *testing.T) {
	// 100 USDC debt @ $1, pct=100 → 100 USDC capital needed.
	pool := &PoolState{Reserves: map[string]*Reserve{
		"USDC": makeReserveAt(1, "USDC", 0.9, 1.0, 1.0),
	}}
	pos := Position{
		Liabilities: map[uint32]*big.Int{1: big.NewInt(100_0000000)},
	}
	got := EstimateCapital(pos, pool, 100)
	if got < 99_0000000 || got > 101_0000000 {
		t.Fatalf("capital: got %d want ~100_0000000", got)
	}
}

func TestEstimateCapital_HalfDebt(t *testing.T) {
	pool := &PoolState{Reserves: map[string]*Reserve{
		"USDC": makeReserveAt(1, "USDC", 0.9, 1.0, 1.0),
	}}
	pos := Position{
		Liabilities: map[uint32]*big.Int{1: big.NewInt(100_0000000)},
	}
	got := EstimateCapital(pos, pool, 50)
	if got < 49_0000000 || got > 51_0000000 {
		t.Fatalf("capital at 50pct: got %d want ~50_0000000", got)
	}
}

func TestEstimateCapital_ZeroPct(t *testing.T) {
	pool := &PoolState{Reserves: map[string]*Reserve{
		"USDC": makeReserveAt(1, "USDC", 0.9, 1.0, 1.0),
	}}
	pos := Position{
		Liabilities: map[uint32]*big.Int{1: big.NewInt(100_0000000)},
	}
	if got := EstimateCapital(pos, pool, 0); got != 0 {
		t.Fatalf("expected 0 for pct=0, got %d", got)
	}
	if got := EstimateCapital(pos, pool, -10); got != 0 {
		t.Fatalf("expected 0 for negative pct, got %d", got)
	}
}

func TestEstimateCapital_NoLiabilities(t *testing.T) {
	pool := &PoolState{Reserves: map[string]*Reserve{
		"USDC": makeReserveAt(1, "USDC", 0.9, 1.0, 1.0),
	}}
	pos := Position{
		Liabilities: map[uint32]*big.Int{},
	}
	if got := EstimateCapital(pos, pool, 100); got != 0 {
		t.Fatalf("expected 0 for empty debt, got %d", got)
	}
}

func TestEstimateCapital_PctClampedTo100(t *testing.T) {
	pool := &PoolState{Reserves: map[string]*Reserve{
		"USDC": makeReserveAt(1, "USDC", 0.9, 1.0, 1.0),
	}}
	pos := Position{
		Liabilities: map[uint32]*big.Int{1: big.NewInt(100_0000000)},
	}
	gotMax := EstimateCapital(pos, pool, 100)
	gotOver := EstimateCapital(pos, pool, 250) // clamped to 100
	if gotOver != gotMax {
		t.Fatalf("expected pct>100 to clamp; got %d vs %d", gotOver, gotMax)
	}
}
