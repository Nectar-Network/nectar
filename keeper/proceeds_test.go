package main

import (
	"math/big"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/nectar-network/keeper/blend"
	"github.com/nectar-network/keeper/soroban"
	"github.com/stellar/go/xdr"
)

// encodeI128XDR returns a base64-encoded ScVal::I128 for an int64 amount.
func encodeI128XDR(t *testing.T, v int64) string {
	t.Helper()
	hi := xdr.Int64(0)
	if v < 0 {
		hi = -1
	}
	val := xdr.ScVal{
		Type: xdr.ScValTypeScvI128,
		I128: &xdr.Int128Parts{Hi: hi, Lo: xdr.Uint64(uint64(v))},
	}
	b, err := xdr.MarshalBase64(val)
	if err != nil {
		t.Fatalf("xdr marshal: %v", err)
	}
	return b
}

// fakeBalanceServer mints simulateTransaction responses that always return the
// given i128 balance. The keeper uses it to test TokenBalance behaviour.
func fakeBalanceServer(t *testing.T, balance int64) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := encodeI128XDR(t, balance)
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"results":[{"xdr":"` + b + `"}],"latestLedger":1}}`))
	}))
}

// TestComputeProceeds_RealDelta locks in the production path: proceeds equal
// the keeper's USDC balance delta from before-draw to after-fill.
func TestComputeProceeds_RealDelta(t *testing.T) {
	srv := fakeBalanceServer(t, 110_0000000) // post-fill balance
	defer srv.Close()
	rpc := soroban.NewClient(srv.URL)

	cfg := Config{
		USDCContract:  "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
		Passphrase:    "Test SDF Network ; September 2015",
		DemoProfitBPS: 0,
	}
	// Before draw the keeper had 0 USDC. The vault gave it 100 USDC (drawAmount),
	// the fill earned another 10, so the post-fill balance is 110.
	proceeds, profit := computeProceeds(rpc, cfg, "GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF",
		/*drawAmount*/ 100_0000000, /*balBefore*/ 0, /*balKnown*/ true)

	if proceeds != 110_0000000 {
		t.Errorf("proceeds: got %d want 110_0000000", proceeds)
	}
	if profit != 10_0000000 {
		t.Errorf("profit: got %d want 10_0000000", profit)
	}
}

// TestComputeProceeds_LossClampedToZero proves the keeper never reports negative
// proceeds — if the fill cost more USDC than was drawn, profit is 0 (the vault
// can't book a loss against capital it didn't supply).
func TestComputeProceeds_LossClampedToZero(t *testing.T) {
	srv := fakeBalanceServer(t, 50_0000000) // post-fill balance < before+draw
	defer srv.Close()
	rpc := soroban.NewClient(srv.URL)

	cfg := Config{
		USDCContract: "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
		Passphrase:   "Test SDF Network ; September 2015",
	}
	// Keeper had 100 USDC before, drew 100, post-fill 50 → real delta = -50.
	proceeds, profit := computeProceeds(rpc, cfg, "GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF",
		/*drawAmount*/ 100_0000000, /*balBefore*/ 100_0000000, /*balKnown*/ true)

	if proceeds != 0 {
		t.Errorf("proceeds on loss: got %d want 0", proceeds)
	}
	if profit != 0 {
		t.Errorf("profit on loss: got %d want 0", profit)
	}
}

// TestComputeProceeds_DemoModeTopsUp confirms that DEMO_PROFIT_BPS only kicks in
// when real proceeds fall short — and only against a drawn position.
func TestComputeProceeds_DemoModeTopsUp(t *testing.T) {
	// Keeper has 0 actual delta (e.g. LiquidationLab doesn't move USDC).
	srv := fakeBalanceServer(t, 100_0000000) // pre+draw, no change
	defer srv.Close()
	rpc := soroban.NewClient(srv.URL)

	cfg := Config{
		USDCContract:  "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
		Passphrase:    "Test SDF Network ; September 2015",
		DemoProfitBPS: 1000, // 10% synthetic profit
	}
	proceeds, profit := computeProceeds(rpc, cfg, "GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF",
		/*drawAmount*/ 100_0000000, /*balBefore*/ 0, /*balKnown*/ true)

	// Real delta = 100 (just got the draw back), demo target = 110 → keeper tops up 10.
	if proceeds != 110_0000000 {
		t.Errorf("proceeds in demo mode: got %d want 110_0000000", proceeds)
	}
	if profit != 10_0000000 {
		t.Errorf("profit in demo mode: got %d want 10_0000000", profit)
	}
}

// TestComputeProceeds_DemoModeRespectsRealProfit ensures demo mode never lowers
// the proceeds when the real delta is already above the demo target.
func TestComputeProceeds_DemoModeRespectsRealProfit(t *testing.T) {
	srv := fakeBalanceServer(t, 130_0000000)
	defer srv.Close()
	rpc := soroban.NewClient(srv.URL)
	cfg := Config{
		USDCContract:  "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
		Passphrase:    "Test SDF Network ; September 2015",
		DemoProfitBPS: 1000,
	}
	proceeds, profit := computeProceeds(rpc, cfg, "GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF",
		100_0000000, 0, true)
	// Real delta = 130; demo target = 110 → keep real.
	if proceeds != 130_0000000 || profit != 30_0000000 {
		t.Errorf("proceeds=%d profit=%d want 130/30", proceeds, profit)
	}
}

// TestComputeProceeds_BalanceUnknown returns zero proceeds when the keeper
// can't read its own USDC balance (USDC_CONTRACT not configured).
func TestComputeProceeds_BalanceUnknown(t *testing.T) {
	cfg := Config{
		// No USDCContract — falls back to "balance unknown".
		Passphrase: "Test SDF Network ; September 2015",
	}
	proceeds, profit := computeProceeds(nil, cfg, "G…", 100, 0, false)
	if proceeds != 0 || profit != 0 {
		t.Errorf("unknown balance must return 0/0, got %d/%d", proceeds, profit)
	}
}

// TestAuctionPricesKnown_AllPresent gates the fill path on every referenced
// asset having a non-zero oracle price.
func TestAuctionPricesKnown_AllPresent(t *testing.T) {
	pool := &blend.PoolState{Reserves: map[string]*blend.Reserve{
		"A": {OraclePrice: 1.0},
		"B": {OraclePrice: 2.0},
	}}
	a := blend.Auction{
		Lot: map[string]*big.Int{"A": big.NewInt(1)},
		Bid: map[string]*big.Int{"B": big.NewInt(1)},
	}
	if !auctionPricesKnown(a, pool) {
		t.Fatal("expected all-present auction to be allowed")
	}
}

func TestAuctionPricesKnown_MissingPrice(t *testing.T) {
	pool := &blend.PoolState{Reserves: map[string]*blend.Reserve{
		"A": {OraclePrice: 1.0},
	}}
	a := blend.Auction{
		Lot: map[string]*big.Int{"A": big.NewInt(1)},
		Bid: map[string]*big.Int{"B": big.NewInt(1)}, // B has no reserve
	}
	if auctionPricesKnown(a, pool) {
		t.Fatal("expected missing-price auction to be skipped")
	}
}
