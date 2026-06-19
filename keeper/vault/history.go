package vault

import (
	"fmt"
	"sort"
	"sync"
	"time"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// maxHistoryPerAddr bounds the in-memory deposit/withdraw history kept per
// depositor. Like the keeper's fill history it is a rolling window — old events
// are evicted once an address exceeds the cap — so a long-lived keeper does not
// grow unbounded. The chain remains the authoritative full history.
const maxHistoryPerAddr = 50

// historyLookbackLedgers is how far back each cycle's getEvents query starts.
// Soroban RPC only retains events within a bounded window (~17k ledgers on
// testnet), so a single query can never reach genesis; the indexer accumulates
// across cycles instead (see HistoryIndexer). Mirrors the lookback the Blend
// adapter uses for position discovery.
const historyLookbackLedgers = 1000

// HistoryEvent is one decoded vault deposit or withdraw, normalized so the API
// and dashboard need not know the on-chain (amount,shares) vs (shares,usdc_out)
// argument ordering of the two event shapes.
type HistoryEvent struct {
	Type      string    `json:"type"` // "deposit" | "withdraw"
	Address   string    `json:"address"`
	Amount    int64     `json:"amount"` // USDC stroops moved (deposited or paid out)
	Shares    int64     `json:"shares"` // shares minted (deposit) or burned (withdraw)
	Ledger    int64     `json:"ledger"`
	TxHash    string    `json:"tx_hash,omitempty"`
	Timestamp time.Time `json:"ts"`
}

// HistoryIndexer accumulates vault deposit/withdraw events per depositor across
// keeper cycles. getEvents only returns events inside the RPC's bounded
// retention window, so any single Update sees only recent activity; merging into
// the map preserves events first seen in earlier cycles even after they age out
// of RPC. Events that closed before the keeper first ran (or before the
// retention window at startup) are NOT reconstructable from RPC — a full
// historical index would need a dedicated event-streaming indexer, which is out
// of scope here. Safe for concurrent use.
type HistoryIndexer struct {
	mu     sync.Mutex
	byAddr map[string][]HistoryEvent
	seen   map[string]struct{} // dedupe key: ledger|txHash|type per event
}

// NewHistoryIndexer returns an empty indexer ready for Update.
func NewHistoryIndexer() *HistoryIndexer {
	return &HistoryIndexer{
		byAddr: make(map[string][]HistoryEvent),
		seen:   make(map[string]struct{}),
	}
}

// Update fetches recent vault deposit/withdraw events and folds any new ones
// into the per-address history. It queries from the latest retained ledger
// window forward; errors (RPC down, bad ledger) are returned, never panicked.
func (h *HistoryIndexer) Update(rpc *soroban.Client, vaultAddr string) error {
	if vaultAddr == "" {
		return nil
	}
	ledger, err := rpc.LatestLedger()
	if err != nil {
		return fmt.Errorf("latest ledger: %w", err)
	}
	start := ledger - historyLookbackLedgers
	if start < 1 {
		start = 1
	}
	events, err := rpc.GetEvents(start, vaultAddr)
	if err != nil {
		return fmt.Errorf("get events: %w", err)
	}
	for _, ev := range events {
		rec, ok := decodeHistoryEvent(ev)
		if !ok {
			continue
		}
		h.add(rec)
	}
	return nil
}

// add records one event under its address, deduping on (ledger,txHash,type) so
// re-querying overlapping windows each cycle does not duplicate rows, and
// trimming the per-address window to maxHistoryPerAddr (oldest first).
func (h *HistoryIndexer) add(rec HistoryEvent) {
	key := fmt.Sprintf("%d|%s|%s", rec.Ledger, rec.TxHash, rec.Type)
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, dup := h.seen[key]; dup {
		return
	}
	h.seen[key] = struct{}{}
	list := append(h.byAddr[rec.Address], rec)
	sort.SliceStable(list, func(i, j int) bool { return list[i].Ledger < list[j].Ledger })
	if len(list) > maxHistoryPerAddr {
		list = list[len(list)-maxHistoryPerAddr:]
	}
	h.byAddr[rec.Address] = list
}

// History returns an address's deposit/withdraw events newest-first. The result
// is a copy, safe to hand to a JSON encoder without holding the lock.
func (h *HistoryIndexer) History(addr string) []HistoryEvent {
	h.mu.Lock()
	defer h.mu.Unlock()
	src := h.byAddr[addr]
	if len(src) == 0 {
		return nil
	}
	out := make([]HistoryEvent, len(src))
	for i, rec := range src {
		out[len(src)-1-i] = rec // reverse: stored oldest-first, returned newest-first
	}
	return out
}

// decodeHistoryEvent turns a raw vault event into a typed record. It reuses the
// soroban ScVal decoders (topic[1] = user Address, value = (i128,i128) vec) the
// way blend/positions.go reads pool events. Returns ok=false for any event that
// is not a well-formed deposit/withdraw (e.g. other vault events).
func decodeHistoryEvent(ev soroban.Event) (HistoryEvent, bool) {
	if len(ev.Topic) < 2 {
		return HistoryEvent{}, false
	}
	var nameVal xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(ev.Topic[0], &nameVal); err != nil {
		return HistoryEvent{}, false
	}
	name := scSymbol(nameVal)
	if name != "deposit" && name != "withdraw" {
		return HistoryEvent{}, false
	}

	var addrVal xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(ev.Topic[1], &addrVal); err != nil {
		return HistoryEvent{}, false
	}
	if addrVal.Type != xdr.ScValTypeScvAddress || addrVal.Address == nil {
		return HistoryEvent{}, false
	}
	addr, err := soroban.ParseAddress(*addrVal.Address)
	if err != nil {
		return HistoryEvent{}, false
	}

	var dataVal xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(ev.Value, &dataVal); err != nil {
		return HistoryEvent{}, false
	}
	if dataVal.Type != xdr.ScValTypeScvVec || dataVal.Vec == nil || *dataVal.Vec == nil {
		return HistoryEvent{}, false
	}
	vec := **dataVal.Vec
	if len(vec) < 2 {
		return HistoryEvent{}, false
	}

	// Normalize the two on-chain shapes onto (Amount=USDC, Shares):
	//   deposit  data = (amount, shares)
	//   withdraw data = (shares, usdc_out)
	var amt, shares int64
	switch name {
	case "deposit":
		amt = scI128(vec[0])
		shares = scI128(vec[1])
	case "withdraw":
		shares = scI128(vec[0])
		amt = scI128(vec[1])
	}

	rec := HistoryEvent{
		Type:    name,
		Address: addr,
		Amount:  amt,
		Shares:  shares,
		Ledger:  ev.Ledger,
		TxHash:  ev.TxHash,
	}
	if ev.LedgerClosedAt != "" {
		if t, err := time.Parse(time.RFC3339, ev.LedgerClosedAt); err == nil {
			rec.Timestamp = t.UTC()
		}
	}
	return rec, true
}
