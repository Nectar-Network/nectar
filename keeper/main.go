package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os/signal"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/joho/godotenv"
	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/blend"
	"github.com/nectar-network/keeper/registry"
	"github.com/nectar-network/keeper/soroban"
	"github.com/nectar-network/keeper/vault"
)

const maxSSEClients = 100

// LiquidationRecord is appended on every successful auction fill.
type LiquidationRecord struct {
	User           string    `json:"user"`
	AuctionType    string    `json:"auction_type"`
	Block          int64     `json:"block"`
	Drew           int64     `json:"drew"`
	Proceeds       int64     `json:"proceeds"`
	Profit         int64     `json:"profit"`
	ResponseTimeMs int64     `json:"response_time_ms"`
	Timestamp      time.Time `json:"ts"`
}

// KeeperStat tracks per-keeper performance metrics.
type KeeperStat struct {
	Name         string `json:"name"`
	Address      string `json:"address"`
	Liquidations int    `json:"liquidations"`
	TotalProfit  int64  `json:"total_profit"`
}

// DepositorRow represents one known depositor's live vault balance.
type DepositorRow struct {
	Address   string  `json:"address"`
	Shares    int64   `json:"shares"`
	USDCValue int64   `json:"usdc_value"`
	PnLPct    float64 `json:"pnl_pct"`
}

// appMetrics are updated atomically to avoid lock contention.
type appMetrics struct {
	cyclesTotal       atomic.Int64
	liquidationsTotal atomic.Int64
	sseActive         atomic.Int64
}

// State is the shared data bag for HTTP handlers and the keeper loop.
type State struct {
	mu           sync.RWMutex
	Keepers      []keeperRow            `json:"keepers"`
	Positions    []posRow               `json:"positions"`
	Events       []string               `json:"events"`
	VaultState   *vault.VaultState      `json:"vault"`
	Depositors   []DepositorRow         `json:"depositors"`
	Liquidations []LiquidationRecord    `json:"liquidations"`
	KeeperStats  map[string]*KeeperStat `json:"keeper_stats"`

	// subsMu guards the SSE subscriber list independently to prevent
	// addEvent from holding the data lock while iterating channels.
	subsMu sync.Mutex
	subs   []chan string
}

type keeperRow struct {
	Name    string `json:"name"`
	Address string `json:"address"`
	Active  bool   `json:"active"`
}

type posRow struct {
	Address string  `json:"address"`
	HF      float64 `json:"hf"`
}

var (
	state  = &State{KeeperStats: map[string]*KeeperStat{}}
	appMet = &appMetrics{}
)

// addEvent appends msg to the ring-buffer and broadcasts to SSE subscribers.
// Subscriber channels are iterated outside the data lock to avoid deadlock.
func (s *State) addEvent(msg string) {
	s.mu.Lock()
	if len(s.Events) >= 100 {
		s.Events = s.Events[1:]
	}
	s.Events = append(s.Events, msg)
	s.mu.Unlock()

	payload, _ := json.Marshal(map[string]string{"msg": msg})
	p := string(payload)

	// copy subscriber list under its own lock, then send without any lock held
	s.subsMu.Lock()
	subs := make([]chan string, len(s.subs))
	copy(subs, s.subs)
	s.subsMu.Unlock()

	for _, ch := range subs {
		select {
		case ch <- p:
		default: // drop when buffer full — subscriber is slow
		}
	}
}

func (s *State) subscribe() chan string {
	ch := make(chan string, 32)
	s.subsMu.Lock()
	s.subs = append(s.subs, ch)
	s.subsMu.Unlock()
	return ch
}

func (s *State) unsubscribe(ch chan string) {
	s.subsMu.Lock()
	defer s.subsMu.Unlock()
	for i, c := range s.subs {
		if c == ch {
			s.subs = append(s.subs[:i], s.subs[i+1:]...)
			return
		}
	}
}

func main() {
	_ = godotenv.Load()
	cfg := LoadConfig()

	logInfo("parsing keypair", "key_len", len(cfg.SecretKey), "prefix", cfg.SecretKey[:1])
	kp, err := keypair.ParseFull(cfg.SecretKey)
	if err != nil {
		logErr("parse keypair", "err", err, "key_len", len(cfg.SecretKey))
		return
	}

	rpc := soroban.NewClient(cfg.RpcURL)

	logInfo("registering keeper", "name", cfg.KeeperName)
	if err := registry.Register(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.RegistryID, cfg.KeeperName); err != nil {
		logWarn("registration skipped (may already be registered)", "err", err)
	} else {
		logInfo("registered", "name", cfg.KeeperName)
	}

	state.mu.Lock()
	state.Keepers = append(state.Keepers, keeperRow{
		Name:    cfg.KeeperName,
		Address: kp.Address(),
		Active:  true,
	})
	state.KeeperStats[cfg.KeeperName] = &KeeperStat{
		Name:    cfg.KeeperName,
		Address: kp.Address(),
	}
	state.mu.Unlock()

	go serveHTTP(cfg.APIPort)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	ticker := time.NewTicker(time.Duration(cfg.PollInterval) * time.Second)
	defer ticker.Stop()

	if cfg.DemoProfitBPS > 0 {
		logWarn("DEMO_PROFIT_BPS active — keeper will top up returned proceeds from its own USDC; do NOT use against a real Blend pool",
			"bps", cfg.DemoProfitBPS)
	}
	if cfg.USDCContract == "" {
		logWarn("USDC_CONTRACT not set — keeper cannot read its own balance and will skip real-proceeds accounting; fills will return 0 to the vault unless DEMO_PROFIT_BPS is on")
	}

	logInfo("keeper started", "pool", short(cfg.BlendPool), "interval", cfg.PollInterval)

	for {
		select {
		case <-ctx.Done():
			logInfo("shutdown signal received, exiting")
			return
		case <-ticker.C:
			cycles := appMet.cyclesTotal.Add(1)
			if err := cycle(rpc, kp, cfg); err != nil {
				logWarn("cycle error", "err", err)
				state.addEvent(fmt.Sprintf("cycle error: %v", err))
			}
			// Periodic slasher sweep — anyone can call slash(), so this
			// works as long as the keeper has XLM for fees.
			if cfg.SlashScanEvery > 0 && cycles%int64(cfg.SlashScanEvery) == 0 {
				if err := runSlashSweep(rpc, kp, cfg); err != nil {
					logWarn("slash sweep error", "err", err)
				}
			}
		}
	}
}

func cycle(rpc *soroban.Client, kp *keypair.Full, cfg Config) error {
	if cfg.BlendPool == "" {
		state.addEvent("vault monitor mode — no Blend pool configured")
	} else {
		if err := runAuctionCycle(rpc, kp, cfg); err != nil {
			return err
		}
	}

	// refresh vault state
	if vs, err := vault.GetState(rpc, cfg.Passphrase, cfg.VaultID); err == nil {
		state.mu.Lock()
		state.VaultState = vs
		state.mu.Unlock()
	}

	// refresh depositor balances for the performance page
	if len(cfg.KnownDepositors) > 0 {
		var depRows []DepositorRow
		for _, addr := range cfg.KnownDepositors {
			bal, err := vault.Balance(rpc, cfg.Passphrase, cfg.VaultID, addr)
			if err != nil {
				continue
			}
			depRows = append(depRows, DepositorRow{
				Address:   addr,
				Shares:    bal.Shares,
				USDCValue: bal.USDCValue,
			})
		}
		state.mu.Lock()
		state.Depositors = depRows
		state.mu.Unlock()
	}

	return nil
}

func runAuctionCycle(rpc *soroban.Client, kp *keypair.Full, cfg Config) error {
	pool, err := blend.LoadPool(rpc, cfg.Passphrase, cfg.BlendPool)
	if err != nil {
		return fmt.Errorf("load pool: %w", err)
	}
	if pool.OracleStale {
		logWarn("oracle stale — some reserves missing prices, profitability checks will skip those auctions")
		state.addEvent("oracle stale: skipping price-gated fills")
	}

	ledger, err := rpc.LatestLedger()
	if err != nil {
		return fmt.Errorf("latest ledger: %w", err)
	}

	positions, err := blend.GetPositions(rpc, cfg.Passphrase, cfg.BlendPool, ledger-1000)
	if err != nil {
		return fmt.Errorf("get positions: %w", err)
	}

	// 1) User-liquidation pass: keeper-initiated for any underwater position.
	rows := make([]posRow, 0, len(positions))
	scanned := map[string]struct{}{}
	for i := range positions {
		pos := &positions[i]
		pos.HF = blend.CalcHealthFactor(*pos, pool)
		rows = append(rows, posRow{Address: pos.Address, HF: pos.HF})
		scanned[pos.Address] = struct{}{}

		if pos.HF >= 1.0 {
			continue
		}
		logInfo("underwater position", "user", short(pos.Address), "hf", fmt.Sprintf("%.4f", pos.HF))
		state.addEvent(fmt.Sprintf("underwater: %s hf=%.4f", short(pos.Address), pos.HF))

		if err := handleUserLiquidation(rpc, kp, cfg, pool, *pos, ledger); err != nil {
			logWarn("user-liquidation failed", "user", short(pos.Address), "err", err)
			state.addEvent(fmt.Sprintf("user-liq failed: %s %v", short(pos.Address), err))
		}
	}

	// 2) Pool-managed auctions pass (bad-debt + interest): detect+fill if profitable.
	//    We scan every address seen in pool events — same set as GetPositions
	//    builds — because Blend keys bad-debt/interest auctions by address too.
	for addr := range scanned {
		if err := handlePoolManagedAuctions(rpc, kp, cfg, pool, addr, ledger); err != nil {
			logWarn("pool-auction sweep failed", "addr", short(addr), "err", err)
		}
	}

	state.mu.Lock()
	state.Positions = rows
	state.mu.Unlock()
	return nil
}

// handleUserLiquidation initiates a user-liquidation auction (request_type 6)
// for an underwater position, evaluates profitability with live oracle prices,
// and fills it through the vault if the numbers clear MIN_PROFIT.
func handleUserLiquidation(rpc *soroban.Client, kp *keypair.Full, cfg Config, pool *blend.PoolState, pos blend.Position, ledger int64) error {
	// Pre-flight: size the draw from oracle-priced debt rather than blind bid sum.
	estimated := blend.EstimateCapital(pos, pool, 50)
	if estimated <= 0 {
		// Nothing to liquidate (or no priced reserves on the debt side).
		return nil
	}

	if err := blend.CreateAuction(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.BlendPool, pos.Address, 50); err != nil {
		return fmt.Errorf("create auction: %w", err)
	}

	auction, err := blend.GetAuctionByType(rpc, cfg.Passphrase, cfg.BlendPool, pos.Address, blend.AuctionUserLiquidation)
	if err != nil {
		return fmt.Errorf("get user-liq auction: %w", err)
	}
	if auction == nil {
		// Auction creation reported success but the read came back empty — race or
		// AuctionExists from another keeper. Either way nothing for us to fill.
		return nil
	}

	return executeFill(rpc, kp, cfg, pool, auction, ledger, pos.Address, estimated)
}

// handlePoolManagedAuctions detects bad-debt and interest auctions tied to the
// given address (typically the pool's backstop) and fills any that are profitable.
// We never call new_liquidation_auction for these — the pool creates them.
func handlePoolManagedAuctions(rpc *soroban.Client, kp *keypair.Full, cfg Config, pool *blend.PoolState, addr string, ledger int64) error {
	for _, kind := range []blend.AuctionType{blend.AuctionBadDebt, blend.AuctionInterest} {
		a, err := blend.GetAuctionByType(rpc, cfg.Passphrase, cfg.BlendPool, addr, kind)
		if err != nil {
			return fmt.Errorf("%s detect: %w", kind, err)
		}
		if a == nil {
			continue
		}
		logInfo("detected pool-managed auction", "kind", kind.String(), "addr", short(addr), "block", a.StartBlock)
		state.addEvent(fmt.Sprintf("detected %s auction: %s", kind, short(addr)))

		// drawAmount=0: pool-managed fills don't consume USDC from the vault —
		// the keeper pays the bid in non-USDC assets (BLND for interest, debt
		// take-on for bad-debt). Real fills require those assets in the keeper
		// wallet; for Tranche 1 we detect+log and (best-effort) attempt to
		// fill. Without DEX integration the keeper can't synthesize BLND so
		// most pool-managed fills will revert at submit() — that's expected
		// and logged.
		if err := executeFill(rpc, kp, cfg, pool, a, ledger, addr, 0); err != nil {
			logWarn("pool-auction fill failed (expected without non-USDC liquidity)",
				"kind", kind.String(), "addr", short(addr), "err", err)
		}
	}
	return nil
}

// executeFill runs the common draw → fill → proceeds-return path. drawAmount==0
// skips the vault draw (used for pool-managed auctions where the keeper funds
// the bid out-of-band).
func executeFill(rpc *soroban.Client, kp *keypair.Full, cfg Config, pool *blend.PoolState, auction *blend.Auction, ledger int64, target string, drawAmount int64) error {
	if !auctionPricesKnown(*auction, pool) {
		logInfo("auction references assets without oracle prices — skipping", "kind", auction.Type.String(), "addr", short(target))
		return nil
	}

	profit := blend.Profitability(*auction, pool, ledger)
	logInfo("auction profitability",
		"kind", auction.Type.String(),
		"addr", short(target),
		"ratio", fmt.Sprintf("%.4f", profit),
		"draw", drawAmount,
	)
	if profit < cfg.MinProfit {
		logInfo("skipping — not profitable yet", "ratio", fmt.Sprintf("%.4f", profit), "min", cfg.MinProfit)
		return nil
	}

	// Snapshot the keeper's USDC balance BEFORE the draw so we can compute the
	// real proceeds delta after the fill. If USDC_CONTRACT isn't configured we
	// fall back to "drawAmount" (treated as zero profit on success) so the
	// vault accounting at least stays consistent.
	balBefore := int64(0)
	balKnown := false
	if cfg.USDCContract != "" {
		if bal, err := rpc.TokenBalance(cfg.Passphrase, cfg.USDCContract, kp.Address()); err == nil {
			balBefore = bal
			balKnown = true
		} else {
			logWarn("keeper balance read failed; will assume zero profit", "err", err)
		}
	}

	if drawAmount > 0 {
		if err := vault.Draw(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.VaultID, drawAmount); err != nil {
			return fmt.Errorf("vault draw: %w", err)
		}
		logInfo("drew vault capital", "amount", drawAmount)
		state.addEvent(fmt.Sprintf("drew %d from vault", drawAmount))
	}

	drawStart := time.Now()
	fillErr := blend.FillByType(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.BlendPool, target, auction.Type)

	switch {
	case fillErr == nil:
		responseMs := time.Since(drawStart).Milliseconds()
		proceeds, profit := computeProceeds(rpc, cfg, kp.Address(), drawAmount, balBefore, balKnown)
		appMet.liquidationsTotal.Add(1)
		logInfo("filled auction",
			"kind", auction.Type.String(),
			"addr", short(target),
			"response_ms", responseMs,
			"proceeds", proceeds,
			"profit", profit,
		)
		state.addEvent(fmt.Sprintf("filled %s auction: %s (+%d USDC profit)", auction.Type, short(target), profit))

		if proceeds > 0 {
			if err := vault.ReturnProceeds(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.VaultID, proceeds, responseMs); err != nil {
				logWarn("return proceeds failed", "err", err)
				state.addEvent(fmt.Sprintf("return proceeds failed: %v", err))
				return nil
			}
			logInfo("returned proceeds", "amount", proceeds, "response_ms", responseMs)
			state.addEvent(fmt.Sprintf("returned %d to vault", proceeds))
		} else if drawAmount > 0 {
			logWarn("zero proceeds despite a vault draw — capital remains outstanding until DEX integration unlocks collateral conversion")
		}

		recordLiquidation(cfg.KeeperName, target, auction.Type, ledger, drawAmount, proceeds, profit, responseMs)
		return nil

	case fillErr == blend.ErrAlreadyFilled:
		logInfo("auction already filled by another keeper", "kind", auction.Type.String(), "addr", short(target))
		state.addEvent(fmt.Sprintf("already filled: %s (%s)", short(target), auction.Type))
		if drawAmount > 0 {
			// Return ONLY the drawn capital, no profit, no response time (the
			// avg-response metric should reflect real fills only).
			if err := vault.ReturnProceeds(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.VaultID, drawAmount, 0); err != nil {
				logWarn("return capital after lost race failed", "err", err)
				state.addEvent(fmt.Sprintf("return-after-race failed: %v", err))
			}
		}
		return nil

	default:
		// Hard failure (insufficient balance, contract error, etc).
		// Leave drawn capital outstanding — the slasher will recover it after
		// slash_timeout if the keeper doesn't reattempt and return.
		return fmt.Errorf("fill %s auction: %w", auction.Type, fillErr)
	}
}

// computeProceeds returns (proceeds_to_return_to_vault, realized_profit).
// proceeds is the keeper's USDC balance delta from before-draw to after-fill.
// If DemoProfitBPS is set and real proceeds fall short, the keeper tops up
// the gap from its own balance (lab/demo mode only).
func computeProceeds(rpc *soroban.Client, cfg Config, keeperAddr string, drawAmount, balBefore int64, balKnown bool) (int64, int64) {
	var proceeds int64
	if balKnown && cfg.USDCContract != "" {
		balAfter, err := rpc.TokenBalance(cfg.Passphrase, cfg.USDCContract, keeperAddr)
		if err == nil {
			delta := balAfter - balBefore
			if delta > 0 {
				proceeds = delta
			}
		}
	}

	// Demo mode top-up — never set this against a real Blend pool.
	if cfg.DemoProfitBPS > 0 && drawAmount > 0 {
		target := drawAmount + drawAmount*cfg.DemoProfitBPS/10_000
		if proceeds < target {
			proceeds = target
		}
	}

	profit := proceeds - drawAmount
	if profit < 0 {
		profit = 0
	}
	return proceeds, profit
}

func recordLiquidation(keeperName, user string, kind blend.AuctionType, ledger, drew, proceeds, profit, responseMs int64) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.Liquidations = append(state.Liquidations, LiquidationRecord{
		User:           user,
		AuctionType:    kind.String(),
		Block:          ledger,
		Drew:           drew,
		Proceeds:       proceeds,
		Profit:         profit,
		ResponseTimeMs: responseMs,
		Timestamp:      time.Now().UTC(),
	})
	if ks := state.KeeperStats[keeperName]; ks != nil {
		ks.Liquidations++
		ks.TotalProfit += profit
	}
}

// auctionPricesKnown returns true iff every asset referenced by the auction's
// lot or bid map has a non-zero oracle price in the pool snapshot. We don't
// want to fill blind.
func auctionPricesKnown(a blend.Auction, pool *blend.PoolState) bool {
	for asset := range a.Lot {
		if _, ok := pool.PriceFor(asset); !ok {
			return false
		}
	}
	for asset := range a.Bid {
		if _, ok := pool.PriceFor(asset); !ok {
			return false
		}
	}
	return true
}

// runSlashSweep scans every registered keeper and slashes anyone past the
// slash_timeout. Anyone with an XLM-funded account can call slash(), so the
// keeper doesn't need admin rights to clean up stuck draws.
func runSlashSweep(rpc *soroban.Client, kp *keypair.Full, cfg Config) error {
	addrs, err := registry.ListKeepers(rpc, cfg.Passphrase, cfg.RegistryID)
	if err != nil {
		return fmt.Errorf("list keepers: %w", err)
	}
	if len(addrs) == 0 {
		return nil
	}
	regCfg, err := registry.GetConfig(rpc, cfg.Passphrase, cfg.RegistryID)
	if err != nil {
		return fmt.Errorf("get registry config: %w", err)
	}
	now := time.Now().Unix()
	for _, k := range addrs {
		info, err := registry.GetKeeper(rpc, cfg.Passphrase, cfg.RegistryID, k)
		if err != nil {
			continue
		}
		if !info.HasActiveDraw {
			continue
		}
		if uint64(now)-info.LastDrawTime <= regCfg.SlashTimeout {
			continue
		}
		logWarn("slashing stuck keeper", "keeper", short(k), "drawn_at", info.LastDrawTime)
		if err := registry.Slash(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.RegistryID, k); err != nil {
			logWarn("slash failed", "keeper", short(k), "err", err)
			continue
		}
		state.addEvent(fmt.Sprintf("slashed stuck keeper %s", short(k)))
	}
	return nil
}

// ── HTTP handlers ─────────────────────────────────────────────────────────────

func serveHTTP(port string) {
	mux := http.NewServeMux()
	mux.HandleFunc("/api/state", corsMiddleware(handleState))
	mux.HandleFunc("/api/events", corsMiddleware(handleSSE))
	mux.HandleFunc("/api/performance", corsMiddleware(handlePerformance))
	mux.HandleFunc("/metrics", handleMetrics)
	mux.HandleFunc("/healthz", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})

	addr := ":" + port
	logInfo("API server listening", "addr", addr)
	if err := http.ListenAndServe(addr, mux); err != nil {
		logErr("API server", "err", err)
	}
}

func corsMiddleware(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next(w, r)
	}
}

func handleState(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	snap := struct {
		Keepers    []keeperRow       `json:"keepers"`
		Positions  []posRow          `json:"positions"`
		Events     []string          `json:"events"`
		Vault      *vault.VaultState `json:"vault"`
		Depositors []DepositorRow    `json:"depositors"`
	}{
		Keepers:    state.Keepers,
		Positions:  state.Positions,
		Events:     state.Events,
		Vault:      state.VaultState,
		Depositors: state.Depositors,
	}
	state.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(snap); err != nil {
		logWarn("handleState encode error", "err", err)
	}
}

func handleSSE(w http.ResponseWriter, r *http.Request) {
	if appMet.sseActive.Load() >= maxSSEClients {
		http.Error(w, "too many SSE clients", http.StatusServiceUnavailable)
		return
	}
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming not supported", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	appMet.sseActive.Add(1)
	defer appMet.sseActive.Add(-1)

	ch := state.subscribe()
	defer state.unsubscribe(ch)

	for {
		select {
		case <-r.Context().Done():
			return
		case msg := <-ch:
			if _, err := fmt.Fprintf(w, "data: %s\n\n", msg); err != nil {
				return
			}
			flusher.Flush()
		}
	}
}

type perfResponse struct {
	Vault        *vault.VaultState      `json:"vault"`
	Depositors   []DepositorRow         `json:"depositors"`
	KeeperStats  map[string]*KeeperStat `json:"keeper_stats"`
	Liquidations []LiquidationRecord    `json:"liquidations"`
}

func handlePerformance(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	resp := perfResponse{
		Vault:        state.VaultState,
		Depositors:   state.Depositors,
		KeeperStats:  state.KeeperStats,
		Liquidations: state.Liquidations,
	}
	state.mu.RUnlock()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		logWarn("handlePerformance encode error", "err", err)
	}
}

func handleMetrics(w http.ResponseWriter, r *http.Request) {
	state.mu.RLock()
	tvl := int64(0)
	if state.VaultState != nil {
		tvl = state.VaultState.TotalUSDC
	}
	state.mu.RUnlock()

	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "# HELP nectar_cycles_total Number of keeper poll cycles\n")
	fmt.Fprintf(w, "nectar_cycles_total %d\n", appMet.cyclesTotal.Load())
	fmt.Fprintf(w, "# HELP nectar_liquidations_total Number of successful auction fills\n")
	fmt.Fprintf(w, "nectar_liquidations_total %d\n", appMet.liquidationsTotal.Load())
	fmt.Fprintf(w, "# HELP nectar_vault_tvl Vault total USDC (7 decimals)\n")
	fmt.Fprintf(w, "nectar_vault_tvl %d\n", tvl)
	fmt.Fprintf(w, "# HELP nectar_sse_active Active SSE connections\n")
	fmt.Fprintf(w, "nectar_sse_active %d\n", appMet.sseActive.Load())
}
