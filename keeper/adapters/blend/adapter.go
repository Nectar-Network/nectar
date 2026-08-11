// Package blend adapts the Blend liquidation protocol to the generic
// adapters.ProtocolAdapter interface. It wraps the lower-level
// github.com/nectar-network/keeper/blend package (pool/auction/position logic)
// and the dex package (token conversion, both directions), turning underwater
// positions into Tasks and filling their auctions in Execute. The underlying
// blend package is left intact (and fully tested); this is a thin translation
// layer, which is what gets extracted into the keeper-sdk in Phase 4.
package blend

import (
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"time"

	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/adapters"
	core "github.com/nectar-network/keeper/blend"
	"github.com/nectar-network/keeper/dex"
	"github.com/nectar-network/keeper/soroban"
)

// Config holds the per-adapter settings not passed on each call.
type Config struct {
	PoolAddr   string
	MinProfit  float64
	HorizonURL string
	Passphrase string
	UsdcAddr   string
	// SlippageBps bounds how far above the oracle-implied value the keeper
	// will pay when acquiring a non-USDC debt asset before a fill (the dex
	// client applies the same tolerance on exits). 100 = 1%.
	SlippageBps int
	// Monitor makes the adapter observe-only: it scans the pool, computes
	// health factors and publishes a ScanReport, but never emits tasks (so no
	// capital can move toward this pool). Used for pools whose settlement
	// asset has no verified conversion route to the vault's USDC.
	Monitor bool
	// PoolUsdc is the pool's own USDC contract when it differs from the
	// vault's UsdcAddr. Such pools are monitor-only (FACTS.md "USDC asset
	// bridging"): a fill would strand pre-converted capital.
	PoolUsdc string
	// NativeSAC is the network's native-XLM Stellar Asset Contract
	// (soroban.NativeContractID). Balance deltas of THIS asset are depressed
	// by the measuring transaction's own fee, so acquisitions of it are
	// padded by nativeFeePad. Empty disables the pad (native debt legs then
	// risk spurious under-acquisition rollbacks).
	NativeSAC string
	// EventLookback is how many ledgers of pool events to scan for position
	// discovery. 0 means the default of 1000 (~83 min at 5s ledgers).
	EventLookback int64
	// KeeperAddress is the keeper's own G-address, used by scans to report the
	// keeper's backstop-LP holdings. Empty disables that report line only.
	KeeperAddress string
	// BadDebtMaxSpend caps the keeper-FLOAT USDC (7-dec stroops) committed to
	// one bad-debt fill; 0 disables bad-debt fills entirely. Bad-debt fills
	// never draw vault capital: the fill pays USDC (debt repay) and receives
	// backstop LP tokens that cannot be liquidated into the vault's settlement
	// asset before mainnet, while vault draws must return within the registry's
	// slash_timeout — so the LP position is keeper-operator risk by design.
	// The reward is operator-side too: even after the mainnet comet exit the
	// USDC stays with the operator, because return_proceeds rejects a keeper
	// with no outstanding draw (VLT-2 NoDraw)
	// (docs/FACTS.md Decisions, fill-and-hold).
	BadDebtMaxSpend int64
	// BadDebtHaircutBps discounts the backstop's token_spot_price when valuing
	// the LP lot (5000 = value at 50% of spot). The haircut prices in the
	// deferred unwind: comet exit fees/slippage and spot drift.
	BadDebtHaircutBps int
}

// drawBufferBps pads every debt leg over the snapshot-time underlying amount
// (100 = 1%). It covers d_rate interest accrual between the auction read and
// the landed fill plus fixed-point truncation, so the atomic repay always
// extinguishes the assumed dTokens — a leg that comes up short leaves dust
// debt, and the submit's final health check then reverts the whole fill. The
// pool refunds everything above the true debt, so the buffer's only cost is a
// slightly larger draw.
const drawBufferBps = 100

// nativeFeePad (stroops) is added to exact-out acquisitions of the NATIVE
// asset: the swap tx's own fee is paid from the same balance whose delta
// measures the output, so without headroom the measured amount comes up
// short of the request by the fee and a `got < debt` check would spuriously
// roll back (adversarial-review finding, 2026-08-09). 1 XLM comfortably
// covers observed Soroban resource fees (~0.05–0.6 XLM); the excess is
// refunded by the pool's repay and swept back to USDC with the leftovers.
const nativeFeePad = 10_000_000

// badDebtBoundaryLedgers is the no-fill margin before the t=400 curve end for
// bad-debt repay-carrying fills. A fill built at t=399 can land at t≥400,
// where the scaled bid is empty: nothing is assumed, the repay leg has no
// dTokens to burn, and the submit reverts InvalidDTokenBurnAmount (#1219) —
// observed live (docs/evidence/c-bad-debt.md). Inside the margin the keeper
// waits the few remaining ledgers for the strictly-better free capture.
const badDebtBoundaryLedgers = 2

// dexClient is the slice of dex.SwapClient the adapter actually calls; an
// interface so failure-injection tests can fake the DEX without an RPC
// server. (dex.SwapClient's generic Swap/SwapFromUSDC remain public API for
// other callers.)
type dexClient interface {
	SwapToUSDC(kp *keypair.Full, tokenAddr string, amount, refValueUSDC int64) (*dex.SwapResult, error)
	QuoteIn(from, to string, amountOut int64) (int64, error)
	ConvertExactOut(kp *keypair.Full, from, to string, amountOut, maxIn int64) (*dex.SwapResult, error)
	QuoteConvertIn(from, to string, amountOut int64) (int64, error)
}

// Seams for the blend/dex package functions Execute orchestrates, injectable
// by failure-injection tests (same pattern as the log hooks below). Production
// never reassigns them.
var (
	coreGetAuction       = core.GetAuction
	coreFindLiqPct       = core.FindLiquidationPercent
	coreCreateAuction    = core.CreateAuction
	coreFillAndUnwind    = core.FillAndUnwind
	coreDeleteStale      = core.DeleteStaleAuction
	coreGetAuctionByType = core.GetAuctionByType
	coreGetBackstop      = core.GetBackstop
	coreGetBackstopToken = core.GetBackstopToken
	coreBackstopPoolData = core.GetBackstopPoolData
	coreLoadPosition     = core.LoadPosition
	coreCreateBadDebt    = core.CreateBadDebtAuction
	coreFillBadDebt      = core.FillBadDebtAndRepay
	coreFillBadDebtFree  = core.FillBadDebtFree
	tokenBalance         = dex.TokenBalance
	latestLedger         = func(rpc *soroban.Client) (int64, error) { return rpc.LatestLedger() }
	awaitTx              = func(rpc *soroban.Client, hash string, timeout time.Duration) error {
		_, err := rpc.AwaitTx(hash, timeout)
		return err
	}
)

// ambiguityWait is how long Execute re-polls an ambiguous transaction before
// giving up. Signed txs carry a 30-second timebound (soroban/tx.go) and the
// Invoke path already polled 30s, so this window covers the tx's entire
// remaining life: at its end the tx has either landed (SUCCESS/FAILED
// visible) or expired. Residual "unknown" therefore implies a sustained RPC
// outage — the conservative hold path then applies, and the next cycle's
// recovery (whose chain reads would fail the same way) cannot race a
// still-pending tx.
const ambiguityWait = 45 * time.Second

// ambiguity classifies an ambiguous (possibly-landed) transaction error by
// re-polling its hash: landed (it SUCCEEDED), failed (it definitively landed
// and FAILED — safe to treat deterministically), or neither (still unknown).
func ambiguity(rpc *soroban.Client, err error) (hash string, landed, failed bool) {
	var u *soroban.TxStatusUnknownError
	if !errors.As(err, &u) || u.Hash == "" {
		return "", false, false
	}
	aerr := awaitTx(rpc, u.Hash, ambiguityWait)
	switch {
	case aerr == nil:
		return u.Hash, true, false
	case !soroban.IsTxStatusUnknown(aerr):
		return u.Hash, false, true
	default:
		return u.Hash, false, false
	}
}

// Adapter implements adapters.ProtocolAdapter for one Blend pool. Run several
// instances to monitor several pools — Name() is unique per pool.
type Adapter struct {
	cfg      Config
	dex      dexClient
	lastScan *adapters.ScanReport

	// backstop / backstopToken cache the pool's backstop address and its LP
	// token once resolved — both are constructor-fixed on chain (backstop is a
	// pool __constructor arg with no setter), so caching is safe.
	backstop      string
	backstopToken string
	// loggedInterestBlock dedupes the per-auction interest-deferral log line:
	// one INFO per auction start block instead of one per 10-second cycle.
	loggedInterestBlock int64
}

// NewAdapter builds a Blend adapter. dexc may be nil to disable all token
// conversion: proceeds are then only the USDC directly produced by the unwind,
// and auctions with non-USDC debt legs are skipped.
func NewAdapter(cfg Config, dexc *dex.SwapClient) *Adapter {
	a := &Adapter{cfg: cfg}
	if dexc != nil { // avoid a typed-nil interface that would pass != nil checks
		a.dex = dexc
	}
	return a
}

// Name returns the protocol identifier, unique per monitored pool.
func (a *Adapter) Name() string {
	if a.cfg.PoolAddr == "" {
		return "blend"
	}
	return "blend:" + shortAddr(a.cfg.PoolAddr)
}

// LastScan implements adapters.ScanReporter.
func (a *Adapter) LastScan() *adapters.ScanReport { return a.lastScan }

// shortAddr elides a contract address to prefix..suffix for labels.
func shortAddr(addr string) string {
	if len(addr) <= 8 {
		return addr
	}
	return addr[:4] + ".." + addr[len(addr)-4:]
}

// taskData is the per-task payload threaded from GetTasks to Execute so the
// pool snapshot (oracle prices, reserves) is reused without re-loading.
type taskData struct {
	pool *core.PoolState
	// bidAssets / lotAssets are the position's liability / collateral
	// underlying addresses, required by v2's new_auction entry point.
	bidAssets []string
	lotAssets []string
}

// GetTasks loads the pool and returns one liquidation task per underwater
// position (health factor < 1). In Monitor mode it still scans and reports,
// but returns no tasks.
func (a *Adapter) GetTasks(rpc *soroban.Client) ([]adapters.Task, error) {
	if a.cfg.PoolAddr == "" {
		return nil, nil
	}
	pool, err := core.LoadPool(rpc, a.cfg.Passphrase, a.cfg.PoolAddr)
	if err != nil {
		return nil, fmt.Errorf("load pool: %w", err)
	}
	ledger, err := rpc.LatestLedger()
	if err != nil {
		return nil, fmt.Errorf("latest ledger: %w", err)
	}
	lookback := a.cfg.EventLookback
	if lookback <= 0 {
		lookback = 1000
	}
	exclude := make(map[string]bool, len(pool.Reserves))
	for asset := range pool.Reserves {
		exclude[asset] = true
	}
	positions, err := core.GetPositions(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, ledger-lookback, exclude)
	if err != nil {
		return nil, fmt.Errorf("get positions: %w", err)
	}

	report := &adapters.ScanReport{
		Pool:           a.cfg.PoolAddr,
		Monitor:        a.cfg.Monitor,
		Status:         pool.Status,
		Reserves:       len(pool.Reserves),
		OracleDecimals: pool.OracleDecimals,
		Prices:         make(map[string]float64, len(pool.Reserves)),
	}
	for asset, r := range pool.Reserves {
		report.Prices[asset] = r.OraclePrice
	}

	// Pools that settle a different USDC than the vault are scanned but never
	// executed. A DEX route between the two USDCs exists on testnet
	// (docs/evidence/a2-route-checks.json) but routing vault capital through
	// it is NOT safe with the current fill model: a user-liquidation fill
	// transfers no tokens (the filler assumes dToken debt — docs/FACTS.md
	// "Auction asset flows"), so pre-converted capital would sit stranded in
	// the pool's USDC, and a failed fill or an ambiguous conversion send has
	// no sound recovery path. The route quote is still reported each scan so
	// the operator can see live parity.
	crossUsdc := a.cfg.PoolUsdc != "" && a.cfg.PoolUsdc != a.cfg.UsdcAddr
	if crossUsdc {
		report.Note = "cross-USDC pool: monitored only, execution disabled (FACTS.md 'USDC asset bridging')"
		if a.dex != nil {
			if _, err := a.dex.QuoteConvertIn(a.cfg.UsdcAddr, a.cfg.PoolUsdc, 1_0000000); err != nil {
				report.Note += fmt.Sprintf("; 1-USDC route quote: %v", err)
			} else {
				report.Note += "; 1-USDC route quote within par bound"
			}
		}
	}
	suppressTasks := a.cfg.Monitor || crossUsdc

	indexToAsset := make(map[uint32]string, len(pool.Reserves))
	for asset, r := range pool.Reserves {
		indexToAsset[r.Index] = asset
	}

	var tasks []adapters.Task
	for i := range positions {
		pos := &positions[i]
		hf, hfErr := core.HealthFactor(*pos, pool)
		if hfErr != nil {
			// Unpriceable position: report it, never act on it. Acting would
			// mean creating an auction the pool itself would reject
			// (InvalidPrice) off a health factor we effectively invented.
			report.Positions = append(report.Positions, adapters.PositionHealth{Address: pos.Address, Unpriced: true})
			continue
		}
		report.Positions = append(report.Positions, adapters.PositionHealth{Address: pos.Address, HF: hf})
		if hf >= 1.0 || suppressTasks {
			continue
		}
		// A position with debt but no collateral is not user-liquidatable:
		// the lot would be empty (new_auction panics InvalidLot) and the
		// protocol handles it as bad debt instead. This also keeps the
		// BACKSTOP — which holds transferred bad debt and appears in
		// bad-debt/interest auction event topics — out of the task list; the
		// pool exposes no backstop getter to exclude it by address
		// (docs/FACTS.md "Pool public read interface").
		if len(pos.Collateral) == 0 {
			continue
		}
		// Lot assets come from `collateral` ONLY: non-collateral `supply` is
		// not auctionable and new_auction panics InvalidLot if it appears
		// (user_liquidation_auction.rs:73-89 @ ba22b48).
		var bidAssets, lotAssets []string
		for idx := range pos.Liabilities {
			if asset, ok := indexToAsset[idx]; ok {
				bidAssets = append(bidAssets, asset)
			}
		}
		for idx := range pos.Collateral {
			if asset, ok := indexToAsset[idx]; ok {
				lotAssets = append(lotAssets, asset)
			}
		}
		if len(bidAssets) == 0 || len(lotAssets) == 0 {
			continue // unmappable reserve indices — never guess an auction shape
		}
		tasks = append(tasks, adapters.Task{
			Protocol: a.Name(),
			Type:     "liquidation",
			Target:   pos.Address,
			Priority: priorityFromHF(hf),
			Health:   hf,
			Data:     taskData{pool: pool, bidAssets: bidAssets, lotAssets: lotAssets},
		})
	}
	tasks = append(tasks, a.scanBackstop(rpc, pool, report, suppressTasks)...)
	a.lastScan = report
	return tasks, nil
}

// badDebtData is the per-task payload for bad-debt tasks: the pool snapshot
// plus the backstop identity and its LP valuation read in the same cycle.
type badDebtData struct {
	pool          *core.PoolState
	backstop      string
	backstopToken string
	liabAssets    []string // underlying assets the backstop owes (mapped from indices)
	spot          *core.BackstopPoolData
}

// appendNote joins operational caveats in the scan report.
func appendNote(report *adapters.ScanReport, s string) {
	if report.Note == "" {
		report.Note = s
		return
	}
	report.Note += "; " + s
}

// scanBackstop covers the auction kinds that live on the BACKSTOP address
// rather than on borrower positions: bad-debt auctions (type 1 — actionable,
// emitted as tasks) and interest auctions (type 2 — DETECTION ONLY, deferred:
// the bid is backstop LP tokens the filler must pre-hold and pre-approve,
// which a vault-USDC keeper does not carry; docs/FACTS.md Decisions). It also
// reports the keeper's own backstop-LP holdings from earlier fill-and-hold
// fills so their value stays visible every cycle. All findings go into the
// scan report; every chain-read failure is reported, never guessed around.
func (a *Adapter) scanBackstop(rpc *soroban.Client, pool *core.PoolState, report *adapters.ScanReport, suppressTasks bool) []adapters.Task {
	if a.backstop == "" {
		bs, err := coreGetBackstop(rpc, a.cfg.PoolAddr)
		if err != nil {
			appendNote(report, fmt.Sprintf("backstop unresolved (bad-debt/interest scan skipped): %v", err))
			return nil
		}
		a.backstop = bs
	}
	if a.backstopToken == "" {
		tok, err := coreGetBackstopToken(rpc, a.cfg.Passphrase, a.backstop)
		if err != nil {
			appendNote(report, fmt.Sprintf("backstop token unresolved (bad-debt/interest scan skipped): %v", err))
			return nil
		}
		a.backstopToken = tok
	}

	// Interest auctions: awareness only, never a task. Log once per auction.
	if ia, err := coreGetAuctionByType(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, a.backstop, core.AuctionInterest); err != nil {
		appendNote(report, fmt.Sprintf("interest-auction check failed: %v", err))
	} else if ia != nil {
		appendNote(report, "interest auction seen, deferred: bid requires pre-held backstop LP inventory (docs/FACTS.md Decisions)")
		if ia.StartBlock != a.loggedInterestBlock {
			a.loggedInterestBlock = ia.StartBlock
			logAuction("interest auction seen, deferred: fill bid is backstop LP tokens (120% of interest value at spot) the keeper would have to pre-hold and pre-approve — vault USDC cannot fill it",
				a.cfg.PoolAddr, a.backstop, 0, ia.StartBlock)
		}
	}

	// Value the keeper's held LP (fill-and-hold inventory) every cycle.
	var spot *core.BackstopPoolData
	loadSpot := func() *core.BackstopPoolData {
		if spot != nil {
			return spot
		}
		pd, err := coreBackstopPoolData(rpc, a.cfg.Passphrase, a.backstop, a.cfg.PoolAddr)
		if err != nil {
			appendNote(report, fmt.Sprintf("backstop pool_data unreadable: %v", err))
			return nil
		}
		spot = pd
		return spot
	}
	if a.cfg.KeeperAddress != "" {
		if lpBal, err := tokenBalance(rpc, a.cfg.Passphrase, a.backstopToken, a.cfg.KeeperAddress); err == nil && lpBal > 0 {
			if pd := loadSpot(); pd != nil {
				lp := big.NewInt(lpBal)
				appendNote(report, fmt.Sprintf("keeper holds %d backstop LP ≈ $%.4f spot / $%.4f after %d bps haircut (fill-and-hold, unwind deferred)",
					lpBal, core.BackstopLPValueUSD(lp, pd.SpotPrice, 0), core.BackstopLPValueUSD(lp, pd.SpotPrice, a.cfg.BadDebtHaircutBps), a.cfg.BadDebtHaircutBps))
			}
		}
	}

	// Bad debt: the backstop's dToken liabilities (left behind by full fills
	// of underwater positions) are auctionable by anyone.
	pos, err := coreLoadPosition(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, a.backstop)
	if err != nil {
		appendNote(report, fmt.Sprintf("backstop positions unreadable: %v", err))
		return nil
	}
	if len(pos.Liabilities) == 0 {
		return nil
	}
	indexToAsset := make(map[uint32]string, len(pool.Reserves))
	for asset, r := range pool.Reserves {
		indexToAsset[r.Index] = asset
	}
	liabAssets := make([]string, 0, len(pos.Liabilities))
	for idx := range pos.Liabilities {
		if asset, ok := indexToAsset[idx]; ok {
			liabAssets = append(liabAssets, asset)
		}
	}
	sort.Strings(liabAssets)
	if len(liabAssets) == 0 {
		appendNote(report, "backstop has bad debt in unmappable reserves — skipped")
		return nil
	}
	appendNote(report, fmt.Sprintf("backstop carries bad debt in %d reserve(s)", len(liabAssets)))
	if suppressTasks {
		return nil
	}
	if a.cfg.BadDebtMaxSpend <= 0 {
		appendNote(report, "bad-debt fills disabled (BAD_DEBT_MAX_SPEND=0)")
		return nil
	}
	pd := loadSpot()
	if pd == nil {
		return nil // LP lot cannot be valued — no fill without a price
	}
	return []adapters.Task{{
		Protocol: a.Name(),
		Type:     "bad_debt",
		Target:   a.backstop,
		Priority: 1, // house-cleaning: never outranks a real liquidation
		Data: badDebtData{
			pool:          pool,
			backstop:      a.backstop,
			backstopToken: a.backstopToken,
			liabAssets:    liabAssets,
			spot:          pd,
		},
	}}
}

// debtLeg is one bid asset's underlying owed: debt is the ceil'd dToken ×
// d_rate amount, need pads it by drawBufferBps.
type debtLeg struct {
	asset string
	debt  int64
	need  int64
}

// debtNeeds converts an auction's bid map (dToken amounts at 100% scale) into
// underlying-token repay requirements per asset, rounded UP and padded by
// drawBufferBps. Erroring (rather than skipping a leg) is deliberate: a bid
// leg we cannot price or convert must abort the whole task, or the atomic
// repay would come up short and revert the fill.
func debtNeeds(pool *core.PoolState, auction *core.Auction) ([]debtLeg, error) {
	legs := make([]debtLeg, 0, len(auction.Bid))
	for asset, amt := range auction.Bid {
		if amt == nil || amt.Sign() <= 0 {
			continue
		}
		r, ok := pool.Reserves[asset]
		if !ok {
			return nil, fmt.Errorf("bid asset %s not in pool reserves", shortAddr(asset))
		}
		// User-liquidation and bad-debt bids are both dToken amounts
		// (docs/FACTS.md "Auction asset flows"). Interest auctions never get
		// here: their bid is backstop LP, and they are detection-only.
		rate := r.DRate
		if rate <= 0 {
			return nil, fmt.Errorf("bid asset %s has no d_rate", shortAddr(asset))
		}
		f := new(big.Float).SetInt(amt)
		f.Mul(f, big.NewFloat(rate))
		i, acc := f.Int(nil)
		if acc == big.Below { // truncated down — round the owed amount UP
			i.Add(i, big.NewInt(1))
		}
		if !i.IsInt64() {
			return nil, fmt.Errorf("bid asset %s underlying debt exceeds int64", shortAddr(asset))
		}
		debt := i.Int64()
		if debt <= 0 {
			continue
		}
		legs = append(legs, debtLeg{asset: asset, debt: debt, need: padBps(debt, drawBufferBps)})
	}
	if len(legs) == 0 {
		return nil, fmt.Errorf("auction has no priceable bid legs")
	}
	sort.Slice(legs, func(i, j int) bool { return legs[i].asset < legs[j].asset })
	return legs, nil
}

// padBps returns v grown by bps basis points, rounding up, saturating at
// MaxInt64 (a saturated draw then fails at the vault instead of wrapping).
func padBps(v int64, bps int) int64 {
	if v <= 0 || bps <= 0 {
		return v
	}
	p := new(big.Int).Mul(big.NewInt(v), big.NewInt(int64(bps)))
	p.Add(p, big.NewInt(9999))
	p.Div(p, big.NewInt(10000))
	p.Add(p, big.NewInt(v))
	if !p.IsInt64() {
		return math.MaxInt64
	}
	return p.Int64()
}

// acqPlan is one pre-fill acquisition: buy ask of asset (need, plus
// nativeFeePad for the native asset) for at most maxIn USDC so the atomic
// submit can repay a non-USDC debt leg.
type acqPlan struct {
	asset string
	debt  int64
	ask   int64
	maxIn int64
}

// Execute runs the full verified liquidation sequence for task.Target:
//
//  1. create the user-liquidation auction if absent, sized by contract
//     simulation (FindLiquidationPercent)
//  2. wait until the verified two-phase price curve makes lot/bid ≥ MinProfit
//     (re-entered every keeper cycle until profitable — no capital is drawn
//     while waiting)
//  3. draw exactly the capital the fill needs: each settle-asset debt leg in
//     underlying + drawBufferBps, plus the oracle-capped router quote for
//     every non-USDC debt leg (routes verified BEFORE the draw)
//  4. acquire non-USDC debt assets via exact-out swaps
//  5. ONE atomic submit: fill 100% + repay every leg + withdraw all seized
//     collateral (blend.FillAndUnwind)
//  6. swap seized collateral and acquisition leftovers back to USDC
//  7. return measured proceeds (keeper's USDC delta over its own pre-draw
//     float) to the vault
//
// Failure discipline: any deterministic failure after the draw rolls back —
// re-sell what was acquired, return every USDC above the float. An AMBIGUOUS
// fill (tx may still land) returns nothing this cycle; the next cycle's
// stale-draw recovery reconciles from chain state, which a double-spending
// rollback here would corrupt.
func (a *Adapter) Execute(rpc *soroban.Client, kp *keypair.Full, task adapters.Task, vc adapters.VaultClient) (*adapters.Result, error) {
	start := time.Now()
	if task.Type == "bad_debt" {
		return a.executeBadDebt(rpc, kp, task, start)
	}
	td, ok := task.Data.(taskData)
	if !ok || td.pool == nil {
		return &adapters.Result{Note: "missing pool snapshot"}, nil
	}
	pool := td.pool
	user := task.Target

	if a.cfg.Monitor {
		return &adapters.Result{Note: "monitor-only pool — execution disabled"}, nil
	}

	// An auction for this user may already be running — ours from an earlier
	// cycle, or another keeper's. Creating unconditionally would just panic
	// AuctionInProgress (1212), so look first and only create when absent.
	auction, err := coreGetAuction(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, user)
	if err != nil {
		return nil, fmt.Errorf("get auction: %w", err)
	}
	if auction != nil {
		ledger, lerr := latestLedger(rpc)
		if lerr != nil {
			return nil, fmt.Errorf("latest ledger: %w", lerr)
		}
		elapsed := ledger - auction.StartBlock
		// Past t=400 the scaled bid is EMPTY (auction.rs:217-221): our
		// repay-carrying atomic fill would revert on the repay leg, and
		// re-trying it every cycle would churn vault draws. The pool's own
		// remedy is stale deletion + re-creation (auction.rs:97-109), allowed
		// from t=500. Between 400 and 500 nothing safe can be done — wait.
		switch {
		case elapsed >= core.StaleAuctionBlocks:
			if derr := coreDeleteStale(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr, user,
				core.AuctionUserLiquidation); derr != nil {
				return nil, fmt.Errorf("delete stale auction (age %d): %w", elapsed, derr)
			}
			auction = nil // fall through to re-creation below
		case elapsed >= 400:
			return &adapters.Result{Block: ledger, Note: fmt.Sprintf(
				"auction past the price curve (age %d, scaled bid empty) but not yet stale-deletable — waiting for t=500", elapsed)}, nil
		}
	}
	if auction == nil {
		// Blend rejects a liquidation whose post-liq health factor misses the
		// [1.03, 1.15] band, so the percentage is chosen by simulating against
		// the pool rather than hardcoded (a fixed 50% fails with
		// InvalidLiqTooSmall on most positions).
		pct, perr := coreFindLiqPct(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, user, td.bidAssets, td.lotAssets)
		if perr != nil {
			return &adapters.Result{Note: fmt.Sprintf("no acceptable liquidation size: %v", perr)}, nil
		}
		if err := coreCreateAuction(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr, user, pct,
			td.bidAssets, td.lotAssets); err != nil {
			return nil, fmt.Errorf("create auction: %w", err)
		}
		auction, err = coreGetAuction(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, user)
		if err != nil {
			return nil, fmt.Errorf("get auction: %w", err)
		}
		if auction == nil {
			return &adapters.Result{Note: "auction created but not readable"}, nil
		}
		logAuction("auction created", a.cfg.PoolAddr, user, pct, auction.StartBlock)
	}

	ledger, err := latestLedger(rpc)
	if err != nil {
		return nil, fmt.Errorf("latest ledger: %w", err)
	}
	ratio := core.Profitability(*auction, pool, ledger)
	if ratio < a.cfg.MinProfit {
		return &adapters.Result{Block: ledger, Note: fmt.Sprintf("not profitable (%.4f < %.4f)", ratio, a.cfg.MinProfit)}, nil
	}

	// Cross-USDC pools never reach here: GetTasks emits no tasks for them
	// (see the conversion note in GetTasks). Belt-and-braces so a future
	// caller cannot route capital through an unconverted settle asset.
	if a.cfg.PoolUsdc != "" && a.cfg.PoolUsdc != a.cfg.UsdcAddr {
		return &adapters.Result{Block: ledger,
			Note: "pool settles a different USDC — execution disabled (see FACTS.md 'USDC asset bridging')"}, nil
	}
	settleAsset := a.cfg.UsdcAddr
	if settleAsset == "" {
		return &adapters.Result{Block: ledger, Note: "no USDC/settle asset configured — cannot size a draw"}, nil
	}
	// The vault draws/returns are 7-decimal stroops; a settle reserve with
	// different decimals would make every amount below unit-wrong.
	if r, ok := pool.Reserves[settleAsset]; !ok || r.Decimals != 7 {
		return &adapters.Result{Block: ledger, Note: "settle asset missing from pool or not 7-decimal — skipping"}, nil
	}

	legs, err := debtNeeds(pool, auction)
	if err != nil {
		return &adapters.Result{Block: ledger, Note: fmt.Sprintf("cannot size debt legs: %v", err)}, nil
	}

	// Draw estimator: settle-asset legs cost their padded underlying amount
	// 1:1. Every non-USDC leg is route-checked NOW — required router input,
	// capped at oracle value + SlippageBps — so a missing route or an
	// off-oracle price skips the task BEFORE any capital moves. The bid map
	// is at 100% scale, an upper bound on the assumed debt at any fill time
	// (the bid modifier only shrinks it after t=200), so the draw always
	// covers the fill; the pool and the exact-out swaps refund what goes
	// unused and it all returns with the proceeds.
	var drawAmt int64
	var plans []acqPlan
	for _, leg := range legs {
		if leg.asset == settleAsset {
			drawAmt += leg.need
			continue
		}
		if a.dex == nil {
			return &adapters.Result{Block: ledger,
				Note: fmt.Sprintf("debt leg in %s but no DEX configured — skipping", shortAddr(leg.asset))}, nil
		}
		// Balance deltas of the native asset are depressed by the measuring
		// tx's own fee — ask for headroom so the NET delta still covers the
		// debt (see nativeFeePad).
		ask := leg.need
		if a.cfg.NativeSAC != "" && leg.asset == a.cfg.NativeSAC {
			ask += nativeFeePad
		}
		requiredIn, qerr := a.dex.QuoteIn(settleAsset, leg.asset, ask)
		if qerr != nil {
			return &adapters.Result{Block: ledger,
				Note: fmt.Sprintf("no route to acquire debt asset %s: %v — skipping", shortAddr(leg.asset), qerr)}, nil
		}
		oracleCost := oracleValueUSDC(pool, leg.asset, ask)
		if oracleCost <= 0 {
			return &adapters.Result{Block: ledger,
				Note: fmt.Sprintf("debt asset %s unpriced by oracle — skipping", shortAddr(leg.asset))}, nil
		}
		maxIn := padBps(oracleCost, a.cfg.SlippageBps)
		if requiredIn > maxIn {
			return &adapters.Result{Block: ledger,
				Note: fmt.Sprintf("acquiring %s costs %d USDC > oracle cap %d — skipping", shortAddr(leg.asset), requiredIn, maxIn)}, nil
		}
		plans = append(plans, acqPlan{asset: leg.asset, debt: leg.debt, ask: ask, maxIn: maxIn})
		drawAmt += maxIn
	}
	if drawAmt <= 0 {
		return &adapters.Result{Block: ledger, Note: "bid not priceable in underlying — skipping"}, nil
	}

	res := &adapters.Result{Block: ledger, Drew: drawAmt}

	// The keeper's own USDC before any capital moves. Everything it holds
	// above this line at the end is what this operation produced — proceeds
	// are measured against it, never inferred from auction amounts.
	floatBefore, err := tokenBalance(rpc, a.cfg.Passphrase, a.cfg.UsdcAddr, kp.Address())
	if err != nil {
		return &adapters.Result{Block: ledger, Note: fmt.Sprintf("pre-draw balance read failed: %v", err)}, nil
	}

	// Baseline balances for every non-USDC asset this operation may touch —
	// seized lot assets and acquired debt assets — taken BEFORE any capital
	// moves. Swaps later act only on deltas above these baselines (critical
	// for native XLM, where the keeper's fee balance must never be swapped
	// away). An unreadable baseline aborts pre-draw: a zero default would
	// turn the keeper's whole pre-held balance into "seized" funds.
	touched := make([]string, 0, len(auction.Lot)+len(plans))
	seen := map[string]bool{}
	for asset := range auction.Lot {
		if asset != a.cfg.UsdcAddr && !seen[asset] {
			touched = append(touched, asset)
			seen[asset] = true
		}
	}
	for _, p := range plans {
		if !seen[p.asset] {
			touched = append(touched, p.asset)
			seen[p.asset] = true
		}
	}
	sort.Strings(touched) // deterministic snapshot/sweep order
	baseline := make(map[string]int64, len(touched))
	for _, asset := range touched {
		bal, berr := tokenBalance(rpc, a.cfg.Passphrase, asset, kp.Address())
		if berr != nil {
			return &adapters.Result{Block: ledger,
				Note: fmt.Sprintf("baseline balance read failed for %s: %v — skipping", shortAddr(asset), berr)}, nil
		}
		baseline[asset] = bal
	}
	lotAssets := make([]string, 0, len(auction.Lot))
	for asset := range auction.Lot {
		lotAssets = append(lotAssets, asset)
	}
	sort.Strings(lotAssets) // deterministic request order

	drawStart := time.Now()
	if err := vc.Draw(drawAmt); err != nil {
		// Nothing was acquired yet. If the draw itself is ambiguous, the next
		// cycle's stale-draw recovery reconciles against get_keeper_draw.
		return nil, fmt.Errorf("vault draw: %w", err)
	}

	// Acquire every non-USDC debt leg. From here on, any deterministic
	// failure must put the vault back together: re-sell measured acquisitions
	// and return every USDC above the keeper's own float.
	repays := make([]core.RepayLeg, 0, len(legs))
	for _, leg := range legs {
		if leg.asset == settleAsset {
			repays = append(repays, core.RepayLeg{Asset: settleAsset, Amount: leg.need})
		}
	}
	for _, p := range plans {
		swapRes, serr := a.dex.ConvertExactOut(kp, settleAsset, p.asset, p.ask, p.maxIn)
		var got int64
		switch {
		case serr == nil:
			got = swapRes.OutputAmount
		case soroban.IsTxStatusUnknown(serr):
			// The swap tx may still land. Rolling back now could return USDC
			// the late-landing swap will then spend from the keeper's own
			// float, stranding the bought asset. Re-poll the hash across the
			// tx's remaining timebound life first.
			_, landed, failedDet := ambiguity(rpc, serr)
			switch {
			case landed:
				// Measure what actually arrived instead of trusting the plan.
				bal, berr := tokenBalance(rpc, a.cfg.Passphrase, p.asset, kp.Address())
				if berr != nil {
					res.Note = fmt.Sprintf("acquisition %s landed but balance unreadable: %v — holding, next cycle reconciles", shortAddr(p.asset), berr)
					res.Latency = time.Since(start)
					return res, nil
				}
				got = bal - baseline[p.asset]
			case failedDet:
				a.rollback(rpc, kp, vc, pool, res, floatBefore, baseline, plans,
					fmt.Sprintf("debt acquisition %s definitively failed: %v", shortAddr(p.asset), serr))
				return res, nil
			default:
				res.Note = fmt.Sprintf("acquisition %s outcome UNKNOWN (tx may land): %v — holding funds, next cycle reconciles", shortAddr(p.asset), serr)
				res.Latency = time.Since(start)
				return res, nil
			}
		default:
			a.rollback(rpc, kp, vc, pool, res, floatBefore, baseline, plans,
				fmt.Sprintf("debt acquisition %s failed: %v", shortAddr(p.asset), serr))
			return res, nil
		}
		if got < p.debt {
			a.rollback(rpc, kp, vc, pool, res, floatBefore, baseline, plans,
				fmt.Sprintf("acquired %d of %s < debt %d", got, shortAddr(p.asset), p.debt))
			return res, nil
		}
		// Repay everything acquired: the pool refunds the excess over the
		// true debt, and the refund is swept back to USDC below.
		repays = append(repays, core.RepayLeg{Asset: p.asset, Amount: got})
	}

	// Fill + repay every leg + withdraw all collateral in one atomic submit.
	fillTx, fillErr := coreFillAndUnwind(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr,
		user, core.FullFillPct, repays, lotAssets)
	var failNote string
	switch {
	case fillErr == nil:
		res.Success = true
		res.TxHash = fillTx
		res.ResponseTimeMs = time.Since(drawStart).Milliseconds()
		a.sweepToUSDC(kp, rpc, pool, touched, baseline)
	case errors.Is(fillErr, core.ErrAlreadyFilled):
		// Another keeper consumed the auction — or our own earlier ambiguous
		// submit actually landed and the retry found the auction gone. Either
		// way the truth is in the balances: sweep every touched asset's delta
		// and return the measured USDC.
		failNote = "auction no longer fillable (raced, or an earlier ambiguous submit landed)"
		a.sweepToUSDC(kp, rpc, pool, touched, baseline)
	case soroban.IsTxStatusUnknown(fillErr):
		// The submit may still land. Selling acquired assets or returning
		// USDC now could double-move funds the landed fill will also move —
		// so first re-poll the hash across the tx's remaining timebound life.
		hash, landed, failedDet := ambiguity(rpc, fillErr)
		switch {
		case landed:
			res.Success = true
			res.TxHash = hash
			res.ResponseTimeMs = time.Since(drawStart).Milliseconds()
			a.sweepToUSDC(kp, rpc, pool, touched, baseline)
		case failedDet:
			a.rollback(rpc, kp, vc, pool, res, floatBefore, baseline, plans,
				fmt.Sprintf("fill definitively failed: %v", fillErr))
			return res, nil
		default:
			// Still unknown after the timebound window: sustained RPC outage.
			// Hold everything; the next cycle's stale-draw recovery re-reads
			// chain state and finishes the unwind idempotently.
			res.Note = fmt.Sprintf("fill outcome UNKNOWN (tx may land): %v — holding funds, next cycle reconciles", fillErr)
			res.Latency = time.Since(start)
			return res, nil
		}
	default:
		// Deterministic failure — the fill did not happen. Return the draw.
		a.rollback(rpc, kp, vc, pool, res, floatBefore, baseline, plans,
			fmt.Sprintf("fill failed: %v", fillErr))
		return res, nil
	}

	// Measured proceeds: whatever USDC the keeper now holds above its own
	// pre-draw float. Covers the drawn capital that came back plus realized
	// profit, and stays honest when a swap failed (collateral still held).
	usdcNow, balErr := tokenBalance(rpc, a.cfg.Passphrase, a.cfg.UsdcAddr, kp.Address())
	if balErr != nil {
		res.Note = fmt.Sprintf("post-fill balance read failed, proceeds unknown: %v", balErr)
	} else {
		res.Proceeds = usdcNow - floatBefore
		if res.Proceeds < 0 {
			res.Proceeds = 0
		}
		res.Profit = res.Proceeds - drawAmt
		if res.Profit < 0 {
			res.Profit = 0
		}
		if res.Success && res.Proceeds == 0 {
			res.Note = "zero returnable proceeds — outstanding draw at slash risk"
		}
	}

	// Return only when capital was actually drawn AND there is something to send.
	// A return failure is non-fatal: the fill already happened on-chain, so we
	// keep the result (and its accounting) and surface the outstanding-capital
	// risk via Note rather than discarding a realized fill.
	if drawAmt > 0 && res.Proceeds > 0 {
		if err := vc.ReturnProceeds(res.Proceeds, res.ResponseTimeMs); err != nil {
			res.Note = fmt.Sprintf("return proceeds failed (capital outstanding): %v", err)
		}
	}
	if failNote != "" {
		if res.Note != "" {
			res.Note = failNote + "; " + res.Note
		} else {
			res.Note = fmt.Sprintf("%s; returned %d of %d drawn", failNote, res.Proceeds, drawAmt)
		}
	}
	res.Latency = time.Since(start)
	return res, nil
}

// executeBadDebt runs the bad-debt fill path (docs/FACTS.md Decisions,
// "fill-and-hold"):
//
//  1. create the backstop's bad-debt auction if absent — permissionless
//     new_auction(1, backstop, [settle asset], [backstop LP token], 100).
//     Only settle-asset (vault USDC) debt legs are supported; non-USDC bad
//     debt is skipped with a note (the lot may be auctioned by others).
//  2. wait on the verified price curve until (LP lot value after haircut) /
//     (assumed debt cost) ≥ MinProfit — BadDebtProfitability, using the
//     backstop's OWN token_spot_price for the lot
//  3. ONE atomic submit: fill + repay from the KEEPER FLOAT (never a vault
//     draw — the LP received cannot be liquidated to vault USDC before
//     mainnet, and vault draws must return within the registry slash
//     timeout). Spend is capped by BadDebtMaxSpend and the available float;
//     if the full fill doesn't fit, the fill percent is scaled down.
//  4. past t=400 the scaled bid is empty: capture the lot for free (bare
//     fill, no repay, no spend) instead of deleting the stale auction.
//  5. the received LP is HELD at the keeper and reported at spot and at the
//     configured haircut every scan; unwinding it (single-sided comet exit)
//     is deferred until the comet's USDC leg is the vault asset (mainnet).
//
// No vault capital is at risk on any path here, so failures need no
// rollback: a deterministic failure spent nothing (atomic submit), and an
// ambiguous one is resolved by hash or left to the next cycle's re-scan.
func (a *Adapter) executeBadDebt(rpc *soroban.Client, kp *keypair.Full, task adapters.Task, start time.Time) (*adapters.Result, error) {
	td, ok := task.Data.(badDebtData)
	if !ok || td.pool == nil || td.spot == nil {
		return &adapters.Result{Note: "missing bad-debt snapshot"}, nil
	}
	if a.cfg.Monitor {
		return &adapters.Result{Note: "monitor-only pool — execution disabled"}, nil
	}
	if a.cfg.BadDebtMaxSpend <= 0 {
		return &adapters.Result{Note: "bad-debt fills disabled (BAD_DEBT_MAX_SPEND=0)"}, nil
	}
	settle := a.cfg.UsdcAddr
	if settle == "" {
		return &adapters.Result{Note: "no USDC/settle asset configured — cannot size a bad-debt repay"}, nil
	}
	if a.cfg.PoolUsdc != "" && a.cfg.PoolUsdc != settle {
		return &adapters.Result{Note: "pool settles a different USDC — execution disabled (see FACTS.md 'USDC asset bridging')"}, nil
	}

	auction, err := coreGetAuctionByType(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, td.backstop, core.AuctionBadDebt)
	if err != nil {
		return nil, fmt.Errorf("get bad-debt auction: %w", err)
	}
	if auction == nil {
		hasSettleLeg := false
		for _, asset := range td.liabAssets {
			if asset == settle {
				hasSettleLeg = true
				break
			}
		}
		if !hasSettleLeg {
			return &adapters.Result{Note: "backstop bad debt has no settle-asset leg — non-USDC bad debt is not fillable by this keeper (skipped)"}, nil
		}
		// Bid ONLY the settle-asset leg: the contract allows auctioning a
		// subset of the backstop's debt, and a leg we cannot repay would make
		// the whole auction unfillable for us.
		if cerr := coreCreateBadDebt(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr,
			td.backstop, td.backstopToken, []string{settle}); cerr != nil {
			return nil, fmt.Errorf("create bad-debt auction: %w", cerr)
		}
		auction, err = coreGetAuctionByType(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, td.backstop, core.AuctionBadDebt)
		if err != nil {
			return nil, fmt.Errorf("get bad-debt auction: %w", err)
		}
		if auction == nil {
			return &adapters.Result{Note: "bad-debt auction created but not readable"}, nil
		}
		logAuction("bad-debt auction created", a.cfg.PoolAddr, td.backstop, 100, auction.StartBlock)
	}

	// Every bid leg must be repayable from the float, i.e. the settle asset.
	for asset := range auction.Bid {
		if asset != settle {
			return &adapters.Result{Note: fmt.Sprintf(
				"bad-debt auction bids %s — only settle-asset bad debt is supported (skipped)", shortAddr(asset))}, nil
		}
	}

	ledger, err := latestLedger(rpc)
	if err != nil {
		return nil, fmt.Errorf("latest ledger: %w", err)
	}
	res := &adapters.Result{Block: ledger}
	elapsed := ledger - auction.StartBlock

	// Balance baselines BEFORE acting; LP gain and USDC spend are MEASURED,
	// never inferred from auction figures.
	lpBefore, err := tokenBalance(rpc, a.cfg.Passphrase, td.backstopToken, kp.Address())
	if err != nil {
		res.Note = fmt.Sprintf("LP baseline read failed: %v — skipping", err)
		return res, nil
	}
	floatBefore, err := tokenBalance(rpc, a.cfg.Passphrase, settle, kp.Address())
	if err != nil {
		res.Note = fmt.Sprintf("float balance read failed: %v — skipping", err)
		return res, nil
	}

	fillStart := time.Now()
	var fillTx string
	var fillErr error
	var pct int64 = 100
	var spend int64
	switch {
	case elapsed >= 400:
		// Past the curve the scaled bid is EMPTY: the lot is free. Unlike
		// user liquidations (where the repay-carrying fill reverts and we
		// delete + re-create), the correct bad-debt move is to just take it.
		fillTx, fillErr = coreFillBadDebtFree(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr, td.backstop)
	case elapsed >= 400-badDebtBoundaryLedgers:
		// t=400 boundary: a repay-carrying fill built now can land after the
		// bid scales to empty, where the repay leg has no dTokens to burn and
		// the whole submit reverts InvalidDTokenBurnAmount (#1219) — observed
		// live on the sandbox (docs/evidence/c-bad-debt.md). Waiting the last
		// ledgers out costs nothing: the free capture is strictly better.
		res.Note = fmt.Sprintf("at the t=400 boundary (elapsed %d) — waiting for the free-capture window", elapsed)
		res.Latency = time.Since(start)
		return res, nil
	default:
		ratio := core.BadDebtProfitability(*auction, td.pool, td.backstopToken, td.spot.SpotPrice, a.cfg.BadDebtHaircutBps, ledger)
		if ratio < a.cfg.MinProfit {
			res.Note = fmt.Sprintf("bad debt not profitable at %d bps haircut (%.4f < %.4f)", a.cfg.BadDebtHaircutBps, ratio, a.cfg.MinProfit)
			return res, nil
		}
		legs, lerr := debtNeeds(td.pool, auction)
		if lerr != nil {
			res.Note = fmt.Sprintf("cannot size bad-debt legs: %v", lerr)
			return res, nil
		}
		if len(legs) != 1 || legs[0].asset != settle {
			res.Note = "bad-debt bid legs not settle-only after sizing — skipping"
			return res, nil
		}
		// The bid map is at 100% time scale; the modifier only shrinks it
		// after t=200, so scaling by the CURRENT bidPct still upper-bounds the
		// debt assumed at landing (drawBufferBps covers d_rate accrual and the
		// contract's double round-up).
		_, _, bidPct := core.PhaseAt(elapsed)
		scaledNeed := int64(math.Ceil(float64(legs[0].need) * bidPct))
		if scaledNeed <= 0 {
			res.Note = "scaled bad-debt need is zero before t=400 — skipping cycle"
			return res, nil
		}
		spendCap := a.cfg.BadDebtMaxSpend
		if floatBefore < spendCap {
			spendCap = floatBefore
		}
		if scaledNeed > spendCap {
			pct = spendCap * 100 / scaledNeed
			if pct < 1 {
				res.Note = fmt.Sprintf("keeper float cannot cover bad-debt fill: need %d, spendable %d (float %d, cap %d)",
					scaledNeed, spendCap, floatBefore, a.cfg.BadDebtMaxSpend)
				return res, nil
			}
		}
		spend = (scaledNeed*pct + 99) / 100
		if spend > spendCap {
			spend = spendCap
		}
		fillTx, fillErr = coreFillBadDebt(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr,
			td.backstop, pct, []core.RepayLeg{{Asset: settle, Amount: spend}})
	}

	switch {
	case fillErr == nil:
		res.Success = true
		res.TxHash = fillTx
	case errors.Is(fillErr, core.ErrAlreadyFilled):
		res.Note = "bad-debt auction no longer fillable (raced, or an earlier ambiguous submit landed)"
		res.Latency = time.Since(start)
		return res, nil
	case soroban.IsTxStatusUnknown(fillErr):
		hash, landed, failedDet := ambiguity(rpc, fillErr)
		switch {
		case landed:
			res.Success = true
			res.TxHash = hash
		case failedDet:
			res.Note = fmt.Sprintf("bad-debt fill definitively failed: %v", fillErr)
			res.Latency = time.Since(start)
			return res, nil
		default:
			// Only keeper float is at stake (no draw, no acquisitions): hold
			// and let the next cycle's re-scan see whatever actually landed.
			res.Note = fmt.Sprintf("bad-debt fill outcome UNKNOWN (tx may land): %v — keeper float at stake, re-checking next cycle", fillErr)
			res.Latency = time.Since(start)
			return res, nil
		}
	default:
		res.Note = fmt.Sprintf("bad-debt fill failed: %v", fillErr)
		res.Latency = time.Since(start)
		return res, nil
	}
	res.ResponseTimeMs = time.Since(fillStart).Milliseconds()

	// Measure what actually moved. Vault accounting stays zero on purpose:
	// no capital was drawn and no USDC proceeds exist — the LP is held.
	lpGained, spent := int64(0), int64(0)
	if lpNow, berr := tokenBalance(rpc, a.cfg.Passphrase, td.backstopToken, kp.Address()); berr == nil {
		lpGained = lpNow - lpBefore
	} else {
		res.Note = fmt.Sprintf("fill landed but LP balance unreadable: %v", berr)
	}
	if floatNow, berr := tokenBalance(rpc, a.cfg.Passphrase, settle, kp.Address()); berr == nil {
		spent = floatBefore - floatNow
	}
	lp := big.NewInt(lpGained)
	note := fmt.Sprintf("bad-debt fill %d%%: spent %d USDC keeper float, received %d backstop LP ≈ $%.4f spot / $%.4f after %d bps haircut — LP held (fill-and-hold, unwind deferred: FACTS.md Decisions)",
		pct, spent, lpGained, core.BackstopLPValueUSD(lp, td.spot.SpotPrice, 0),
		core.BackstopLPValueUSD(lp, td.spot.SpotPrice, a.cfg.BadDebtHaircutBps), a.cfg.BadDebtHaircutBps)
	if elapsed >= 400 {
		note += " (past-curve free capture: empty bid, no repay)"
	}
	if res.Note != "" {
		res.Note = note + "; " + res.Note
	} else {
		res.Note = note
	}
	res.Latency = time.Since(start)
	return res, nil
}

// rollback is the deterministic-failure recovery path after a draw: re-sell
// whatever the acquisition swaps actually delivered (measured deltas over the
// pre-draw baseline), then return every USDC above the keeper's own float to
// the vault. Sets res.Note with what happened; anything unrecoverable stays
// with the keeper and the next cycle's stale-draw recovery keeps trying.
func (a *Adapter) rollback(rpc *soroban.Client, kp *keypair.Full, vc adapters.VaultClient, pool *core.PoolState,
	res *adapters.Result, floatBefore int64, baseline map[string]int64, plans []acqPlan, why string) {
	acqAssets := make([]string, 0, len(plans))
	for _, p := range plans {
		acqAssets = append(acqAssets, p.asset)
	}
	a.sweepToUSDC(kp, rpc, pool, acqAssets, baseline)

	usdcNow, err := tokenBalance(rpc, a.cfg.Passphrase, a.cfg.UsdcAddr, kp.Address())
	if err != nil {
		res.Note = fmt.Sprintf("%s; rollback balance read failed: %v — draw outstanding, next cycle recovers", why, err)
		return
	}
	returnable := usdcNow - floatBefore
	if returnable <= 0 {
		res.Note = fmt.Sprintf("%s; nothing returnable above keeper float — draw outstanding, next cycle recovers", why)
		return
	}
	if err := vc.ReturnProceeds(returnable, 0); err != nil {
		res.Note = fmt.Sprintf("%s; rollback return failed: %v — draw outstanding, next cycle recovers", why, err)
		return
	}
	res.Proceeds = returnable
	shortfall := res.Drew - returnable
	res.Note = fmt.Sprintf("%s; rolled back: returned %d of %d drawn (shortfall %d)", why, returnable, res.Drew, shortfall)
}

// EstimateCapital is best-effort: the bid is only known after the auction is
// created, so Execute sizes the draw itself. Returns 0 here.
func (a *Adapter) EstimateCapital(task adapters.Task) (int64, error) {
	return 0, nil
}

// sweepToUSDC converts every listed asset's balance ABOVE its pre-draw
// baseline into USDC. Amounts come from measured balance deltas, never from
// auction figures — the keeper may hold the same asset for other reasons, and
// for native XLM its fee balance must not be swapped away. Assets whose swap
// fails are simply held (the next cycle's stale-draw recovery retries);
// nothing is booked that did not happen.
func (a *Adapter) sweepToUSDC(kp *keypair.Full, rpc *soroban.Client, pool *core.PoolState, assets []string, baseline map[string]int64) {
	if a.dex == nil {
		logSwap("no DEX configured — seized collateral held", "", 0, nil)
		return
	}
	for _, asset := range assets {
		if asset == a.cfg.UsdcAddr {
			continue // already the settle asset; counted by the balance delta
		}
		now, err := tokenBalance(rpc, a.cfg.Passphrase, asset, kp.Address())
		if err != nil {
			// Silently skipping here once cost a full post-mortem: with no
			// swap attempted and no error surfaced, the keeper looked like it
			// had simply chosen not to sell.
			logSwap("post-fill balance read failed — collateral held", asset, 0, err)
			continue
		}
		gained := now - baseline[asset]
		if gained <= 0 {
			logSwap("no measurable collateral gain — nothing to swap", asset, gained, nil)
			continue
		}
		ref := oracleValueUSDC(pool, asset, gained)
		swapRes, err := a.dex.SwapToUSDC(kp, asset, gained, ref)
		if err != nil {
			logSwap("collateral swap refused — holding asset", asset, gained, err)
			continue
		}
		logSwap("collateral swapped to USDC", asset, swapRes.OutputAmount, nil)
	}
}

// logSwap reports collateral-conversion outcomes. Package main installs a real
// logger; the default keeps the adapter importable without it.
var logSwap = func(msg, asset string, amount int64, err error) {}

// priorityFromHF maps a health factor to a task priority: the more underwater,
// the more urgent.
func priorityFromHF(hf float64) int {
	switch {
	case hf < 0.5:
		return 10
	case hf < 0.8:
		return 7
	case hf < 0.95:
		return 4
	default:
		return 1
	}
}

// oracleValueUSDC returns the Blend-oracle-implied USDC value in 7-decimal
// stroops for amt raw units of asset, or 0 when no price is available. amt is
// scaled by the asset's own decimals before pricing — a 6-decimal token's raw
// amount is not a 7-decimal stroop count.
func oracleValueUSDC(pool *core.PoolState, asset string, amt int64) int64 {
	if pool == nil {
		return 0
	}
	r, ok := pool.Reserves[asset]
	if !ok || r.OraclePrice <= 0 {
		return 0
	}
	whole := float64(amt) / r.TokenScalar()
	return int64(whole * r.OraclePrice * 1e7)
}

// logAuction is a hook for auction lifecycle visibility; the keeper's logger
// lives in package main, so this stays a no-op seam the adapter can call
// without importing it.
var logAuction = func(msg, pool, user string, pct int, startBlock int64) {}

// SetLoggers installs the keeper's logger for auction and swap events. Called
// once at startup; adapters are constructed before this, so the hooks default
// to no-ops rather than nil checks at every call site.
func SetLoggers(auctionFn func(msg, pool, user string, pct int, startBlock int64), swapFn func(msg, asset string, amount int64, err error)) {
	if auctionFn != nil {
		logAuction = auctionFn
	}
	if swapFn != nil {
		logSwap = swapFn
	}
}
