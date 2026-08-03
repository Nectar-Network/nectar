package main

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stellar/go/strkey"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

const (
	guardPool = "CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V"
	guardUSDC = "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
	guardXLM  = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
)

// reserveListServer answers get_reserve_list with the given assets.
func reserveListServer(t *testing.T, assets ...string) *httptest.Server {
	t.Helper()
	vals := make([]xdr.ScVal, 0, len(assets))
	for _, a := range assets {
		raw, err := strkey.Decode(strkey.VersionByteContract, a)
		if err != nil {
			t.Fatalf("decode %s: %v", a, err)
		}
		var h xdr.ContractId
		copy(h[:], raw)
		addr := xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract, ContractId: &h}
		vals = append(vals, xdr.ScVal{Type: xdr.ScValTypeScvAddress, Address: &addr})
	}
	b64, err := xdr.MarshalBase64(soroban.ScvVec(vals...))
	if err != nil {
		t.Fatal(err)
	}
	resp := `{"jsonrpc":"2.0","id":1,"result":{"latestLedger":1,"results":[{"xdr":"` + b64 + `"}]}}`
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(resp))
	}))
}

// A pool that really does list the vault's USDC stays active.
func TestVerifySettleAssets_MatchingPoolStaysActive(t *testing.T) {
	srv := reserveListServer(t, guardUSDC, guardXLM)
	defer srv.Close()
	cfg := Config{UsdcAddr: guardUSDC, BlendPools: []BlendPoolConfig{{Addr: guardPool}}}
	got := verifySettleAssets(soroban.NewClient(srv.URL), cfg)
	if len(got) != 1 || got[0].Monitor {
		t.Fatalf("matching pool must stay active, got %+v", got)
	}
}

// When the check cannot be performed (RPC down) the pool is demoted rather
// than exiting — a transient outage must not crash-loop the keeper, and
// monitor is the capital-safe direction.
func TestVerifySettleAssets_UnreadablePoolDemotedNotFatal(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-32000,"message":"boom"}}`))
	}))
	defer srv.Close()
	cfg := Config{UsdcAddr: guardUSDC, BlendPools: []BlendPoolConfig{{Addr: guardPool}}}
	got := verifySettleAssets(soroban.NewClient(srv.URL), cfg)
	if len(got) != 1 || !got[0].Monitor {
		t.Fatalf("unreadable pool must be demoted to monitor, got %+v", got)
	}
}

// Monitor pools are never checked — they hold no capital either way, and a
// monitor-only pool legitimately settles a different asset.
func TestVerifySettleAssets_MonitorPoolsSkipTheCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("monitor pool must not trigger a reserve read")
	}))
	defer srv.Close()
	cfg := Config{UsdcAddr: guardUSDC, BlendPools: []BlendPoolConfig{{Addr: guardPool, Monitor: true}}}
	got := verifySettleAssets(soroban.NewClient(srv.URL), cfg)
	if len(got) != 1 || !got[0].Monitor {
		t.Fatalf("monitor pool must pass through, got %+v", got)
	}
}

// With no vault USDC configured there is nothing to verify against.
func TestVerifySettleAssets_NoUsdcConfigured(t *testing.T) {
	cfg := Config{BlendPools: []BlendPoolConfig{{Addr: guardPool}}}
	got := verifySettleAssets(soroban.NewClient("http://invalid.local"), cfg)
	if len(got) != 1 || got[0].Monitor {
		t.Fatalf("no-USDC config must pass through unchanged, got %+v", got)
	}
}

// NOTE: the definitive-mismatch path calls os.Exit(1) by design (a pool
// configured active that cannot settle is an operator error, not a runtime
// condition), so it is exercised by the live keeper rather than in-process.
