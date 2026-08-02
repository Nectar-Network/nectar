package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

const faucetTestAddr = "GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W"

func withFaucet(t *testing.T, f *Faucet) {
	t.Helper()
	old := faucet
	faucet = f
	t.Cleanup(func() { faucet = old })
}

func TestFaucetInfo_Disabled(t *testing.T) {
	withFaucet(t, nil)
	rec := httptest.NewRecorder()
	handleFaucetInfo(rec, httptest.NewRequest(http.MethodGet, "/api/faucet/info", nil))
	var resp faucetInfoResponse
	if err := json.NewDecoder(rec.Body).Decode(&resp); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if resp.Enabled {
		t.Fatal("faucet must report disabled when unconfigured")
	}
}

func TestFaucetClaim_Disabled(t *testing.T) {
	withFaucet(t, nil)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/faucet", strings.NewReader(`{"address":"`+faucetTestAddr+`"}`))
	handleFaucetClaim(rec, req)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status: got %d want 503", rec.Code)
	}
}

func TestFaucetClaim_BadAddress(t *testing.T) {
	kp, _ := keypair.Random()
	withFaucet(t, &Faucet{kp: kp, lastClaim: map[string]time.Time{}})
	for _, body := range []string{`{"address":"not-an-address"}`, `{}`, `garbage`} {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/api/faucet", strings.NewReader(body))
		handleFaucetClaim(rec, req)
		if rec.Code != http.StatusBadRequest {
			t.Errorf("body %q: got %d want 400", body, rec.Code)
		}
	}
}

func TestFaucetClaim_GetRejected(t *testing.T) {
	kp, _ := keypair.Random()
	withFaucet(t, &Faucet{kp: kp, lastClaim: map[string]time.Time{}})
	rec := httptest.NewRecorder()
	handleFaucetClaim(rec, httptest.NewRequest(http.MethodGet, "/api/faucet", nil))
	if rec.Code != http.StatusMethodNotAllowed {
		t.Fatalf("status: got %d want 405", rec.Code)
	}
}

// A claim inside the cooldown window is refused before any network I/O — the
// Faucet here has no rpc client, so reaching the trustline check would panic.
func TestFaucetClaim_CooldownBeforeAnyNetworkIO(t *testing.T) {
	kp, _ := keypair.Random()
	withFaucet(t, &Faucet{
		kp:       kp,
		cooldown: time.Hour,
		lastClaim: map[string]time.Time{
			faucetTestAddr: time.Now().Add(-time.Minute),
		},
	})
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/faucet", strings.NewReader(`{"address":"`+faucetTestAddr+`"}`))
	handleFaucetClaim(rec, req)
	if rec.Code != http.StatusTooManyRequests {
		t.Fatalf("status: got %d want 429", rec.Code)
	}
	var resp map[string]any
	_ = json.NewDecoder(rec.Body).Decode(&resp)
	if resp["error"] != "cooldown" {
		t.Fatalf("error: got %v want cooldown", resp["error"])
	}
	if retry, ok := resp["retryAfterSecs"].(float64); !ok || retry <= 0 || retry > 3600 {
		t.Fatalf("retryAfterSecs: got %v", resp["retryAfterSecs"])
	}
}

func TestDecodeScString(t *testing.T) {
	str := xdr.ScString("USDC:" + faucetTestAddr)
	b64, err := xdr.MarshalBase64(xdr.ScVal{Type: xdr.ScValTypeScvString, Str: &str})
	if err != nil {
		t.Fatal(err)
	}
	got, err := decodeScString(b64)
	if err != nil || got != "USDC:"+faucetTestAddr {
		t.Fatalf("got %q err %v", got, err)
	}
	if _, err := decodeScString("!!!"); err == nil {
		t.Fatal("garbage must error")
	}
}

func TestHasTrustline(t *testing.T) {
	const issuer = "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, "/notfunded"):
			w.WriteHeader(http.StatusNotFound)
		case strings.HasSuffix(r.URL.Path, "/notrust"):
			_, _ = w.Write([]byte(`{"balances":[{"asset_type":"native"}]}`))
		default:
			_, _ = w.Write([]byte(`{"balances":[{"asset_type":"native"},{"asset_code":"USDC","asset_issuer":"` + issuer + `"}]}`))
		}
	}))
	defer srv.Close()

	if ok, err := hasTrustline(srv.URL, "notfunded", "USDC", issuer); err != nil || ok {
		t.Errorf("unfunded: got (%v,%v) want (false,nil)", ok, err)
	}
	if ok, err := hasTrustline(srv.URL, "notrust", "USDC", issuer); err != nil || ok {
		t.Errorf("no trustline: got (%v,%v) want (false,nil)", ok, err)
	}
	if ok, err := hasTrustline(srv.URL, "funded", "USDC", issuer); err != nil || !ok {
		t.Errorf("trustline present: got (%v,%v) want (true,nil)", ok, err)
	}
}

// sacClassicAsset must reject a pure Soroban token whose name carries no
// CODE:ISSUER suffix (faucet supports classic-asset SACs only).
func TestSacClassicAsset(t *testing.T) {
	mkServer := func(name string) *httptest.Server {
		str := xdr.ScString(name)
		b64, err := xdr.MarshalBase64(xdr.ScVal{Type: xdr.ScValTypeScvString, Str: &str})
		if err != nil {
			t.Fatal(err)
		}
		resp := `{"jsonrpc":"2.0","id":1,"result":{"latestLedger":1,"results":[{"xdr":"` + b64 + `"}]}}`
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(resp))
		}))
	}

	sacSrv := mkServer("USDC:GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5")
	defer sacSrv.Close()
	code, issuer, err := sacClassicAsset(soroban.NewClient(sacSrv.URL), "pass", "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA")
	if err != nil || code != "USDC" || !strings.HasPrefix(issuer, "GBBD47IF") {
		t.Fatalf("SAC parse: got (%s,%s,%v)", code, issuer, err)
	}

	pureSrv := mkServer("Mock USD Coin")
	defer pureSrv.Close()
	if _, _, err := sacClassicAsset(soroban.NewClient(pureSrv.URL), "pass", "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"); err == nil {
		t.Fatal("pure Soroban token must be rejected")
	}
}
