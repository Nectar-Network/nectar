package defindex

import (
	"testing"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/adapters"
	"github.com/nectar-network/keeper/soroban"
)

func TestAdapter_Name(t *testing.T) {
	if NewAdapter(Config{}).Name() != "defindex" {
		t.Fatal("expected defindex")
	}
}

func TestAdapter_EstimateCapital_IsZero(t *testing.T) {
	got, err := NewAdapter(Config{}).EstimateCapital(adapters.Task{})
	if err != nil || got != 0 {
		t.Fatalf("rebalancing needs no Nectar capital; got (%d,%v)", got, err)
	}
}

func TestAdapter_GetTasks_NoVault(t *testing.T) {
	tasks, err := NewAdapter(Config{}).GetTasks(soroban.NewClient("http://invalid.local"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks with no vault, got %d", len(tasks))
	}
}

func TestPlanRebalance_WithinTolerance(t *testing.T) {
	a := NewAdapter(Config{DriftThreshold: 0.05})
	// 50/50 target, currently 52/48 → 2% drift, under 5% threshold.
	assets := []assetState{{
		asset: "USDC", total: 1000, idle: 0, invested: 1000,
		strategies: []strategyState{
			{address: "S1", amount: 520},
			{address: "S2", amount: 480},
		},
	}}
	if plan := a.planRebalance(assets); plan != nil {
		t.Fatalf("expected no plan within tolerance, got %+v", plan)
	}
}

func TestPlanRebalance_Drifted(t *testing.T) {
	a := NewAdapter(Config{DriftThreshold: 0.05})
	// 50/50 target, currently 800/200 → 30% drift. Expect Unwind S1, Invest S2.
	assets := []assetState{{
		asset: "USDC", total: 1_000_000_000, idle: 0, invested: 1_000_000_000,
		strategies: []strategyState{
			{address: "S1", amount: 800_000_000},
			{address: "S2", amount: 200_000_000},
		},
	}}
	plan := a.planRebalance(assets)
	if plan == nil {
		t.Fatal("expected a rebalance plan")
	}
	if len(plan.unwinds) != 1 || plan.unwinds[0].strategy != "S1" {
		t.Fatalf("expected one Unwind of S1, got %+v", plan.unwinds)
	}
	if len(plan.invests) != 1 || plan.invests[0].strategy != "S2" {
		t.Fatalf("expected one Invest of S2, got %+v", plan.invests)
	}
	// Unwind 300M out of S1, invest up to freed idle into S2.
	if plan.unwinds[0].amount != 300_000_000 {
		t.Fatalf("expected unwind 300000000, got %d", plan.unwinds[0].amount)
	}
	if plan.invests[0].amount > plan.unwinds[0].amount {
		t.Fatalf("invest must not exceed freed idle: invest=%d unwind=%d", plan.invests[0].amount, plan.unwinds[0].amount)
	}
	if plan.maxDrift < 0.29 || plan.maxDrift > 0.31 {
		t.Fatalf("expected ~0.30 drift, got %f", plan.maxDrift)
	}
}

func TestPlanRebalance_UnwindsPaused(t *testing.T) {
	a := NewAdapter(Config{DriftThreshold: 0.05})
	// S2 paused → equal-weight target puts 100% on S1; S2 should be fully unwound
	// and we must NOT invest into the paused S2.
	assets := []assetState{{
		asset: "USDC", total: 1_000_000_000, idle: 0, invested: 1_000_000_000,
		strategies: []strategyState{
			{address: "S1", amount: 500_000_000},
			{address: "S2", amount: 500_000_000, paused: true},
		},
	}}
	plan := a.planRebalance(assets)
	if plan == nil {
		t.Fatal("expected a plan")
	}
	for _, in := range plan.invests {
		if in.strategy == "S2" {
			t.Fatalf("must not invest into paused strategy S2")
		}
	}
	foundUnwindS2 := false
	for _, in := range plan.unwinds {
		if in.strategy == "S2" {
			foundUnwindS2 = true
		}
	}
	if !foundUnwindS2 {
		t.Fatal("expected paused S2 to be unwound toward 0 target")
	}
}

func TestTargetsFor_EqualWeightExcludesPaused(t *testing.T) {
	a := NewAdapter(Config{})
	as := assetState{strategies: []strategyState{
		{address: "S1"},
		{address: "S2", paused: true},
		{address: "S3"},
	}}
	tgt := a.targetsFor(as)
	if tgt["S1"] != 0.5 || tgt["S3"] != 0.5 {
		t.Fatalf("expected 0.5/0.5 across non-paused, got %+v", tgt)
	}
	if _, ok := tgt["S2"]; ok {
		t.Fatalf("paused strategy must have no target (gets unwound), got %+v", tgt)
	}
}

func TestInstructionsOrder_UnwindsBeforeInvests(t *testing.T) {
	p := &rebalancePlan{
		unwinds: []instruction{{kind: "Unwind", strategy: "S1", amount: 10}},
		invests: []instruction{{kind: "Invest", strategy: "S2", amount: 10}},
	}
	instrs := p.instructions()
	if instrs[0].kind != "Unwind" || instrs[1].kind != "Invest" {
		t.Fatalf("unwinds must precede invests so idle is freed first: %+v", instrs)
	}
}

func TestDriftPriority(t *testing.T) {
	if driftPriority(0.25) != 8 || driftPriority(0.12) != 5 || driftPriority(0.06) != 3 {
		t.Fatal("unexpected drift priority mapping")
	}
}

func TestScaleDown_LargeAmountsNoOverflow(t *testing.T) {
	// 1e13 stroops (10M USDC) factors would overflow a naive int64 multiply.
	if got := scaleDown(6_000_000_000_000, 5_000_000_000_000, 10_000_000_000_000); got != 3_000_000_000_000 {
		t.Fatalf("expected 3e12, got %d", got)
	}
	if got := scaleDown(100, 50, 0); got != 0 {
		t.Fatalf("den 0 must be 0, got %d", got)
	}
}

// Custom targets for one asset must never disturb another asset (which falls
// back to per-asset equal weight).
func TestPlanRebalance_CustomTargetsScopedPerAsset(t *testing.T) {
	a := NewAdapter(Config{
		DriftThreshold: 0.05,
		Targets: map[string]map[string]float64{
			"A": {"A1": 0.5, "A2": 0.5},
		},
	})
	assets := []assetState{
		{asset: "A", total: 1_000_000_000, invested: 1_000_000_000, strategies: []strategyState{
			{address: "A1", amount: 800_000_000}, {address: "A2", amount: 200_000_000},
		}},
		{asset: "B", total: 1_000_000_000, invested: 1_000_000_000, strategies: []strategyState{
			{address: "B1", amount: 500_000_000}, {address: "B2", amount: 500_000_000},
		}},
	}
	plan := a.planRebalance(assets)
	if plan == nil {
		t.Fatal("expected a plan for asset A's drift")
	}
	for _, in := range append(append([]instruction{}, plan.unwinds...), plan.invests...) {
		if in.strategy == "B1" || in.strategy == "B2" {
			t.Fatalf("balanced asset B must not be touched, got %+v", in)
		}
	}
}

func TestScI128_RejectsOutOfRange(t *testing.T) {
	// Hi=1 (value >= 2^64) does not fit int64 → 0 so the asset is skipped.
	hi1 := xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: 1, Lo: 0}}
	if got := scI128(hi1); got != 0 {
		t.Fatalf("out-of-range i128 must decode to 0, got %d", got)
	}
	// In-range positive decodes normally.
	ok := xdr.ScVal{Type: xdr.ScValTypeScvI128, I128: &xdr.Int128Parts{Hi: 0, Lo: 1_000_000_000}}
	if got := scI128(ok); got != 1_000_000_000 {
		t.Fatalf("expected 1e9, got %d", got)
	}
}

var _ adapters.ProtocolAdapter = (*Adapter)(nil)
