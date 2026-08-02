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

	// Canary route check for pools settling a different USDC: quote a 1-USDC
	// conversion. Off-parity (or no route/DEX) suppresses task emission this
	// cycle — the pool is effectively monitor-only until the route heals, and
	// the report says so explicitly.
	convertible := a.cfg.PoolUsdc != "" && a.cfg.PoolUsdc != a.cfg.UsdcAddr
	if convertible && !a.cfg.Monitor {
		if a.dex == nil {
			report.Note = "pool settles different USDC and no DEX configured — not emitting tasks"
			convertible = false
		} else if _, err := a.dex.QuoteConvertIn(a.cfg.UsdcAddr, a.cfg.PoolUsdc, 1_0000000); err != nil {
			report.Note = fmt.Sprintf("conversion route not viable (%v) — not emitting tasks", err)
			convertible = false
		}
	}
	suppressTasks := a.cfg.Monitor || (a.cfg.PoolUsdc != "" && a.cfg.PoolUsdc != a.cfg.UsdcAddr && !convertible)

	indexToAsset := make(map[uint32]string, len(pool.Reserves))
	for asset, r := range pool.Reserves {
		indexToAsset[r.Index] = asset
	}

	var tasks []adapters.Task
	for i := range positions {
		pos := &positions[i]
		hf := core.CalcHealthFactor(*pos, pool)
		report.Positions = append(report.Positions, adapters.PositionHealth{Address: pos.Address, HF: hf})
		if hf >= 1.0 || suppressTasks {
			continue
		}
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

	if err := core.CreateAuction(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr, user, 50,
		td.bidAssets, td.lotAssets); err != nil {
		return nil, fmt.Errorf("create auction: %w", err)
	}
	auction, err := core.GetAuction(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, user)
	if err != nil {
		return nil, fmt.Errorf("get auction: %w", err)
	}
	if auction == nil {
		return &adapters.Result{Note: "no auction"}, nil
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

	// Conversion pools: price the entry conversion BEFORE drawing anything.
	// An off-parity or missing route aborts here with zero capital moved.
	needConvert := a.cfg.PoolUsdc != "" && a.cfg.PoolUsdc != a.cfg.UsdcAddr && bidAmt > 0
	drawAmt := bidAmt
	if needConvert {
		if a.dex == nil {
			return &adapters.Result{Block: ledger, Note: "pool settles different USDC and no DEX configured — skipping"}, nil
		}
		required, err := a.dex.QuoteConvertIn(a.cfg.UsdcAddr, a.cfg.PoolUsdc, bidAmt)
		if err != nil {
			return &adapters.Result{Block: ledger,
				Note: fmt.Sprintf("entry conversion refused pre-draw: %v", err)}, nil
		}
		drawAmt = required
	}

	res := &adapters.Result{Block: ledger, Drew: drawAmt}

	drawStart := time.Now()
	if drawAmt > 0 {
		if err := vc.Draw(drawAmt); err != nil {
			return nil, fmt.Errorf("vault draw: %w", err)
		}
	}
	if needConvert {
		conv, err := a.dex.ConvertExactOut(kp, a.cfg.UsdcAddr, a.cfg.PoolUsdc, bidAmt, drawAmt)
		if err != nil {
			// Nothing spent (or spend failed): return the drawn capital as-is.
			res.Proceeds = drawAmt
			res.Note = fmt.Sprintf("entry conversion failed after draw, capital returned: %v", err)
			if drawAmt > 0 {
				if rerr := vc.ReturnProceeds(res.Proceeds, 0); rerr != nil {
					res.Note = fmt.Sprintf("entry conversion failed AND return failed (capital outstanding): %v / %v", err, rerr)
				}
			}
			res.Latency = time.Since(start)
			return res, nil
		}
		_ = conv // keeper now holds bidAmt of pool USDC to settle the assumed debt
	}

	fillTx, fillErr := core.FillAuction(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr, user)
	switch {
	case fillErr == nil:
		res.Success = true
		res.TxHash = fillTx
		res.ResponseTimeMs = time.Since(drawStart).Milliseconds()
		if drawAmt > 0 {
			res.Proceeds = a.swapCollateral(kp, pool, auction)
			res.Profit = res.Proceeds - drawAmt
			if res.Profit < 0 {
				res.Profit = 0
			}
			if res.Proceeds == 0 {
				res.Note = "zero returnable proceeds — outstanding draw at slash risk"
			}
		}
	case errors.Is(fillErr, core.ErrAlreadyFilled):
		// Another keeper won. Return what we still hold: unconverted capital
		// as-is; converted pool-USDC swapped back under the same guard.
		res.Note = "already filled by another keeper"
		if needConvert {
			if back, err := a.dex.SwapToUSDC(kp, a.cfg.PoolUsdc, bidAmt, bidAmt); err == nil {
				res.Proceeds = back.OutputAmount
			} else {
				res.Proceeds = 0
				res.Note = fmt.Sprintf("already filled; exit conversion refused, pool-USDC held (%v)", err)
			}
		} else {
			res.Proceeds = drawAmt
		}
	default:
		return nil, fmt.Errorf("fill auction: %w", fillErr)
	}

	// Return only when capital was actually drawn AND there is something to send.
	// A return failure is non-fatal: the fill already happened on-chain, so we
	// keep the result (and its accounting) and surface the outstanding-capital
	// risk via Note rather than discarding a realized fill.
	if bidAmt > 0 && res.Proceeds > 0 {
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

// swapCollateral converts every non-USDC asset in the auction lot to USDC and
// returns the total real USDC obtained. USDC already in the lot counts
// directly; assets whose swap fails are held (excluded) rather than booked as
// phantom profit.
func (a *Adapter) swapCollateral(kp *keypair.Full, pool *core.PoolState, auction *core.Auction) int64 {
	var total int64
	for asset, amt := range auction.Lot {
		if amt == nil {
			continue
		}
		if !amt.IsInt64() {
			continue // implausibly large lot amount — skip rather than truncate
		}
		v := amt.Int64()
		if v <= 0 {
			continue
		}
		if asset == a.cfg.UsdcAddr {
			total += v
			continue
		}
		if a.dex == nil {
			continue
		}
		ref := oracleValueUSDC(pool, asset, v)
		res, err := a.dex.SwapToUSDC(kp, asset, v, ref)
		if err != nil {
			continue
		}
		total += res.OutputAmount
	}
	return total
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

// oracleValueUSDC returns the Blend-oracle-implied USDC value (7-decimal
// stroops) of amt of asset, or 0 when no price is available.
func oracleValueUSDC(pool *core.PoolState, asset string, amt int64) int64 {
	if pool == nil {
		return 0
	}
	r, ok := pool.Reserves[asset]
	if !ok || r.OraclePrice <= 0 {
		return 0
	}
	return int64(float64(amt) * r.OraclePrice)
}
