// Package blend adapts the Blend liquidation protocol to the generic
// adapters.ProtocolAdapter interface. It wraps the lower-level
// github.com/nectar-network/keeper/blend package (pool/auction/position logic)
// and the dex package (collateral→USDC conversion), turning underwater
// positions into Tasks and filling their auctions in Execute. The underlying
// blend package is left intact (and fully tested); this is a thin translation
// layer, which is what gets extracted into the keeper-sdk in Phase 4.
package blend

import (
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
}

// Adapter implements adapters.ProtocolAdapter for Blend.
type Adapter struct {
	cfg Config
	dex *dex.SwapClient
}

// NewAdapter builds a Blend adapter. dexc may be nil to disable collateral
// swapping (proceeds are then only the USDC directly present in the lot).
func NewAdapter(cfg Config, dexc *dex.SwapClient) *Adapter {
	return &Adapter{cfg: cfg, dex: dexc}
}

// Name returns the protocol identifier.
func (a *Adapter) Name() string { return "blend" }

// taskData is the per-task payload threaded from GetTasks to Execute so the
// pool snapshot (oracle prices, reserves) is reused without re-loading.
type taskData struct {
	pool *core.PoolState
}

// GetTasks loads the pool and returns one liquidation task per underwater
// position (health factor < 1).
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
	positions, err := core.GetPositions(rpc, a.cfg.Passphrase, a.cfg.PoolAddr, ledger-1000)
	if err != nil {
		return nil, fmt.Errorf("get positions: %w", err)
	}

	var tasks []adapters.Task
	for i := range positions {
		pos := &positions[i]
		hf := core.CalcHealthFactor(*pos, pool)
		if hf >= 1.0 {
			continue
		}
		tasks = append(tasks, adapters.Task{
			Protocol: a.Name(),
			Type:     "liquidation",
			Target:   pos.Address,
			Priority: priorityFromHF(hf),
			Health:   hf,
			Data:     taskData{pool: pool},
		})
	}
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

	if err := core.CreateAuction(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr, user, 50); err != nil {
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

	bidAmt := int64(0)
	for _, amt := range auction.Bid {
		if amt != nil {
			bidAmt += amt.Int64()
		}
	}

	res := &adapters.Result{Block: ledger, Drew: bidAmt}

	drawStart := time.Now()
	if bidAmt > 0 {
		if err := vc.Draw(bidAmt); err != nil {
			return nil, fmt.Errorf("vault draw: %w", err)
		}
	}

	fillErr := core.FillAuction(rpc, a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.PoolAddr, user)
	switch {
	case fillErr == nil:
		res.Success = true
		res.ResponseTimeMs = time.Since(drawStart).Milliseconds()
		if bidAmt > 0 {
			res.Proceeds = a.swapCollateral(kp, pool, auction)
			res.Profit = res.Proceeds - bidAmt
			if res.Profit < 0 {
				res.Profit = 0
			}
			if res.Proceeds == 0 {
				res.Note = "zero returnable proceeds — outstanding draw at slash risk"
			}
		}
	case fillErr == core.ErrAlreadyFilled:
		// Another keeper won. We drew capital but never spent it — return it
		// unchanged (no profit, no loss).
		res.Note = "already filled by another keeper"
		res.Proceeds = bidAmt
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
