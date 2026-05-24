package vault

import (
	"strings"
	"testing"

	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/soroban"
)

func mustKP(t *testing.T) *keypair.Full {
	t.Helper()
	kp, err := keypair.Random()
	if err != nil {
		t.Fatalf("keypair: %v", err)
	}
	return kp
}

func TestDraw_RejectsNonPositiveAmount(t *testing.T) {
	rpc := soroban.NewClient("http://invalid.local")
	kp := mustKP(t)

	for _, amt := range []int64{0, -1, -100} {
		err := Draw(rpc, "http://invalid.local", kp, "Test SDF Network", "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM", amt)
		if err == nil {
			t.Errorf("draw(%d): expected error", amt)
			continue
		}
		if !strings.Contains(err.Error(), "amount must be > 0") {
			t.Errorf("draw(%d): unexpected error %v", amt, err)
		}
	}
}

func TestReturnProceeds_RejectsNonPositiveAmount(t *testing.T) {
	rpc := soroban.NewClient("http://invalid.local")
	kp := mustKP(t)

	for _, amt := range []int64{0, -1, -100} {
		err := ReturnProceeds(rpc, "http://invalid.local", kp, "Test SDF Network", "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAD2KM", amt, 100)
		if err == nil {
			t.Errorf("return_proceeds(%d): expected error", amt)
			continue
		}
		if !strings.Contains(err.Error(), "amount must be > 0") {
			t.Errorf("return_proceeds(%d): unexpected error %v", amt, err)
		}
	}
}

func TestVaultState_StructDefaults(t *testing.T) {
	// Sanity: zero-value VaultState has all fields at zero.
	var s VaultState
	if s.TotalUSDC != 0 || s.TotalShares != 0 || s.TotalProfit != 0 || s.ActiveLiq != 0 {
		t.Errorf("zero VaultState should be all zeros, got %+v", s)
	}
}

func TestBalanceResult_StructDefaults(t *testing.T) {
	var b BalanceResult
	if b.Shares != 0 || b.USDCValue != 0 {
		t.Errorf("zero BalanceResult should be all zeros, got %+v", b)
	}
}
