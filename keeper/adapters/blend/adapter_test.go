package blend

import (
	"testing"

	"github.com/nectar-network/keeper/adapters"
	core "github.com/nectar-network/keeper/blend"
	"github.com/nectar-network/keeper/soroban"
)

func TestAdapter_Name(t *testing.T) {
	a := NewAdapter(Config{}, nil)
	if a.Name() != "blend" {
		t.Fatalf("expected blend, got %s", a.Name())
	}
}

// GetTasks with an empty pool returns no work without touching the network.
func TestAdapter_GetTasks_NoPool(t *testing.T) {
	a := NewAdapter(Config{}, nil)
	tasks, err := a.GetTasks(soroban.NewClient("http://invalid.local"))
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(tasks) != 0 {
		t.Fatalf("expected no tasks, got %d", len(tasks))
	}
}

func TestAdapter_EstimateCapital(t *testing.T) {
	a := NewAdapter(Config{}, nil)
	got, err := a.EstimateCapital(adapters.Task{})
	if err != nil || got != 0 {
		t.Fatalf("expected (0,nil), got (%d,%v)", got, err)
	}
}

func TestPriorityFromHF(t *testing.T) {
	cases := []struct {
		hf   float64
		want int
	}{
		{0.4, 10}, {0.7, 7}, {0.9, 4}, {0.99, 1},
	}
	for _, c := range cases {
		if got := priorityFromHF(c.hf); got != c.want {
			t.Errorf("priorityFromHF(%.2f)=%d want %d", c.hf, got, c.want)
		}
	}
}

func TestOracleValueUSDC(t *testing.T) {
	pool := &core.PoolState{Reserves: map[string]*core.Reserve{
		"CTKN": {OraclePrice: 0.5},
	}}
	if got := oracleValueUSDC(pool, "CTKN", 100); got != 50 {
		t.Fatalf("expected 50 (100 * 0.5), got %d", got)
	}
	if got := oracleValueUSDC(pool, "UNKNOWN", 100); got != 0 {
		t.Fatalf("expected 0 for unknown asset, got %d", got)
	}
	if got := oracleValueUSDC(nil, "CTKN", 100); got != 0 {
		t.Fatalf("expected 0 for nil pool, got %d", got)
	}
}

// Adapter must satisfy the ProtocolAdapter interface.
var _ adapters.ProtocolAdapter = (*Adapter)(nil)
