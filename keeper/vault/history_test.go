package vault

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

const (
	histUserA = "GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF"
	histUserB = "GDQ7VA37AB7YRQ6CNNKFFWTR2QQ5Z232GPHX5U6IQCQFENTASBAV6DCV"
	histVault = "CDZR6VDCPQFOFFKKZ2KMVB67Z54LI5OY73NHBFVI6DR6RE6TL7NN7345"
)

// mustB64 marshals an ScVal to base64 XDR, the on-wire form of getEvents
// topics/values, failing the test on error.
func mustB64(t *testing.T, v xdr.ScVal) string {
	t.Helper()
	b, err := xdr.MarshalBase64(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return b
}

// mustAddrB64 encodes a Stellar address as a topic ScVal (Address).
func mustAddrB64(t *testing.T, addr string) string {
	t.Helper()
	v, err := soroban.ScvAddress(addr)
	if err != nil {
		t.Fatalf("ScvAddress: %v", err)
	}
	return mustB64(t, v)
}

// depositEvent builds a raw vault deposit event: topics=(Symbol,Address),
// value=(amount, shares).
func depositEvent(t *testing.T, addr string, amount, shares int64, ledger int64, tx, closedAt string) soroban.Event {
	t.Helper()
	return soroban.Event{
		Type:       "contract",
		ContractID: histVault,
		Topic: []string{
			mustB64(t, soroban.ScvSymbol("deposit")),
			mustAddrB64(t, addr),
		},
		Value:          mustB64(t, soroban.ScvVec(soroban.ScvI128(amount), soroban.ScvI128(shares))),
		Ledger:         ledger,
		TxHash:         tx,
		LedgerClosedAt: closedAt,
	}
}

// withdrawEvent builds a raw vault withdraw event: topics=(Symbol,Address),
// value=(shares, usdc_out) — note the reversed argument order vs deposit.
func withdrawEvent(t *testing.T, addr string, shares, usdcOut int64, ledger int64, tx, closedAt string) soroban.Event {
	t.Helper()
	return soroban.Event{
		Type:       "contract",
		ContractID: histVault,
		Topic: []string{
			mustB64(t, soroban.ScvSymbol("withdraw")),
			mustAddrB64(t, addr),
		},
		Value:          mustB64(t, soroban.ScvVec(soroban.ScvI128(shares), soroban.ScvI128(usdcOut))),
		Ledger:         ledger,
		TxHash:         tx,
		LedgerClosedAt: closedAt,
	}
}

func TestDecodeHistoryEvent(t *testing.T) {
	tests := []struct {
		name      string
		ev        soroban.Event
		ok        bool
		wantType  string
		wantAddr  string
		wantAmt   int64
		wantShare int64
	}{
		{
			name:      "deposit decodes amount then shares",
			ev:        depositEvent(t, histUserA, 100_0000000, 95_0000000, 42, "abc", "2026-06-19T10:00:00Z"),
			ok:        true,
			wantType:  "deposit",
			wantAddr:  histUserA,
			wantAmt:   100_0000000,
			wantShare: 95_0000000,
		},
		{
			name:      "withdraw normalizes shares/usdc_out into shares/amount",
			ev:        withdrawEvent(t, histUserB, 50_0000000, 52_0000000, 43, "def", "2026-06-19T11:00:00Z"),
			ok:        true,
			wantType:  "withdraw",
			wantAddr:  histUserB,
			wantAmt:   52_0000000, // usdc_out lands in Amount
			wantShare: 50_0000000, // shares burned
		},
		{
			name: "unrelated event symbol is skipped",
			ev: soroban.Event{
				Topic: []string{
					mustB64(t, soroban.ScvSymbol("draw")),
					mustAddrB64(t, histUserA),
				},
				Value: mustB64(t, soroban.ScvVec(soroban.ScvI128(1), soroban.ScvI128(2))),
			},
			ok: false,
		},
		{
			name: "too few topics is skipped",
			ev:   soroban.Event{Topic: []string{mustB64(t, soroban.ScvSymbol("deposit"))}},
			ok:   false,
		},
		{
			name: "non-vec data is skipped",
			ev: soroban.Event{
				Topic: []string{
					mustB64(t, soroban.ScvSymbol("deposit")),
					mustAddrB64(t, histUserA),
				},
				Value: mustB64(t, soroban.ScvI128(5)),
			},
			ok: false,
		},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			rec, ok := decodeHistoryEvent(tc.ev)
			if ok != tc.ok {
				t.Fatalf("ok: got %v want %v", ok, tc.ok)
			}
			if !tc.ok {
				return
			}
			if rec.Type != tc.wantType {
				t.Errorf("type: got %q want %q", rec.Type, tc.wantType)
			}
			if rec.Address != tc.wantAddr {
				t.Errorf("addr: got %q want %q", rec.Address, tc.wantAddr)
			}
			if rec.Amount != tc.wantAmt {
				t.Errorf("amount: got %d want %d", rec.Amount, tc.wantAmt)
			}
			if rec.Shares != tc.wantShare {
				t.Errorf("shares: got %d want %d", rec.Shares, tc.wantShare)
			}
		})
	}
}

func TestHistoryIndexer_AggregatesPerAddressNewestFirst(t *testing.T) {
	h := NewHistoryIndexer()
	h.add(mustDecode(t, depositEvent(t, histUserA, 100_0000000, 100_0000000, 10, "t1", "2026-06-19T10:00:00Z")))
	h.add(mustDecode(t, withdrawEvent(t, histUserA, 40_0000000, 41_0000000, 12, "t2", "2026-06-19T12:00:00Z")))
	h.add(mustDecode(t, depositEvent(t, histUserB, 5_0000000, 5_0000000, 11, "t3", "2026-06-19T11:00:00Z")))

	a := h.History(histUserA)
	if len(a) != 2 {
		t.Fatalf("userA history: got %d want 2", len(a))
	}
	// Newest first: ledger 12 (withdraw) before ledger 10 (deposit).
	if a[0].Ledger != 12 || a[0].Type != "withdraw" {
		t.Errorf("expected newest withdraw first, got ledger=%d type=%s", a[0].Ledger, a[0].Type)
	}
	if a[1].Ledger != 10 || a[1].Type != "deposit" {
		t.Errorf("expected deposit second, got ledger=%d type=%s", a[1].Ledger, a[1].Type)
	}
	if got := h.History(histUserB); len(got) != 1 {
		t.Fatalf("userB history: got %d want 1", len(got))
	}
	if got := h.History("GUNKNOWN"); got != nil {
		t.Errorf("unknown address: got %v want nil", got)
	}
}

func TestHistoryIndexer_DedupesAcrossCycles(t *testing.T) {
	h := NewHistoryIndexer()
	ev := depositEvent(t, histUserA, 1_0000000, 1_0000000, 7, "same", "2026-06-19T10:00:00Z")
	// Same event re-seen on overlapping getEvents windows must not duplicate.
	h.add(mustDecode(t, ev))
	h.add(mustDecode(t, ev))
	h.add(mustDecode(t, ev))
	if got := h.History(histUserA); len(got) != 1 {
		t.Fatalf("expected dedupe to 1 row, got %d", len(got))
	}
}

func TestHistoryIndexer_BoundsPerAddress(t *testing.T) {
	h := NewHistoryIndexer()
	// Insert more than the cap; only the newest maxHistoryPerAddr survive.
	total := maxHistoryPerAddr + 25
	for i := 0; i < total; i++ {
		tx := string(rune('a'+i%26)) + string(rune('0'+i/26))
		h.add(mustDecode(t, depositEvent(t, histUserA, 1_0000000, 1_0000000, int64(i+1), tx, "2026-06-19T10:00:00Z")))
	}
	got := h.History(histUserA)
	if len(got) != maxHistoryPerAddr {
		t.Fatalf("expected cap %d, got %d", maxHistoryPerAddr, len(got))
	}
	// Newest (highest ledger) must be retained at the front.
	if got[0].Ledger != int64(total) {
		t.Errorf("newest ledger: got %d want %d", got[0].Ledger, total)
	}
	// Oldest survivor's ledger is total-cap+1; anything older was evicted.
	if got[len(got)-1].Ledger != int64(total-maxHistoryPerAddr+1) {
		t.Errorf("oldest survivor ledger: got %d want %d", got[len(got)-1].Ledger, total-maxHistoryPerAddr+1)
	}
}

func TestHistoryIndexer_UpdateFromMockRPC(t *testing.T) {
	depB64 := mustB64(t, soroban.ScvVec(soroban.ScvI128(100_0000000), soroban.ScvI128(98_0000000)))
	wdrB64 := mustB64(t, soroban.ScvVec(soroban.ScvI128(20_0000000), soroban.ScvI128(21_0000000)))
	topicDep := mustB64(t, soroban.ScvSymbol("deposit"))
	topicWdr := mustB64(t, soroban.ScvSymbol("withdraw"))
	addrTopic := mustAddrB64(t, histUserA)

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		body := string(raw)
		w.Header().Set("Content-Type", "application/json")
		switch {
		case strings.Contains(body, "getLatestLedger"):
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"sequence":5000}}`))
		case strings.Contains(body, "getEvents"):
			resp := `{"jsonrpc":"2.0","id":1,"result":{"latestLedger":5000,"events":[` +
				`{"type":"contract","ledger":4990,"txHash":"h1","ledgerClosedAt":"2026-06-19T10:00:00Z","topic":["` + topicDep + `","` + addrTopic + `"],"value":"` + depB64 + `"},` +
				`{"type":"contract","ledger":4995,"txHash":"h2","ledgerClosedAt":"2026-06-19T11:00:00Z","topic":["` + topicWdr + `","` + addrTopic + `"],"value":"` + wdrB64 + `"}` +
				`]}}`
			_, _ = w.Write([]byte(resp))
		default:
			t.Fatalf("unexpected RPC body: %s", body)
		}
	}))
	defer srv.Close()

	h := NewHistoryIndexer()
	rpc := soroban.NewClient(srv.URL)
	if err := h.Update(rpc, histVault); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got := h.History(histUserA)
	if len(got) != 2 {
		t.Fatalf("expected 2 indexed events, got %d", len(got))
	}
	if got[0].Type != "withdraw" || got[0].TxHash != "h2" {
		t.Errorf("newest-first: got type=%s tx=%s", got[0].Type, got[0].TxHash)
	}
	if got[1].Type != "deposit" || got[1].Amount != 100_0000000 {
		t.Errorf("deposit row: got type=%s amount=%d", got[1].Type, got[1].Amount)
	}
	if got[0].Timestamp.IsZero() {
		t.Errorf("expected ledgerClosedAt parsed into Timestamp")
	}

	// A second Update over the same window must not duplicate (cross-cycle dedupe).
	if err := h.Update(rpc, histVault); err != nil {
		t.Fatalf("Update (2nd): %v", err)
	}
	if got := h.History(histUserA); len(got) != 2 {
		t.Fatalf("expected dedupe across cycles, got %d", len(got))
	}
}

func TestHistoryIndexer_UpdateEmptyVaultNoop(t *testing.T) {
	h := NewHistoryIndexer()
	if err := h.Update(soroban.NewClient("http://invalid.local"), ""); err != nil {
		t.Fatalf("empty vault should be a no-op, got %v", err)
	}
}

func mustDecode(t *testing.T, ev soroban.Event) HistoryEvent {
	t.Helper()
	rec, ok := decodeHistoryEvent(ev)
	if !ok {
		t.Fatalf("decodeHistoryEvent failed for %v", ev)
	}
	return rec
}
