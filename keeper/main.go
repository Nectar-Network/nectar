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

	"github.com/nectar-network/keeper/adapters"
	blendadapter "github.com/nectar-network/keeper/adapters/blend"
	defindexadapter "github.com/nectar-network/keeper/adapters/defindex"
	"github.com/nectar-network/keeper/dex"
	"github.com/nectar-network/keeper/registry"
	"github.com/nectar-network/keeper/soroban"
	"github.com/nectar-network/keeper/vault"
)

const maxSSEClients = 100

// LiquidationRecord is appended on every successful auction fill.
type LiquidationRecord struct {
	User           string    `json:"user"`
	Block          int64     `json:"block"`
	Drew           int64     `json:"drew"`
	Proceeds       int64     `json:"proceeds"`
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

// Keeper runs a set of protocol adapters against shared vault capital each cycle.
type Keeper struct {
	rpc       *soroban.Client
	kp        *keypair.Full
	cfg       Config
	vault     *vault.Client
	protocols []adapters.ProtocolAdapter
}

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

	// Build the protocol adapters this keeper runs each cycle. The DEX client is
	// shared by adapters that convert collateral; nil when no router is set.
	var dexc *dex.SwapClient
	if cfg.SoroswapRouter != "" || cfg.PhoenixRouter != "" {
		dexc = dex.NewSwapClient(rpc, dexConfig(cfg))
	}
	k := &Keeper{
		rpc:   rpc,
		kp:    kp,
		cfg:   cfg,
		vault: vault.NewClient(rpc, kp, cfg.HorizonURL, cfg.Passphrase, cfg.VaultID),
	}
	k.protocols = append(k.protocols, blendadapter.NewAdapter(blendadapter.Config{
		PoolAddr:   cfg.BlendPool,
		MinProfit:  cfg.MinProfit,
		HorizonURL: cfg.HorizonURL,
		Passphrase: cfg.Passphrase,
		UsdcAddr:   cfg.UsdcAddr,
	}, dexc))
	if cfg.DeFindexVault != "" {
		k.protocols = append(k.protocols, defindexadapter.NewAdapter(defindexadapter.Config{
			VaultAddr:      cfg.DeFindexVault,
			HorizonURL:     cfg.HorizonURL,
			Passphrase:     cfg.Passphrase,
			DriftThreshold: float64(cfg.DriftBps) / 10000.0,
		}))
	}
	logInfo("adapters registered", "count", len(k.protocols))

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
			if err := k.cycle(); err != nil {
				logWarn("cycle error", "err", err)
				state.addEvent(fmt.Sprintf("cycle error: %v", err))
			}
		}
	}
}

// cycle runs every adapter once: scan tasks, execute them by priority, fold the
// results into dashboard state, then refresh vault + depositor balances.
func (k *Keeper) cycle() error {
	var posRows []posRow
	for _, ad := range k.protocols {
		tasks, err := ad.GetTasks(k.rpc)
		if err != nil {
			logWarn("get tasks failed", "protocol", ad.Name(), "err", err)
			state.addEvent(fmt.Sprintf("%s scan error: %v", ad.Name(), err))
			continue
		}
		adapters.SortByPriority(tasks)
		for _, task := range tasks {
			// Surface liquidation targets (with their health factor) on the
			// dashboard; rebalance/other task types are not positions and must
			// not push a drift value into the HF field.
			if task.Type == "liquidation" {
				posRows = append(posRows, posRow{Address: task.Target, HF: task.Health})
				if task.Health > 0 && task.Health < 1.0 {
					state.addEvent(fmt.Sprintf("underwater: %s hf=%.4f", short(task.Target), task.Health))
				}
			}
			logInfo("executing task", "protocol", task.Protocol, "type", task.Type,
				"target", short(task.Target), "priority", task.Priority)
			res, err := ad.Execute(k.rpc, k.kp, task, k.vault)
			if err != nil {
				logWarn("execute failed", "protocol", task.Protocol, "type", task.Type,
					"target", short(task.Target), "err", err)
				state.addEvent(fmt.Sprintf("%s %s failed: %s %v", task.Protocol, task.Type, short(task.Target), err))
				continue
			}
			k.recordResult(task, res)
		}
	}
	state.mu.Lock()
	state.Positions = posRows
	state.mu.Unlock()

	// refresh vault state
	if vs, err := vault.GetState(k.rpc, k.cfg.Passphrase, k.cfg.VaultID); err == nil {
		state.mu.Lock()
		state.VaultState = vs
		state.mu.Unlock()
	}

	// refresh depositor balances for the performance page
	if len(k.cfg.KnownDepositors) > 0 {
		var depRows []DepositorRow
		for _, addr := range k.cfg.KnownDepositors {
			bal, err := vault.Balance(k.rpc, k.cfg.Passphrase, k.cfg.VaultID, addr)
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

// recordResult folds an adapter Result into dashboard state and metrics.
func (k *Keeper) recordResult(task adapters.Task, res *adapters.Result) {
	if res == nil {
		return
	}
	if !res.Success {
		if res.Note != "" {
			logInfo("task not executed", "protocol", task.Protocol, "target", short(task.Target), "note", res.Note)
		}
		return
	}

	logInfo("task executed", "protocol", task.Protocol, "type", task.Type, "target", short(task.Target),
		"drew", res.Drew, "proceeds", res.Proceeds, "profit", res.Profit, "response_ms", res.ResponseTimeMs)
	state.addEvent(fmt.Sprintf("%s %s: %s drew=%d proceeds=%d", task.Protocol, task.Type, short(task.Target), res.Drew, res.Proceeds))

	if res.Drew > 0 && res.Proceeds == 0 {
		logWarn("fill succeeded but produced zero returnable proceeds — outstanding draw at slash risk",
			"protocol", task.Protocol, "target", short(task.Target), "drew", res.Drew)
		state.addEvent(fmt.Sprintf("zero proceeds, draw outstanding: %s", short(task.Target)))
	}

	// Liquidation-specific accounting; other task types (e.g. rebalance) only log.
	if task.Type == "liquidation" {
		appMet.liquidationsTotal.Add(1)
		state.mu.Lock()
		state.Liquidations = append(state.Liquidations, LiquidationRecord{
			User:           task.Target,
			Block:          res.Block,
			Drew:           res.Drew,
			Proceeds:       res.Proceeds,
			ResponseTimeMs: res.ResponseTimeMs,
			Timestamp:      time.Now().UTC(),
		})
		if ks := state.KeeperStats[k.cfg.KeeperName]; ks != nil {
			ks.Liquidations++
			ks.TotalProfit += res.Profit
		}
		state.mu.Unlock()
	}
}

// dexConfig projects the keeper Config into the dex package's Config.
func dexConfig(cfg Config) dex.Config {
	return dex.Config{
		HorizonURL:     cfg.HorizonURL,
		Passphrase:     cfg.Passphrase,
		UsdcAddr:       cfg.UsdcAddr,
		SoroswapRouter: cfg.SoroswapRouter,
		PhoenixRouter:  cfg.PhoenixRouter,
		SlippageBps:    cfg.SlippageBps,
	}
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
