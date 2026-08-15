package blend

// Failure-injection tests for Execute (Task B4): every step after vault.Draw
// has a defined recovery path, and each test asserts funds are accounted for
// by MEASURING balances the same way production does — the fakes mutate a
// shared balance table exactly as the chain would, and the assertions read
// what the vault actually got back.

import (
	"errors"
	"fmt"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/adapters"
	core "github.com/nectar-network/keeper/blend"
	"github.com/nectar-network/keeper/dex"
	"github.com/nectar-network/keeper/soroban"
)

const (
	tUSDC = "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
	tXLM  = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
	tPool = "CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V"
	tUser = "GCCTPHRTKMZMQEOH26GZGD3VQUYALUZZDWRP37ZC3QBDOXQHWQ2MNOIJ"

	feeXLM     = int64(10_000_000_000) // keeper's own 1000 XLM fee balance
	floatUSDC  = int64(50_000_000)     // keeper's own 5 USDC float
	settleDebt = int64(100_000_000)    // 10 USDC dTokens owed (DRate 1.0)
	settleNeed = int64(101_000_000)    // padded by drawBufferBps (1%)
	seizedXLM  = int64(1_000_000_000)  // 100 XLM seized collateral
)

type fakeVault struct {
	draws   []int64
	returns []int64
	drawErr error
	retErr  error
	bal     *balTable // credits draws to the keeper's USDC like the chain would
}

func (f *fakeVault) Draw(amount int64, asset string) error {
	if f.drawErr != nil {
		return f.drawErr
	}
	f.draws = append(f.draws, amount)
	f.bal.add(tUSDC, amount)
	return nil
}

func (f *fakeVault) ReturnProceeds(amount, responseTimeMs int64) error {
	if f.retErr != nil {
		return f.retErr
	}
	f.returns = append(f.returns, amount)
	f.bal.add(tUSDC, -amount)
	return nil
}

// balTable is the fake chain state: token balances of the keeper.
type balTable struct {
	m    map[string]int64
	errs map[string]error // per-asset read failures
}

func newBalTable() *balTable {
	return &balTable{m: map[string]int64{tUSDC: floatUSDC, tXLM: feeXLM}, errs: map[string]error{}}
}
func (b *balTable) add(asset string, d int64) { b.m[asset] += d }

type fakeDex struct {
	bal     *balTable
	quoteFn func(from, to string, out int64) (int64, error)
	convFn  func(from, to string, out, maxIn int64) (*dex.SwapResult, error)
	swapFn  func(token string, amount, ref int64) (*dex.SwapResult, error)

	swapToCalls  []int64 // amounts passed to SwapToUSDC
	convertCalls []int64 // amountOut passed to ConvertExactOut
}

func (f *fakeDex) QuoteConvertIn(from, to string, amountOut int64) (int64, error) {
	panic("QuoteConvertIn not expected in these tests")
}
func (f *fakeDex) QuoteIn(from, to string, amountOut int64) (int64, error) {
	if f.quoteFn == nil {
		return 0, dex.ErrNoRoute
	}
	return f.quoteFn(from, to, amountOut)
}
func (f *fakeDex) ConvertExactOut(kp *keypair.Full, from, to string, amountOut, maxIn int64) (*dex.SwapResult, error) {
	f.convertCalls = append(f.convertCalls, amountOut)
	if f.convFn == nil {
		return nil, errors.New("no convert configured")
	}
	return f.convFn(from, to, amountOut, maxIn)
}
func (f *fakeDex) SwapToUSDC(kp *keypair.Full, token string, amount, ref int64) (*dex.SwapResult, error) {
	f.swapToCalls = append(f.swapToCalls, amount)
	if f.swapFn == nil {
		return nil, dex.ErrNoRoute
	}
	return f.swapFn(token, amount, ref)
}

// testPool builds the two-reserve pool the sandbox uses: USDC $1, XLM $0.10.
func testPool() *core.PoolState {
	return &core.PoolState{Reserves: map[string]*core.Reserve{
		tUSDC: {Asset: tUSDC, Index: 0, Decimals: 7, BRate: 1.0, DRate: 1.0, OraclePrice: 1.0},
		tXLM:  {Asset: tXLM, Index: 1, Decimals: 7, BRate: 1.0, DRate: 1.0, OraclePrice: 0.1},
	}}
}

// installSeams wires the package seams to the fakes for one test. elapsed=300
// puts the auction in phase 2 (bid 50%), well past MinProfit.
func installSeams(t *testing.T, bal *balTable, auction *core.Auction, fill func(repays []core.RepayLeg, collateral []string) (string, error)) *[]core.RepayLeg {
	t.Helper()
	origGet, origLedger, origBal, origFill := coreGetAuction, latestLedger, tokenBalance, coreFillAndUnwind
	t.Cleanup(func() {
		coreGetAuction, latestLedger, tokenBalance, coreFillAndUnwind = origGet, origLedger, origBal, origFill
	})

	coreGetAuction = func(rpc *soroban.Client, passphrase, poolAddr, user string) (*core.Auction, error) {
		return auction, nil
	}
	latestLedger = func(rpc *soroban.Client) (int64, error) { return auction.StartBlock + 300, nil }
	tokenBalance = func(rpc *soroban.Client, passphrase, tokenAddr, owner string) (int64, error) {
		if err := bal.errs[tokenAddr]; err != nil {
			return 0, err
		}
		return bal.m[tokenAddr], nil
	}
	var gotRepays []core.RepayLeg
	coreFillAndUnwind = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, user string,
		fillPct int64, repays []core.RepayLeg, collateralAssets []string) (string, error) {
		gotRepays = append([]core.RepayLeg{}, repays...)
		if fill == nil {
			return "", errors.New("no fill configured")
		}
		return fill(repays, collateralAssets)
	}
	return &gotRepays
}

func testAdapter(fd *fakeDex) *Adapter {
	a := NewAdapter(Config{
		PoolAddr:    tPool,
		MinProfit:   1.02,
		Passphrase:  "test",
		UsdcAddr:    tUSDC,
		SlippageBps: 100,
	}, nil)
	if fd != nil {
		a.dex = fd
	}
	return a
}

func usdcAuction() *core.Auction {
	return &core.Auction{
		User:       tUser,
		Type:       core.AuctionUserLiquidation,
		StartBlock: 1000,
		Bid:        map[string]*big.Int{tUSDC: big.NewInt(settleDebt)},
		Lot:        map[string]*big.Int{tXLM: big.NewInt(seizedXLM)},
	}
}

func mkTask() adapters.Task {
	return adapters.Task{
		Protocol: "blend",
		Type:     "liquidation",
		Target:   tUser,
		Data:     taskData{pool: testPool(), bidAssets: []string{tUSDC}, lotAssets: []string{tXLM}},
	}
}

func mustKPExec(t *testing.T) *keypair.Full {
	t.Helper()
	kp, err := keypair.Random()
	if err != nil {
		t.Fatal(err)
	}
	return kp
}

// Happy path, settle-asset-only debt: draw = padded bid, one USDC repay leg,
// seized XLM swept via measured delta (fee balance untouched), full proceeds
// returned.
func TestExecute_HappyPath_SettleOnlyDebt(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	// Exit swap: sell 100 XLM for 9.7 USDC.
	fd.swapFn = func(token string, amount, ref int64) (*dex.SwapResult, error) {
		bal.add(token, -amount)
		bal.add(tUSDC, 97_000_000)
		return &dex.SwapResult{OutputAmount: 97_000_000, Route: "soroswap", TxHash: "swaptx"}, nil
	}
	auction := usdcAuction()
	// Atomic fill at 50% bid scale: repay consumes 5 USDC net, seizes 100 XLM.
	repaysGot := installSeams(t, bal, auction, func(repays []core.RepayLeg, collateral []string) (string, error) {
		bal.add(tUSDC, -50_000_000)
		bal.add(tXLM, seizedXLM)
		return "filltx", nil
	})
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success || res.TxHash != "filltx" {
		t.Fatalf("expected success with fill tx, got %+v", res)
	}
	if len(fv.draws) != 1 || fv.draws[0] != settleNeed {
		t.Fatalf("draw: got %v want [%d]", fv.draws, settleNeed)
	}
	if len(*repaysGot) != 1 || (*repaysGot)[0] != (core.RepayLeg{Asset: tUSDC, Amount: settleNeed}) {
		t.Fatalf("repays: got %v", *repaysGot)
	}
	// proceeds = drawn − 5 USDC repaid + 9.7 USDC swap output
	wantProceeds := settleNeed - 50_000_000 + 97_000_000
	if res.Proceeds != wantProceeds {
		t.Fatalf("proceeds: got %d want %d", res.Proceeds, wantProceeds)
	}
	if len(fv.returns) != 1 || fv.returns[0] != wantProceeds {
		t.Fatalf("returned: got %v want [%d]", fv.returns, wantProceeds)
	}
	if res.Profit != wantProceeds-settleNeed {
		t.Fatalf("profit: got %d want %d", res.Profit, wantProceeds-settleNeed)
	}
	// The keeper's own float and fee balance are untouched.
	if bal.m[tUSDC] != floatUSDC {
		t.Fatalf("keeper USDC float: got %d want %d", bal.m[tUSDC], floatUSDC)
	}
	if bal.m[tXLM] != feeXLM {
		t.Fatalf("keeper XLM fee balance: got %d want %d", bal.m[tXLM], feeXLM)
	}
}

// Multi-asset debt: the XLM leg is quoted, oracle-capped, acquired exact-out,
// repaid, and the refund + seized collateral swept back.
func TestExecute_MultiAssetDebt_AcquiredAndRepaid(t *testing.T) {
	const xlmDebt = int64(400_000_000)  // 40 XLM dTokens owed
	const xlmNeed = int64(404_000_000)  // padded 1%
	const quoteIn = int64(40_500_000)   // 4.05 USDC to buy 40.4 XLM
	const oracleCap = int64(40_804_000) // 4.04 USDC oracle value + 1%

	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	fd.quoteFn = func(from, to string, out int64) (int64, error) {
		if from != tUSDC || to != tXLM || out != xlmNeed {
			return 0, fmt.Errorf("unexpected quote %s→%s %d", from, to, out)
		}
		return quoteIn, nil
	}
	fd.convFn = func(from, to string, out, maxIn int64) (*dex.SwapResult, error) {
		if maxIn != oracleCap {
			return nil, fmt.Errorf("maxIn: got %d want %d", maxIn, oracleCap)
		}
		bal.add(tUSDC, -quoteIn)
		bal.add(tXLM, out)
		return &dex.SwapResult{OutputAmount: out, Route: "soroswap"}, nil
	}
	fd.swapFn = func(token string, amount, ref int64) (*dex.SwapResult, error) {
		out := amount / 10 * 97 / 100 // sell XLM at ~$0.097
		bal.add(token, -amount)
		bal.add(tUSDC, out)
		return &dex.SwapResult{OutputAmount: out, Route: "soroswap"}, nil
	}

	auction := usdcAuction()
	auction.Bid[tXLM] = big.NewInt(xlmDebt)
	repaysGot := installSeams(t, bal, auction, func(repays []core.RepayLeg, collateral []string) (string, error) {
		// Fill at 50% bid scale: consume 5 USDC + 20 XLM of the repays
		// (refunding the rest), seize 100 XLM of collateral.
		bal.add(tUSDC, -50_000_000)
		bal.add(tXLM, -200_000_000+seizedXLM)
		return "filltx", nil
	})
	fv := &fakeVault{bal: bal}
	task := mkTask()
	td := task.Data.(taskData)
	td.bidAssets = []string{tUSDC, tXLM}
	task.Data = td

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), task, fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	wantDraw := settleNeed + oracleCap
	if len(fv.draws) != 1 || fv.draws[0] != wantDraw {
		t.Fatalf("draw: got %v want [%d]", fv.draws, wantDraw)
	}
	if len(*repaysGot) != 2 {
		t.Fatalf("repays: got %v", *repaysGot)
	}
	if (*repaysGot)[0] != (core.RepayLeg{Asset: tUSDC, Amount: settleNeed}) {
		t.Fatalf("settle repay leg: %+v", (*repaysGot)[0])
	}
	if (*repaysGot)[1] != (core.RepayLeg{Asset: tXLM, Amount: xlmNeed}) {
		t.Fatalf("acquired repay leg: %+v", (*repaysGot)[1])
	}
	// Every XLM above the fee balance was swept back to USDC.
	if bal.m[tXLM] != feeXLM {
		t.Fatalf("keeper XLM after sweep: got %d want %d", bal.m[tXLM], feeXLM)
	}
	// Funds accounted: proceeds = draw − 4.05 (buy) − 5 (usdc repay) + sweep
	// output of (404 − 200 + 1000 = 1204 XLM → 11.6788 USDC).
	wantProceeds := wantDraw - quoteIn - 50_000_000 + (1_204_000_000 / 10 * 97 / 100)
	if res.Proceeds != wantProceeds {
		t.Fatalf("proceeds: got %d want %d", res.Proceeds, wantProceeds)
	}
	if len(fv.returns) != 1 || fv.returns[0] != wantProceeds {
		t.Fatalf("returned: got %v want [%d]", fv.returns, wantProceeds)
	}
}

// A missing route for a debt leg must skip the task BEFORE any draw.
func TestExecute_NoRouteForDebtLeg_SkipsBeforeDraw(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal} // quoteFn nil → ErrNoRoute
	auction := usdcAuction()
	auction.Bid[tXLM] = big.NewInt(400_000_000)
	installSeams(t, bal, auction, nil)
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fv.draws) != 0 {
		t.Fatalf("no capital may move without a route, drew %v", fv.draws)
	}
	if !strings.Contains(res.Note, "no route") {
		t.Fatalf("note should name the missing route, got %q", res.Note)
	}
}

// A quote above the oracle-anchored cap must skip before the draw too.
func TestExecute_QuoteAboveOracleCap_SkipsBeforeDraw(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	fd.quoteFn = func(from, to string, out int64) (int64, error) { return 60_000_000, nil } // 6 USDC for $4.04 of XLM
	auction := usdcAuction()
	auction.Bid[tXLM] = big.NewInt(400_000_000)
	installSeams(t, bal, auction, nil)
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fv.draws) != 0 {
		t.Fatalf("no capital may move on an off-oracle route, drew %v", fv.draws)
	}
	if !strings.Contains(res.Note, "oracle cap") {
		t.Fatalf("note should name the cap breach, got %q", res.Note)
	}
}

// Non-settle debt with no DEX configured must skip before the draw.
func TestExecute_NonSettleDebtWithoutDex_Skips(t *testing.T) {
	bal := newBalTable()
	auction := usdcAuction()
	auction.Bid[tXLM] = big.NewInt(400_000_000)
	installSeams(t, bal, auction, nil)
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(nil).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fv.draws) != 0 {
		t.Fatalf("no capital may move without a DEX, drew %v", fv.draws)
	}
	if !strings.Contains(res.Note, "no DEX") {
		t.Fatalf("note should name the missing DEX, got %q", res.Note)
	}
}

// Acquisition failure AFTER the draw: the full draw is returned immediately.
func TestExecute_AcquisitionFails_ReturnsFullDraw(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	fd.quoteFn = func(from, to string, out int64) (int64, error) { return 40_500_000, nil }
	fd.convFn = func(from, to string, out, maxIn int64) (*dex.SwapResult, error) {
		return nil, errors.New("router: pair drained")
	}
	auction := usdcAuction()
	auction.Bid[tXLM] = big.NewInt(400_000_000)
	installSeams(t, bal, auction, nil)
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatal("must not report success")
	}
	if len(fv.draws) != 1 {
		t.Fatalf("expected one draw, got %v", fv.draws)
	}
	if len(fv.returns) != 1 || fv.returns[0] != fv.draws[0] {
		t.Fatalf("full draw must come back: drew %v returned %v", fv.draws, fv.returns)
	}
	if !strings.Contains(res.Note, "rolled back") {
		t.Fatalf("note should describe the rollback, got %q", res.Note)
	}
	if bal.m[tUSDC] != floatUSDC {
		t.Fatalf("keeper float disturbed: %d != %d", bal.m[tUSDC], floatUSDC)
	}
}

// Deterministic fill failure: the fill did not happen, the draw is returned
// in full (B4: "Fill failed → return drawn USDC immediately").
func TestExecute_FillDeterministicFailure_ReturnsFullDraw(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	auction := usdcAuction()
	installSeams(t, bal, auction, func(repays []core.RepayLeg, collateral []string) (string, error) {
		return "", errors.New("submit: HostError: Error(Contract, #1200)")
	})
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatal("must not report success")
	}
	if len(fv.returns) != 1 || fv.returns[0] != settleNeed {
		t.Fatalf("full draw must come back: returned %v want [%d]", fv.returns, settleNeed)
	}
	if !strings.Contains(res.Note, "fill failed") {
		t.Fatalf("note should name the failure, got %q", res.Note)
	}
}

// setAwaitTx overrides the ambiguity re-poll seam for one test.
func setAwaitTx(t *testing.T, fn func(hash string) error) {
	t.Helper()
	orig := awaitTx
	t.Cleanup(func() { awaitTx = orig })
	awaitTx = func(rpc *soroban.Client, hash string, timeout time.Duration) error { return fn(hash) }
}

// Ambiguous fill that stays unknown after the timebound re-poll: NOTHING
// moves this cycle — no sweep, no return. Next cycle's recovery reconciles.
func TestExecute_FillAmbiguous_HoldsEverything(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	auction := usdcAuction()
	amb := &soroban.TxStatusUnknownError{Hash: "deadbeef", Err: errors.New("timed out")}
	installSeams(t, bal, auction, func(repays []core.RepayLeg, collateral []string) (string, error) {
		return "", fmt.Errorf("submit: %w", amb)
	})
	setAwaitTx(t, func(hash string) error {
		return &soroban.TxStatusUnknownError{Hash: hash, Err: errors.New("still pending")}
	})
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fv.returns) != 0 {
		t.Fatalf("ambiguous outcome must not return funds, returned %v", fv.returns)
	}
	if len(fd.swapToCalls) != 0 {
		t.Fatalf("ambiguous outcome must not swap, swapped %v", fd.swapToCalls)
	}
	if !strings.Contains(res.Note, "UNKNOWN") {
		t.Fatalf("note should flag the unknown outcome, got %q", res.Note)
	}
}

// An ambiguous fill whose re-poll finds the tx LANDED is a success: the
// chain effects happened, so the sweep and the measured return proceed.
func TestExecute_FillAmbiguous_ResolvesLanded(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	fd.swapFn = func(token string, amount, ref int64) (*dex.SwapResult, error) {
		bal.add(token, -amount)
		bal.add(tUSDC, 97_000_000)
		return &dex.SwapResult{OutputAmount: 97_000_000, Route: "soroswap"}, nil
	}
	auction := usdcAuction()
	amb := &soroban.TxStatusUnknownError{Hash: "cafebabe", Err: errors.New("poll timeout")}
	installSeams(t, bal, auction, func(repays []core.RepayLeg, collateral []string) (string, error) {
		// The tx DID land on-chain despite the ambiguous report.
		bal.add(tUSDC, -50_000_000)
		bal.add(tXLM, seizedXLM)
		return "", fmt.Errorf("submit: %w", amb)
	})
	setAwaitTx(t, func(hash string) error { return nil }) // resolved: SUCCESS
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success || res.TxHash != "cafebabe" {
		t.Fatalf("resolved-landed must be a success with the polled hash, got %+v", res)
	}
	wantProceeds := settleNeed - 50_000_000 + 97_000_000
	if len(fv.returns) != 1 || fv.returns[0] != wantProceeds {
		t.Fatalf("proceeds after resolution: returned %v want [%d]", fv.returns, wantProceeds)
	}
}

// An ambiguous fill whose re-poll finds the tx definitively FAILED rolls
// back like any deterministic failure: the full draw comes home.
func TestExecute_FillAmbiguous_ResolvesFailed(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	auction := usdcAuction()
	amb := &soroban.TxStatusUnknownError{Hash: "feedface", Err: errors.New("poll timeout")}
	installSeams(t, bal, auction, func(repays []core.RepayLeg, collateral []string) (string, error) {
		return "", fmt.Errorf("submit: %w", amb)
	})
	setAwaitTx(t, func(hash string) error { return errors.New("tx feedface failed: AAAA") })
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatal("resolved-failed must not be a success")
	}
	if len(fv.returns) != 1 || fv.returns[0] != settleNeed {
		t.Fatalf("full draw must come back: %v want [%d]", fv.returns, settleNeed)
	}
}

// An ambiguous acquisition that stays unknown holds everything — no
// rollback (a late-landing swap would spend returned USDC from the float).
func TestExecute_AcquisitionAmbiguous_Holds(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	fd.quoteFn = func(from, to string, out int64) (int64, error) { return 40_500_000, nil }
	amb := &soroban.TxStatusUnknownError{Hash: "0ddball", Err: errors.New("poll timeout")}
	fd.convFn = func(from, to string, out, maxIn int64) (*dex.SwapResult, error) {
		return nil, fmt.Errorf("swap_tokens_for_exact_tokens: %w", amb)
	}
	auction := usdcAuction()
	auction.Bid[tXLM] = big.NewInt(400_000_000)
	installSeams(t, bal, auction, nil)
	setAwaitTx(t, func(hash string) error {
		return &soroban.TxStatusUnknownError{Hash: hash, Err: errors.New("still pending")}
	})
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fv.returns) != 0 {
		t.Fatalf("ambiguous acquisition must not return funds: %v", fv.returns)
	}
	if len(fd.swapToCalls) != 0 {
		t.Fatalf("ambiguous acquisition must not re-sell: %v", fd.swapToCalls)
	}
	if !strings.Contains(res.Note, "UNKNOWN") {
		t.Fatalf("note: %q", res.Note)
	}
}

// An ambiguous acquisition whose re-poll finds the swap LANDED continues the
// fill with the MEASURED balance delta as the repay amount.
func TestExecute_AcquisitionAmbiguous_ResolvesLanded(t *testing.T) {
	const xlmNeed = int64(404_000_000)
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	fd.quoteFn = func(from, to string, out int64) (int64, error) { return 40_500_000, nil }
	amb := &soroban.TxStatusUnknownError{Hash: "5eed", Err: errors.New("poll timeout")}
	fd.convFn = func(from, to string, out, maxIn int64) (*dex.SwapResult, error) {
		// The swap actually landed: tokens arrived, USDC left.
		bal.add(tUSDC, -40_500_000)
		bal.add(tXLM, out)
		return nil, fmt.Errorf("swap_tokens_for_exact_tokens: %w", amb)
	}
	fd.swapFn = func(token string, amount, ref int64) (*dex.SwapResult, error) {
		out := amount / 10 * 97 / 100
		bal.add(token, -amount)
		bal.add(tUSDC, out)
		return &dex.SwapResult{OutputAmount: out, Route: "soroswap"}, nil
	}
	auction := usdcAuction()
	auction.Bid[tXLM] = big.NewInt(400_000_000)
	repaysGot := installSeams(t, bal, auction, func(repays []core.RepayLeg, collateral []string) (string, error) {
		bal.add(tUSDC, -50_000_000)
		bal.add(tXLM, -200_000_000+seizedXLM)
		return "filltx", nil
	})
	setAwaitTx(t, func(hash string) error { return nil }) // resolved: landed
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("expected success, got %+v", res)
	}
	if len(*repaysGot) != 2 || (*repaysGot)[1] != (core.RepayLeg{Asset: tXLM, Amount: xlmNeed}) {
		t.Fatalf("repay must use the MEASURED delta: %v", *repaysGot)
	}
}

// Auction already gone at fill time: nothing was spent, the measured delta
// (the untouched draw) goes straight back.
func TestExecute_AlreadyFilled_ReturnsDraw(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	auction := usdcAuction()
	installSeams(t, bal, auction, func(repays []core.RepayLeg, collateral []string) (string, error) {
		return "", core.ErrAlreadyFilled
	})
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if res.Success {
		t.Fatal("must not report success")
	}
	if len(fv.returns) != 1 || fv.returns[0] != settleNeed {
		t.Fatalf("untouched draw must come back: returned %v want [%d]", fv.returns, settleNeed)
	}
	if !strings.Contains(res.Note, "no longer fillable") {
		t.Fatalf("note: %q", res.Note)
	}
}

// Stuck after fill (exit swap refused): the USDC that did come back is
// returned, the collateral stays held for the next cycle's recovery, and the
// fill still counts as a success.
func TestExecute_ExitSwapFails_ReturnsPartialHoldsCollateral(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal} // swapFn nil → SwapToUSDC fails ErrNoRoute
	auction := usdcAuction()
	installSeams(t, bal, auction, func(repays []core.RepayLeg, collateral []string) (string, error) {
		bal.add(tUSDC, -50_000_000)
		bal.add(tXLM, seizedXLM)
		return "filltx", nil
	})
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("the fill landed; expected success, got %+v", res)
	}
	wantPartial := settleNeed - 50_000_000
	if len(fv.returns) != 1 || fv.returns[0] != wantPartial {
		t.Fatalf("partial USDC must come back: returned %v want [%d]", fv.returns, wantPartial)
	}
	if bal.m[tXLM] != feeXLM+seizedXLM {
		t.Fatalf("seized collateral must still be held: %d", bal.m[tXLM])
	}
}

// An unreadable baseline balance aborts before the draw: a zero default would
// have turned the keeper's whole pre-held balance into "seized" funds.
func TestExecute_BaselineReadFails_SkipsBeforeDraw(t *testing.T) {
	bal := newBalTable()
	bal.errs[tXLM] = errors.New("rpc: boom")
	fd := &fakeDex{bal: bal}
	auction := usdcAuction()
	installSeams(t, bal, auction, nil)
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if len(fv.draws) != 0 {
		t.Fatalf("no capital may move without baselines, drew %v", fv.draws)
	}
	if !strings.Contains(res.Note, "baseline") {
		t.Fatalf("note: %q", res.Note)
	}
}

// The draw estimator: dToken amounts scale by d_rate (rounded up) before the
// buffer, so accrued interest cannot under-fund the repay.
func TestDebtNeeds_ScalesByDRateAndPads(t *testing.T) {
	pool := testPool()
	pool.Reserves[tUSDC].DRate = 1.0500001
	auction := usdcAuction() // 100_000_000 dTokens
	legs, err := debtNeeds(pool, auction)
	if err != nil {
		t.Fatalf("debtNeeds: %v", err)
	}
	if len(legs) != 1 {
		t.Fatalf("legs: %v", legs)
	}
	// Exact product is 105_000_010; float64 representation may push the ceil
	// up one stroop, which is the conservative (over-fund) direction.
	if legs[0].debt < 105_000_010 || legs[0].debt > 105_000_011 {
		t.Fatalf("debt: got %d want 105000010..105000011 (never under)", legs[0].debt)
	}
	if legs[0].need != padBps(legs[0].debt, drawBufferBps) {
		t.Fatalf("need: got %d want %d", legs[0].need, padBps(legs[0].debt, drawBufferBps))
	}
	if legs[0].need <= legs[0].debt {
		t.Fatal("need must strictly exceed debt")
	}
}

func TestPadBps(t *testing.T) {
	cases := []struct {
		v    int64
		bps  int
		want int64
	}{
		{100_000_000, 100, 101_000_000},
		{1, 100, 2}, // rounds up
		{0, 100, 0}, // nothing to pad
		{100_000_000, 0, 100_000_000},
	}
	for _, c := range cases {
		if got := padBps(c.v, c.bps); got != c.want {
			t.Errorf("padBps(%d,%d)=%d want %d", c.v, c.bps, got, c.want)
		}
	}
}

// Past t=400 the scaled bid is empty and a repay-carrying fill would revert;
// between 400 and 500 the auction cannot be deleted yet either. Execute must
// wait — no draw, no delete attempt.
func TestExecute_PastCurveNotYetStale_Waits(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	auction := usdcAuction()
	installSeams(t, bal, auction, nil)
	// elapsed = 450: past the curve, before stale-delete eligibility.
	latestLedger = func(rpc *soroban.Client) (int64, error) { return auction.StartBlock + 450, nil }
	deleted := false
	origDel := coreDeleteStale
	t.Cleanup(func() { coreDeleteStale = origDel })
	coreDeleteStale = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, user string, kind core.AuctionType) error {
		deleted = true
		return nil
	}
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if deleted {
		t.Fatal("must not attempt deletion before t=500")
	}
	if len(fv.draws) != 0 {
		t.Fatalf("must not draw against an empty scaled bid: %v", fv.draws)
	}
	if !strings.Contains(res.Note, "past the price curve") {
		t.Fatalf("note: %q", res.Note)
	}
}

// At t>=500 the stale auction is deleted and a fresh one created in the same
// cycle (the pool's own documented remedy: delete + re-create).
func TestExecute_StaleAuction_DeletedAndRecreated(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	stale := usdcAuction()
	installSeams(t, bal, stale, nil)
	// get_auction: first call returns the stale auction, after deletion nil.
	calls := 0
	coreGetAuction = func(rpc *soroban.Client, passphrase, poolAddr, user string) (*core.Auction, error) {
		calls++
		if calls == 1 {
			return stale, nil
		}
		return nil, nil // deleted; the re-created auction is also "not readable" to end the test early
	}
	latestLedger = func(rpc *soroban.Client) (int64, error) { return stale.StartBlock + 100_000, nil }
	deleted := false
	origDel, origFind, origCreate := coreDeleteStale, coreFindLiqPct, coreCreateAuction
	t.Cleanup(func() { coreDeleteStale, coreFindLiqPct, coreCreateAuction = origDel, origFind, origCreate })
	coreDeleteStale = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, user string, kind core.AuctionType) error {
		deleted = true
		return nil
	}
	found := false
	coreFindLiqPct = func(rpc *soroban.Client, passphrase, poolAddr, user string, bid, lot []string) (int, error) {
		found = true
		return 100, nil
	}
	created := false
	coreCreateAuction = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, user string, pct int, bid, lot []string) error {
		created = true
		return nil
	}
	fv := &fakeVault{bal: bal}

	res, err := testAdapter(fd).Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !deleted || !found || !created {
		t.Fatalf("expected delete+resize+recreate, got deleted=%v found=%v created=%v", deleted, found, created)
	}
	if len(fv.draws) != 0 {
		t.Fatalf("no draw in the recreate cycle: %v", fv.draws)
	}
	_ = res
}

// Acquiring the NATIVE asset must request extra headroom (nativeFeePad): the
// swap tx's own fee depresses the measured balance delta, and without the
// pad a `got < debt` check would spuriously roll back every native debt leg
// whose fee exceeds the 1% draw buffer (adversarial-review finding).
func TestExecute_NativeDebtLeg_FeePadded(t *testing.T) {
	const xlmDebt = int64(400_000_000)
	const xlmNeed = int64(404_000_000)    // 1% draw buffer
	const xlmAsk = xlmNeed + nativeFeePad // + 1 XLM fee headroom
	const swapFee = int64(6_000_000)      // 0.6 XLM tx fee > 1% pad alone? (4.04)
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	var quotedOut int64
	fd.quoteFn = func(from, to string, out int64) (int64, error) {
		quotedOut = out
		return 41_500_000, nil // under the oracle cap (41.4 ask × $0.10 + 1%)
	}
	fd.convFn = func(from, to string, out, maxIn int64) (*dex.SwapResult, error) {
		if out != xlmAsk {
			return nil, fmt.Errorf("exact-out request %d, want padded %d", out, xlmAsk)
		}
		bal.add(tUSDC, -41_500_000)
		bal.add(tXLM, out-swapFee) // the fee bites the measured delta
		return &dex.SwapResult{OutputAmount: out - swapFee, Route: "soroswap"}, nil
	}
	fd.swapFn = func(token string, amount, ref int64) (*dex.SwapResult, error) {
		out := amount / 10 * 97 / 100
		bal.add(token, -amount)
		bal.add(tUSDC, out)
		return &dex.SwapResult{OutputAmount: out, Route: "soroswap"}, nil
	}
	auction := usdcAuction()
	auction.Bid[tXLM] = big.NewInt(xlmDebt)
	repaysGot := installSeams(t, bal, auction, func(repays []core.RepayLeg, collateral []string) (string, error) {
		bal.add(tUSDC, -50_000_000)
		bal.add(tXLM, -200_000_000+seizedXLM)
		return "filltx", nil
	})
	fv := &fakeVault{bal: bal}

	a := NewAdapter(Config{
		PoolAddr:    tPool,
		MinProfit:   1.02,
		Passphrase:  "test",
		UsdcAddr:    tUSDC,
		NativeSAC:   tXLM, // XLM is the native asset in these fixtures
		SlippageBps: 100,
	}, nil)
	a.dex = fd

	res, err := a.Execute(nil, mustKPExec(t), mkTask(), fv)
	if err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if !res.Success {
		t.Fatalf("fee-padded native leg must succeed, got %+v", res)
	}
	if quotedOut != xlmAsk {
		t.Fatalf("route quote must use the padded ask: got %d want %d", quotedOut, xlmAsk)
	}
	// The repay leg carries the measured (fee-depressed) amount — still >= debt.
	if len(*repaysGot) != 2 || (*repaysGot)[1].Amount != xlmAsk-swapFee {
		t.Fatalf("repays: %v (want XLM leg %d)", *repaysGot, xlmAsk-swapFee)
	}
	if (*repaysGot)[1].Amount < xlmDebt {
		t.Fatal("measured acquisition must still cover the debt")
	}
}

// Without a NativeSAC configured, non-native legs get NO pad (exact need).
func TestExecute_NonNativeDebtLeg_NotPadded(t *testing.T) {
	bal := newBalTable()
	fd := &fakeDex{bal: bal}
	var quotedOut int64
	fd.quoteFn = func(from, to string, out int64) (int64, error) {
		quotedOut = out
		return 0, dex.ErrNoRoute // stop before any draw; we only assert the ask
	}
	auction := usdcAuction()
	auction.Bid[tXLM] = big.NewInt(400_000_000)
	installSeams(t, bal, auction, nil)
	fv := &fakeVault{bal: bal}

	a := NewAdapter(Config{
		PoolAddr: tPool, MinProfit: 1.02, Passphrase: "test",
		UsdcAddr: tUSDC, NativeSAC: "CNOTNATIVEXLM", SlippageBps: 100,
	}, nil)
	a.dex = fd
	if _, err := a.Execute(nil, mustKPExec(t), mkTask(), fv); err != nil {
		t.Fatalf("Execute: %v", err)
	}
	if quotedOut != 404_000_000 {
		t.Fatalf("non-native ask must be the unpadded need: got %d", quotedOut)
	}
}
