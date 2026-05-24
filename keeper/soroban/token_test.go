package soroban

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// helper: mock RPC that returns a fixed simulateTransaction payload.
func mockRPCResponse(t *testing.T, sim map[string]any) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req map[string]any
		_ = json.NewDecoder(r.Body).Decode(&req)
		method, _ := req["method"].(string)
		if method != "simulateTransaction" {
			t.Fatalf("unexpected method %q", method)
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"jsonrpc": "2.0",
			"id":      req["id"],
			"result":  sim,
		})
	}))
}

func TestTokenBalance_NormalI128(t *testing.T) {
	// XDR for ScVal::I128{ hi=0, lo=100_0000000 } (100 USDC in 7-decimal stroops).
	// Hand-crafted base64; if Stellar XDR ever rewrites this we'll see a clear failure.
	const balanceXDR = "AAAACgAAAAAAAAAAAAAAADuaygA="
	srv := mockRPCResponse(t, map[string]any{
		"results": []map[string]any{
			{"xdr": balanceXDR},
		},
		"latestLedger": 1,
	})
	defer srv.Close()

	c := NewClient(srv.URL)
	holder := "GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF"
	token := "CAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA"
	_ = token
	_ = holder
	// We don't have a fake G/C-encoded address that decodes cleanly with strkey,
	// so we use a real one and just rely on the mock to short-circuit the RPC.
	bal, err := c.TokenBalance(
		"Test SDF Network ; September 2015",
		"CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
		"GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF",
	)
	if err != nil {
		t.Fatalf("balance err: %v", err)
	}
	if bal != 100_0000000 {
		t.Errorf("balance: got %d want 100_0000000", bal)
	}
}

func TestTokenBalance_EmptyResults(t *testing.T) {
	srv := mockRPCResponse(t, map[string]any{
		"results":      []map[string]any{},
		"latestLedger": 1,
	})
	defer srv.Close()

	c := NewClient(srv.URL)
	bal, err := c.TokenBalance(
		"Test SDF Network ; September 2015",
		"CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
		"GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF",
	)
	if err != nil {
		t.Fatalf("err: %v", err)
	}
	if bal != 0 {
		t.Errorf("empty result must return 0, got %d", bal)
	}
}

func TestTokenBalance_SimError(t *testing.T) {
	srv := mockRPCResponse(t, map[string]any{
		"error":        "HostError: NotInit",
		"latestLedger": 1,
	})
	defer srv.Close()

	c := NewClient(srv.URL)
	_, err := c.TokenBalance(
		"Test SDF Network ; September 2015",
		"CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC",
		"GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF",
	)
	if err == nil {
		t.Fatal("expected sim error to propagate")
	}
	if !strings.Contains(err.Error(), "NotInit") {
		t.Errorf("expected NotInit in error, got %v", err)
	}
}
