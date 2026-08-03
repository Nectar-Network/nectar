// Package blend adapts the Blend liquidation protocol to the generic
// adapters.ProtocolAdapter interface. It wraps the lower-level
// github.com/nectar-network/keeper/blend package (pool/auction/position logic)
// and the dex package (collateral→USDC conversion), turning underwater
// positions into Tasks and filling their auctions in Execute. The underlying
// blend package is left intact (and fully tested); this is a thin translation
// layer, which is what gets extracted into the keeper-sdk in Phase 4.
package blend

import (
	"errors"
	"fmt"
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
	// Monitor makes the adapter observe-only: it scans the pool, computes
	// health factors and publishes a ScanReport, but never emits tasks (so no
	// capital can move toward this pool). Used for pools whose settlement
	// asset has no verified conversion route to the vault's USDC.
	Monitor bool
	// PoolUsdc is the pool's own USDC contract when it differs from the
	// vault's UsdcAddr. Fills then require a DEX conversion vault-USDC ->
	// pool-USDC (entry) under a par-anchored slippage guard; an off-parity
	// route blocks execution BEFORE any vault draw. Empty means the pool
	// settles in the vault's USDC directly.
	PoolUsdc string
	// EventLookback is how many ledgers of pool events to scan for position
	// discovery. 0 means the default of 1000 (~83 min at 5s ledgers).
	EventLookback int64
}

// Adapter implements adapters.ProtocolAdapter for one Blend pool. Run several
// instances to monitor several pools — Name() is unique per pool.
type Adapter struct {
	cfg      Config
	dex      *dex.SwapClient
	lastScan *adapters.ScanReport
}

// NewAdapter builds a Blend adapter. dexc may be nil to disable collateral
// swapping (proceeds are then only the USDC directly present in the lot).
func NewAdapter(cfg Config, dexc *dex.SwapClient) *Adapter {
	return &Adapter{cfg: cfg, dex: dexc}
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
	a.lastScan = report
	return tasks, nil
}

// Execute creates and fills the user-liquidation auction for task.Target, swaps
// the seized collateral to USDC, and returns the real proceeds via the vault.
// Proceeds are measured, never synthesized; capital is only returned when it was
// actually drawn (the vault's drawn==0 path would otherwise book output as
// cost-free profit).
func (a *Adapter) Execute(rpc *soroban.Client, kp *keypair.Full, task adapters.Task, vc adapters.VaultClient) (*adapters.Result, error) {
	start := time.Now()
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
	auction, err := core.GetAuction(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, user)
	if err != nil {
		return nil, fmt.Errorf("get auction: %w", err)
	}
	if auction == nil {
		// Blend rejects a liquidation whose post-liq health factor misses the
		// [1.03, 1.15] band, so the percentage is chosen by simulating against
		// the pool rather than hardcoded (a fixed 50% fails with
		// InvalidLiqTooSmall on most positions).
		pct, perr := core.FindLiquidationPercent(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, user, td.bidAssets, td.lotAssets)
		if perr != nil {
			return &adapters.Result{Note: fmt.Sprintf("no acceptable liquidation size: %v", perr)}, nil
		}
		if err := core.CreateAuction(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr, user, pct,
			td.bidAssets, td.lotAssets); err != nil {
			return nil, fmt.Errorf("create auction: %w", err)
		}
		auction, err = core.GetAuction(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, user)
		if err != nil {
			return nil, fmt.Errorf("get auction: %w", err)
		}
		if auction == nil {
			return &adapters.Result{Note: "auction created but not readable"}, nil
		}
		logAuction("auction created", a.cfg.PoolAddr, user, pct, auction.StartBlock)
	}

	ledger, err := rpc.LatestLedger()
	if err != nil {
		return nil, fmt.Errorf("latest ledger: %w", err)
	}
	ratio := core.Profitability(*auction, pool, ledger)
	if ratio < a.cfg.MinProfit {
		return &adapters.Result{Block: ledger, Note: fmt.Sprintf("not profitable (%.4f < %.4f)", ratio, a.cfg.MinProfit)}, nil
	}

	// The draw is sized by summing raw bid amounts, which is only valid when
	// the debt being settled is a single known USDC. The pool's settle asset
	// is its own USDC when configured (conversion pools), the vault's
	// otherwise. Refuse mixed/other bids rather than drawing a number of USDC
	// stroops that matches a different asset.
	settleAsset := a.cfg.UsdcAddr
	if a.cfg.PoolUsdc != "" {
		settleAsset = a.cfg.PoolUsdc
	}
	if settleAsset != "" {
		for asset := range auction.Bid {
			if asset != settleAsset {
				return &adapters.Result{Block: ledger,
					Note: fmt.Sprintf("unsupported non-USDC bid asset %s — skipping", asset)}, nil
			}
		}
	}

	bidAmt := int64(0)
	for _, amt := range auction.Bid {
		if amt == nil {
			continue
		}
		if !amt.IsInt64() {
			return &adapters.Result{Block: ledger, Note: "bid amount exceeds int64 range — skipping"}, nil
		}
		bidAmt += amt.Int64()
	}

	// Cross-USDC pools never reach here: GetTasks emits no tasks for them
	// (see the conversion note in GetTasks). Belt-and-braces so a future
	// caller cannot route capital through an unconverted settle asset.
	if a.cfg.PoolUsdc != "" && a.cfg.PoolUsdc != a.cfg.UsdcAddr {
		return &adapters.Result{Block: ledger,
			Note: "pool settles a different USDC — execution disabled (see FACTS.md 'USDC asset bridging')"}, nil
	}

	// Size the draw in UNDERLYING tokens: bid maps hold dToken amounts for
	// user-liquidation and bad-debt auctions, and dTokens are worth d_rate
	// underlying each (>1 in any pool with accrued interest). Drawing the raw
	// dToken number would under-fund the assumed debt.
	drawAmt := a.underlyingBid(pool, auction)
	if drawAmt <= 0 {
		return &adapters.Result{Block: ledger, Note: "bid not priceable in underlying — skipping"}, nil
	}

	res := &adapters.Result{Block: ledger, Drew: drawAmt}

	// The keeper's own USDC before any capital moves. Everything it holds
	// above this line at the end is what this operation produced — proceeds
	// are measured against it, never inferred from auction amounts.
	floatBefore, err := dex.TokenBalance(rpc, a.cfg.Passphrase, a.cfg.UsdcAddr, kp.Address())
	if err != nil {
		return &adapters.Result{Block: ledger, Note: fmt.Sprintf("pre-draw balance read failed: %v", err)}, nil
	}

	drawStart := time.Now()
	if err := vc.Draw(drawAmt); err != nil {
		return nil, fmt.Errorf("vault draw: %w", err)
	}

	// Seized collateral balances before the submit, so the swap only touches
	// what the liquidation actually produced (critical for native XLM, where
	// the keeper's fee balance must not be swapped away).
	lotAssets := make([]string, 0, len(auction.Lot))
	for asset := range auction.Lot {
		lotAssets = append(lotAssets, asset)
	}
	sort.Strings(lotAssets) // deterministic request order
	before := make(map[string]int64, len(lotAssets))
	for _, asset := range lotAssets {
		if bal, err := dex.TokenBalance(rpc, a.cfg.Passphrase, asset, kp.Address()); err == nil {
			before[asset] = bal
		}
	}

	// Fill + repay + withdraw in one atomic submit.
	fillTx, fillErr := core.FillAndUnwind(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr,
		user, core.FullFillPct, settleAsset, drawAmt, lotAssets)
	switch {
	case fillErr == nil:
		res.Success = true
		res.TxHash = fillTx
		res.ResponseTimeMs = time.Since(drawStart).Milliseconds()
		a.swapSeized(kp, rpc, pool, lotAssets, before)
	case errors.Is(fillErr, core.ErrAlreadyFilled):
		// Another keeper won. Nothing was spent — the draw is still in hand.
		res.Note = "already filled by another keeper"
	default:
		return nil, fmt.Errorf("fill auction: %w", fillErr)
	}

	// Measured proceeds: whatever USDC the keeper now holds above its own
	// pre-draw float. Covers the drawn capital that came back plus realized
	// profit, and stays honest when a swap failed (collateral still held).
	usdcNow, balErr := dex.TokenBalance(rpc, a.cfg.Passphrase, a.cfg.UsdcAddr, kp.Address())
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
	res.Latency = time.Since(start)
	return res, nil
}

// EstimateCapital is best-effort: the bid is only known after the auction is
// created, so Execute sizes the draw itself. Returns 0 here.
func (a *Adapter) EstimateCapital(task adapters.Task) (int64, error) {
	return 0, nil
}

// swapSeized converts collateral the liquidation actually delivered into
// USDC. Amounts come from measured balance DELTAS (post-unwind minus the
// pre-submit snapshot), never from auction lot figures — the keeper may hold
// the same asset for other reasons, and for native XLM its fee balance must
// not be swapped away. Assets whose swap fails are simply held; nothing is
// booked that did not happen.
func (a *Adapter) swapSeized(kp *keypair.Full, rpc *soroban.Client, pool *core.PoolState, assets []string, before map[string]int64) {
	if a.dex == nil {
		return
	}
	for _, asset := range assets {
		if asset == a.cfg.UsdcAddr {
			continue // already the settle asset; counted by the balance delta
		}
		now, err := dex.TokenBalance(rpc, a.cfg.Passphrase, asset, kp.Address())
		if err != nil {
			continue
		}
		gained := now - before[asset]
		if gained <= 0 {
			continue
		}
		ref := oracleValueUSDC(pool, asset, gained)
		if _, err := a.dex.SwapToUSDC(kp, asset, gained, ref); err != nil {
			continue
		}
	}
}

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

// underlyingBid totals an auction's bid leg in 7-decimal USDC stroops using
// each reserve's rate and decimals. Bid maps hold dToken amounts for
// user-liquidation and bad-debt auctions (docs/FACTS.md "Auction asset
// flows"), so the raw number understates the debt whenever d_rate > 1.
// Returns 0 if any bid asset is missing from the pool or unpriced — an
// unpriceable bid must never be turned into a draw.
func (a *Adapter) underlyingBid(pool *core.PoolState, auction *core.Auction) int64 {
	var total int64
	for asset, amt := range auction.Bid {
		if amt == nil || !amt.IsInt64() {
			return 0
		}
		r, ok := pool.Reserves[asset]
		if !ok || r.OraclePrice <= 0 {
			return 0
		}
		rate := r.DRate
		if auction.Type == core.AuctionInterest {
			rate = 1.0 // interest bids are backstop LP, not dTokens
		}
		if rate <= 0 {
			return 0
		}
		whole := float64(amt.Int64()) / r.TokenScalar() * rate
		total += int64(whole * 1e7)
	}
	return total
}

// logAuction is a hook for auction lifecycle visibility; the keeper's logger
// lives in package main, so this stays a no-op seam the adapter can call
// without importing it.
var logAuction = func(msg, pool, user string, pct int, startBlock int64) {}
