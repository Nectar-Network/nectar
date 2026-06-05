# Writing a Nectar Protocol Adapter

Nectar's keeper drives any number of Soroban protocols through one small
interface, `adapters.ProtocolAdapter`. Blend liquidations and DeFindex
rebalancing are both implemented as adapters; this guide shows how to add your
own. The interface is intentionally minimal — it is the contract that gets
extracted into the public `keeper-sdk` (Tranche 2 Phase 4), so keep adapters
free of keeper-daemon concerns (no logging, no global state).

> Module path: `github.com/nectar-network/keeper`. Adapters live under
> `keeper/adapters/<name>/`.

## The interface

```go
// keeper/adapters/adapter.go
type ProtocolAdapter interface {
    Name() string
    GetTasks(rpc *soroban.Client) ([]Task, error)
    Execute(rpc *soroban.Client, kp *keypair.Full, task Task, vault VaultClient) (*Result, error)
    EstimateCapital(task Task) (int64, error)
}
```

- **`Name()`** — stable identifier (`"blend"`, `"defindex"`). Used in logs and metrics.
- **`GetTasks(rpc)`** — scan the protocol this cycle and return actionable
  `Task`s. Pure discovery: do reads (`SimulateRead`), no writes. Return `nil`
  (not an error) when there is simply nothing to do or the adapter is
  unconfigured.
- **`Execute(rpc, kp, task, vault)`** — perform one task. Draw/return capital via
  the `VaultClient` only when the task actually needs Nectar capital. Return a
  `Result`; never log.
- **`EstimateCapital(task)`** — best-effort USDC needed (0 if the task uses no
  Nectar capital, e.g. a DeFindex rebalance).

### Task and Result

```go
type Task struct {
    Protocol  string  // your Name()
    Type      string  // "liquidation", "rebalance", …
    Target    string  // address / vault id the task acts on
    Priority  int     // 0..10; the keeper runs higher first (SortByPriority)
    EstProfit float64 // optional
    Health    float64 // optional (e.g. health factor / drift) for the dashboard
    Data      any     // your payload, threaded back to Execute
}

type Result struct {
    Success        bool
    TxHash         string
    Block          int64
    Drew           int64  // vault capital drawn (0 if none)
    Proceeds       int64  // USDC returned to the vault (0 if none)
    Profit         int64  // realized profit booked, max(0, proceeds-drew)
    ResponseTimeMs int64  // draw→act latency, fed to the registry metric
    Latency        time.Duration
    Note           string // human-readable status when not Success
}
```

Stash whatever `Execute` needs in `Task.Data` (a snapshot, a precomputed plan)
so you don't re-read the same state twice. `Data` is `any`; type-assert it back
in `Execute` and tolerate a failed assertion.

### VaultClient

```go
type VaultClient interface {
    Draw(amount int64) error
    ReturnProceeds(amount, responseTimeMs int64) error
}
```

The keeper passes a concrete `vault.Client`. Use it only when your task consumes
Nectar capital. **Only return proceeds when you actually drew** — the vault's
`drawn==0` path would otherwise book the return as cost-free profit.

## Conventions (match the existing adapters)

1. **No logging, no global state.** Adapters are libraries. Return values and
   errors; the keeper logs from the `Result`.
2. **Reads via `SimulateRead`, writes via `rpc.Invoke`.** Do **not** auto-retry
   state-changing calls — a re-broadcast can double-execute a non-idempotent
   action (a swap sold twice, a rebalance applied twice). Transient failures are
   retried on the next cycle.
3. **Measured, never synthesized.** Report real on-chain outcomes (balance
   deltas, returned amounts). Never fabricate profit.
4. **Encode with the `soroban.Scv*` builders.** `ScvAddress`, `ScvI128`,
   `ScvU64`, `ScvSymbol`, `ScvVec`, `ScvVoid`. Soroban structs are `ScMap` keyed
   by `Symbol` (sorted lexicographically); enums-with-fields are
   `Vec[Symbol(variant), field0, …]`; `Option::None` is `ScvVoid()`.
5. **Decode** with `xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val)` then walk
   the `ScVal` (remember `val.Vec`/`val.Map` are double pointers: `**val.Vec`).
6. **Fail fast on auth.** If your protocol gates the action behind a role
   (DeFindex `rebalance` needs `RebalanceManager`/`Manager`), check it in
   `Execute` and return a `Result{Note: …}` instead of submitting a doomed tx.

## Skeleton

```go
package myproto

type Config struct { ContractAddr, HorizonURL, Passphrase string }
type Adapter struct { cfg Config }

func NewAdapter(cfg Config) *Adapter { return &Adapter{cfg: cfg} }
func (a *Adapter) Name() string { return "myproto" }

func (a *Adapter) GetTasks(rpc *soroban.Client) ([]adapters.Task, error) {
    if a.cfg.ContractAddr == "" { return nil, nil }
    // SimulateRead → decode → detect work → return []adapters.Task
}

func (a *Adapter) Execute(rpc *soroban.Client, kp *keypair.Full, task adapters.Task, vc adapters.VaultClient) (*adapters.Result, error) {
    // optional: draw via vc; encode args; rpc.Invoke(...); build Result
}

func (a *Adapter) EstimateCapital(task adapters.Task) (int64, error) { return 0, nil }

var _ adapters.ProtocolAdapter = (*Adapter)(nil) // compile-time interface check
```

## Wiring it in

Register the adapter in `keeper/main.go` where the others are built:

```go
if cfg.MyProtoContract != "" {
    k.protocols = append(k.protocols, myproto.NewAdapter(myproto.Config{ /* … */ }))
}
```

The keeper runs every registered adapter each cycle: `GetTasks` →
`SortByPriority` → `Execute` per task → fold the `Result` into dashboard
state/metrics.

## Reference implementations

- **`keeper/adapters/blend`** — draws Nectar capital, fills a Blend auction,
  swaps the seized collateral to USDC (via the `dex` package), returns the real
  proceeds. Shows the capital-drawing flow and the `Task.Data` snapshot pattern.
- **`keeper/adapters/defindex`** — pure reallocation (no Nectar capital): reads
  `fetch_total_managed_funds`, computes drift vs target weights, and submits a
  role-gated `rebalance`. Shows struct/enum encode-decode and the auth pre-check.

## Testing

Follow the repo convention: unit-test the pure logic (planning, drift/slippage
math, decoders) and the no-RPC guards (`GetTasks` with an empty contract,
validation errors). Full on-chain execution is verified on testnet, not mocked.
Add `var _ adapters.ProtocolAdapter = (*Adapter)(nil)` so the interface is
enforced at compile time.
