package soroban

import "testing"

// The testnet native SAC is a verified on-chain fact: reserve 0 of both the
// Blend TestnetV2 pool and the Nectar Sandbox, symbol()="native"
// (docs/FACTS.md "Testnet addresses", live-read 2026-08-03). The derivation
// must reproduce it exactly.
func TestNativeContractID_TestnetMatchesVerifiedSAC(t *testing.T) {
	got, err := NativeContractID("Test SDF Network ; September 2015")
	if err != nil {
		t.Fatalf("NativeContractID: %v", err)
	}
	const want = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
	if got != want {
		t.Fatalf("testnet native SAC: got %s want %s", got, want)
	}
}

func TestNativeContractID_DiffersByNetwork(t *testing.T) {
	tn, err := NativeContractID("Test SDF Network ; September 2015")
	if err != nil {
		t.Fatal(err)
	}
	mn, err := NativeContractID("Public Global Stellar Network ; September 2015")
	if err != nil {
		t.Fatal(err)
	}
	if tn == mn {
		t.Fatal("native SAC must differ between networks")
	}
}
