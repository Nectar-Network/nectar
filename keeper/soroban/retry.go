package soroban

import (
	"strings"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"
)

// RetryConfig controls exponential-backoff retries for transient RPC failures.
type RetryConfig struct {
	MaxAttempts   int
	InitialDelay  time.Duration
	BackoffFactor float64
}

// DefaultRetry is the recommended config for write-side calls (Invoke).
func DefaultRetry() RetryConfig {
	return RetryConfig{MaxAttempts: 3, InitialDelay: time.Second, BackoffFactor: 2.0}
}

// isRetryable reports whether the err is worth retrying. Transient sequence /
// fee / resource issues retry; deterministic contract failures do not.
func isRetryable(err error) bool {
	if err == nil {
		return false
	}
	s := strings.ToLower(err.Error())

	// Hard "no" — these are deterministic failures; retrying just burns fees.
	nonRetryable := []string{
		"insufficient_balance",
		"insufficient balance",
		"already filled",
		"alreadyfilled",
		"auctionnotfound",
		"contract error",
		"contract panic",
		"unauthorized",
		"already registered",
	}
	for _, sub := range nonRetryable {
		if strings.Contains(s, sub) {
			return false
		}
	}

	// Soft "yes" — transient infra/network issues.
	retryable := []string{
		"tx_too_late",
		"tx_insufficient_fee",
		"resource_exhaust",
		"timeout",
		"timed out",
		"connection reset",
		"connection refused",
		"eof",
		"sequence",
	}
	for _, sub := range retryable {
		if strings.Contains(s, sub) {
			return true
		}
	}
	return false
}

// InvokeWithRetry wraps Invoke with exponential backoff. On a non-retryable
// error or after MaxAttempts, the last error is returned.
func (c *Client) InvokeWithRetry(
	horizonURL string,
	kp *keypair.Full,
	passphrase, contractID, fn string,
	retry RetryConfig,
	args ...xdr.ScVal,
) (*TxResult, error) {
	if retry.MaxAttempts < 1 {
		retry.MaxAttempts = 1
	}
	delay := retry.InitialDelay
	var lastErr error
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		result, err := c.Invoke(horizonURL, kp, passphrase, contractID, fn, args...)
		if err == nil {
			return result, nil
		}
		lastErr = err
		if attempt == retry.MaxAttempts || !isRetryable(err) {
			return nil, err
		}
		time.Sleep(delay)
		if retry.BackoffFactor > 0 {
			delay = time.Duration(float64(delay) * retry.BackoffFactor)
		}
	}
	return nil, lastErr
}

// retryWith is a small inner helper used by tests: it executes `op` under the
// retry policy. Production code should call InvokeWithRetry; this exists so the
// retry policy can be unit-tested without spinning a real RPC server.
func retryWith(retry RetryConfig, op func() error) error {
	if retry.MaxAttempts < 1 {
		retry.MaxAttempts = 1
	}
	delay := retry.InitialDelay
	var lastErr error
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		err := op()
		if err == nil {
			return nil
		}
		lastErr = err
		if attempt == retry.MaxAttempts || !isRetryable(err) {
			return err
		}
		time.Sleep(delay)
		if retry.BackoffFactor > 0 {
			delay = time.Duration(float64(delay) * retry.BackoffFactor)
		}
	}
	return lastErr
}

// IsRetryable is the exported wrapper for callers (e.g. vault client) that
// want to honour the same retry policy without re-implementing the logic.
func IsRetryable(err error) bool { return isRetryable(err) }

// RetryWith is the exported wrapper around the inner retry runner.
func RetryWith(retry RetryConfig, op func() error) error { return retryWith(retry, op) }
