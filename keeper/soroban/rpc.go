package soroban

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

type Client struct {
	url  string
	http *http.Client
	seq  atomic.Int64
}

func NewClient(url string) *Client {
	return &Client{url: url, http: &http.Client{Timeout: 30 * time.Second}}
}

type SimulateResult struct {
	Results         []SimEntry `json:"results,omitempty"`
	Error           string     `json:"error,omitempty"`
	TransactionData string     `json:"transactionData,omitempty"`
	MinResourceFee  string     `json:"minResourceFee,omitempty"`
	LatestLedger    int64      `json:"latestLedger"`
}

type SimEntry struct {
	XDR  string   `json:"xdr"`
	Auth []string `json:"auth,omitempty"`
}

type TxResult struct {
	Status         string `json:"status"`
	Hash           string
	ResultXDR      string `json:"resultXdr,omitempty"`
	ErrorResultXDR string `json:"errorResultXdr,omitempty"`
}

type Event struct {
	Type       string   `json:"type"`
	ContractID string   `json:"contractId"`
	Topic      []string `json:"topic"`
	Value      string   `json:"value"`
	Ledger     int64    `json:"ledger"`
	// TxHash and LedgerClosedAt are present on getEvents results; older callers
	// that only read topics/value ignore them. LedgerClosedAt is RFC3339.
	TxHash         string `json:"txHash,omitempty"`
	LedgerClosedAt string `json:"ledgerClosedAt,omitempty"`
}

func (c *Client) Simulate(txXDR string) (*SimulateResult, error) {
	var r SimulateResult
	return &r, c.call("simulateTransaction", map[string]string{"transaction": txXDR}, &r)
}

func (c *Client) Send(txXDR string) (string, error) {
	var r struct {
		Hash           string `json:"hash"`
		Status         string `json:"status"`
		ErrorResultXDR string `json:"errorResultXdr"`
	}
	if err := c.call("sendTransaction", map[string]string{"transaction": txXDR}, &r); err != nil {
		return "", err
	}
	if r.Status == "ERROR" {
		return "", fmt.Errorf("send tx: %s", r.ErrorResultXDR)
	}
	return r.Hash, nil
}

// TxStatusUnknownError marks failures that happened AFTER the transaction was
// accepted by the network: polling errored or timed out, so the tx may still
// land. Auto-retrying a state-changing call on this class risks executing it
// twice; callers that must not double-execute disable retries for it via
// RetryConfig.RetryAmbiguous.
type TxStatusUnknownError struct {
	Hash string
	Err  error
}

func (e *TxStatusUnknownError) Error() string {
	return fmt.Sprintf("tx %s status unknown: %v", short8(e.Hash), e.Err)
}

func (e *TxStatusUnknownError) Unwrap() error { return e.Err }

// IsTxStatusUnknown reports whether err (anywhere in its chain) is a
// post-send ambiguous failure — the transaction may have landed.
func IsTxStatusUnknown(err error) bool {
	var u *TxStatusUnknownError
	return errors.As(err, &u)
}

func short8(s string) string {
	if len(s) > 8 {
		return s[:8]
	}
	return s
}

func (c *Client) AwaitTx(hash string, timeout time.Duration) (*TxResult, error) {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		var r TxResult
		if err := c.call("getTransaction", map[string]string{"hash": hash}, &r); err != nil {
			// The tx was already sent — a polling failure leaves its fate unknown.
			return nil, &TxStatusUnknownError{Hash: hash, Err: err}
		}
		r.Hash = hash
		switch r.Status {
		case "SUCCESS":
			return &r, nil
		case "FAILED":
			// Definitive: the tx landed and failed. Safe to classify normally.
			return nil, fmt.Errorf("tx %s failed: %s", short8(hash), r.ResultXDR)
		}
		select {
		case <-time.After(3 * time.Second):
		}
	}
	// Still NOT_FOUND/PENDING at the deadline — it may land after we stop looking.
	return nil, &TxStatusUnknownError{Hash: hash, Err: fmt.Errorf("tx %s timed out", short8(hash))}
}

// eventPageLimit is the RPC's maximum accepted pagination.limit. Verified
// live 2026-08-13: 10000 is accepted, 10001 returns -32602 "limit must not
// exceed 10000" (docs/FACTS.md "Soroban RPC getEvents — limits observed live").
const eventPageLimit = 10000

// segmentSentinel is the event-index suffix the RPC puts on a cursor when it
// drained a whole ledger segment rather than stopping at the page limit. The
// two cases have to be told apart: a limit-bound cursor points at the last
// RETURNED event, so the scan has NOT covered the rest of the segment, while a
// sentinel cursor means every ledger up to its TOID is done. Verified live.
const segmentSentinel = "-4294967295"

// EventScan reports where a paged getEvents sweep actually got to.
type EventScan struct {
	// LastLedger is the newest ledger the sweep is known to have covered.
	// Resuming a later sweep AT this ledger (not after it) is gapless; events
	// re-seen across the overlap are the caller's to make idempotent.
	LastLedger int64
	// Truncated is true when a page or event cap stopped the sweep before it
	// reached the RPC's latest ledger. The caller MUST keep resuming — this is
	// the case that used to be a silent, unreported gap.
	Truncated bool
	// Count is how many events the visitor was handed.
	Count int
}

// ScanEvents pages contract events from startLedger to the RPC's latest ledger,
// handing each one to visit. It streams rather than accumulating so a busy pool
// cannot blow the keeper's memory, and it reports where it stopped.
//
// Two verified RPC behaviours drive the loop (docs/FACTS.md):
//   - One request scans a bounded 10000-ledger SEGMENT. A page with zero events
//     does NOT mean the window is empty; events may sit in a later segment.
//   - A page returning exactly `limit` events does NOT prove the segment is
//     drained. Only the "-4294967295" sentinel cursor proves that.
//
// So the sweep follows the cursor (omitting startLedger on paged requests, which
// the RPC rejects if both are set) until a sentinel cursor reaches the latest
// ledger, the cursor runs out, or a cap trips.
func (c *Client) ScanEvents(startLedger int64, contractID string, visit func(Event)) (*EventScan, error) {
	const (
		maxPages  = 256
		maxEvents = 200000
	)
	scan := &EventScan{LastLedger: startLedger}
	cursor := ""
	for page := 0; page < maxPages; page++ {
		var r struct {
			Events       []Event `json:"events"`
			Cursor       string  `json:"cursor"`
			LatestLedger int64   `json:"latestLedger"`
		}
		params := map[string]any{
			"filters": []map[string]any{
				{"type": "contract", "contractIds": []string{contractID}},
			},
			"pagination": map[string]any{"limit": eventPageLimit},
		}
		if cursor == "" {
			params["startLedger"] = startLedger
		} else {
			// startLedger and cursor are mutually exclusive: sending both is
			// -32602 "ledger ranges and cursor cannot both be set".
			params["pagination"].(map[string]any)["cursor"] = cursor
		}
		if err := c.call("getEvents", params, &r); err != nil {
			if scan.Count > 0 {
				// A transient failure mid-scan must not discard the pages we
				// already walked, but it must not be reported as a completed
				// sweep either — the caller resumes from LastLedger next cycle.
				scan.Truncated = true
				return scan, nil
			}
			return nil, err
		}
		for _, ev := range r.Events {
			visit(ev)
			scan.Count++
			if ev.Ledger > scan.LastLedger {
				scan.LastLedger = ev.Ledger
			}
		}
		if r.Cursor == "" {
			return scan, nil
		}
		// The sentinel decides how far we may CLAIM to have covered; reaching
		// the head of the chain decides when to STOP. They are different
		// questions and conflating them is the bug: a limit-bound cursor names
		// the last returned event, so the rest of its segment is unscanned and
		// LastLedger must not jump to it — but if that cursor is already at the
		// latest ledger there is nothing more to fetch this sweep either way,
		// and the next sweep resumes from LastLedger without a gap.
		curLg := cursorLedger(r.Cursor)
		if strings.HasSuffix(r.Cursor, segmentSentinel) && curLg > scan.LastLedger {
			scan.LastLedger = curLg
		}
		if r.LatestLedger > 0 && curLg >= r.LatestLedger {
			return scan, nil
		}
		if scan.Count >= maxEvents {
			scan.Truncated = true
			return scan, nil
		}
		cursor = r.Cursor
	}
	scan.Truncated = true
	return scan, nil
}

// GetEvents collects contract events from startLedger to the current ledger.
// Prefer ScanEvents for anything that could be large — this buffers everything.
func (c *Client) GetEvents(startLedger int64, contractID string) ([]Event, error) {
	var all []Event
	if _, err := c.ScanEvents(startLedger, contractID, func(ev Event) {
		all = append(all, ev)
	}); err != nil {
		return nil, err
	}
	return all, nil
}

// cursorLedger extracts the ledger sequence from a getEvents cursor
// ("<toid>-<eventIdx>"). Returns 0 when unparsable.
func cursorLedger(cursor string) int64 {
	dash := strings.IndexByte(cursor, '-')
	if dash <= 0 {
		return 0
	}
	toid, err := strconv.ParseUint(cursor[:dash], 10, 64)
	if err != nil {
		return 0
	}
	return int64(toid >> 32)
}

// Health is the getHealth response. OldestLedger/LatestLedger bound what
// getEvents will accept as startLedger — outside that range the RPC returns
// -32600 "startLedger must be within the ledger range" — and the window slides
// forward roughly one ledger every 5s, so re-read it rather than caching.
type Health struct {
	Status                string `json:"status"`
	LatestLedger          int64  `json:"latestLedger"`
	OldestLedger          int64  `json:"oldestLedger"`
	LedgerRetentionWindow int64  `json:"ledgerRetentionWindow"`
}

// Health reports the RPC's event retention window as the node itself sees it.
func (c *Client) Health() (*Health, error) {
	var h Health
	if err := c.call("getHealth", nil, &h); err != nil {
		return nil, err
	}
	if h.LatestLedger <= 0 || h.OldestLedger <= 0 {
		return nil, fmt.Errorf("getHealth returned no ledger range (status %q)", h.Status)
	}
	return &h, nil
}

func (c *Client) LatestLedger() (int64, error) {
	var r struct {
		Sequence int64 `json:"sequence"`
	}
	return r.Sequence, c.call("getLatestLedger", nil, &r)
}

// LedgerEntry is a single getLedgerEntries result entry.
type LedgerEntry struct {
	Key                string `json:"key"`
	XDR                string `json:"xdr"`
	LastModifiedLedger int64  `json:"lastModifiedLedgerSeq"`
	LiveUntilLedgerSeq int64  `json:"liveUntilLedgerSeq,omitempty"`
}

// GetLedgerEntries fetches raw ledger entries for the given base64-XDR keys.
// Useful for direct contract-storage lookups when SimulateRead is overkill.
func (c *Client) GetLedgerEntries(keys []string) ([]LedgerEntry, error) {
	var r struct {
		Entries []LedgerEntry `json:"entries"`
	}
	params := map[string]any{"keys": keys}
	return r.Entries, c.call("getLedgerEntries", params, &r)
}

func (c *Client) GetAccount(horizonURL, address string) (int64, error) {
	url := fmt.Sprintf("%s/accounts/%s", horizonURL, address)
	resp, err := c.http.Get(url)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	var r struct {
		Sequence string `json:"sequence"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&r); err != nil {
		return 0, err
	}
	var seq int64
	fmt.Sscanf(r.Sequence, "%d", &seq)
	return seq, nil
}

func (c *Client) call(method string, params any, out any) error {
	id := c.seq.Add(1)
	body, _ := json.Marshal(map[string]any{
		"jsonrpc": "2.0",
		"method":  method,
		"params":  params,
		"id":      id,
	})
	req, err := http.NewRequest(http.MethodPost, c.url, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	var rr struct {
		Result json.RawMessage `json:"result"`
		// Decoded loosely: the JSON-RPC spec says "error" is an {code,message}
		// object, but some Soroban RPC nodes/proxies return it as a bare string —
		// or even an empty string "" on success. Forcing it into a struct made
		// every call crash with "cannot unmarshal string into Go struct field".
		Error json.RawMessage `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&rr); err != nil {
		return err
	}
	if msg := rpcErrorMessage(rr.Error); msg != "" {
		return fmt.Errorf("rpc %s: %s", method, msg)
	}
	return json.Unmarshal(rr.Result, out)
}

// rpcErrorMessage extracts a human-readable error from the JSON-RPC "error"
// field, which nodes return inconsistently: an {code,message} object, a bare
// string, an empty string, null, or absent. Returns "" when there is no real
// error so a successful response with `"error":""` is not treated as a failure.
func rpcErrorMessage(raw json.RawMessage) string {
	s := strings.TrimSpace(string(raw))
	if s == "" || s == "null" || s == `""` || s == "{}" {
		return ""
	}
	var obj struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
	}
	if json.Unmarshal(raw, &obj) == nil && (obj.Message != "" || obj.Code != 0) {
		if obj.Message == "" {
			return fmt.Sprintf("code %d", obj.Code)
		}
		if obj.Code != 0 {
			return fmt.Sprintf("%s (code %d)", obj.Message, obj.Code)
		}
		return obj.Message
	}
	var str string
	if json.Unmarshal(raw, &str) == nil {
		if strings.TrimSpace(str) == "" {
			return ""
		}
		return str
	}
	return s
}
