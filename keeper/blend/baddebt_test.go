package blend

import (
	"math"
	"math/big"
	"strings"
	"testing"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

const (
	testBackstop = "CCT4FMLHPJVYBC6SCAOFIRSYU74ZBU36ADREQUQEFCN5C5MRG26S6PTH"
	testLPToken  = "CAMYNQY4L6BM45HJ6KJMNAX7SK474QHAFJWQBBAKCGV7XP7VJGZ7RUFD"
)

// ── BackstopLPValueUSD ────────────────────────────────────────────────────────

func TestBackstopLPValueUSD_SpotAndHaircut(t *testing.T) {
	lp := big.NewInt(200_000_000) // 20 LP tokens (7-dec)
	if got := BackstopLPValueUSD(lp, 1.25, 0); math.Abs(got-25.0) > 1e-9 {
		t.Errorf("no haircut: want $25, got %f", got)
	}
	if got := BackstopLPValueUSD(lp, 1.25, 5000); math.Abs(got-12.5) > 1e-9 {
		t.Errorf("50%% haircut: want $12.50, got %f", got)
	}
	if got := BackstopLPValueUSD(lp, 1.25, 10000); got != 0 {
		t.Errorf("100%% haircut must value at zero, got %f", got)
	}
	if got := BackstopLPValueUSD(lp, 1.25, -100); math.Abs(got-25.0) > 1e-9 {
		t.Errorf("negative haircut clamps to 0: want $25, got %f", got)
	}
}

func TestBackstopLPValueUSD_DegenerateInputs(t *testing.T) {
	if got := BackstopLPValueUSD(nil, 1.25, 0); got != 0 {
		t.Errorf("nil amount: want 0, got %f", got)
	}
	if got := BackstopLPValueUSD(big.NewInt(-5), 1.25, 0); got != 0 {
		t.Errorf("negative amount: want 0, got %f", got)
	}
	if got := BackstopLPValueUSD(big.NewInt(100), 0, 0); got != 0 {
		t.Errorf("zero spot price: want 0, got %f", got)
	}
}

// ── BadDebtProfitability ──────────────────────────────────────────────────────

// badDebtAuction: lot 20 LP (spot $1.25 → $25 at face), bid 20 USDC dTokens
// (DRate 1.0, $1 → $20 at face). Fair-point ratio at no haircut = 1.25.
func badDebtAuction() Auction {
	return Auction{
		User:       testBackstop,
		Type:       AuctionBadDebt,
		StartBlock: 1000,
		Lot:        map[string]*big.Int{testLPToken: big.NewInt(200_000_000)},
		Bid:        map[string]*big.Int{"USDC": big.NewInt(200_000_000)},
	}
}

func TestBadDebtProfitability_Curve(t *testing.T) {
	pool := makePool(1.0)
	a := badDebtAuction()
	cases := []struct {
		block int64
		want  float64
	}{
		{1100, 0.625}, // t=100: lot 50%, bid 100% → 12.5/20
		{1200, 1.25},  // t=200 fair point: 25/20
		{1300, 2.5},   // t=300: lot 100%, bid 50% → 25/10
	}
	for _, c := range cases {
		if got := BadDebtProfitability(a, pool, testLPToken, 1.25, 0, c.block); math.Abs(got-c.want) > 1e-9 {
			t.Errorf("block %d: want %f, got %f", c.block, c.want, got)
		}
	}
	// Past t=400 the bid is empty: free capture reads as infinite.
	if got := BadDebtProfitability(a, pool, testLPToken, 1.25, 0, 1450); !math.IsInf(got, 1) {
		t.Errorf("t=450: want +Inf, got %f", got)
	}
}

func TestBadDebtProfitability_HaircutScalesLot(t *testing.T) {
	pool := makePool(1.0)
	a := badDebtAuction()
	// At the fair point a 50% haircut turns 1.25 into 0.625 — under any sane
	// MinProfit, so the keeper waits deeper into phase 2.
	if got := BadDebtProfitability(a, pool, testLPToken, 1.25, 5000, 1200); math.Abs(got-0.625) > 1e-9 {
		t.Errorf("want 0.625, got %f", got)
	}
}

func TestBadDebtProfitability_UnpricedBidNeverGreenlit(t *testing.T) {
	a := badDebtAuction()
	pool := makePool(1.0)
	pool.Reserves["USDC"].OraclePrice = 0
	if got := BadDebtProfitability(a, pool, testLPToken, 1.25, 0, 1300); got != 0 {
		t.Errorf("unpriced bid asset: want 0 (cost unknowable), got %f", got)
	}
	pool2 := makePool(1.0)
	pool2.Reserves["USDC"].DRate = 0
	if got := BadDebtProfitability(a, pool2, testLPToken, 1.25, 0, 1300); got != 0 {
		t.Errorf("zero d_rate: want 0, got %f", got)
	}
	// Bid asset missing from reserves entirely.
	a2 := badDebtAuction()
	a2.Bid = map[string]*big.Int{"NOT_A_RESERVE": big.NewInt(1_0000000)}
	if got := BadDebtProfitability(a2, makePool(1.0), testLPToken, 1.25, 0, 1300); got != 0 {
		t.Errorf("unknown bid asset: want 0, got %f", got)
	}
}

func TestBadDebtProfitability_MissingLotEntry(t *testing.T) {
	a := badDebtAuction()
	a.Lot = map[string]*big.Int{} // no LP entry → nothing of value
	if got := BadDebtProfitability(a, makePool(1.0), testLPToken, 1.25, 0, 1300); got != 0 {
		t.Errorf("missing LP lot: want 0, got %f", got)
	}
	// Wrong token key (lot present but not the backstop token we price).
	a2 := badDebtAuction()
	a2.Lot = map[string]*big.Int{"OTHER": big.NewInt(200_000_000)}
	if got := BadDebtProfitability(a2, makePool(1.0), testLPToken, 1.25, 0, 1300); got != 0 {
		t.Errorf("wrong lot token: want 0, got %f", got)
	}
}

// ── Fill guards ───────────────────────────────────────────────────────────────

func TestFillBadDebtAndRepay_Guards(t *testing.T) {
	leg := []RepayLeg{{Asset: "USDC", Amount: 100}}
	if _, err := FillBadDebtAndRepay(nil, "", nil, "p", "pool", testBackstop, 0, leg); err == nil ||
		!strings.Contains(err.Error(), "out of range") {
		t.Errorf("pct 0 must be rejected, got %v", err)
	}
	if _, err := FillBadDebtAndRepay(nil, "", nil, "p", "pool", testBackstop, 101, leg); err == nil ||
		!strings.Contains(err.Error(), "out of range") {
		t.Errorf("pct 101 must be rejected, got %v", err)
	}
	if _, err := FillBadDebtAndRepay(nil, "", nil, "p", "pool", testBackstop, 100, nil); err == nil ||
		!strings.Contains(err.Error(), "repay leg required") {
		t.Errorf("empty repays must be rejected, got %v", err)
	}
	if _, err := FillBadDebtAndRepay(nil, "", nil, "p", "pool", testBackstop, 100,
		[]RepayLeg{{Asset: "", Amount: 100}}); err == nil {
		t.Error("empty repay asset must be rejected")
	}
	if _, err := FillBadDebtAndRepay(nil, "", nil, "p", "pool", testBackstop, 100,
		[]RepayLeg{{Asset: "USDC", Amount: 0}}); err == nil {
		t.Error("zero repay amount must be rejected")
	}
}

func TestCreateBadDebtAuction_EmptyBidRejected(t *testing.T) {
	if err := CreateBadDebtAuction(nil, "", nil, "p", "pool", testBackstop, testLPToken, nil); err == nil {
		t.Error("empty bid asset list must be rejected before any RPC use")
	}
}

// TestBadDebtRequestABITypes locks the on-chain scalar types of a bad-debt
// fill request: request_type MUST be u32(7), amount i128 (the fill percent),
// address the backstop — same ABI-lock discipline as
// TestSubmitPayload_BlendABITypes for the user-liquidation path.
func TestBadDebtRequestABITypes(t *testing.T) {
	val, err := requestsVal([]PoolRequest{{Type: ReqFillBadDebt, Address: testBackstop, Amount: 37}})
	if err != nil {
		t.Fatal(err)
	}
	if val.Type != xdr.ScValTypeScvVec || len(**val.Vec) != 1 {
		t.Fatalf("want 1-entry vec, got %v", val.Type)
	}
	entry := (**val.Vec)[0]
	if entry.Type != xdr.ScValTypeScvMap {
		t.Fatalf("request must be a map, got %v", entry.Type)
	}
	for _, e := range **entry.Map {
		switch scSymbol(e.Key) {
		case "request_type":
			if e.Val.Type != xdr.ScValTypeScvU32 || uint32(*e.Val.U32) != 7 {
				t.Errorf("request_type must be u32(7), got %v", e.Val)
			}
		case "amount":
			if e.Val.Type != xdr.ScValTypeScvI128 {
				t.Errorf("amount must be i128, got %v", e.Val.Type)
			}
		case "address":
			if e.Val.Type != xdr.ScValTypeScvAddress {
				t.Errorf("address must be Address, got %v", e.Val.Type)
			}
		}
	}
}

// ── Backstop parsers ──────────────────────────────────────────────────────────

func scvI128Val(n int64) xdr.ScVal { return soroban.ScvI128(n) }

func TestParseBackstopPoolData(t *testing.T) {
	m := xdr.ScMap{
		{Key: soroban.ScvSymbol("blnd"), Val: scvI128Val(5_000_100_000_000)},
		{Key: soroban.ScvSymbol("token_spot_price"), Val: scvI128Val(12_500_000)},
		{Key: soroban.ScvSymbol("tokens"), Val: scvI128Val(500_010_000_000)},
	}
	mp := &m
	pd, err := parseBackstopPoolData(xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &mp})
	if err != nil {
		t.Fatal(err)
	}
	if pd.SpotPrice != 1.25 {
		t.Errorf("spot price: want 1.25, got %f", pd.SpotPrice)
	}
	if pd.Tokens.Int64() != 500_010_000_000 {
		t.Errorf("tokens: want 500010000000, got %v", pd.Tokens)
	}
}

func TestParseBackstopPoolData_MissingFields(t *testing.T) {
	m := xdr.ScMap{{Key: soroban.ScvSymbol("blnd"), Val: scvI128Val(1)}}
	mp := &m
	if _, err := parseBackstopPoolData(xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &mp}); err == nil {
		t.Error("missing tokens/spot price must error, not default")
	}
	if _, err := parseBackstopPoolData(xdr.ScVal{Type: xdr.ScValTypeScvVoid}); err == nil {
		t.Error("non-map must error")
	}
	// Zero spot price is unusable for valuation.
	m2 := xdr.ScMap{
		{Key: soroban.ScvSymbol("token_spot_price"), Val: scvI128Val(0)},
		{Key: soroban.ScvSymbol("tokens"), Val: scvI128Val(100)},
	}
	mp2 := &m2
	if _, err := parseBackstopPoolData(xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &mp2}); err == nil {
		t.Error("zero spot price must error")
	}
}

func TestBackstopFromInstance(t *testing.T) {
	addrVal, err := soroban.ScvAddress(testBackstop)
	if err != nil {
		t.Fatal(err)
	}
	storage := xdr.ScMap{
		{Key: soroban.ScvSymbol("Admin"), Val: soroban.ScvU32(1)}, // unrelated entry
		{Key: soroban.ScvSymbol("Backstop"), Val: addrVal},
	}
	inst := xdr.ScContractInstance{
		Executable: xdr.ContractExecutable{Type: xdr.ContractExecutableTypeContractExecutableWasm, WasmHash: &xdr.Hash{}},
		Storage:    &storage,
	}
	poolVal, err := soroban.ScvAddress("CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V")
	if err != nil {
		t.Fatal(err)
	}
	data := xdr.LedgerEntryData{
		Type: xdr.LedgerEntryTypeContractData,
		ContractData: &xdr.ContractDataEntry{
			Contract:   *poolVal.Address,
			Key:        xdr.ScVal{Type: xdr.ScValTypeScvLedgerKeyContractInstance},
			Durability: xdr.ContractDataDurabilityPersistent,
			Val:        xdr.ScVal{Type: xdr.ScValTypeScvContractInstance, Instance: &inst},
		},
	}
	got, err := backstopFromInstance(data)
	if err != nil {
		t.Fatal(err)
	}
	if got != testBackstop {
		t.Errorf("want %s, got %s", testBackstop, got)
	}
}

func TestBackstopFromInstance_NoBackstopKey(t *testing.T) {
	storage := xdr.ScMap{{Key: soroban.ScvSymbol("Admin"), Val: soroban.ScvU32(1)}}
	inst := xdr.ScContractInstance{
		Executable: xdr.ContractExecutable{Type: xdr.ContractExecutableTypeContractExecutableWasm, WasmHash: &xdr.Hash{}},
		Storage:    &storage,
	}
	poolVal, _ := soroban.ScvAddress("CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V")
	data := xdr.LedgerEntryData{
		Type: xdr.LedgerEntryTypeContractData,
		ContractData: &xdr.ContractDataEntry{
			Contract:   *poolVal.Address,
			Key:        xdr.ScVal{Type: xdr.ScValTypeScvLedgerKeyContractInstance},
			Durability: xdr.ContractDataDurabilityPersistent,
			Val:        xdr.ScVal{Type: xdr.ScValTypeScvContractInstance, Instance: &inst},
		},
	}
	if _, err := backstopFromInstance(data); err == nil {
		t.Error("missing Backstop key must error, never guess")
	}
}
