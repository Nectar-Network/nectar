// Package defindex adapts the DeFindex vault protocol to the generic
// adapters.ProtocolAdapter interface as a proof of multi-protocol
// extensibility. Unlike the Blend adapter (which draws Nectar vault capital to
// fill auctions), DeFindex rebalancing only reshuffles the DeFindex vault's OWN
// funds between strategies, so it never draws Nectar capital. It detects
// allocation drift vs target weights and submits a rebalance instruction set.
//
// ABIs verified against paltalabs/defindex (main):
//   - fetch_total_managed_funds() -> Vec<CurrentAssetInvestmentAllocation>
//   - rebalance(caller: Address, instructions: Vec<Instruction>)
//     Instruction::Unwind(Address, i128) / Invest(Address, i128) encode as a
//     Soroban enum: Vec[Symbol(variant), strategy_address, amount].
//   - rebalance is role-gated (RebalanceManager or Manager); the keeper must be
//     assigned that role on-chain or Execute returns a clear, non-fatal note.
package defindex

import (
	"fmt"
	"math/big"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/adapters"
	"github.com/nectar-network/keeper/soroban"
)

// dustAmount ignores sub-0.01 (7-decimal) deltas so tiny rounding never emits
// an instruction.
const dustAmount int64 = 100000

// Config holds the DeFindex vault and rebalance policy.
type Config struct {
	VaultAddr      string
	HorizonURL     string
	Passphrase     string
	DriftThreshold float64 // fraction, e.g. 0.05 = 5%; default 0.05
	// Targets maps asset_address -> (strategy_address -> target weight). Each
	// asset's weights should sum to ~1.0 over its strategies. An asset absent
	// here (or empty Targets) falls back to equal weight across that asset's
	// non-paused strategies, so configuring one asset never disturbs another.
	Targets map[string]map[string]float64
}

// Adapter implements adapters.ProtocolAdapter for DeFindex.
type Adapter struct {
	cfg Config
}

// NewAdapter builds a DeFindex adapter; a zero DriftThreshold defaults to 5%.
func NewAdapter(cfg Config) *Adapter {
	if cfg.DriftThreshold <= 0 {
		cfg.DriftThreshold = 0.05
	}
	return &Adapter{cfg: cfg}
}

// Name returns the protocol identifier.
func (a *Adapter) Name() string { return "defindex" }

type strategyState struct {
	address string
	amount  int64
	paused  bool
}

type assetState struct {
	asset      string
	total      int64
	idle       int64
	invested   int64
	strategies []strategyState
}

type instruction struct {
	kind     string // "Unwind" | "Invest"
	strategy string
	amount   int64
}

type rebalancePlan struct {
	unwinds  []instruction
	invests  []instruction
	maxDrift float64
}

func (p *rebalancePlan) instructions() []instruction {
	out := make([]instruction, 0, len(p.unwinds)+len(p.invests))
	out = append(out, p.unwinds...) // unwinds first so idle is freed before invests
	out = append(out, p.invests...)
	return out
}

// GetTasks reads the vault's managed funds and, if any asset's allocation has
// drifted beyond the threshold, returns a single rebalance task carrying the
// computed instruction plan.
func (a *Adapter) GetTasks(rpc *soroban.Client) ([]adapters.Task, error) {
	if a.cfg.VaultAddr == "" {
		return nil, nil
	}
	assets, err := a.fetchManagedFunds(rpc)
	if err != nil {
		return nil, err
	}
	plan := a.planRebalance(assets)
	if plan == nil {
		return nil, nil
	}
	return []adapters.Task{{
		Protocol: a.Name(),
		Type:     "rebalance",
		Target:   a.cfg.VaultAddr,
		Priority: driftPriority(plan.maxDrift),
		Health:   plan.maxDrift,
		Data:     plan,
	}}, nil
}

// Execute submits the rebalance plan, after confirming the keeper holds the
// RebalanceManager/Manager role (an unauthorized call would always revert).
func (a *Adapter) Execute(rpc *soroban.Client, kp *keypair.Full, task adapters.Task, _ adapters.VaultClient) (*adapters.Result, error) {
	start := time.Now()
	plan, ok := task.Data.(*rebalancePlan)
	if !ok || plan == nil {
		return &adapters.Result{Note: "no rebalance plan"}, nil
	}
	instrs := plan.instructions()
	if len(instrs) == 0 {
		return &adapters.Result{Note: "empty rebalance plan"}, nil
	}
	if authorized, who := a.isAuthorized(rpc, kp.Address()); !authorized {
		return &adapters.Result{Note: fmt.Sprintf("keeper not authorized to rebalance (need RebalanceManager/Manager; on-chain %s)", who)}, nil
	}
	hash, err := a.rebalance(rpc, kp, instrs)
	if err != nil {
		return nil, fmt.Errorf("rebalance: %w", err)
	}
	return &adapters.Result{
		Success: true,
		TxHash:  hash,
		Latency: time.Since(start),
		Note:    fmt.Sprintf("rebalanced %d instructions (max drift %.2f%%)", len(instrs), plan.maxDrift*100),
	}, nil
}

// EstimateCapital is always 0: rebalancing moves the DeFindex vault's own funds.
func (a *Adapter) EstimateCapital(task adapters.Task) (int64, error) {
	return 0, nil
}

// planRebalance computes Unwind/Invest instructions to move each drifted asset
// back toward its target weights. Returns nil when everything is within
// tolerance. Invests are capped to the idle freed by unwinds so the vault never
// tries to deploy more than it holds.
func (a *Adapter) planRebalance(assets []assetState) *rebalancePlan {
	plan := &rebalancePlan{}
	for _, as := range assets {
		if as.total <= 0 || len(as.strategies) == 0 {
			continue
		}
		targets := a.targetsFor(as)

		type adj struct {
			strat string
			delta int64
		}
		var unwinds, invests []adj
		var assetDrift float64
		for _, s := range as.strategies {
			tw := targets[s.address]
			curW := float64(s.amount) / float64(as.total)
			if d := absf(curW - tw); d > assetDrift {
				assetDrift = d
			}
			desired := int64(float64(as.total) * tw)
			delta := desired - s.amount
			switch {
			case delta < -dustAmount:
				unwinds = append(unwinds, adj{s.address, -delta}) // pull out the excess
			case delta > dustAmount && !s.paused:
				invests = append(invests, adj{s.address, delta})
			}
		}
		if assetDrift < a.cfg.DriftThreshold {
			continue
		}
		plan.maxDrift = maxf(plan.maxDrift, assetDrift)

		availableIdle := as.idle
		for _, u := range unwinds {
			plan.unwinds = append(plan.unwinds, instruction{kind: "Unwind", strategy: u.strat, amount: u.delta})
			availableIdle += u.delta
		}
		var investTotal int64
		for _, v := range invests {
			investTotal += v.delta
		}
		for _, v := range invests {
			amt := v.delta
			if investTotal > availableIdle && investTotal > 0 {
				amt = scaleDown(v.delta, availableIdle, investTotal) // 128-bit-safe proportional cap to idle on hand
			}
			if amt > dustAmount {
				plan.invests = append(plan.invests, instruction{kind: "Invest", strategy: v.strat, amount: amt})
			}
		}
	}
	if len(plan.unwinds) == 0 && len(plan.invests) == 0 {
		return nil
	}
	return plan
}

// targetsFor returns the configured target weights, or an equal weight across
// non-paused strategies (paused strategies target 0 so they get unwound).
func (a *Adapter) targetsFor(as assetState) map[string]float64 {
	if t, ok := a.cfg.Targets[as.asset]; ok && len(t) > 0 {
		return t
	}
	active := 0
	for _, s := range as.strategies {
		if !s.paused {
			active++
		}
	}
	if active == 0 {
		return map[string]float64{}
	}
	w := 1.0 / float64(active)
	m := make(map[string]float64, len(as.strategies))
	for _, s := range as.strategies {
		if !s.paused {
			m[s.address] = w
		}
	}
	return m
}

func (a *Adapter) fetchManagedFunds(rpc *soroban.Client) ([]assetState, error) {
	sim, err := rpc.SimulateRead(a.cfg.Passphrase, a.cfg.VaultAddr, "fetch_total_managed_funds")
	if err != nil {
		return nil, fmt.Errorf("fetch_total_managed_funds: %w", err)
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("fetch_total_managed_funds: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return nil, nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil, err
	}
	return parseManagedFunds(val), nil
}

// rebalance builds and submits the rebalance(caller, instructions) call. Not
// retried — a re-broadcast could double-apply the moves.
func (a *Adapter) rebalance(rpc *soroban.Client, kp *keypair.Full, instrs []instruction) (string, error) {
	callerVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return "", err
	}
	instrVals := make([]xdr.ScVal, 0, len(instrs))
	for _, in := range instrs {
		stratVal, err := soroban.ScvAddress(in.strategy)
		if err != nil {
			return "", err
		}
		// Soroban enum variant with fields: Vec[Symbol(variant), field0, field1].
		instrVals = append(instrVals, soroban.ScvVec(soroban.ScvSymbol(in.kind), stratVal, soroban.ScvI128(in.amount)))
	}
	tx, err := rpc.Invoke(a.cfg.HorizonURL, kp, a.cfg.Passphrase, a.cfg.VaultAddr, "rebalance",
		callerVal, soroban.ScvVec(instrVals...))
	if err != nil {
		return "", err
	}
	return tx.Hash, nil
}

// isAuthorized reports whether addr currently holds the RebalanceManager or
// Manager role on the vault.
func (a *Adapter) isAuthorized(rpc *soroban.Client, addr string) (bool, string) {
	rm, _ := a.readAddress(rpc, "get_rebalance_manager")
	if rm == addr {
		return true, rm
	}
	mgr, _ := a.readAddress(rpc, "get_manager")
	if mgr == addr {
		return true, mgr
	}
	if rm == "" && mgr == "" {
		return false, "roles unreadable"
	}
	return false, fmt.Sprintf("rebalance_manager=%s manager=%s", rm, mgr)
}

func (a *Adapter) readAddress(rpc *soroban.Client, fn string) (string, error) {
	sim, err := rpc.SimulateRead(a.cfg.Passphrase, a.cfg.VaultAddr, fn)
	if err != nil {
		return "", err
	}
	if sim.Error != "" {
		return "", fmt.Errorf("%s: %s", fn, sim.Error)
	}
	if len(sim.Results) == 0 {
		return "", nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return "", err
	}
	return scAddress(val), nil
}

func driftPriority(d float64) int {
	switch {
	case d >= 0.2:
		return 8
	case d >= 0.1:
		return 5
	default:
		return 3
	}
}

// ── ScVal decoding ────────────────────────────────────────────────────────────

func parseManagedFunds(val xdr.ScVal) []assetState {
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		return nil
	}
	var out []assetState
	for _, item := range **val.Vec {
		if item.Type != xdr.ScValTypeScvMap || item.Map == nil || *item.Map == nil {
			continue
		}
		as := assetState{}
		for _, e := range **item.Map {
			switch scSymbol(e.Key) {
			case "asset":
				as.asset = scAddress(e.Val)
			case "total_amount":
				as.total = scI128(e.Val)
			case "idle_amount":
				as.idle = scI128(e.Val)
			case "invested_amount":
				as.invested = scI128(e.Val)
			case "strategy_allocations":
				as.strategies = parseStrategyAllocs(e.Val)
			}
		}
		out = append(out, as)
	}
	return out
}

func parseStrategyAllocs(val xdr.ScVal) []strategyState {
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		return nil
	}
	var out []strategyState
	for _, item := range **val.Vec {
		if item.Type != xdr.ScValTypeScvMap || item.Map == nil || *item.Map == nil {
			continue
		}
		s := strategyState{}
		for _, e := range **item.Map {
			switch scSymbol(e.Key) {
			case "amount":
				s.amount = scI128(e.Val)
			case "paused":
				s.paused = scBool(e.Val)
			case "strategy_address":
				s.address = scAddress(e.Val)
			}
		}
		out = append(out, s)
	}
	return out
}

func scSymbol(v xdr.ScVal) string {
	if v.Type == xdr.ScValTypeScvSymbol && v.Sym != nil {
		return string(*v.Sym)
	}
	return ""
}

func scI128(v xdr.ScVal) int64 {
	// The rebalance math is int64; out-of-range amounts return 0 so the asset is
	// skipped (total<=0 guard) rather than driving the planner with a wrapped value.
	n, _ := soroban.I128ToInt64(v)
	return n
}

func scBool(v xdr.ScVal) bool {
	return v.Type == xdr.ScValTypeScvBool && v.B != nil && *v.B
}

func scAddress(v xdr.ScVal) string {
	if v.Type == xdr.ScValTypeScvAddress && v.Address != nil {
		if s, err := soroban.ParseAddress(*v.Address); err == nil {
			return s
		}
	}
	return ""
}

// scaleDown returns x*num/den computed in 128-bit to avoid int64 overflow when
// x and num are both large stroop amounts. Returns 0 if the result doesn't fit.
func scaleDown(x, num, den int64) int64 {
	if den == 0 {
		return 0
	}
	r := new(big.Int).Mul(big.NewInt(x), big.NewInt(num))
	r.Div(r, big.NewInt(den))
	if !r.IsInt64() {
		return 0
	}
	return r.Int64()
}

func absf(x float64) float64 {
	if x < 0 {
		return -x
	}
	return x
}

func maxf(a, b float64) float64 {
	if a > b {
		return a
	}
	return b
}
