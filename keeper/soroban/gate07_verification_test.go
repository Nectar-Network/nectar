// VERIFICATION TEST — Gate 0.7 (throwaway; not part of the keeper test suite)
//
// Proves our Go XDR encoding of Blend's submit() args matches the deployed
// contract's expected shape by running a SIMULATION ONLY (nothing is signed or
// sent) against the live TestnetV2 pool.
//
// Request shape per blend-contracts-v2 @ ba22b487 pool/src/pool/actions.rs:14-34:
//
//	struct Request { request_type: u32, address: Address, amount: i128 }
//	RequestType::Supply = 0
//
// Return shape per pool/src/pool/user.rs:11-15:
//
//	struct Positions { liabilities: Map<u32,i128>, collateral: Map<u32,i128>, supply: Map<u32,i128> }
//
// Run (network test, opt-in only):
//
//	GATE07_LIVE=1 GATE07_EVIDENCE=../../docs/evidence/gate-0-7-simulate.json \
//	  go test ./soroban -run TestGate07SubmitSimulateEncoding -v
package soroban

import (
	"encoding/json"
	"os"
	"testing"

	"github.com/stellar/go/xdr"
)

const (
	gate07RPC        = "https://soroban-testnet.stellar.org"
	gate07Passphrase = "Test SDF Network ; September 2015"
	// Blend TestnetV2 pool — docs/FACTS.md "Testnet addresses" (live-read 2026-08-03).
	gate07Pool = "CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF"
	// Native XLM SAC, reserve 0 of the pool (docs/FACTS.md).
	gate07XLM = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
	// Funded testnet account (our deployer/admin) used as from/spender/to and tx
	// source. Simulation only — no signature, no state change.
	gate07From = "GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W"
)

func TestGate07SubmitSimulateEncoding(t *testing.T) {
	if os.Getenv("GATE07_LIVE") == "" {
		t.Skip("Gate 0.7 verification test: set GATE07_LIVE=1 to run against testnet")
	}

	fromVal, err := ScvAddress(gate07From)
	if err != nil {
		t.Fatalf("encode from address: %v", err)
	}
	assetVal, err := ScvAddress(gate07XLM)
	if err != nil {
		t.Fatalf("encode asset address: %v", err)
	}

	// Request { request_type: 0 (Supply), address: XLM SAC, amount: 10_000 stroops }.
	// Map keys MUST be lexicographically sorted for Soroban Map<Symbol, Val>.
	reqMap := xdr.ScMap{
		{Key: ScvSymbol("address"), Val: assetVal},
		{Key: ScvSymbol("amount"), Val: ScvI128(10_000)},
		{Key: ScvSymbol("request_type"), Val: ScvU32(0)},
	}
	reqMapPtr := &reqMap
	reqVec := xdr.ScVec{{Type: xdr.ScValTypeScvMap, Map: &reqMapPtr}}
	reqVecPtr := &reqVec
	requestsVal := xdr.ScVal{Type: xdr.ScValTypeScvVec, Vec: &reqVecPtr}

	txXDR, err := buildTx(gate07Pool, "submit", []xdr.ScVal{fromVal, fromVal, fromVal, requestsVal},
		gate07From, 0, gate07Passphrase, nil)
	if err != nil {
		t.Fatalf("build tx: %v", err)
	}

	sim, err := NewClient(gate07RPC).Simulate(txXDR)
	if err != nil {
		t.Fatalf("simulateTransaction RPC: %v", err)
	}
	if sim.Error != "" {
		t.Fatalf("simulation returned error (encoding/shape mismatch?): %s", sim.Error)
	}
	if len(sim.Results) != 1 {
		t.Fatalf("expected 1 simulation result, got %d", len(sim.Results))
	}

	// The return value must decode as Positions: ScMap{collateral, liabilities, supply}.
	var ret xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &ret); err != nil {
		t.Fatalf("decode return value XDR: %v", err)
	}
	if ret.Type != xdr.ScValTypeScvMap || ret.Map == nil || *ret.Map == nil {
		t.Fatalf("expected Positions ScvMap return, got type %v", ret.Type)
	}
	keys := map[string]bool{}
	for _, entry := range **ret.Map {
		if entry.Key.Type == xdr.ScValTypeScvSymbol && entry.Key.Sym != nil {
			keys[string(*entry.Key.Sym)] = true
		}
	}
	for _, want := range []string{"collateral", "liabilities", "supply"} {
		if !keys[want] {
			t.Fatalf("Positions map missing key %q (got %v)", want, keys)
		}
	}

	// from.require_auth() must have produced at least one auth entry, and it must
	// decode — proving the full invocation tree round-trips through our XDR.
	if len(sim.Results[0].Auth) == 0 {
		t.Fatalf("expected simulation to require auth for from=%s, got none", gate07From)
	}
	var auth xdr.SorobanAuthorizationEntry
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].Auth[0], &auth); err != nil {
		t.Fatalf("decode auth entry XDR: %v", err)
	}
	var sorobanData xdr.SorobanTransactionData
	if err := xdr.SafeUnmarshalBase64(sim.TransactionData, &sorobanData); err != nil {
		t.Fatalf("decode transactionData XDR: %v", err)
	}

	t.Logf("simulation OK: latestLedger=%d minResourceFee=%s positionsKeys=%v authEntries=%d",
		sim.LatestLedger, sim.MinResourceFee, keys, len(sim.Results[0].Auth))

	if out := os.Getenv("GATE07_EVIDENCE"); out != "" {
		evidence := map[string]any{
			"gate":         "0.7",
			"date":         "2026-08-03",
			"network":      "testnet",
			"rpc":          gate07RPC,
			"pool":         gate07Pool,
			"request":      map[string]any{"request_type": 0, "address": gate07XLM, "amount": "10000"},
			"submit_args":  "from=spender=to=" + gate07From,
			"sent":         false,
			"latestLedger": sim.LatestLedger,
			"minResourceFee": sim.MinResourceFee,
			"returnValueXDR": sim.Results[0].XDR,
			"authEntryCount": len(sim.Results[0].Auth),
			"positionsKeys":  []string{"collateral", "liabilities", "supply"},
		}
		b, _ := json.MarshalIndent(evidence, "", "  ")
		if err := os.WriteFile(out, append(b, '\n'), 0o644); err != nil {
			t.Fatalf("write evidence file: %v", err)
		}
		t.Logf("evidence written to %s", out)
	}
}
