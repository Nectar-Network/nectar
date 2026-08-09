package blend

// Failure-injection tests for the bad-debt path (Session C): scanBackstop
// detection (bad debt → task; interest → note + deferral log, never a task)
// and executeBadDebt (create-if-absent, haircut profitability gate, float
// sizing, atomic fill+repay, past-curve free capture, ambiguity resolution).
// Like execute_test.go, fakes mutate a shared balance table exactly as the
// chain would and assertions MEASURE outcomes the way production does.

import (
	"errors"
	"math/big"
	"strings"
	"testing"
	"time"

	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/adapters"
	core "github.com/nectar-network/keeper/blend"
	"github.com/nectar-network/keeper/soroban"
)

const (
	tBackstop = "CCT4FMLHPJVYBC6SCAOFIRSYU74ZBU36ADREQUQEFCN5C5MRG26S6PTH"
	tLP       = "CAMYNQY4L6BM45HJ6KJMNAX7SK474QHAFJWQBBAKCGV7XP7VJGZ7RUFD"
	tKeeperG  = "GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W"

	badDebtDTokens = int64(200_000_000) // 20 USDC of backstop dToken debt (DRate 1.0)
	badDebtNeed    = int64(202_000_000) // padded by drawBufferBps (1%)
	badDebtLotLP   = int64(200_000_000) // 20 LP lot (spot $1.25 → $25 face)
)

// backstopAuction is the on-chain bad-debt auction fixture: bid = settle-asset
// dTokens, lot = backstop LP (NOT a pool reserve).
func backstopAuction() *core.Auction {
	return &core.Auction{
		User:       tBackstop,
		Type:       core.AuctionBadDebt,
		StartBlock: 1000,
		Bid:        map[string]*big.Int{tUSDC: big.NewInt(badDebtDTokens)},
		Lot:        map[string]*big.Int{tLP: big.NewInt(badDebtLotLP)},
	}
}

func badDebtSpot() *core.BackstopPoolData {
	return &core.BackstopPoolData{Tokens: big.NewInt(500_010_000_000), SpotPrice: 1.25}
}

func badDebtTask() adapters.Task {
	return adapters.Task{
		Protocol: "blend",
		Type:     "bad_debt",
		Target:   tBackstop,
		Priority: 1,
		Data: badDebtData{
			pool:          testPool(),
			backstop:      tBackstop,
			backstopToken: tLP,
			liabAssets:    []string{tUSDC},
			spot:          badDebtSpot(),
		},
	}
}

func badDebtAdapter() *Adapter {
	return NewAdapter(Config{
		PoolAddr:          tPool,
		MinProfit:         1.02,
		Passphrase:        "test",
		UsdcAddr:          tUSDC,
		KeeperAddress:     tKeeperG,
		BadDebtMaxSpend:   1_000_000_000, // 100 USDC cap
		BadDebtHaircutBps: 5000,
	}, nil)
}

// installBadDebtSeams wires the bad-debt chain seams to fakes. The default
// fill fake settles like the chain: keeper pays the TRUE scaled debt (refund
// of the overpayment is implicit — only the net leaves the balance) and
// receives the scaled LP lot.
func installBadDebtSeams(t *testing.T, bal *balTable, auction *core.Auction, elapsed int64) (fillPcts *[]int64, fillSpends *[]int64, freeCalls *int, createBids *[][]string) {
	t.Helper()
	origGetByType, origLedger, origBal := coreGetAuctionByType, latestLedger, tokenBalance
	origCreate, origFill, origFree := coreCreateBadDebt, coreFillBadDebt, coreFillBadDebtFree
	t.Cleanup(func() {
		coreGetAuctionByType, latestLedger, tokenBalance = origGetByType, origLedger, origBal
		coreCreateBadDebt, coreFillBadDebt, coreFillBadDebtFree = origCreate, origFill, origFree
	})

	coreGetAuctionByType = func(rpc *soroban.Client, passphrase, poolAddr, user string, kind core.AuctionType) (*core.Auction, error) {
		if kind == core.AuctionBadDebt {
			return auction, nil
		}
		return nil, nil
	}
	latestLedger = func(rpc *soroban.Client) (int64, error) { return 1000 + elapsed, nil }
	tokenBalance = func(rpc *soroban.Client, passphrase, tokenAddr, owner string) (int64, error) {
		if err := bal.errs[tokenAddr]; err != nil {
			return 0, err
		}
		return bal.m[tokenAddr], nil
	}

	pcts := &[]int64{}
	spends := &[]int64{}
	free := new(int)
	bids := &[][]string{}
	coreCreateBadDebt = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, backstop, backstopToken string, bidAssets []string) error {
		*bids = append(*bids, append([]string{}, bidAssets...))
		return nil
	}
	coreFillBadDebt = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, backstop string,
		fillPct int64, repays []core.RepayLeg) (string, error) {
		*pcts = append(*pcts, fillPct)
		for _, leg := range repays {
			*spends = append(*spends, leg.Amount)
		}
		// Chain effect: pay true scaled debt net of refund, receive scaled lot.
		_, _, bidPct := core.PhaseAt(elapsed)
		trueDebt := int64(float64(badDebtDTokens) * bidPct * float64(fillPct) / 100)
		bal.add(tUSDC, -trueDebt)
		bal.add(tLP, badDebtLotLP*fillPct/100)
		return "badtx", nil
	}
	coreFillBadDebtFree = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, backstop string) (string, error) {
		*free++
		bal.add(tLP, badDebtLotLP) // full lot, empty bid: nothing paid
		return "freetx", nil
	}
	return pcts, spends, free, bids
}

// Happy path at t=300 (lot 100%, bid 50%): ratio = $12.50 haircut lot value /
// $10 debt = 1.25 ≥ 1.02. Float 30 USDC covers the scaled need → 100% fill,
// no vault involvement, LP measured and reported.
func TestExecuteBadDebt_HappyPath(t *testing.T) {
	bal := newBalTable()
	bal.m[tUSDC] = 300_000_000
	bal.m[tLP] = 0
	pcts, spends, free, _ := installBadDebtSeams(t, bal, backstopAuction(), 300)

	a := badDebtAdapter()
	res, err := a.Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("want success, got note=%q", res.Note)
	}
	if res.TxHash != "badtx" {
		t.Errorf("tx hash: got %q", res.TxHash)
	}
	if len(*pcts) != 1 || (*pcts)[0] != 100 {
		t.Errorf("want one 100%% fill, got %v", *pcts)
	}
	// Repay leg = ceil(padded need × bidPct 0.5) = 101 USDC-stroops ×1e6.
	if len(*spends) != 1 || (*spends)[0] != 101_000_000 {
		t.Errorf("want repay leg 101000000, got %v", *spends)
	}
	if *free != 0 {
		t.Errorf("free-capture path must not run before t=400")
	}
	if res.Drew != 0 || res.Proceeds != 0 || res.Profit != 0 {
		t.Errorf("vault accounting must stay zero (float-funded): drew=%d proceeds=%d profit=%d", res.Drew, res.Proceeds, res.Profit)
	}
	if !strings.Contains(res.Note, "fill-and-hold") ||
		!strings.Contains(res.Note, "received 200000000 backstop LP") ||
		!strings.Contains(res.Note, "spent 100000000 USDC keeper float") {
		t.Errorf("note must report measured spend and held LP, got %q", res.Note)
	}
}

// Phase 1 at t=100 with a 50% haircut: lot value $6.25 vs cost $20 → 0.3125,
// far below MinProfit. No fill attempt, no balances touched.
func TestExecuteBadDebt_WaitsWhenUnprofitable(t *testing.T) {
	bal := newBalTable()
	bal.m[tUSDC] = 300_000_000
	pcts, _, free, _ := installBadDebtSeams(t, bal, backstopAuction(), 100)

	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || len(*pcts) != 0 || *free != 0 {
		t.Fatalf("must wait, got success=%v fills=%v", res.Success, *pcts)
	}
	if !strings.Contains(res.Note, "not profitable") {
		t.Errorf("note: %q", res.Note)
	}
}

// Float 5 USDC < scaled need 10.1 USDC → partial fill at 49%, spend within
// the float.
func TestExecuteBadDebt_PartialFillWhenFloatShort(t *testing.T) {
	bal := newBalTable() // float 5 USDC
	bal.m[tLP] = 0
	pcts, spends, _, _ := installBadDebtSeams(t, bal, backstopAuction(), 300)

	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("want success, note=%q", res.Note)
	}
	if len(*pcts) != 1 || (*pcts)[0] != 49 {
		t.Errorf("want 49%% fill (floor of 5/10.1), got %v", *pcts)
	}
	if len(*spends) != 1 || (*spends)[0] > floatUSDC {
		t.Errorf("spend must fit the float %d, got %v", floatUSDC, *spends)
	}
	if !strings.Contains(res.Note, "fill 49%") {
		t.Errorf("note must carry the scaled percent, got %q", res.Note)
	}
}

// A float too small for even a 1% fill skips cleanly.
func TestExecuteBadDebt_InsufficientFloatSkips(t *testing.T) {
	bal := newBalTable()
	bal.m[tUSDC] = 500_000 // 0.05 USDC
	pcts, _, _, _ := installBadDebtSeams(t, bal, backstopAuction(), 300)

	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || len(*pcts) != 0 {
		t.Fatal("must skip, not fill")
	}
	if !strings.Contains(res.Note, "cannot cover") {
		t.Errorf("note: %q", res.Note)
	}
}

// BadDebtMaxSpend=0 disables the path before any chain read.
func TestExecuteBadDebt_DisabledByCap(t *testing.T) {
	a := NewAdapter(Config{PoolAddr: tPool, MinProfit: 1.02, Passphrase: "test", UsdcAddr: tUSDC}, nil)
	res, err := a.Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: newBalTable()})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || !strings.Contains(res.Note, "disabled") {
		t.Errorf("want disabled note, got %q", res.Note)
	}
}

// An auction bidding a non-settle asset is skipped: the keeper can only
// repay vault-USDC legs from its float.
func TestExecuteBadDebt_NonSettleBidSkipped(t *testing.T) {
	auction := backstopAuction()
	auction.Bid = map[string]*big.Int{tXLM: big.NewInt(1_000_000_000)}
	bal := newBalTable()
	pcts, _, free, _ := installBadDebtSeams(t, bal, auction, 300)

	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || len(*pcts) != 0 || *free != 0 {
		t.Fatal("must skip non-settle bid")
	}
	if !strings.Contains(res.Note, "only settle-asset bad debt is supported") {
		t.Errorf("note: %q", res.Note)
	}
}

// No auction yet: Execute creates it (settle-asset bid only) and fills once
// readable.
func TestExecuteBadDebt_CreatesWhenAbsent(t *testing.T) {
	bal := newBalTable()
	bal.m[tUSDC] = 300_000_000
	bal.m[tLP] = 0
	pcts, _, _, bids := installBadDebtSeams(t, bal, nil, 300)
	// First lookup: absent. After create: present.
	created := false
	prevGet, prevCreate := coreGetAuctionByType, coreCreateBadDebt
	coreGetAuctionByType = func(rpc *soroban.Client, passphrase, poolAddr, user string, kind core.AuctionType) (*core.Auction, error) {
		if kind == core.AuctionBadDebt && created {
			return backstopAuction(), nil
		}
		return nil, nil
	}
	coreCreateBadDebt = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, backstop, backstopToken string, bidAssets []string) error {
		created = true
		if backstopToken != tLP {
			t.Errorf("lot token: want %s, got %s", tLP, backstopToken)
		}
		return prevCreate(rpc, horizonURL, kp, passphrase, poolAddr, backstop, backstopToken, bidAssets)
	}
	t.Cleanup(func() { coreGetAuctionByType, coreCreateBadDebt = prevGet, prevCreate })

	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success {
		t.Fatalf("want success after create, note=%q", res.Note)
	}
	if len(*bids) != 1 || len((*bids)[0]) != 1 || (*bids)[0][0] != tUSDC {
		t.Errorf("create must bid ONLY the settle asset, got %v", *bids)
	}
	if len(*pcts) != 1 {
		t.Errorf("want one fill after create, got %v", *pcts)
	}
}

// Backstop owes only non-settle assets and no auction exists: nothing this
// keeper can do — no creation, clear note.
func TestExecuteBadDebt_NoSettleLegNoCreate(t *testing.T) {
	bal := newBalTable()
	_, _, _, bids := installBadDebtSeams(t, bal, nil, 300)

	task := badDebtTask()
	td := task.Data.(badDebtData)
	td.liabAssets = []string{tXLM}
	task.Data = td

	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), task, &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || len(*bids) != 0 {
		t.Fatal("must not create an auction it cannot fill")
	}
	if !strings.Contains(res.Note, "no settle-asset leg") {
		t.Errorf("note: %q", res.Note)
	}
}

// Past t=400 the scaled bid is empty: the bare free-capture fill runs (no
// repay, no spend) and the full lot is measured in.
func TestExecuteBadDebt_FreeCaptureAfterCurve(t *testing.T) {
	bal := newBalTable()
	bal.m[tLP] = 0
	usdcBefore := bal.m[tUSDC]
	pcts, _, free, _ := installBadDebtSeams(t, bal, backstopAuction(), 450)

	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || *free != 1 || len(*pcts) != 0 {
		t.Fatalf("want exactly the free path, got success=%v free=%d fills=%v note=%q", res.Success, *free, *pcts, res.Note)
	}
	if bal.m[tUSDC] != usdcBefore {
		t.Errorf("free capture must spend nothing, float moved %d → %d", usdcBefore, bal.m[tUSDC])
	}
	if !strings.Contains(res.Note, "free capture") || !strings.Contains(res.Note, "received 200000000 backstop LP") {
		t.Errorf("note: %q", res.Note)
	}
}

// Losing the race is a clean no-op.
func TestExecuteBadDebt_RacedLost(t *testing.T) {
	bal := newBalTable()
	bal.m[tUSDC] = 300_000_000
	installBadDebtSeams(t, bal, backstopAuction(), 300)
	prev := coreFillBadDebt
	coreFillBadDebt = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, backstop string,
		fillPct int64, repays []core.RepayLeg) (string, error) {
		return "", core.ErrAlreadyFilled
	}
	t.Cleanup(func() { coreFillBadDebt = prev })

	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || !strings.Contains(res.Note, "no longer fillable") {
		t.Errorf("want raced note, got success=%v %q", res.Success, res.Note)
	}
}

// installAwait pins the ambiguity re-poll outcome.
func installAwait(t *testing.T, fn func(hash string) error) {
	t.Helper()
	orig := awaitTx
	t.Cleanup(func() { awaitTx = orig })
	awaitTx = func(rpc *soroban.Client, hash string, timeout time.Duration) error { return fn(hash) }
}

// Ambiguous fill that actually LANDED: resolved by hash, balances (already
// mutated by the landed tx) are measured, success.
func TestExecuteBadDebt_AmbiguousResolvedLanded(t *testing.T) {
	bal := newBalTable()
	bal.m[tUSDC] = 300_000_000
	bal.m[tLP] = 0
	installBadDebtSeams(t, bal, backstopAuction(), 300)
	prev := coreFillBadDebt
	coreFillBadDebt = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, backstop string,
		fillPct int64, repays []core.RepayLeg) (string, error) {
		// The tx landed on chain but the RPC lost track of it.
		bal.add(tUSDC, -100_000_000)
		bal.add(tLP, badDebtLotLP)
		return "", &soroban.TxStatusUnknownError{Hash: "badc0ffee", Err: errors.New("poll timeout")}
	}
	t.Cleanup(func() { coreFillBadDebt = prev })
	installAwait(t, func(hash string) error { return nil }) // landed

	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || res.TxHash != "badc0ffee" {
		t.Fatalf("want resolved-landed success, got success=%v hash=%q note=%q", res.Success, res.TxHash, res.Note)
	}
	if !strings.Contains(res.Note, "received 200000000 backstop LP") {
		t.Errorf("landed fill must measure the LP delta, note=%q", res.Note)
	}
}

// Ambiguity that stays unknown: hold, nothing double-moved, clear note. No
// vault capital is involved, so no rollback machinery may run.
func TestExecuteBadDebt_AmbiguousUnknownHolds(t *testing.T) {
	bal := newBalTable()
	bal.m[tUSDC] = 300_000_000
	installBadDebtSeams(t, bal, backstopAuction(), 300)
	prev := coreFillBadDebt
	coreFillBadDebt = func(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, backstop string,
		fillPct int64, repays []core.RepayLeg) (string, error) {
		return "", &soroban.TxStatusUnknownError{Hash: "unknowable", Err: errors.New("timeout")}
	}
	t.Cleanup(func() { coreFillBadDebt = prev })
	installAwait(t, func(hash string) error {
		return &soroban.TxStatusUnknownError{Hash: hash, Err: errors.New("still pending")}
	})

	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if res.Success || !strings.Contains(res.Note, "UNKNOWN") {
		t.Errorf("want unknown-hold note, got success=%v %q", res.Success, res.Note)
	}
}

// ── scanBackstop ──────────────────────────────────────────────────────────────

// installScanSeams fakes the backstop-resolution and position reads.
func installScanSeams(t *testing.T, liabIdx map[uint32]*big.Int, interest *core.Auction, lpHeld int64) {
	t.Helper()
	origBS, origTok, origLoad, origPD := coreGetBackstop, coreGetBackstopToken, coreLoadPosition, coreBackstopPoolData
	origGetByType, origBal := coreGetAuctionByType, tokenBalance
	t.Cleanup(func() {
		coreGetBackstop, coreGetBackstopToken, coreLoadPosition, coreBackstopPoolData = origBS, origTok, origLoad, origPD
		coreGetAuctionByType, tokenBalance = origGetByType, origBal
	})
	coreGetBackstop = func(rpc *soroban.Client, poolAddr string) (string, error) { return tBackstop, nil }
	coreGetBackstopToken = func(rpc *soroban.Client, passphrase, backstopAddr string) (string, error) { return tLP, nil }
	coreGetAuctionByType = func(rpc *soroban.Client, passphrase, poolAddr, user string, kind core.AuctionType) (*core.Auction, error) {
		if kind == core.AuctionInterest {
			return interest, nil
		}
		return nil, nil
	}
	coreLoadPosition = func(rpc *soroban.Client, passphrase, poolAddr, user string) (*core.Position, error) {
		return &core.Position{Address: user, Liabilities: liabIdx,
			Collateral: map[uint32]*big.Int{}, Supply: map[uint32]*big.Int{}}, nil
	}
	coreBackstopPoolData = func(rpc *soroban.Client, passphrase, backstopAddr, poolAddr string) (*core.BackstopPoolData, error) {
		return badDebtSpot(), nil
	}
	tokenBalance = func(rpc *soroban.Client, passphrase, tokenAddr, owner string) (int64, error) {
		if tokenAddr == tLP {
			return lpHeld, nil
		}
		return 0, nil
	}
}

func TestScanBackstop_BadDebtTaskEmitted(t *testing.T) {
	installScanSeams(t, map[uint32]*big.Int{0: big.NewInt(badDebtDTokens)}, nil, 0)
	a := badDebtAdapter()
	report := &adapters.ScanReport{}
	tasks := a.scanBackstop(nil, testPool(), report, false)
	if len(tasks) != 1 || tasks[0].Type != "bad_debt" || tasks[0].Target != tBackstop {
		t.Fatalf("want one bad_debt task, got %+v", tasks)
	}
	td, ok := tasks[0].Data.(badDebtData)
	if !ok || td.backstopToken != tLP || len(td.liabAssets) != 1 || td.liabAssets[0] != tUSDC || td.spot == nil {
		t.Errorf("task data incomplete: %+v", td)
	}
	if tasks[0].Priority != 1 {
		t.Errorf("bad-debt priority must not outrank liquidations, got %d", tasks[0].Priority)
	}
	if !strings.Contains(report.Note, "backstop carries bad debt") {
		t.Errorf("report note: %q", report.Note)
	}
}

func TestScanBackstop_SuppressedTasksStillReported(t *testing.T) {
	installScanSeams(t, map[uint32]*big.Int{0: big.NewInt(badDebtDTokens)}, nil, 0)
	a := badDebtAdapter()
	report := &adapters.ScanReport{}
	if tasks := a.scanBackstop(nil, testPool(), report, true); len(tasks) != 0 {
		t.Fatalf("suppressed scan must emit no tasks, got %+v", tasks)
	}
	if !strings.Contains(report.Note, "backstop carries bad debt") {
		t.Errorf("presence must still be reported: %q", report.Note)
	}
}

func TestScanBackstop_DisabledCapNote(t *testing.T) {
	installScanSeams(t, map[uint32]*big.Int{0: big.NewInt(badDebtDTokens)}, nil, 0)
	a := badDebtAdapter()
	a.cfg.BadDebtMaxSpend = 0
	report := &adapters.ScanReport{}
	if tasks := a.scanBackstop(nil, testPool(), report, false); len(tasks) != 0 {
		t.Fatal("disabled cap must emit no tasks")
	}
	if !strings.Contains(report.Note, "BAD_DEBT_MAX_SPEND=0") {
		t.Errorf("report note: %q", report.Note)
	}
}

// Interest auctions: note + ONE deferral log per auction, never a task.
func TestScanBackstop_InterestDeferredAndLoggedOnce(t *testing.T) {
	interest := &core.Auction{User: tBackstop, Type: core.AuctionInterest, StartBlock: 7777,
		Bid: map[string]*big.Int{tLP: big.NewInt(1)}, Lot: map[string]*big.Int{tUSDC: big.NewInt(1)}}
	installScanSeams(t, nil, interest, 0)

	var logged []string
	origLog := logAuction
	logAuction = func(msg, pool, user string, pct int, startBlock int64) { logged = append(logged, msg) }
	t.Cleanup(func() { logAuction = origLog })

	a := badDebtAdapter()
	report := &adapters.ScanReport{}
	if tasks := a.scanBackstop(nil, testPool(), report, false); len(tasks) != 0 {
		t.Fatalf("interest must NEVER become a task, got %+v", tasks)
	}
	if !strings.Contains(report.Note, "interest auction seen, deferred") {
		t.Errorf("report note: %q", report.Note)
	}
	// Second scan of the same auction: note again, but no duplicate log.
	a.scanBackstop(nil, testPool(), &adapters.ScanReport{}, false)
	if len(logged) != 1 || !strings.Contains(logged[0], "deferred") {
		t.Errorf("want exactly one deferral log per auction, got %v", logged)
	}
}

func TestScanBackstop_LPHoldingsReported(t *testing.T) {
	installScanSeams(t, nil, nil, 40_000_000) // keeper holds 4 LP
	a := badDebtAdapter()
	report := &adapters.ScanReport{}
	a.scanBackstop(nil, testPool(), report, false)
	if !strings.Contains(report.Note, "keeper holds 40000000 backstop LP") {
		t.Errorf("LP holdings must be reported every scan: %q", report.Note)
	}
}

func TestScanBackstop_ResolveFailureNotes(t *testing.T) {
	orig := coreGetBackstop
	coreGetBackstop = func(rpc *soroban.Client, poolAddr string) (string, error) {
		return "", errors.New("rpc down")
	}
	t.Cleanup(func() { coreGetBackstop = orig })
	a := badDebtAdapter()
	report := &adapters.ScanReport{}
	if tasks := a.scanBackstop(nil, testPool(), report, false); len(tasks) != 0 {
		t.Fatal("unresolved backstop must emit no tasks")
	}
	if !strings.Contains(report.Note, "backstop unresolved") {
		t.Errorf("report note: %q", report.Note)
	}
}
