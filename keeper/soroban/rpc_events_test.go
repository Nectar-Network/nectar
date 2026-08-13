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

// toid builds a getEvents cursor TOID for a ledger. The RPC's segment-end
// sentinel carries tx=0xFFFFF, op=0xFFF and event index 0xFFFFFFFF; a
// limit-bound cursor carries the real tx/op of the last returned event.
func toid(ledger uint64, tx, op uint64) uint64 { return ledger<<32 | tx<<12 | op }

func sentinelCursor(ledger uint64) string {
	return fmt.Sprintf("%d-4294967295", toid(ledger, 0xFFFFF, 0xFFF))
}

// A page that returns exactly `limit` events does NOT prove its 10000-ledger
// segment is drained (verified live 2026-08-13, docs/FACTS.md). Only the
// "-4294967295" sentinel proves that. So a limit-bound cursor must never let
// LastLedger jump past the last event actually seen — otherwise the next
// resume silently skips every event between them.
func TestScanEvents_LimitBoundCursorDoesNotOverclaimCoverage(t *testing.T) {
	const latest = int64(4124500)
	// Page 1 stops at the limit inside the segment: cursor names event ledger
	// 4100000, while the segment it was scanning runs to 4109999.
	limitCursor := fmt.Sprintf("%d-0000000001", toid(4100000, 3, 1))

	var requests []map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		requests = append(requests, req.Params)
		w.Header().Set("Content-Type", "application/json")
		switch len(requests) {
		case 1:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"events":[
				{"type":"contract","contractId":"CBUB","topic":["dG9waWMw"],"value":"dg==","ledger":4100000}
			],"cursor":%q,"latestLedger":%d}}`, limitCursor, latest)
		default:
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"events":[],"cursor":%q,"latestLedger":%d}}`,
				sentinelCursor(uint64(latest)), latest)
		}
	}))
	defer srv.Close()

	var seen int
	scan, err := NewClient(srv.URL).ScanEvents(4090000, "CBUB", func(Event) { seen++ })
	if err != nil {
		t.Fatalf("ScanEvents: %v", err)
	}
	if seen != 1 {
		t.Fatalf("events: got %d want 1", seen)
	}
	if scan.Truncated {
		t.Error("scan reached the latest ledger; Truncated must be false")
	}
	// After page 1 the client may only claim coverage to the last event it saw.
	// Page 2's sentinel then carries it to the head.
	if scan.LastLedger != latest {
		t.Errorf("LastLedger: got %d want %d (sentinel at head)", scan.LastLedger, latest)
	}
	if len(requests) != 2 {
		t.Fatalf("requests: got %d want 2 (a limit-bound cursor must not end the sweep)", len(requests))
	}
}

// A sweep that stops on a limit-bound cursor must report where it got to, so
// the caller resumes from there rather than assuming it is caught up.
func TestScanEvents_StopsAtHeadAndReportsLastLedger(t *testing.T) {
	const latest = int64(4124500)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"events":[
			{"type":"contract","contractId":"CBUB","topic":["dG9waWMw"],"value":"dg==","ledger":4124490}
		],"cursor":%q,"latestLedger":%d}}`, fmt.Sprintf("%d-0000000003", toid(uint64(latest), 9, 1)), latest)
	}))
	defer srv.Close()

	scan, err := NewClient(srv.URL).ScanEvents(4124000, "CBUB", func(Event) {})
	if err != nil {
		t.Fatalf("ScanEvents: %v", err)
	}
	// Cursor is at the head, so the sweep stops — but it was NOT a sentinel, so
	// coverage is only claimed to the last event seen.
	if scan.LastLedger != 4124490 {
		t.Errorf("LastLedger: got %d want 4124490 (last event, not the cursor)", scan.LastLedger)
	}
}
