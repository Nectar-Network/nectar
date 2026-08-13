package blend

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

const (
	ixPool     = "CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V"
	ixBackstop = "CCT4FMLHPJVYBC6SCAOFIRSYU74ZBU36ADREQUQEFCN5C5MRG26S6PTH"
	ixUsdc     = "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
	ixBorrower = "GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF"
	ixFiller   = "GATK27P6LOQBSXMVCYBBSKPUYKX5HVZ5AI4AAKF7UEYNKELSEBH53P7W"
)

func b64(t *testing.T, v xdr.ScVal) string {
	t.Helper()
	s, err := xdr.MarshalBase64(v)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	return s
}

func addrB64(t *testing.T, addr string) string {
	t.Helper()
	v, err := soroban.ScvAddress(addr)
	if err != nil {
		t.Fatalf("ScvAddress(%s): %v", addr, err)
	}
	return b64(t, v)
}

// ev builds a pool event from already-encoded topics.
func ev(ledger int64, value string, topics ...string) soroban.Event {
	return soroban.Event{Type: "contract", ContractID: ixPool, Topic: topics, Value: value, Ledger: ledger}
}

func noSkip(string) bool { return false }

// The six action events put the ASSET at topic[1] and the USER at topic[2],
// bad_debt inverts that to [user, asset], and the auction events put a u32 at
// topic[1]. Indexing topics positionally without reading the event name is
// therefore wrong — this pins the verified layouts (docs/FACTS.md).
func TestClassify_TopicLayoutsPerEventName(t *testing.T) {
	action := func(name string) soroban.Event {
		return ev(100, b64(t, soroban.ScvVec(soroban.ScvI128(1), soroban.ScvI128(2))),
			b64(t, soroban.ScvSymbol(name)), addrB64(t, ixUsdc), addrB64(t, ixBorrower))
	}

	t.Run("borrow proves debt at topic[2]", func(t *testing.T) {
		got := classify(action("borrow"), noSkip)
		if len(got) != 1 || got[0].addr != ixBorrower || !got[0].debt {
			t.Fatalf("got %+v, want one debt fact for the topic[2] user", got)
		}
	})

	t.Run("supply_collateral is a candidate, not a debtor", func(t *testing.T) {
		got := classify(action("supply_collateral"), noSkip)
		if len(got) != 1 || got[0].addr != ixBorrower {
			t.Fatalf("got %+v, want one fact for the topic[2] user", got)
		}
		if got[0].debt || got[0].cleared {
			t.Errorf("supply_collateral proves nothing about debt, got %+v", got[0])
		}
	})

	t.Run("repay is a candidate — it publishes deltas only", func(t *testing.T) {
		// The event cannot say whether the position reached zero, so it must
		// not be treated as either proof.
		got := classify(action("repay"), noSkip)
		if len(got) != 1 || got[0].debt || got[0].cleared {
			t.Fatalf("got %+v, want a bare candidate", got)
		}
	})

	t.Run("bad_debt inverts the topics and proves zero", func(t *testing.T) {
		e := ev(101, b64(t, soroban.ScvI128(5000)),
			b64(t, soroban.ScvSymbol("bad_debt")), addrB64(t, ixBorrower), addrB64(t, ixUsdc))
		got := classify(e, noSkip)
		if len(got) != 1 || got[0].addr != ixBorrower || !got[0].cleared {
			t.Fatalf("got %+v, want a cleared fact for the topic[1] user", got)
		}
	})

	t.Run("defaulted_debt names no debtor", func(t *testing.T) {
		e := ev(102, b64(t, soroban.ScvI128(5000)),
			b64(t, soroban.ScvSymbol("defaulted_debt")), addrB64(t, ixUsdc))
		if got := classify(e, noSkip); len(got) != 0 {
			t.Fatalf("got %+v, want nothing", got)
		}
	})

	t.Run("asset addresses are excluded", func(t *testing.T) {
		skip := func(a string) bool { return a == ixUsdc }
		got := classify(action("supply"), skip)
		if len(got) != 1 || got[0].addr != ixBorrower {
			t.Fatalf("got %+v, want only the user", got)
		}
	})
}

// Auction events key on a u32 auction_type at topic[1]. topic[2] is a real
// borrower ONLY for type 0; for bad-debt (1) and interest (2) the contract
// requires user == backstop, so indexing topic[2] would spend a probe every
// cycle on an address that can never be user-liquidated.
func TestClassify_AuctionTypeDecidesWhetherTopic2IsABorrower(t *testing.T) {
	auction := func(name string, atype uint32, user string, value string) soroban.Event {
		return ev(200, value,
			b64(t, soroban.ScvSymbol(name)), b64(t, soroban.ScvU32(atype)), addrB64(t, user))
	}
	void := b64(t, xdr.ScVal{Type: xdr.ScValTypeScvVoid})

	t.Run("type 0 topic[2] is the liquidated borrower", func(t *testing.T) {
		got := classify(auction("new_auction", 0, ixBorrower, void), noSkip)
		if len(got) != 1 || got[0].addr != ixBorrower || !got[0].debt {
			t.Fatalf("got %+v, want a debt fact", got)
		}
	})

	t.Run("type 1 topic[2] is the backstop and is ignored", func(t *testing.T) {
		if got := classify(auction("new_auction", 1, ixBackstop, void), noSkip); len(got) != 0 {
			t.Fatalf("got %+v, want nothing — topic[2] is the backstop", got)
		}
	})

	t.Run("delete_auction carries a Void body without exploding", func(t *testing.T) {
		got := classify(auction("delete_auction", 0, ixBorrower, void), noSkip)
		if len(got) != 1 || got[0].addr != ixBorrower {
			t.Fatalf("got %+v", got)
		}
	})
}

// The FILLER of an auction takes the bid dToken map onto its own position, so
// it is a genuine borrower — and it lives in the event DATA, never a topic.
// Topic-only scanning (and server-side getEvents filtering) can never find it.
func TestClassify_FillAuctionFindsTheFillerInTheData(t *testing.T) {
	fillBody := func(filler string) string {
		fv, err := soroban.ScvAddress(filler)
		if err != nil {
			t.Fatalf("ScvAddress: %v", err)
		}
		return b64(t, soroban.ScvVec(fv, soroban.ScvI128(100), xdr.ScVal{Type: xdr.ScValTypeScvVoid}))
	}

	t.Run("type 0 yields both the liquidated user and the filler", func(t *testing.T) {
		e := ev(300, fillBody(ixFiller),
			b64(t, soroban.ScvSymbol("fill_auction")), b64(t, soroban.ScvU32(0)), addrB64(t, ixBorrower))
		got := classify(e, noSkip)
		if len(got) != 2 {
			t.Fatalf("got %+v, want the liquidated user AND the filler", got)
		}
		seen := map[string]bool{}
		for _, f := range got {
			if !f.debt {
				t.Errorf("%s should be a debt fact", f.addr)
			}
			seen[f.addr] = true
		}
		if !seen[ixBorrower] || !seen[ixFiller] {
			t.Errorf("got %v, want both %s and %s", seen, ixBorrower, ixFiller)
		}
	})

	t.Run("type 1 yields the filler only — topic[2] is the backstop", func(t *testing.T) {
		e := ev(301, fillBody(ixFiller),
			b64(t, soroban.ScvSymbol("fill_auction")), b64(t, soroban.ScvU32(1)), addrB64(t, ixBackstop))
		got := classify(e, noSkip)
		if len(got) != 1 || got[0].addr != ixFiller || !got[0].debt {
			t.Fatalf("got %+v, want just the filler", got)
		}
	})

	t.Run("an excluded filler (the keeper itself) is dropped", func(t *testing.T) {
		e := ev(302, fillBody(ixFiller),
			b64(t, soroban.ScvSymbol("fill_auction")), b64(t, soroban.ScvU32(0)), addrB64(t, ixBorrower))
		got := classify(e, func(a string) bool { return a == ixFiller })
		if len(got) != 1 || got[0].addr != ixBorrower {
			t.Fatalf("got %+v, want only the liquidated user", got)
		}
	})
}

// An unrecognised event still contributes candidates: reach beats precision,
// because one probe settles an address permanently and a Blend upgrade adding
// an event must not make borrowers invisible.
func TestClassify_UnknownEventStillYieldsCandidates(t *testing.T) {
	e := ev(400, b64(t, soroban.ScvI128(1)),
		b64(t, soroban.ScvSymbol("some_future_event")), addrB64(t, ixUsdc), addrB64(t, ixBorrower))
	got := classify(e, func(a string) bool { return a == ixUsdc })
	if len(got) != 1 || got[0].addr != ixBorrower || got[0].debt {
		t.Fatalf("got %+v, want one bare candidate", got)
	}
}

// The probe list is the cycle's dominant cost, so it must contain exactly the
// addresses worth a round-trip: confirmed debtors, anything never probed,
// anything touched since its last probe — and nothing else.
func TestToProbe_OnlyWhatIsWorthARoundTrip(t *testing.T) {
	ix := NewBorrowerIndex("")
	ps := &poolState{Addrs: map[string]*addrState{
		"DEBTOR":  {Debt: true, Probed: 500, LastEvent: 100},
		"NEVER":   {Probed: 0, LastEvent: 100},
		"TOUCHED": {Probed: 400, LastEvent: 450},
		"QUIET":   {Probed: 500, LastEvent: 100},
	}}
	ix.pools[ixPool] = ps

	got := ix.ToProbe(ixPool, nil)
	want := map[string]bool{"DEBTOR": true, "NEVER": true, "TOUCHED": true}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for _, a := range got {
		if !want[a] {
			t.Errorf("%s should not be probed", a)
		}
	}

	t.Run("a debt-free probe retires an address from the list", func(t *testing.T) {
		ix.RecordProbe(ixPool, "TOUCHED", false, 500)
		for _, a := range ix.ToProbe(ixPool, nil) {
			if a == "TOUCHED" {
				t.Error("TOUCHED was probed and found debt-free; it must go quiet")
			}
		}
	})

	t.Run("a new event revives it", func(t *testing.T) {
		ps.Addrs["TOUCHED"].LastEvent = 600
		var found bool
		for _, a := range ix.ToProbe(ixPool, nil) {
			found = found || a == "TOUCHED"
		}
		if !found {
			t.Error("a new event must put the address back in the probe list")
		}
	})

	t.Run("watch addresses are always probed and never duplicated", func(t *testing.T) {
		got := ix.ToProbe(ixPool, []string{"WATCHED", "DEBTOR"})
		n := map[string]int{}
		for _, a := range got {
			n[a]++
		}
		if n["WATCHED"] != 1 {
			t.Errorf("watch address must appear exactly once, got %d", n["WATCHED"])
		}
		if n["DEBTOR"] != 1 {
			t.Errorf("an address both watched and indexed must appear once, got %d", n["DEBTOR"])
		}
	})
}

// A failed probe must never be recorded as debt-free — that is the difference
// between "this address has no debt" and "we could not find out", and
// conflating them retires a real borrower on a transient RPC error.
func TestRecordProbe_DebtStateIsTheProbeResult(t *testing.T) {
	ix := NewBorrowerIndex("")
	ix.RecordProbe(ixPool, ixBorrower, true, 100)
	if _, debtors := ix.Counts(ixPool); debtors != 1 {
		t.Fatalf("debtors: got %d want 1", debtors)
	}
	ix.RecordProbe(ixPool, ixBorrower, false, 200)
	if _, debtors := ix.Counts(ixPool); debtors != 0 {
		t.Fatalf("debtors after a debt-free probe: got %d want 0", debtors)
	}
	if tracked, _ := ix.Counts(ixPool); tracked != 1 {
		t.Errorf("the address stays indexed so a later event can revive it cheaply, got %d", tracked)
	}
}

func TestEvict_NeverDropsAConfirmedDebtor(t *testing.T) {
	ps := &poolState{Addrs: map[string]*addrState{}}
	// Debtors with the OLDEST activity — the ones a naive LRU would drop first.
	ps.Addrs["debtor-old-1"] = &addrState{Debt: true, LastEvent: 1}
	ps.Addrs["debtor-old-2"] = &addrState{Debt: true, LastEvent: 2}
	for i := 0; i < 20; i++ {
		ps.Addrs[string(rune('a'+i))] = &addrState{LastEvent: int64(1000 + i)}
	}
	evicted := ps.evict(5)
	if evicted == 0 {
		t.Fatal("expected evictions")
	}
	for _, a := range []string{"debtor-old-1", "debtor-old-2"} {
		if _, ok := ps.Addrs[a]; !ok {
			t.Errorf("%s is a confirmed debtor and must never be evicted", a)
		}
	}
}

// The cache is an optimisation: it must survive a round-trip, and every way of
// not having one must still leave a working (empty) index.
func TestCache_RoundTripAndColdStarts(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "nested", "borrowers.json")

	ix := NewBorrowerIndex(path)
	ix.RecordProbe(ixPool, ixBorrower, true, 4100000)
	ix.pools[ixPool].LastLedger = 4124000
	if err := ix.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	reloaded := NewBorrowerIndex(path)
	if err := reloaded.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got := reloaded.pools[ixPool].LastLedger; got != 4124000 {
		t.Errorf("LastLedger: got %d want 4124000", got)
	}
	tracked, debtors := reloaded.Counts(ixPool)
	if tracked != 1 || debtors != 1 {
		t.Errorf("counts: got tracked=%d debtors=%d want 1/1", tracked, debtors)
	}

	t.Run("missing file is a cold start, not an error", func(t *testing.T) {
		cold := NewBorrowerIndex(filepath.Join(dir, "absent.json"))
		if err := cold.Load(); err != nil {
			t.Errorf("a missing cache must load cleanly, got %v", err)
		}
		if tracked, _ := cold.Counts(ixPool); tracked != 0 {
			t.Errorf("tracked: got %d want 0", tracked)
		}
	})

	t.Run("corrupt file reports the problem and leaves an empty index", func(t *testing.T) {
		bad := filepath.Join(dir, "corrupt.json")
		if err := os.WriteFile(bad, []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		cold := NewBorrowerIndex(bad)
		if err := cold.Load(); err == nil {
			t.Error("a corrupt cache must be reported, not silently ignored")
		}
		if tracked, _ := cold.Counts(ixPool); tracked != 0 {
			t.Errorf("index must be empty after a failed load, got %d", tracked)
		}
	})

	t.Run("a future cache version is refused rather than misread", func(t *testing.T) {
		future := filepath.Join(dir, "v99.json")
		b, _ := json.Marshal(cacheFile{Version: 99, Pools: map[string]*poolState{}})
		if err := os.WriteFile(future, b, 0o600); err != nil {
			t.Fatal(err)
		}
		if err := NewBorrowerIndex(future).Load(); err == nil {
			t.Error("a version mismatch must be refused")
		}
	})

	t.Run("persistence disabled is not an error", func(t *testing.T) {
		none := NewBorrowerIndex("")
		if err := none.Load(); err != nil {
			t.Errorf("Load: %v", err)
		}
		none.RecordProbe(ixPool, ixBorrower, true, 1)
		if err := none.Save(); err != nil {
			t.Errorf("Save: %v", err)
		}
	})
}

// Save must not leave a truncated file behind: it writes to a temp path and
// renames, so a crash mid-write keeps the previous good copy.
func TestSave_IsAtomicAndSkipsCleanState(t *testing.T) {
	path := filepath.Join(t.TempDir(), "borrowers.json")
	ix := NewBorrowerIndex(path)
	ix.RecordProbe(ixPool, ixBorrower, true, 1)
	if err := ix.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if _, err := os.Stat(path + ".tmp"); !os.IsNotExist(err) {
		t.Error("temp file must not survive a successful save")
	}
	before, _ := os.Stat(path)
	if err := ix.Save(); err != nil { // nothing changed
		t.Fatalf("Save: %v", err)
	}
	after, _ := os.Stat(path)
	if before.ModTime() != after.ModTime() {
		t.Error("a clean index must not rewrite the cache")
	}
}

// mockRPC serves getHealth / getEvents so Sync can be driven end to end.
type mockRPC struct {
	oldest, latest int64
	pages          []string // raw getEvents result JSON, served in order
	seenStarts     []any
	seenCursors    []any
}

func (m *mockRPC) server(t *testing.T) *soroban.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Method string         `json:"method"`
			Params map[string]any `json:"params"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		w.Header().Set("Content-Type", "application/json")
		switch req.Method {
		case "getHealth":
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":{"status":"healthy","latestLedger":%d,"oldestLedger":%d,"ledgerRetentionWindow":120960}}`,
				m.latest, m.oldest)
		case "getEvents":
			m.seenStarts = append(m.seenStarts, req.Params["startLedger"])
			if p, ok := req.Params["pagination"].(map[string]any); ok {
				m.seenCursors = append(m.seenCursors, p["cursor"])
			}
			i := len(m.seenStarts) - 1
			if i >= len(m.pages) {
				i = len(m.pages) - 1
			}
			fmt.Fprintf(w, `{"jsonrpc":"2.0","id":1,"result":%s}`, m.pages[i])
		default:
			t.Errorf("unexpected method %q", req.Method)
		}
	}))
	t.Cleanup(srv.Close)
	return soroban.NewClient(srv.URL)
}

func eventsPage(t *testing.T, latest int64, evs ...soroban.Event) string {
	t.Helper()
	b, err := json.Marshal(evs)
	if err != nil {
		t.Fatal(err)
	}
	// Sentinel cursor at the latest ledger: the sweep is caught up.
	cur := fmt.Sprintf("%d-4294967295", uint64(latest)<<32|0xFFFFF<<12|0xFFF)
	return fmt.Sprintf(`{"events":%s,"cursor":%q,"latestLedger":%d}`, b, cur, latest)
}

// A cold start with no cache backfills from the RPC's oldest RETAINED ledger,
// not from a fixed trailing window — the whole point of D2, since the sandbox
// pool's history sits ~70k ledgers behind head.
func TestSync_ColdStartBackfillsFromOldestRetainedLedger(t *testing.T) {
	borrowEv := ev(4054311, b64(t, soroban.ScvVec(soroban.ScvI128(20), soroban.ScvI128(20))),
		b64(t, soroban.ScvSymbol("borrow")), addrB64(t, ixUsdc), addrB64(t, ixBorrower))

	m := &mockRPC{oldest: 4003584, latest: 4124553, pages: []string{eventsPage(t, 4124553, borrowEv)}}
	rpc := m.server(t)

	ix := NewBorrowerIndex("")
	st, err := ix.Sync(rpc, ixPool, map[string]bool{ixUsdc: true})
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if !st.Backfill {
		t.Error("a cold start must be reported as a backfill")
	}
	// Start is the oldest retained ledger, backed off by the safety margin that
	// covers the window sliding between getHealth and getEvents.
	if want := int64(4003584 + oldestSafetyMargin); st.FromLedger != want {
		t.Errorf("FromLedger: got %d want %d", st.FromLedger, want)
	}
	if st.ToLedger != 4124553 {
		t.Errorf("ToLedger: got %d want 4124553", st.ToLedger)
	}
	if st.Added != 1 || st.Debtors != 1 {
		t.Errorf("got added=%d debtors=%d, want 1/1", st.Added, st.Debtors)
	}
	if got := ix.ToProbe(ixPool, nil); len(got) != 1 || got[0] != ixBorrower {
		t.Errorf("probe list: got %v want [%s]", got, ixBorrower)
	}
}

// A warm start resumes from the cached mark instead of re-reading the window.
func TestSync_WarmStartResumesFromCache(t *testing.T) {
	m := &mockRPC{oldest: 4003584, latest: 4124553, pages: []string{eventsPage(t, 4124553)}}
	rpc := m.server(t)

	ix := NewBorrowerIndex("")
	ix.pools[ixPool] = &poolState{LastLedger: 4100000, Addrs: map[string]*addrState{}}
	st, err := ix.Sync(rpc, ixPool, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if st.Backfill {
		t.Error("a cached resume is not a backfill")
	}
	if st.FromLedger != 4100000 {
		t.Errorf("FromLedger: got %d want 4100000 (the cached mark)", st.FromLedger)
	}
	if got := m.seenStarts[0]; got != float64(4100000) {
		t.Errorf("startLedger sent to the RPC: got %v want 4100000", got)
	}
}

// A cache older than the RPC still retains leaves an unreachable hole. That has
// to be visible: a silent clamp looks exactly like a clean resume.
func TestSync_CachePredatingRetentionReportsTheGap(t *testing.T) {
	m := &mockRPC{oldest: 4003584, latest: 4124553, pages: []string{eventsPage(t, 4124553)}}
	rpc := m.server(t)

	ix := NewBorrowerIndex("")
	ix.pools[ixPool] = &poolState{LastLedger: 3900000, Addrs: map[string]*addrState{}}
	st, err := ix.Sync(rpc, ixPool, nil)
	if err != nil {
		t.Fatalf("Sync: %v", err)
	}
	if st.GapAtStart <= 0 {
		t.Fatal("a cache older than retention must report the unreachable gap")
	}
	if want := int64(4003584+oldestSafetyMargin) - 3900000; st.GapAtStart != want {
		t.Errorf("GapAtStart: got %d want %d", st.GapAtStart, want)
	}
	if st.FromLedger != 4003584+oldestSafetyMargin {
		t.Errorf("FromLedger: got %d, must clamp to the oldest retained ledger", st.FromLedger)
	}
}

// Cold start and cached restart must reach the SAME borrower set — that is what
// makes the cache an optimisation rather than a correctness input.
func TestSync_ColdAndCachedRestartAgree(t *testing.T) {
	borrowEv := ev(4054311, b64(t, soroban.ScvVec(soroban.ScvI128(20), soroban.ScvI128(20))),
		b64(t, soroban.ScvSymbol("borrow")), addrB64(t, ixUsdc), addrB64(t, ixBorrower))
	page := eventsPage(t, 4124553, borrowEv)

	path := filepath.Join(t.TempDir(), "borrowers.json")

	// Run 1: cold, then persist.
	warm := NewBorrowerIndex(path)
	if _, err := warm.Sync((&mockRPC{oldest: 4003584, latest: 4124553, pages: []string{page}}).server(t), ixPool, nil); err != nil {
		t.Fatalf("cold sync: %v", err)
	}
	if err := warm.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// Run 2: restart from the cache, with the RPC now returning nothing new.
	restarted := NewBorrowerIndex(path)
	if err := restarted.Load(); err != nil {
		t.Fatalf("Load: %v", err)
	}
	empty := eventsPage(t, 4124553)
	if _, err := restarted.Sync((&mockRPC{oldest: 4003584, latest: 4124553, pages: []string{empty}}).server(t), ixPool, nil); err != nil {
		t.Fatalf("warm sync: %v", err)
	}

	// Run 3: a genuinely cold start that re-reads everything.
	cold := NewBorrowerIndex("")
	if _, err := cold.Sync((&mockRPC{oldest: 4003584, latest: 4124553, pages: []string{page}}).server(t), ixPool, nil); err != nil {
		t.Fatalf("cold sync 2: %v", err)
	}

	got, want := restarted.ToProbe(ixPool, nil), cold.ToProbe(ixPool, nil)
	if len(got) != len(want) || len(got) != 1 || got[0] != want[0] {
		t.Fatalf("cached restart %v != cold start %v", got, want)
	}
}
