package dex

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

const (
	testUSDC   = "CD34YC6FFI2KIE2U4ZPCGQIRPH7UPG5YY2QBYNP25ATSFOQSG73J4VBW"
	testToken  = "CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF"
	testRouter = "CCJUD55AG6W5HAI5LRVNKAE5WDP5XGZBUDS5WNTIVDU7O264UZZE7BRD"
	testPhx    = "CBQBQXFQAQ5BQH5RATLBVYE5OTRJFRFPC6WBQNPENI22UODKPL2BZZB5"
	testPass   = "Test SDF Network ; September 2015"
)

func mustKP(t *testing.T) *keypair.Full {
	t.Helper()
	kp, err := keypair.Random()
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	return kp
}

func baseCfg() Config {
	return Config{
		HorizonURL:     "http://invalid.local",
		Passphrase:     testPass,
		UsdcAddr:       testUSDC,
		SoroswapRouter: testRouter,
		SlippageBps:    100,
	}
}

func TestSwapToUSDC_AlreadyUSDC(t *testing.T) {
	c := NewSwapClient(soroban.NewClient("http://invalid.local"), baseCfg())
	res, err := c.SwapToUSDC(mustKP(t), testUSDC, 500, 0)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if res.OutputAmount != 500 || res.Route != "none" {
		t.Fatalf("expected passthrough 500/none, got %d/%s", res.OutputAmount, res.Route)
	}
}

func TestSwapToUSDC_RejectsNonPositiveAmount(t *testing.T) {
	c := NewSwapClient(soroban.NewClient("http://invalid.local"), baseCfg())
	for _, amt := range []int64{0, -1, -100} {
		if _, err := c.SwapToUSDC(mustKP(t), testToken, amt, 0); err == nil {
			t.Errorf("amount %d: expected error", amt)
		}
	}
}

func TestSwapToUSDC_RequiresUSDCConfigured(t *testing.T) {
	cfg := baseCfg()
	cfg.UsdcAddr = ""
	c := NewSwapClient(soroban.NewClient("http://invalid.local"), cfg)
	_, err := c.SwapToUSDC(mustKP(t), testToken, 100, 0)
	if !errors.Is(err, ErrUSDCNotConfigured) {
		t.Fatalf("expected ErrUSDCNotConfigured, got %v", err)
	}
}

func TestSwapToUSDC_NoRouteConfigured(t *testing.T) {
	cfg := baseCfg()
	cfg.SoroswapRouter = ""
	cfg.PhoenixRouter = ""
	c := NewSwapClient(soroban.NewClient("http://invalid.local"), cfg)
	_, err := c.SwapToUSDC(mustKP(t), testToken, 100, 0)
	if !errors.Is(err, ErrNoRoute) {
		t.Fatalf("expected ErrNoRoute, got %v", err)
	}
}

func TestSwapToUSDC_BothVenuesFail(t *testing.T) {
	srv := mockRPCError(t)
	defer srv.Close()

	cfg := baseCfg()
	cfg.PhoenixRouter = testPhx
	c := NewSwapClient(soroban.NewClient(srv.URL), cfg)

	_, err := c.SwapToUSDC(mustKP(t), testToken, 100, 0)
	if !errors.Is(err, ErrNoRoute) {
		t.Fatalf("expected ErrNoRoute, got %v", err)
	}
}

func TestSwapToUSDC_SlippageRejected(t *testing.T) {
	// router_get_amounts_out returns a low quote; with a high oracle reference
	// the swap must be rejected before any execution and must NOT fall back.
	srv := mockSimResult(t, vecI128Base64(t, 100_000_000, 50_000_000)) // out = 5 USDC
	defer srv.Close()

	cfg := baseCfg()
	cfg.PhoenixRouter = "" // ensure no fallback masks the rejection
	c := NewSwapClient(soroban.NewClient(srv.URL), cfg)

	// ref = 100 USDC (1e9 stroops), 1% floor ≈ 99 USDC; quote 5 USDC is far below.
	_, err := c.SwapToUSDC(mustKP(t), testToken, 1_000_000_000, 1_000_000_000)
	if !errors.Is(err, ErrSlippageExceeded) {
		t.Fatalf("expected ErrSlippageExceeded, got %v", err)
	}
}

func TestSoroswapQuote_DecodesLastElement(t *testing.T) {
	srv := mockSimResult(t, vecI128Base64(t, 100_000_000, 99_000_000))
	defer srv.Close()

	c := NewSwapClient(soroban.NewClient(srv.URL), baseCfg())
	out, err := c.soroswapQuote(100_000_000, []string{testToken, testUSDC})
	if err != nil {
		t.Fatalf("quote: %v", err)
	}
	if out != 99_000_000 {
		t.Fatalf("expected 99000000, got %d", out)
	}
}

func TestMinOutForSlippage(t *testing.T) {
	cases := []struct {
		quoted int64
		bps    int
		want   int64
	}{
		{1_000_000_000, 100, 990_000_000}, // 1%
		{1_000_000_000, 0, 1_000_000_000}, // 0%
		{1_000_000_000, 10000, 0},         // 100%
		{1_000_000_000, 12000, 0},         // clamped >100%
		{0, 100, 0},                       // zero quote
	}
	for _, c := range cases {
		if got := minOutForSlippage(c.quoted, c.bps); got != c.want {
			t.Errorf("minOutForSlippage(%d,%d)=%d want %d", c.quoted, c.bps, got, c.want)
		}
	}
}

func TestBelowFloor(t *testing.T) {
	if belowFloor(100, 0, 100) {
		t.Error("zero ref should disable the floor")
	}
	if !belowFloor(50, 100, 100) {
		t.Error("quote 50 vs ~99 floor should be below")
	}
	if belowFloor(100, 100, 100) {
		t.Error("quote at ref should pass the floor")
	}
}

func TestSlippageFraction(t *testing.T) {
	if f := slippageFraction(100, 100); f != 0 {
		t.Errorf("equal -> 0, got %f", f)
	}
	if f := slippageFraction(100, 90); f < 0.099 || f > 0.101 {
		t.Errorf("10%% shortfall expected, got %f", f)
	}
	if f := slippageFraction(0, 90); f != 0 {
		t.Errorf("zero ref -> 0, got %f", f)
	}
}

// --- test helpers ---

// vecI128Base64 builds a base64 ScVal Vec<i128> from the given amounts.
func vecI128Base64(t *testing.T, amounts ...int64) string {
	t.Helper()
	vals := make([]xdr.ScVal, len(amounts))
	for i, a := range amounts {
		vals[i] = soroban.ScvI128(a)
	}
	b64, err := xdr.MarshalBase64(soroban.ScvVec(vals...))
	if err != nil {
		t.Fatalf("marshal vec: %v", err)
	}
	return b64
}

// mockSimResult answers every JSON-RPC call with a single simulate result
// carrying the given base64 ScVal.
func mockSimResult(t *testing.T, xdrB64 string) *httptest.Server {
	t.Helper()
	resp := `{"jsonrpc":"2.0","id":1,"result":{"latestLedger":1,"results":[{"xdr":"` + xdrB64 + `"}]}}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
}

// mockRPCError answers every JSON-RPC call with an error envelope.
func mockRPCError(t *testing.T) *httptest.Server {
	t.Helper()
	resp := `{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"boom"}}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
}
