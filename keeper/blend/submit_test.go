package blend

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// decodeRequests pulls (type, address, amount) back out of an encoded
// Vec<Request> so the on-wire shape is asserted, not just the builder inputs.
func decodeRequests(t *testing.T, val xdr.ScVal) []PoolRequest {
	t.Helper()
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		t.Fatal("requests must encode as a vec")
	}
	var out []PoolRequest
	for _, item := range **val.Vec {
		if item.Type != xdr.ScValTypeScvMap || item.Map == nil || *item.Map == nil {
			t.Fatal("each request must be a map")
		}
		var r PoolRequest
		keys := make([]string, 0, 3)
		for _, e := range **item.Map {
			k := scSymbol(e.Key)
			keys = append(keys, k)
			switch k {
			case "request_type":
				r.Type = scU32(e.Val)
			case "amount":
				if v := scI128(e.Val); v != nil {
					r.Amount = v.Int64()
				}
			case "address":
				if e.Val.Type == xdr.ScValTypeScvAddress && e.Val.Address != nil {
					addr, err := soroban.ParseAddress(*e.Val.Address)
					if err != nil {
						t.Fatalf("address decode: %v", err)
					}
					r.Address = addr
				}
			}
		}
		// Soroban Map<Symbol,Val> requires lexicographically sorted keys.
		want := []string{"address", "amount", "request_type"}
		for i := range want {
			if keys[i] != want[i] {
				t.Fatalf("request keys must be sorted %v, got %v", want, keys)
			}
		}
		out = append(out, r)
	}
	return out
}

func TestRequestsVal_FillRepayWithdrawShape(t *testing.T) {
	const user = "GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W"
	const usdc = "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
	reqs := []PoolRequest{
		{Type: ReqFillUserLiquidation, Address: user, Amount: 100},
		{Type: ReqRepay, Address: usdc, Amount: 21_0000000},
		{Type: ReqWithdrawCollateral, Address: testAsset, Amount: MaxWithdraw},
	}
	val, err := requestsVal(reqs)
	if err != nil {
		t.Fatalf("requestsVal: %v", err)
	}
	got := decodeRequests(t, val)
	if len(got) != 3 {
		t.Fatalf("want 3 requests, got %d", len(got))
	}
	// Order matters: the pool applies requests sequentially, so the fill must
	// precede the repay (there is no debt to repay before it) and the
	// withdrawal must come last (collateral is only free once debt is gone).
	if got[0].Type != 6 || got[0].Address != user || got[0].Amount != 100 {
		t.Errorf("fill request: %+v", got[0])
	}
	if got[1].Type != 5 || got[1].Address != usdc || got[1].Amount != 21_0000000 {
		t.Errorf("repay request: %+v", got[1])
	}
	if got[2].Type != 3 || got[2].Address != testAsset || got[2].Amount != MaxWithdraw {
		t.Errorf("withdraw request: %+v", got[2])
	}
}

func TestFillAndUnwind_RejectsBadInputs(t *testing.T) {
	const user = "GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W"
	const usdc = "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
	rpc := soroban.NewClient("http://invalid.local")

	// Fill percent 0 would panic BadRequest on-chain (scale_auction).
	if _, err := FillAndUnwind(rpc, "http://invalid.local", nil, "pass", testAsset, user, 0, usdc, 1, nil); err == nil {
		t.Error("fill percent 0 must be rejected before any network call")
	}
	if _, err := FillAndUnwind(rpc, "http://invalid.local", nil, "pass", testAsset, user, 101, usdc, 1, nil); err == nil {
		t.Error("fill percent 101 must be rejected")
	}
	// Repaying nothing would leave the assumed debt outstanding.
	if _, err := FillAndUnwind(rpc, "http://invalid.local", nil, "pass", testAsset, user, 100, usdc, 0, nil); err == nil {
		t.Error("zero repay amount must be rejected")
	}
	if _, err := FillAndUnwind(rpc, "http://invalid.local", nil, "pass", testAsset, user, 100, "", 1, nil); err == nil {
		t.Error("empty settle asset must be rejected")
	}
}

func TestSubmitRequests_RejectsEmpty(t *testing.T) {
	rpc := soroban.NewClient("http://invalid.local")
	if _, err := SubmitRequests(rpc, "http://invalid.local", nil, "pass", testAsset, nil); err == nil {
		t.Fatal("empty request list must be rejected")
	}
}

// FindLiquidationPercent must use the pool's own verdict on each candidate:
// InvalidLiqTooSmall (1214) means go higher, InvalidLiqTooLarge (1213) lower.
// A fixed percentage (the old hardcoded 50) fails on most real positions.
func TestFindLiquidationPercent_BinarySearchesOnContractVerdict(t *testing.T) {
	const user = "GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W"
	// The synthetic pool accepts only percentages in [60, 80]; below is
	// "too small", above "too large".
	var tried []int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		pct := percentFromSimBody(t, string(body))
		tried = append(tried, pct)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case pct >= 60 && pct <= 80:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"latestLedger":1,"results":[{"xdr":"AAAAAQ=="}]}}`))
		case pct < 60:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"latestLedger":1,"error":"HostError: Error(Contract, #1214)"}}`))
		default:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"latestLedger":1,"error":"HostError: Error(Contract, #1213)"}}`))
		}
	}))
	defer srv.Close()

	got, err := FindLiquidationPercent(soroban.NewClient(srv.URL), "pass", testAsset, user,
		[]string{testAsset}, []string{testOracle})
	if err != nil {
		t.Fatalf("FindLiquidationPercent: %v", err)
	}
	if got != 60 {
		t.Errorf("percent: got %d want 60 (smallest sufficient liquidation)", got)
	}
	if len(tried) > 9 {
		t.Errorf("binary search should converge in ~8 simulations, took %d: %v", len(tried), tried)
	}
	if tried[0] != 100 {
		t.Errorf("full liquidation must be attempted first, got %d", tried[0])
	}
}

func TestFindLiquidationPercent_NoValidSize(t *testing.T) {
	const user = "GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"latestLedger":1,"error":"HostError: Error(Contract, #1214)"}}`))
	}))
	defer srv.Close()
	_, err := FindLiquidationPercent(soroban.NewClient(srv.URL), "pass", testAsset, user,
		[]string{testAsset}, []string{testOracle})
	if !errors.Is(err, ErrNoValidLiqPercent) {
		t.Fatalf("want ErrNoValidLiqPercent, got %v", err)
	}
}

// percentFromSimBody digs the u32 percent (last arg of new_auction) out of the
// simulated transaction envelope in a JSON-RPC request body.
func percentFromSimBody(t *testing.T, body string) int {
	t.Helper()
	var req struct {
		Params struct {
			Transaction string `json:"transaction"`
		} `json:"params"`
	}
	if err := json.Unmarshal([]byte(body), &req); err != nil {
		t.Fatalf("body decode: %v", err)
	}
	var env xdr.TransactionEnvelope
	if err := xdr.SafeUnmarshalBase64(req.Params.Transaction, &env); err != nil {
		t.Fatalf("envelope decode: %v", err)
	}
	ops := env.Operations()
	if len(ops) != 1 {
		t.Fatalf("want 1 op, got %d", len(ops))
	}
	inv := ops[0].Body.InvokeHostFunctionOp
	if inv == nil || inv.HostFunction.InvokeContract == nil {
		t.Fatal("not an invoke-contract op")
	}
	args := inv.HostFunction.InvokeContract.Args
	last := args[len(args)-1]
	if last.Type != xdr.ScValTypeScvU32 || last.U32 == nil {
		t.Fatalf("last arg must be u32 percent, got %v", last.Type)
	}
	return int(*last.U32)
}
