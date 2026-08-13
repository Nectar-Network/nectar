package blend

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// BorrowerIndex is the keeper's own record of who can be liquidated, built by
// replaying pool events rather than by trusting a configured address list.
//
// The design turns on one verified fact (docs/FACTS.md "Pool event schemas"):
// Blend never publishes a post-action liability balance. `repay` carries only
// the delta, so an over-repay that zeroes a position is indistinguishable from
// a partial one, and liabilities also move through paths with entirely
// different event shapes (auction fills, bad-debt transfers, flash loans).
// Events therefore decide WHO to ask about and HOW OFTEN; `get_positions` is
// the only authority on whether debt is still there.
//
// That split is also what makes the index cheap. Every indexed address costs a
// sequential simulateTransaction round-trip (~670 ms against public testnet
// RPC), so probing everything that ever touched the pool does not scale — a
// full-window scan of the Blend testnet pool found 52 addresses of which only
// 3 carried any liability. Steady state probes confirmed debtors plus anything
// with a new event since its last probe, and nothing else.
type BorrowerIndex struct {
	mu       sync.Mutex
	path     string // "" disables persistence entirely
	maxAddrs int
	pools    map[string]*poolState
	dirty    bool
}

// addrState is what the index remembers about one address on one pool.
type addrState struct {
	// Debt is the result of the LAST get_positions probe, not an event
	// inference: true means that probe saw liabilities. An address stays
	// indexed after Debt goes false so a later event can cheaply revive it.
	Debt bool `json:"debt"`
	// Probed is the ledger the last probe was taken at; 0 means never probed.
	Probed int64 `json:"probed"`
	// LastEvent is the newest ledger an event named this address at. Probed
	// lagging LastEvent is exactly the "something changed, re-read it" signal.
	LastEvent int64 `json:"last_event"`
	FirstSeen int64 `json:"first_seen"`
}

type poolState struct {
	// LastLedger is where the next sweep resumes. It is a COVERED-THROUGH
	// mark, not a high-water event mark: resuming AT it (not after it) is
	// gapless, and re-seen events are idempotent here by construction.
	LastLedger int64                 `json:"last_ledger"`
	Addrs      map[string]*addrState `json:"addrs"`
}

// cacheFile is the on-disk shape. Version guards against a future format
// change being read as if it were this one.
type cacheFile struct {
	Version int                   `json:"version"`
	Pools   map[string]*poolState `json:"pools"`
}

const cacheVersion = 1

// defaultMaxAddrs bounds one pool's indexed set. Debtors are never evicted;
// only never-indebted addresses are, oldest event first.
const defaultMaxAddrs = 20000

// oldestSafetyMargin backs a backfill start off the RPC's reported
// oldestLedger. The retention window slides ~1 ledger every 5s and getEvents
// rejects an out-of-range startLedger outright (-32600), so a start computed
// from a getHealth read taken moments earlier can fail. The margin costs a few
// ledgers at the very edge of retention, which are expiring anyway.
const oldestSafetyMargin = 16

// NewBorrowerIndex builds an empty index. path may be "" to run without any
// on-disk cache — the index is then rebuilt by a full backfill on every start,
// which is slower but equally correct.
func NewBorrowerIndex(path string) *BorrowerIndex {
	return &BorrowerIndex{path: path, maxAddrs: defaultMaxAddrs, pools: map[string]*poolState{}}
}

// SyncStats describes one sweep, for logging and metrics.
type SyncStats struct {
	Pool       string
	Backfill   bool  // started from the RPC's oldest retained ledger
	GapAtStart int64 // ledgers lost because the cache predates retention; 0 normally
	FromLedger int64
	ToLedger   int64
	Retention  int64
	Events     int
	Truncated  bool
	Added      int
	Evicted    int
	Tracked    int
	Debtors    int
}

// Sync brings one pool's borrower set up to date from chain events.
//
// On a cold start (or with no cache) it backfills from the earliest ledger the
// RPC still retains; afterwards it reads only what is new. Either way the set
// it produces is the same — the cache is a speed-up, never a correctness input.
//
// exclude drops addresses that are never liquidatable users: the pool itself,
// its reserve assets (which appear in topic[1] of every action event), and the
// backstop once the adapter has resolved it.
func (ix *BorrowerIndex) Sync(rpc *soroban.Client, poolAddr string, exclude map[string]bool) (*SyncStats, error) {
	health, err := rpc.Health()
	if err != nil {
		return nil, fmt.Errorf("rpc health: %w", err)
	}

	ix.mu.Lock()
	ps := ix.pools[poolAddr]
	if ps == nil {
		ps = &poolState{Addrs: map[string]*addrState{}}
		ix.pools[poolAddr] = ps
	}
	start := ps.LastLedger
	ix.mu.Unlock()

	st := &SyncStats{Pool: poolAddr, Retention: health.LedgerRetentionWindow}
	oldest := health.OldestLedger + oldestSafetyMargin
	switch {
	case start <= 0:
		st.Backfill = true
		start = oldest
	case start < oldest:
		// The cache is older than the RPC still remembers. Whatever happened in
		// between is unreachable from this endpoint — say so rather than let a
		// silent hole look like a clean resume.
		st.GapAtStart = oldest - start
		start = oldest
	case start > health.LatestLedger:
		start = health.LatestLedger
	}
	st.FromLedger = start

	skip := func(a string) bool { return a == poolAddr || exclude[a] }
	var facts []eventFact
	scan, err := rpc.ScanEvents(start, poolAddr, func(ev soroban.Event) {
		for _, f := range classify(ev, skip) {
			f.ledger = ev.Ledger
			facts = append(facts, f)
		}
	})
	if err != nil {
		return nil, fmt.Errorf("scan events: %w", err)
	}
	st.Events = scan.Count
	st.Truncated = scan.Truncated
	st.ToLedger = scan.LastLedger

	ix.mu.Lock()
	defer ix.mu.Unlock()
	for _, f := range facts {
		if ps.apply(f) {
			st.Added++
		}
	}
	if scan.LastLedger > ps.LastLedger {
		ps.LastLedger = scan.LastLedger
	}
	st.Evicted = ps.evict(ix.maxAddrs)
	st.Tracked = len(ps.Addrs)
	for _, a := range ps.Addrs {
		if a.Debt {
			st.Debtors++
		}
	}
	if st.Added > 0 || st.Evicted > 0 || scan.Count > 0 {
		ix.dirty = true
	}
	return st, nil
}

// apply folds one event fact in, reporting whether the address is new.
func (ps *poolState) apply(f eventFact) bool {
	a := ps.Addrs[f.addr]
	added := a == nil
	if added {
		a = &addrState{FirstSeen: f.ledger}
		ps.Addrs[f.addr] = a
	}
	if f.ledger > a.LastEvent {
		a.LastEvent = f.ledger
	}
	switch {
	case f.debt:
		a.Debt = true
	case f.cleared:
		// bad_debt moves the user's ENTIRE liabilities map to the backstop, so
		// this is a proof of zero, not a hint. The address still gets one
		// confirming probe, because LastEvent now leads Probed.
		a.Debt = false
	}
	return added
}

// evict trims the set back to max, dropping never-indebted addresses with the
// oldest activity first. A confirmed debtor is never evicted: forgetting one is
// exactly the failure this index exists to prevent.
func (ps *poolState) evict(max int) int {
	if max <= 0 || len(ps.Addrs) <= max {
		return 0
	}
	type row struct {
		addr string
		last int64
	}
	var cold []row
	for addr, a := range ps.Addrs {
		if !a.Debt {
			cold = append(cold, row{addr, a.LastEvent})
		}
	}
	sort.Slice(cold, func(i, j int) bool { return cold[i].last < cold[j].last })
	n := 0
	for _, r := range cold {
		if len(ps.Addrs) <= max {
			break
		}
		delete(ps.Addrs, r.addr)
		n++
	}
	return n
}

// ToProbe returns the addresses whose positions this cycle should read, sorted
// for stable logs. That is: every operator-supplied watch address, every
// confirmed debtor, and anything an event has touched since it was last probed.
// Addresses probed and found debt-free go quiet until an event revives them —
// which is the whole efficiency argument.
func (ix *BorrowerIndex) ToProbe(poolAddr string, watch []string) []string {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	seen := make(map[string]bool, len(watch))
	out := make([]string, 0, len(watch))
	for _, w := range watch {
		if w != "" && !seen[w] {
			seen[w] = true
			out = append(out, w)
		}
	}
	if ps := ix.pools[poolAddr]; ps != nil {
		for addr, a := range ps.Addrs {
			if seen[addr] {
				continue
			}
			if a.Debt || a.Probed == 0 || a.LastEvent > a.Probed {
				seen[addr] = true
				out = append(out, addr)
			}
		}
	}
	sort.Strings(out)
	return out
}

// RecordProbe stores the outcome of one get_positions read. hasDebt is the
// authority that events cannot supply; ledger stamps when it was learned.
func (ix *BorrowerIndex) RecordProbe(poolAddr, addr string, hasDebt bool, ledger int64) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ps := ix.pools[poolAddr]
	if ps == nil {
		ps = &poolState{Addrs: map[string]*addrState{}}
		ix.pools[poolAddr] = ps
	}
	a := ps.Addrs[addr]
	if a == nil {
		a = &addrState{FirstSeen: ledger}
		ps.Addrs[addr] = a
	}
	a.Debt = hasDebt
	a.Probed = ledger
	ix.dirty = true
}

// Counts reports the indexed set size and how many of them carry debt.
func (ix *BorrowerIndex) Counts(poolAddr string) (tracked, debtors int) {
	ix.mu.Lock()
	defer ix.mu.Unlock()
	ps := ix.pools[poolAddr]
	if ps == nil {
		return 0, 0
	}
	for _, a := range ps.Addrs {
		if a.Debt {
			debtors++
		}
	}
	return len(ps.Addrs), debtors
}

// Load reads the cache. A missing file is not an error — that is a cold start,
// which must work. A corrupt or future-version file is reported so the operator
// sees it, and the index is left empty so the next Sync rebuilds by backfill.
func (ix *BorrowerIndex) Load() error {
	if ix.path == "" {
		return nil
	}
	b, err := os.ReadFile(ix.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("read borrower cache: %w", err)
	}
	var cf cacheFile
	if err := json.Unmarshal(b, &cf); err != nil {
		return fmt.Errorf("borrower cache is unreadable, starting cold: %w", err)
	}
	if cf.Version != cacheVersion {
		return fmt.Errorf("borrower cache version %d != %d, starting cold", cf.Version, cacheVersion)
	}
	ix.mu.Lock()
	defer ix.mu.Unlock()
	for pool, ps := range cf.Pools {
		if ps == nil {
			continue
		}
		if ps.Addrs == nil {
			ps.Addrs = map[string]*addrState{}
		}
		ix.pools[pool] = ps
	}
	return nil
}

// Save writes the cache atomically (temp file + rename) so a kill mid-write
// leaves the previous good copy rather than a truncated one. It is a no-op when
// nothing changed or when persistence is disabled.
func (ix *BorrowerIndex) Save() error {
	if ix.path == "" {
		return nil
	}
	ix.mu.Lock()
	if !ix.dirty {
		ix.mu.Unlock()
		return nil
	}
	b, err := json.Marshal(cacheFile{Version: cacheVersion, Pools: ix.pools})
	ix.dirty = false
	ix.mu.Unlock()
	if err != nil {
		return fmt.Errorf("encode borrower cache: %w", err)
	}
	if dir := filepath.Dir(ix.path); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create borrower cache dir: %w", err)
		}
	}
	tmp := ix.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return fmt.Errorf("write borrower cache: %w", err)
	}
	if err := os.Rename(tmp, ix.path); err != nil {
		_ = os.Remove(tmp)
		return fmt.Errorf("replace borrower cache: %w", err)
	}
	return nil
}

// eventFact is what one decoded pool event proves about one address.
type eventFact struct {
	addr   string
	ledger int64
	// debt: the event proves this address took liabilities on.
	debt bool
	// cleared: the event proves this address's liabilities are now zero.
	cleared bool
}

// classify decodes one pool event into the addresses it names and what it
// proves. Topic layouts are NOT uniform across Blend v2 events and indexing
// them positionally without regard to the event name is wrong — see
// docs/FACTS.md "Pool event schemas" for the verified table.
func classify(ev soroban.Event, skip func(string) bool) []eventFact {
	if len(ev.Topic) == 0 {
		return nil
	}
	switch topicSymbol(ev, 0) {
	case "bad_debt":
		// The one event that inverts the order: [bad_debt, user, asset]. Its
		// payload is the user's FULL remaining liability, removed in full, so
		// this is a proof the position is now debt-free.
		if a, ok := topicAddr(ev, 1, skip); ok {
			return []eventFact{{addr: a, cleared: true}}
		}
		return nil

	case "defaulted_debt":
		// [defaulted_debt, asset] — names no debtor at all.
		return nil

	case "borrow", "flash_loan":
		if a, ok := topicAddr(ev, 2, skip); ok {
			return []eventFact{{addr: a, debt: true}}
		}
		return nil

	case "new_auction", "fill_auction", "delete_auction":
		return auctionFacts(ev, skip)

	case "supply", "withdraw", "supply_collateral", "withdraw_collateral", "repay":
		// [name, asset, from]. These prove nothing about debt — repay carries
		// only a delta, and the collateral/supply actions never touch
		// liabilities — but the address is worth ONE probe: it may carry debt
		// taken before the RPC's retention window, which no event can show.
		// topic[1] is definitionally the asset, so it is not even looked at.
		if a, ok := topicAddr(ev, 2, skip); ok {
			return []eventFact{{addr: a}}
		}
		return nil

	case "claim":
		// [claim, from] — 2 topics. Emissions only, so debt-neutral, but the
		// same "may predate retention" argument makes it worth one probe.
		if a, ok := topicAddr(ev, 1, skip); ok {
			return []eventFact{{addr: a}}
		}
		return nil
	}

	// An event this build does not know — a future Blend release, most likely.
	// Its topic layout is unknown, so take every address in the two positions
	// that have ever held one and let a single probe settle each. Reach beats
	// precision here: the cost of a wrong guess is one wasted round-trip, while
	// the cost of ignoring it is an invisible borrower.
	var out []eventFact
	for _, i := range []int{1, 2} {
		if a, ok := topicAddr(ev, i, skip); ok {
			out = append(out, eventFact{addr: a})
		}
	}
	return out
}

// auctionFacts handles the three auction events, whose topic[1] is a u32
// auction type rather than an address.
func auctionFacts(ev soroban.Event, skip func(string) bool) []eventFact {
	at, ok := topicU32(ev, 1)
	if !ok {
		return nil
	}
	var out []eventFact
	// topic[2] is a real borrower only for type 0. For bad-debt (1) and
	// interest (2) auctions the contract REQUIRES user == backstop, so topic[2]
	// is the backstop contract — indexing it would burn a probe every cycle on
	// an address that can never be user-liquidated.
	if AuctionType(at) == AuctionUserLiquidation {
		if a, ok := topicAddr(ev, 2, skip); ok {
			out = append(out, eventFact{addr: a, debt: true})
		}
	}
	if topicSymbol(ev, 0) != "fill_auction" {
		return out
	}
	// The FILLER is in the DATA, never a topic. Filling a user-liquidation or
	// bad-debt auction takes the auction's bid d-token map onto the filler's
	// own position (add_positions -> add_liabilities) and the pool health-checks
	// it on the spot. So a filler is a genuine borrower that no amount of topic
	// scanning — or server-side getEvents filtering — can ever find.
	if t := AuctionType(at); t == AuctionUserLiquidation || t == AuctionBadDebt {
		if a, ok := fillerAddr(ev.Value); ok && !skip(a) {
			out = append(out, eventFact{addr: a, debt: true})
		}
	}
	return out
}

// fillerAddr pulls data[0] out of a fill_auction body, which is
// ScVec[Address filler, i128 fill_percent, AuctionData].
func fillerAddr(valueB64 string) (string, bool) {
	if valueB64 == "" {
		return "", false
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(valueB64, &val); err != nil {
		return "", false
	}
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		return "", false
	}
	vec := **val.Vec
	if len(vec) == 0 || vec[0].Type != xdr.ScValTypeScvAddress || vec[0].Address == nil {
		return "", false
	}
	addr, err := soroban.ParseAddress(*vec[0].Address)
	if err != nil {
		return "", false
	}
	return addr, true
}

func topicVal(ev soroban.Event, i int) (xdr.ScVal, bool) {
	if i >= len(ev.Topic) {
		return xdr.ScVal{}, false
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(ev.Topic[i], &val); err != nil {
		return xdr.ScVal{}, false
	}
	return val, true
}

// topicSymbol reads a topic as a Symbol. Blend v2 uses Symbol::new everywhere
// (symbol_short! appears nowhere in the pinned source), so long names such as
// "withdraw_collateral" arrive intact and never truncated to 9 characters.
func topicSymbol(ev soroban.Event, i int) string {
	val, ok := topicVal(ev, i)
	if !ok {
		return ""
	}
	return scSymbol(val)
}

func topicAddr(ev soroban.Event, i int, skip func(string) bool) (string, bool) {
	val, ok := topicVal(ev, i)
	if !ok || val.Type != xdr.ScValTypeScvAddress || val.Address == nil {
		return "", false
	}
	addr, err := soroban.ParseAddress(*val.Address)
	if err != nil || skip(addr) {
		return "", false
	}
	return addr, true
}

func topicU32(ev soroban.Event, i int) (uint32, bool) {
	val, ok := topicVal(ev, i)
	if !ok || val.Type != xdr.ScValTypeScvU32 {
		return 0, false
	}
	return scU32(val), true
}
