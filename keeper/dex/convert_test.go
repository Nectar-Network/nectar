package dex

import (
	"errors"
	"testing"

	"github.com/nectar-network/keeper/soroban"
)

const testPoolUSDC = "CAQCFVLOBK5GIULPNZRGATJJMIZL5BSP7X5YJVMGCPTUEPFM4AVSRCJU"

// QuoteConvertIn accepts a near-par quote: 100 pool-USDC out for 100.5
// vault-USDC in is within a 100 bps par bound.
func TestQuoteConvertIn_NearParAccepted(t *testing.T) {
	srv := mockSimResult(t, vecI128Base64(t, 100_5000000, 100_0000000))
	defer srv.Close()
	s := NewSwapClient(soroban.NewClient(srv.URL), baseCfg())
	got, err := s.QuoteConvertIn(testUSDC, testPoolUSDC, 100_0000000)
	if err != nil {
		t.Fatalf("near-par quote rejected: %v", err)
	}
	if got != 100_5000000 {
		t.Fatalf("required input: got %d want 100_5000000", got)
	}
}

// A quote needing 108 vault-USDC for 100 pool-USDC is 8% off par — must be
// refused with ErrOffParity so no capital is drawn.
func TestQuoteConvertIn_OffParityRejected(t *testing.T) {
	srv := mockSimResult(t, vecI128Base64(t, 108_0000000, 100_0000000))
	defer srv.Close()
	s := NewSwapClient(soroban.NewClient(srv.URL), baseCfg())
	_, err := s.QuoteConvertIn(testUSDC, testPoolUSDC, 100_0000000)
	if !errors.Is(err, ErrOffParity) {
		t.Fatalf("want ErrOffParity, got %v", err)
	}
}

// Exactly at the par bound (100 bps => 101.0) is still accepted.
func TestQuoteConvertIn_ExactBoundAccepted(t *testing.T) {
	srv := mockSimResult(t, vecI128Base64(t, 101_0000000, 100_0000000))
	defer srv.Close()
	s := NewSwapClient(soroban.NewClient(srv.URL), baseCfg())
	if _, err := s.QuoteConvertIn(testUSDC, testPoolUSDC, 100_0000000); err != nil {
		t.Fatalf("bound quote rejected: %v", err)
	}
}

func TestQuoteConvertIn_NoRouterConfigured(t *testing.T) {
	cfg := baseCfg()
	cfg.SoroswapRouter = ""
	s := NewSwapClient(soroban.NewClient("http://invalid.local"), cfg)
	if _, err := s.QuoteConvertIn(testUSDC, testPoolUSDC, 100_0000000); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("want ErrNoRoute, got %v", err)
	}
}

func TestQuoteConvertIn_RPCErrorSurfaces(t *testing.T) {
	srv := mockRPCError(t)
	defer srv.Close()
	s := NewSwapClient(soroban.NewClient(srv.URL), baseCfg())
	if _, err := s.QuoteConvertIn(testUSDC, testPoolUSDC, 100_0000000); err == nil {
		t.Fatal("RPC error must surface, not pass the guard")
	}
}

func TestQuoteConvertIn_RejectsNonPositiveAmount(t *testing.T) {
	s := NewSwapClient(soroban.NewClient("http://invalid.local"), baseCfg())
	if _, err := s.QuoteConvertIn(testUSDC, testPoolUSDC, 0); err == nil {
		t.Fatal("zero amount must error")
	}
}

func TestMaxInForParity(t *testing.T) {
	cases := []struct {
		out      int64
		bps      int
		expected int64
	}{
		{100_0000000, 100, 101_0000000},
		{100_0000000, 0, 100_0000000},
		{10_0000000, 50, 10_0500000},
	}
	for _, c := range cases {
		if got := maxInForParity(c.out, c.bps); got != c.expected {
			t.Errorf("maxInForParity(%d,%d): got %d want %d", c.out, c.bps, got, c.expected)
		}
	}
}

func TestConvertExactOut_RejectsNonPositive(t *testing.T) {
	s := NewSwapClient(soroban.NewClient("http://invalid.local"), baseCfg())
	if _, err := s.ConvertExactOut(mustKP(t), testUSDC, testPoolUSDC, 0, 100); err == nil {
		t.Fatal("zero amountOut must error")
	}
	if _, err := s.ConvertExactOut(mustKP(t), testUSDC, testPoolUSDC, 100, 0); err == nil {
		t.Fatal("zero maxIn must error")
	}
}

func TestConvertExactOut_NoRouterConfigured(t *testing.T) {
	cfg := baseCfg()
	cfg.SoroswapRouter = ""
	s := NewSwapClient(soroban.NewClient("http://invalid.local"), cfg)
	if _, err := s.ConvertExactOut(mustKP(t), testUSDC, testPoolUSDC, 100, 100); !errors.Is(err, ErrNoRoute) {
		t.Fatalf("want ErrNoRoute, got %v", err)
	}
}
