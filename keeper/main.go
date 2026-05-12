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
	User      string    `json:"user"`
	Block     int64     `json:"block"`
	Drew      int64     `json:"drew"`
	Proceeds  int64     `json:"proceeds"`
	Timestamp time.Time `json:"ts"`
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

	logInfo("keeper started", "pool", short(cfg.BlendPool), "interval", cfg.PollInterval)

	for {
		select {
		case <-ctx.Done():
			logInfo("shutdown signal received, exiting")
			return
		case <-ticker.C:
			appMet.cyclesTotal.Add(1)
			if err := cycle(rpc, kp, cfg); err != nil {
				logWarn("cycle error", "err", err)
				state.addEvent(fmt.Sprintf("cycle error: %v", err))
			}
		}
	}
}

func cycle(rpc *soroban.Client, kp *keypair.Full, cfg Config) error {
	// When BlendPool is configured, monitor positions and execute liquidations
	if cfg.BlendPool != "" {
		pool, err := blend.LoadPool(rpc, cfg.Passphrase, cfg.BlendPool)
		if err != nil {
			return fmt.Errorf("load pool: %w", err)
		}

		ledger, err := rpc.LatestLedger()
		if err != nil {
			return fmt.Errorf("latest ledger: %w", err)
		}

		positions, err := blend.GetPositions(rpc, cfg.Passphrase, cfg.BlendPool, ledger-1000)
		if err != nil {
			return fmt.Errorf("get positions: %w", err)
		}

		var rows []posRow
		for i := range positions {
			pos := &positions[i]
			pos.HF = blend.CalcHealthFactor(*pos, pool)
			rows = append(rows, posRow{Address: pos.Address, HF: pos.HF})

			if pos.HF >= 1.0 {
				continue
			}

			logInfo("underwater position", "user", short(pos.Address), "hf", fmt.Sprintf("%.4f", pos.HF))
			state.addEvent(fmt.Sprintf("underwater: %s hf=%.4f", short(pos.Address), pos.HF))

			if err := handleLiquidation(rpc, kp, cfg, pool, pos.Address, ledger); err != nil {
				logWarn("liquidation failed", "user", short(pos.Address), "err", err)
				state.addEvent(fmt.Sprintf("liq failed: %s %v", short(pos.Address), err))
			}
		}

		state.mu.Lock()
		state.Positions = rows
		state.mu.Unlock()
	} else {
		state.addEvent("vault monitor mode — no Blend pool configured")
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

func handleLiquidation(rpc *soroban.Client, kp *keypair.Full, cfg Config, pool *blend.PoolState, user string, currentLedger int64) error {
	if err := blend.CreateAuction(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.BlendPool, user, 50); err != nil {
		return fmt.Errorf("create auction: %w", err)
	}

	auction, err := blend.GetAuction(rpc, cfg.Passphrase, cfg.BlendPool, user)
	if err != nil {
		return fmt.Errorf("get auction: %w", err)
	}
	if auction == nil {
		return nil
	}

	profit := blend.Profitability(*auction, pool, currentLedger)
	logInfo("auction profitability", "user", short(user), "ratio", fmt.Sprintf("%.4f", profit))

	if profit < cfg.MinProfit {
		logInfo("skipping — not profitable yet", "ratio", fmt.Sprintf("%.4f", profit), "min", cfg.MinProfit)
		return nil
	}

	// sum bid amounts to estimate capital needed from vault
	bidAmt := int64(0)
	for _, amt := range auction.Bid {
		if amt != nil {
			bidAmt += amt.Int64()
		}
	}

	// draw vault capital
	drawStart := time.Now()
	if bidAmt > 0 {
		if err := vault.Draw(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.VaultID, bidAmt); err != nil {
			return fmt.Errorf("vault draw: %w", err)
		}
		logInfo("drew vault capital", "amount", bidAmt)
		state.addEvent(fmt.Sprintf("drew %d from vault", bidAmt))
	}

	// attempt fill — only return proceeds on success or AlreadyFilled
	fillErr := blend.FillAuction(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.BlendPool, user)
	shouldReturn := false
	// Captured for the keeper performance metric (avg_response_time_ms).
	// Forwarded to ReturnProceeds → vault.return_proceeds → registry.record_execution.
	// Zero for the AlreadyFilled branch — we didn't actually execute.
	responseMs := int64(0)

	switch {
	case fillErr == nil:
		responseMs = time.Since(drawStart).Milliseconds()
		logInfo("filled auction", "user", short(user), "response_ms", responseMs)
		state.addEvent(fmt.Sprintf("filled auction: %s", short(user)))
		shouldReturn = bidAmt > 0
		appMet.liquidationsTotal.Add(1)
		proceeds := int64(0)
		if bidAmt > 0 {
			proceeds = bidAmt + bidAmt/10
		}
		state.mu.Lock()
		state.Liquidations = append(state.Liquidations, LiquidationRecord{
			User:      user,
			Block:     currentLedger,
			Drew:      bidAmt,
			Proceeds:  proceeds,
			Timestamp: time.Now().UTC(),
		})
		if ks := state.KeeperStats[cfg.KeeperName]; ks != nil {
			ks.Liquidations++
			ks.TotalProfit += bidAmt / 10
		}
		state.mu.Unlock()

	case fillErr == blend.ErrAlreadyFilled:
		// Another keeper won. Return drawn capital to vault (it won't profit).
		logInfo("auction already filled by another keeper", "user", short(user))
		state.addEvent(fmt.Sprintf("already filled: %s", short(user)))
		shouldReturn = bidAmt > 0

	default:
		// Hard fill failure — do not return proceeds; leave capital outstanding.
		return fmt.Errorf("fill auction: %w", fillErr)
	}

	if shouldReturn {
		proceeds := bidAmt + bidAmt/10
		if err := vault.ReturnProceeds(rpc, cfg.HorizonURL, kp, cfg.Passphrase, cfg.VaultID, proceeds, responseMs); err != nil {
			logWarn("return proceeds failed", "err", err)
			state.addEvent(fmt.Sprintf("return proceeds failed: %v", err))
		} else {
			logInfo("returned proceeds", "amount", proceeds, "response_ms", responseMs)
			state.addEvent(fmt.Sprintf("returned %d to vault", proceeds))
		}
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
