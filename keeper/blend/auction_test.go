package blend

import (
	"errors"
	"math"
	"testing"
	"time"

	"math/big"

	"github.com/nectar-network/keeper/soroban"
)

// fastRetry is the policy used in retry-classifier tests. Short delays keep
// the test fast; the classifier itself doesn't depend on the timing.
func fastRetry() soroban.RetryConfig {
	return soroban.RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, BackoffFactor: 1.5}
}

func makePool(price float64) *PoolState {
	return &PoolState{
		Reserves: map[string]*Reserve{
			"XLM":  {OraclePrice: price},
			"USDC": {OraclePrice: 1.0},
		},
	}
}

func makeBigInt(n int64) *big.Int { return big.NewInt(n) }

func TestProfitability_Block0_LotZero(t *testing.T) {
	// elapsed=0: lotPct=0, bidPct=1.0 → ratio=0 (lot empty, bid full).
	auction := Auction{
		StartBlock: 1000,
		Lot:        map[string]*big.Int{"XLM": makeBigInt(1_0000000)},
		Bid:        map[string]*big.Int{"USDC": makeBigInt(1_0000000)},
	}
	pool := makePool(1.0)
	got := Profitability(auction, pool, 1000)
	if got != 0 {
		t.Fatalf("expected 0 at block 0, got %f", got)
	}
}

func TestProfitability_Block200_FairPrice(t *testing.T) {
	// elapsed=200: lotPct=1.0, bidPct=1.0 → ratio=1.0 (the fair-price point).
	auction := Auction{
		StartBlock: 1000,
		Lot:        map[string]*big.Int{"XLM": makeBigInt(1_0000000)},
		Bid:        map[string]*big.Int{"USDC": makeBigInt(1_0000000)},
	}
	pool := makePool(1.0)
	got := Profitability(auction, pool, 1200)
	const eps = 1e-6
	if math.Abs(got-1.0) > eps {
		t.Fatalf("expected ~1.0 at fair-price block 200, got %f", got)
	}
}

func TestProfitability_Block100_LotScaling(t *testing.T) {
	// elapsed=100, phase 1: lotPct=0.5, bidPct=1.0 → ratio=0.5.
	auction := Auction{
		StartBlock: 1000,
		Lot:        map[string]*big.Int{"XLM": makeBigInt(1_0000000)},
		Bid:        map[string]*big.Int{"USDC": makeBigInt(1_0000000)},
	}
	pool := makePool(1.0)
	got := Profitability(auction, pool, 1100)
	const eps = 1e-6
	if math.Abs(got-0.5) > eps {
		t.Fatalf("expected 0.5 at block 100, got %f", got)
	}
}

func TestProfitability_Block300_BidScaling(t *testing.T) {
	// elapsed=300, phase 2: lotPct=1.0, bidPct=0.5 → ratio=2.0.
	auction := Auction{
		StartBlock: 1000,
		Lot:        map[string]*big.Int{"XLM": makeBigInt(1_0000000)},
		Bid:        map[string]*big.Int{"USDC": makeBigInt(1_0000000)},
	}
	pool := makePool(1.0)
	got := Profitability(auction, pool, 1300)
	const eps = 1e-6
	if math.Abs(got-2.0) > eps {
		t.Fatalf("expected 2.0 at block 300, got %f", got)
	}
}

func TestProfitability_Block400_BidZero(t *testing.T) {
	// elapsed=400: lotPct=1.0, bidPct=0 → +Inf (free money).
	auction := Auction{
		StartBlock: 1000,
		Lot:        map[string]*big.Int{"XLM": makeBigInt(1_0000000)},
		Bid:        map[string]*big.Int{"USDC": makeBigInt(1_0000000)},
	}
	pool := makePool(1.0)
	got := Profitability(auction, pool, 1400)
	if !math.IsInf(got, 1) {
		t.Fatalf("expected +Inf at block 400, got %f", got)
	}
}

func TestProfitability_PastExpiry_StaysInfinite(t *testing.T) {
	// elapsed > 400: still phase Expired with bidPct=0.
	auction := Auction{
		StartBlock: 0,
		Lot:        map[string]*big.Int{"XLM": makeBigInt(1_0000000)},
		Bid:        map[string]*big.Int{"USDC": makeBigInt(1_0000000)},
	}
	pool := makePool(1.0)
	got := Profitability(auction, pool, 800)
	if !math.IsInf(got, 1) {
		t.Fatalf("expected +Inf past expiry, got %f", got)
	}
}

func TestPhaseAt_Boundaries(t *testing.T) {
	cases := []struct {
		name    string
		elapsed int64
		phase   AuctionPhase
		lot     float64
		bid     float64
	}{
		{"genesis", 0, PhaseLotScaling, 0.0, 1.0},
		{"mid-lot", 100, PhaseLotScaling, 0.5, 1.0},
		{"fair-price", 200, PhaseLotScaling, 1.0, 1.0},
		{"early-bid", 250, PhaseBidScaling, 1.0, 0.75},
		{"late-bid", 350, PhaseBidScaling, 1.0, 0.25},
		{"expiry", 400, PhaseBidScaling, 1.0, 0.0},
		{"post-expiry", 500, PhaseExpired, 1.0, 0.0},
		{"negative", -10, PhaseLotScaling, 0.0, 1.0},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			gotPhase, gotLot, gotBid := PhaseAt(c.elapsed)
			if gotPhase != c.phase {
				t.Errorf("phase: got %v want %v", gotPhase, c.phase)
			}
			const eps = 1e-9
			if math.Abs(gotLot-c.lot) > eps {
				t.Errorf("lot: got %f want %f", gotLot, c.lot)
			}
			if math.Abs(gotBid-c.bid) > eps {
				t.Errorf("bid: got %f want %f", gotBid, c.bid)
			}
		})
	}
}

func TestBidValueUSD_ScalesWithBid(t *testing.T) {
	auction := Auction{
		StartBlock: 0,
		Bid:        map[string]*big.Int{"USDC": makeBigInt(100_0000000)}, // 100 USDC
	}
	pool := makePool(1.0)
	// Phase 1 (any block 0-200): bidPct=1.0 → 100 USD.
	if got := BidValueUSD(auction, pool, 50); math.Abs(got-100.0) > 1e-6 {
		t.Errorf("phase 1 bid value: got %f want 100", got)
	}
	// Phase 2 mid (block 300): bidPct=0.5 → 50 USD.
	if got := BidValueUSD(auction, pool, 300); math.Abs(got-50.0) > 1e-6 {
		t.Errorf("phase 2 bid value: got %f want 50", got)
	}
	// Expired (block 500): bidPct=0 → 0 USD.
	if got := BidValueUSD(auction, pool, 500); got != 0 {
		t.Errorf("expired bid value: got %f want 0", got)
	}
}

func TestAuctionType_RequestType(t *testing.T) {
	cases := []struct {
		kind AuctionType
		want uint64
	}{
		{AuctionUserLiquidation, 6},
		{AuctionBadDebt, 7},
		{AuctionInterest, 8},
	}
	for _, c := range cases {
		if got := c.kind.requestType(); got != c.want {
			t.Errorf("requestType(%v) = %d, want %d", c.kind, got, c.want)
		}
	}
}

func TestAuctionType_String(t *testing.T) {
	cases := []struct {
		kind AuctionType
		want string
	}{
		{AuctionUserLiquidation, "user_liquidation"},
		{AuctionBadDebt, "bad_debt"},
		{AuctionInterest, "interest"},
	}
	for _, c := range cases {
		if got := c.kind.String(); got != c.want {
			t.Errorf("String(%d) = %q, want %q", c.kind, got, c.want)
		}
	}
	// Unknown variant should still produce something useful for logs.
	if got := AuctionType(99).String(); got == "" {
		t.Error("unknown variant should not return empty string")
	}
}

func TestAllAuctionTypes_CoversAllVariants(t *testing.T) {
	if len(AllAuctionTypes) != 3 {
		t.Fatalf("AllAuctionTypes should have 3 entries, got %d", len(AllAuctionTypes))
	}
	seen := map[AuctionType]bool{}
	for _, k := range AllAuctionTypes {
		seen[k] = true
	}
	for _, want := range []AuctionType{AuctionUserLiquidation, AuctionBadDebt, AuctionInterest} {
		if !seen[want] {
			t.Errorf("AllAuctionTypes missing variant %v", want)
		}
	}
}

// TestProfitability_AuctionTypeAgnostic asserts that the same Profitability
// formula applies regardless of auction kind — only the lot/bid amounts and
// oracle prices matter, never the type discriminant.
func TestProfitability_AuctionTypeAgnostic(t *testing.T) {
	pool := makePool(1.0)
	build := func(kind AuctionType) Auction {
		return Auction{
			Type:       kind,
			StartBlock: 1000,
			Lot:        map[string]*big.Int{"XLM": makeBigInt(1_0000000)},
			Bid:        map[string]*big.Int{"USDC": makeBigInt(1_0000000)},
		}
	}
	user := Profitability(build(AuctionUserLiquidation), pool, 1200)
	bad := Profitability(build(AuctionBadDebt), pool, 1200)
	intr := Profitability(build(AuctionInterest), pool, 1200)
	if math.Abs(user-bad) > 1e-9 || math.Abs(user-intr) > 1e-9 {
		t.Errorf("Profitability must be type-agnostic, got user=%f bad=%f int=%f", user, bad, intr)
	}
}

func TestErrAlreadyFilled_Sentinel(t *testing.T) {
	if ErrAlreadyFilled == nil {
		t.Fatal("ErrAlreadyFilled should not be nil")
	}
	if ErrAlreadyFilled.Error() == "" {
		t.Fatal("ErrAlreadyFilled should have a non-empty message")
	}
}

// The retry tests below exercise the policy that wraps fillAuctionRequest and
// CreateAuction. They use soroban.RetryWith so we can simulate transient and
// deterministic failures without spinning a live RPC.

func TestFillAuction_RetriesOnSequenceError(t *testing.T) {
	attempts := 0
	err := soroban.RetryWith(fastRetry(), func() error {
		attempts++
		if attempts < 3 {
			return errors.New("sequence number mismatch")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("expected success after retries, got %v", err)
	}
	if attempts != 3 {
		t.Fatalf("expected 3 attempts (2 retries + success), got %d", attempts)
	}
}

func TestFillAuction_DoesNotRetryAlreadyFilled(t *testing.T) {
	attempts := 0
	err := soroban.RetryWith(fastRetry(), func() error {
		attempts++
		return errors.New("AlreadyFilled")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 {
		t.Fatalf("AlreadyFilled is non-retryable; expected 1 attempt, got %d", attempts)
	}
}

func TestFillAuction_RetriesOnResourceExhaust(t *testing.T) {
	attempts := 0
	err := soroban.RetryWith(fastRetry(), func() error {
		attempts++
		return errors.New("resource_exhaust: budget exceeded")
	})
	if err == nil {
		t.Fatal("expected terminal error after max retries")
	}
	if attempts != 3 {
		t.Fatalf("resource_exhaust is retryable; expected MaxAttempts=3, got %d", attempts)
	}
}

func TestRegister_DoesNotRetryAlreadyRegistered(t *testing.T) {
	attempts := 0
	err := soroban.RetryWith(fastRetry(), func() error {
		attempts++
		return errors.New("already registered")
	})
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if attempts != 1 {
		t.Fatalf("already-registered is non-retryable; expected 1 attempt, got %d", attempts)
	}
}

func TestContains(t *testing.T) {
	cases := []struct {
		s, sub string
		want   bool
	}{
		{"AuctionNotFound", "AuctionNotFound", true},
		{"AuctionNotFound", "NotFound", true},
		{"AuctionExists", "AuctionNotFound", false},
		{"", "x", false},
		{"abc", "", true},
	}
	for _, c := range cases {
		if got := contains(c.s, c.sub); got != c.want {
			t.Fatalf("contains(%q, %q) = %v, want %v", c.s, c.sub, got, c.want)
		}
	}
}
