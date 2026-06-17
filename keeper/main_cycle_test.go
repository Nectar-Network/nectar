package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/adapters"
	"github.com/nectar-network/keeper/soroban"
	"github.com/nectar-network/keeper/vault"
)

// fakeAdapter is a scriptable ProtocolAdapter for orchestration tests.
type fakeAdapter struct {
	name     string
	tasks    []adapters.Task
	scanErr  error
	results  map[string]*adapters.Result // by task.Target; nil entry → Execute error
	estimate map[string]int64            // by task.Target
	executed *[]string                   // shared execution-order log "name:target"
}

func (f *fakeAdapter) Name() string { return f.name }

func (f *fakeAdapter) GetTasks(_ *soroban.Client) ([]adapters.Task, error) {
	if f.scanErr != nil {
		return nil, f.scanErr
	}
	return f.tasks, nil
}

func (f *fakeAdapter) Execute(_ *soroban.Client, _ *keypair.Full, task adapters.Task, _ adapters.VaultClient) (*adapters.Result, error) {
	*f.executed = append(*f.executed, f.name+":"+task.Target)
	res, ok := f.results[task.Target]
	if !ok {
		return &adapters.Result{Success: true}, nil
	}
	if res == nil {
		return nil, fmt.Errorf("scripted execute failure")
	}
	return res, nil
}

func (f *fakeAdapter) EstimateCapital(task adapters.Task) (int64, error) {
	return f.estimate[task.Target], nil
}

// mockVaultStateServer serves a get_state simulation whose VaultState yields
// the given total/active (available = total - active) for every RPC call.
func mockVaultStateServer(t *testing.T, totalUsdc, activeLiq int64) *httptest.Server {
	t.Helper()
	entries := xdr.ScMap{
		{Key: soroban.ScvSymbol("active_liq"), Val: soroban.ScvI128(activeLiq)},
		{Key: soroban.ScvSymbol("total_profit"), Val: soroban.ScvI128(0)},
		{Key: soroban.ScvSymbol("total_shares"), Val: soroban.ScvI128(totalUsdc)},
		{Key: soroban.ScvSymbol("total_usdc"), Val: soroban.ScvI128(totalUsdc)},
	}
	entriesPtr := &entries
	b64, err := xdr.MarshalBase64(xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &entriesPtr})
	if err != nil {
		t.Fatalf("marshal state map: %v", err)
	}
	resp := `{"jsonrpc":"2.0","id":1,"result":{"latestLedger":1,"results":[{"xdr":"` + b64 + `"}]}}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
}

// withFreshState swaps the package-global dashboard state for the test and
// restores it afterwards (tests in this package run sequentially).
func withFreshState(t *testing.T) *State {
	t.Helper()
	old := state
	state = &State{KeeperStats: map[string]*KeeperStat{}}
	t.Cleanup(func() { state = old })
	return state
}

func testKeeper(t *testing.T, rpcURL string, protocols ...adapters.ProtocolAdapter) *Keeper {
	t.Helper()
	kp, err := keypair.Random()
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	rpc := soroban.NewClient(rpcURL)
	return &Keeper{
		rpc: rpc,
		kp:  kp,
		// UsdcAddr empty → recoverStaleDraw is a no-op (no RPC in tests).
		cfg:       Config{KeeperName: "test-keeper", VaultID: "CCXDLRE3IV5225LE3Z776KFB2VWD2MTXOJHAUKFA5RPYDJVOWCMHJ4U4"},
		vault:     vault.NewClient(rpc, kp, "http://invalid.local", "pass", "CCXDLRE3IV5225LE3Z776KFB2VWD2MTXOJHAUKFA5RPYDJVOWCMHJ4U4"),
		protocols: protocols,
	}
}

// Tasks from every adapter must execute in one global priority order, not
// per-adapter order — a critical liquidation on the second-registered adapter
// runs before a routine task on the first.
func TestCycle_InterleavesPrioritiesAcrossAdapters(t *testing.T) {
	withFreshState(t)
	var order []string
	a := &fakeAdapter{name: "alpha", executed: &order, tasks: []adapters.Task{
		{Protocol: "alpha", Type: "liquidation", Target: "A1", Priority: 1, Health: 0.99},
		{Protocol: "alpha", Type: "liquidation", Target: "A5", Priority: 5, Health: 0.7},
	}}
	b := &fakeAdapter{name: "beta", executed: &order, tasks: []adapters.Task{
		{Protocol: "beta", Type: "rebalance", Target: "B9", Priority: 9},
		{Protocol: "beta", Type: "rebalance", Target: "B3", Priority: 3},
	}}

	k := testKeeper(t, "http://invalid.local", a, b)
	if err := k.cycle(); err != nil {
		t.Fatalf("cycle: %v", err)
	}

	want := []string{"beta:B9", "alpha:A5", "beta:B3", "alpha:A1"}
	if len(order) != len(want) {
		t.Fatalf("executed %v, want %v", order, want)
	}
	for i := range want {
		if order[i] != want[i] {
			t.Fatalf("execution order %v, want %v", order, want)
		}
	}
}

// A scan failure in one adapter must not stop the others.
func TestCycle_ScanErrorIsIsolated(t *testing.T) {
	withFreshState(t)
	var order []string
	broken := &fakeAdapter{name: "broken", executed: &order, scanErr: fmt.Errorf("rpc down")}
	ok := &fakeAdapter{name: "ok", executed: &order, tasks: []adapters.Task{
		{Protocol: "ok", Type: "liquidation", Target: "T", Priority: 1},
	}}

	k := testKeeper(t, "http://invalid.local", broken, ok)
	if err := k.cycle(); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(order) != 1 || order[0] != "ok:T" {
		t.Fatalf("expected the healthy adapter to run, got %v", order)
	}
}

// When the vault state is readable, tasks whose EstimateCapital exceeds the
// available (total - active_liq) capital are skipped without executing.
func TestCycle_SkipsTasksExceedingAvailableCapital(t *testing.T) {
	withFreshState(t)
	srv := mockVaultStateServer(t, 1000, 900) // available = 100
	defer srv.Close()

	var order []string
	a := &fakeAdapter{
		name:     "alpha",
		executed: &order,
		tasks: []adapters.Task{
			{Protocol: "alpha", Type: "liquidation", Target: "BIG", Priority: 9},
			{Protocol: "alpha", Type: "liquidation", Target: "FITS", Priority: 1},
		},
		estimate: map[string]int64{"BIG": 200, "FITS": 50},
	}

	k := testKeeper(t, srv.URL, a)
	if err := k.cycle(); err != nil {
		t.Fatalf("cycle: %v", err)
	}
	if len(order) != 1 || order[0] != "alpha:FITS" {
		t.Fatalf("expected only the fundable task to execute, got %v", order)
	}
}

// recordResult must append liquidation records (with tx hash + keeper
// attribution), skip non-liquidation types, skip failures, and stay bounded.
func TestRecordResult_LiquidationAccounting(t *testing.T) {
	s := withFreshState(t)
	s.KeeperStats["test-keeper"] = &KeeperStat{Name: "test-keeper"}
	k := testKeeper(t, "http://invalid.local")

	k.recordResult(
		adapters.Task{Type: "liquidation", Target: "USER1"},
		&adapters.Result{Success: true, TxHash: "abc123", Drew: 100, Proceeds: 110, Profit: 10},
	)
	// Non-liquidation success: logged but not recorded as a fill.
	k.recordResult(
		adapters.Task{Type: "rebalance", Target: "VAULT"},
		&adapters.Result{Success: true, TxHash: "def456"},
	)
	// Failed task: never recorded.
	k.recordResult(
		adapters.Task{Type: "liquidation", Target: "USER2"},
		&adapters.Result{Success: false, Note: "not profitable"},
	)

	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Liquidations) != 1 {
		t.Fatalf("expected exactly 1 liquidation record, got %d", len(s.Liquidations))
	}
	rec := s.Liquidations[0]
	if rec.TxHash != "abc123" || rec.Keeper != "test-keeper" || rec.User != "USER1" {
		t.Fatalf("record missing attribution: %+v", rec)
	}
	if got, _ := json.Marshal(rec); !jsonHas(got, "tx_hash") || !jsonHas(got, "keeper") {
		t.Fatalf("record JSON must carry tx_hash and keeper: %s", got)
	}
	if s.KeeperStats["test-keeper"].Liquidations != 1 || s.KeeperStats["test-keeper"].TotalProfit != 10 {
		t.Fatalf("keeper stats not updated: %+v", s.KeeperStats["test-keeper"])
	}
}

func TestRecordResult_HistoryIsBounded(t *testing.T) {
	s := withFreshState(t)
	k := testKeeper(t, "http://invalid.local")
	for i := 0; i < maxLiquidationRecords+25; i++ {
		k.recordResult(
			adapters.Task{Type: "liquidation", Target: fmt.Sprintf("U%d", i)},
			&adapters.Result{Success: true},
		)
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	if len(s.Liquidations) != maxLiquidationRecords {
		t.Fatalf("expected history capped at %d, got %d", maxLiquidationRecords, len(s.Liquidations))
	}
	// Oldest entries are evicted first.
	if s.Liquidations[0].User != "U25" {
		t.Fatalf("expected ring eviction from the front, first user = %s", s.Liquidations[0].User)
	}
}

func jsonHas(b []byte, key string) bool {
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		return false
	}
	_, ok := m[key]
	return ok
}
