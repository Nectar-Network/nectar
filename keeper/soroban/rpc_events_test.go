package soroban

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

// getEvents scans bounded ledger segments and an empty page with a cursor
// means "keep paging", not "no events" (observed live on testnet). The client
// must follow the cursor until it reaches the RPC's latest ledger.
func TestGetEvents_FollowsCursorAcrossEmptySegments(t *testing.T) {
	// TOIDs: ledger<<32. Page 1 covers ledgers up to 3929195 (empty, cursor
	// mid-window); page 2 has the events and a cursor at the latest ledger.
	const latest = int64(3936408)
	cursor1 := fmt.Sprintf("%d-0", uint64(3929195)<<32) // ledger < latest
	cursor2 := fmt.Sprintf("%d-0", uint64(latest)<<32)  // ledger >= latest

	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		requests = append(requests, req.Params)

		w.Header().Set("Content-Type", "application/json")
		page := len(requests)
		switch page {
		case 1:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"events":[],"cursor":"` + cursor1 + `","latestLedger":3936408}}`))
		default:
			_, _ = w.Write([]byte(`{"jsonrpc":"2.0","id":1,"result":{"events":[
				{"type":"contract","contractId":"CBUB","topic":["dG9waWMw","dG9waWMx"],"value":"dg==","ledger":3936350},
				{"type":"contract","contractId":"CBUB","topic":["dG9waWMw"],"value":"dg==","ledger":3936351}
			],"cursor":"` + cursor2 + `","latestLedger":3936408}}`))
		}
	}))
	defer srv.Close()

	events, err := NewClient(srv.URL).GetEvents(latest-17000, "CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V")
	if err != nil {
		t.Fatalf("GetEvents: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("events: got %d want 2 (empty first page must not end the scan)", len(events))
	}
	if len(requests) != 2 {
		t.Fatalf("requests: got %d want 2 (must stop once cursor reaches latest ledger)", len(requests))
	}
	// Paged request must use the cursor and omit startLedger per the RPC spec.
	if _, has := requests[1]["startLedger"]; has {
		t.Error("second request must omit startLedger when paging with a cursor")
	}
	pag, _ := requests[1]["pagination"].(map[string]any)
	if pag == nil || pag["cursor"] != cursor1 {
		t.Errorf("second request pagination: got %v want cursor %s", pag, cursor1)
	}
}

func TestCursorLedger(t *testing.T) {
	// A TOID is ledger<<32 | tx<<12 | op — the ledger must decode back out.
	toid := uint64(3936408)<<32 | 17<<12 | 3
	if got := cursorLedger(fmt.Sprintf("%d-2", toid)); got != 3936408 {
		t.Errorf("cursorLedger: got %d want 3936408", got)
	}
	if got := cursorLedger("garbage"); got != 0 {
		t.Errorf("garbage cursor: got %d want 0", got)
	}
	if got := cursorLedger(""); got != 0 {
		t.Errorf("empty cursor: got %d want 0", got)
	}
}
