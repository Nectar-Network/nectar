package soroban

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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

func (c *Client) GetEvents(startLedger int64, contractID string) ([]Event, error) {
	var r struct {
		Events []Event `json:"events"`
	}
	params := map[string]any{
		"startLedger": startLedger,
		"filters": []map[string]any{
			{"type": "contract", "contractIds": []string{contractID}},
		},
		"pagination": map[string]int{"limit": 200},
	}
	return r.Events, c.call("getEvents", params, &r)
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
