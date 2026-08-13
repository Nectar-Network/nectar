package blend

import (
	"errors"
	"math"
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nectar-network/keeper/soroban"
)

func makeReserveAt(idx uint32, asset string, c, l, price float64) *Reserve {
	return &Reserve{
		Asset:            asset,
		Index:            idx,
		Decimals:         7,
		CollateralFactor: c,
		LiabilityFactor:  l,
		BRate:            1.0, // plain multiplier (b_rate/1e12 on-chain)
		DRate:            1.0,
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

func TestHealthFactor_UnknownAsset_IsAnError(t *testing.T) {
	// Position references reserve idx 99, which the pool doesn't have. Silently
	// skipping it would invent a health factor from partial data, so the
	// position must be reported as unpriceable instead.
	pool := &PoolState{Reserves: map[string]*Reserve{
		"XLM": makeReserveAt(0, "XLM", 0.8, 1.0, 1.0),
	}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(100_0000000), 99: big.NewInt(1_0000000)},
		Liabilities: map[uint32]*big.Int{},
	}
	if _, err := HealthFactor(pos, pool); !errors.Is(err, ErrUnpricedReserve) {
		t.Fatalf("expected ErrUnpricedReserve, got %v", err)
	}
}

func TestHealthFactor_UnpricedLiabilityIsNotInfinite(t *testing.T) {
	// An unpriced DEBT reserve must not read as "no debt" (+Inf) — that would
	// hide a real liquidation.
	unpriced := makeReserveAt(1, "DEBT", 0.9, 1.0, 0)
	pool := &PoolState{Reserves: map[string]*Reserve{
		"XLM":  makeReserveAt(0, "XLM", 0.8, 1.0, 1.0),
		"DEBT": unpriced,
	}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(100_0000000)},
		Liabilities: map[uint32]*big.Int{1: big.NewInt(50_0000000)},
	}
	if _, err := HealthFactor(pos, pool); !errors.Is(err, ErrUnpricedReserve) {
		t.Fatalf("expected ErrUnpricedReserve for unpriced debt, got %v", err)
	}
}

func TestHealthFactor_SupplyDoesNotBackDebt(t *testing.T) {
	// Non-collateral `supply` must not inflate HF: only `collateral` counts,
	// matching health_factor.rs (which reads positions.collateral alone).
	pool := &PoolState{Reserves: map[string]*Reserve{
		"XLM":  makeReserveAt(0, "XLM", 0.8, 1.0, 1.0),
		"USDC": makeReserveAt(1, "USDC", 0.9, 1.0, 1.0),
	}}
	pos := Position{
		Collateral:  map[uint32]*big.Int{0: big.NewInt(100_0000000)}, // 100 @ 0.8 = 80
		Supply:      map[uint32]*big.Int{0: big.NewInt(900_0000000)}, // must be ignored
		Liabilities: map[uint32]*big.Int{1: big.NewInt(50_0000000)},  // 50
	}
	hf, err := HealthFactor(pos, pool)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if hf < 1.59 || hf > 1.61 {
		t.Fatalf("HF: got %v want ~1.6 (supply must not count as collateral)", hf)
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

// A get_positions read that comes back with nothing must be a FAILED read, not
// a debt-free answer. The index is sticky: it records the probe result and then
// stops asking until a new event arrives — and an idle underwater position
// emits no events. So fabricating "holds nothing" here would retire a live
// borrower permanently, which is exactly what this index exists to prevent.
func TestProbePositions_EmptyOrUnreadableResultIsAnError(t *testing.T) {
	cases := []struct {
		name string
		body string
	}{
		{"result without results", `{"jsonrpc":"2.0","id":1,"result":{"latestLedger":100}}`},
		{"null result", `{"jsonrpc":"2.0","id":1,"result":null}`},
		{"results present but not a map", `{"jsonrpc":"2.0","id":1,"result":{"latestLedger":100,"results":[{"xdr":"AAAAAQ=="}]}}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tc.body))
			}))
			defer srv.Close()

			got := ProbePositions(soroban.NewClient(srv.URL), "Test SDF Network ; September 2015",
				"CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V",
				[]string{"GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF"})
			if len(got) != 1 {
				t.Fatalf("got %d results want 1", len(got))
			}
			if got[0].Err == nil {
				t.Fatalf("an unreadable position must report an error, got Pos=%+v", got[0].Pos)
			}
		})
	}
}
