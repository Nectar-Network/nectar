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
type VaultClient interface {
	Draw(amount int64) error
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
