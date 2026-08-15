// Package adapters defines the generic ProtocolAdapter interface that every
// protocol integration implements, so the keeper can monitor and act on
// multiple Soroban protocols (Blend liquidations, DeFindex rebalances, …)
// through a single loop. The interface is intentionally minimal and
// protocol-agnostic — it is the contract extracted into the public keeper-sdk
// in Tranche 2 Phase 4.
package adapters

import (
	"sort"
	"time"

	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/soroban"
)

// Task is one actionable unit of work discovered by an adapter.
type Task struct {
	Protocol  string  // adapter Name(), e.g. "blend"
	Type      string  // "liquidation", "bad_debt", "interest", "rebalance", …
	Target    string  // position address, vault id, …
	Priority  int     // 0=low … 10=critical; higher runs first
	EstProfit float64 // estimated profit ratio (lot/bid), 0 if unknown
	Health    float64 // optional health factor for the target, 0 if n/a
	Data      any     // adapter-specific payload threaded back to Execute
}

// Result is the outcome of executing a Task.
type Result struct {
	Success        bool
	TxHash         string
	Block          int64         // ledger the task acted on (0 if n/a)
	Drew           int64         // vault capital drawn (0 if none)
	Proceeds       int64         // USDC returned to the vault (0 if none)
	Profit         int64         // realized profit booked, max(0, proceeds-drew)
	ResponseTimeMs int64         // observed draw→act latency for registry metrics
	Latency        time.Duration // total Execute wall-clock
	Note           string        // human-readable status (e.g. "already filled")
}

// VaultClient is the capital interface adapters use; the keeper supplies a
// concrete implementation (vault.Client). Kept minimal so adapters never touch
// RPC/keypair plumbing for draw/return.
//
// Draw declares the collateral asset the drawn capital targets (contract
// DECISION F-2a): the vault enforces its global and per-asset liquidation
// circuit breakers on-chain against this declaration.
type VaultClient interface {
	Draw(amount int64, asset string) error
	ReturnProceeds(amount, responseTimeMs int64) error
}

// ProtocolAdapter is implemented by every protocol integration.
type ProtocolAdapter interface {
	// Name is the protocol identifier ("blend", "defindex").
	Name() string
	// GetTasks scans the protocol for actionable work this cycle.
	GetTasks(rpc *soroban.Client) ([]Task, error)
	// Execute performs one task, drawing/returning vault capital as needed.
	Execute(rpc *soroban.Client, kp *keypair.Full, task Task, vault VaultClient) (*Result, error)
	// EstimateCapital returns the USDC needed to execute a task (0 if none).
	EstimateCapital(task Task) (int64, error)
}

// SortByPriority orders tasks highest-priority first (stable).
func SortByPriority(tasks []Task) {
	sort.SliceStable(tasks, func(i, j int) bool {
		return tasks[i].Priority > tasks[j].Priority
	})
}

// PositionHealth is one scanned position's health factor. Unpriced marks a
// position whose HF could not be computed (a reserve it touches has no usable
// oracle price); HF is meaningless then and the keeper must not act on it.
type PositionHealth struct {
	Address  string
	HF       float64
	Unpriced bool
}

// ScanReport summarizes an adapter's most recent GetTasks pass — everything
// the keeper observed, not just the actionable subset. Adapters that monitor
// an external market (e.g. one Blend pool each) expose it via ScanReporter so
// the main loop can log and publish per-pool state.
type ScanReport struct {
	Pool           string             // pool contract address ("" if n/a)
	Monitor        bool               // true = observe only, never execute
	Status         uint32             // protocol-specific status (Blend: 0=Active…6=Setup)
	Reserves       int                // reserves successfully loaded
	OracleDecimals uint32             // oracle price decimals
	Prices         map[string]float64 // asset address -> USD per whole token
	Positions      []PositionHealth   // every position seen this pass, with HF
	Note           string             // operational caveat (e.g. conversion route not viable)
	Discovery      *Discovery         // how the scanned set was arrived at (nil if n/a)
}

// Discovery describes one borrower-index sweep: what was read from chain and
// what it cost. It exists so an operator can tell "no underwater positions"
// apart from "we never looked" — the two used to be indistinguishable in the
// logs, and the second one is a silent liveness failure.
type Discovery struct {
	Backfill   bool  // swept from the RPC's oldest retained ledger
	FromLedger int64 // first ledger this sweep asked for
	ToLedger   int64 // covered-through mark the next sweep resumes at
	GapAtStart int64 // ledgers unreachable because the cache predates retention
	Events     int   // events handed to the classifier
	Truncated  bool  // a cap or an RPC error ended the sweep early
	Added      int   // addresses newly indexed this sweep
	Tracked    int   // addresses indexed for this pool
	Debtors    int   // of those, currently believed to carry debt
	Probed     int   // get_positions reads issued this cycle
	ProbeFails int   // of those, reads that errored
	// Phase timings, so an over-budget cycle points at its own cause instead of
	// leaving the operator to guess between pool reads, the event sweep and the
	// position probes. They have very different fixes.
	PoolLoadMs int64
	SyncMs     int64
	ProbeMs    int64
}

// ScanReporter is optionally implemented by adapters that can describe their
// last scan. The main loop type-asserts; adapters without it are unaffected.
type ScanReporter interface {
	LastScan() *ScanReport
}
