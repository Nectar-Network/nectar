package soroban

import (
	"errors"
	"testing"
	"time"
)

func TestIsRetryable_Transient(t *testing.T) {
	cases := []string{
		"tx_too_late",
		"sequence number conflict",
		"resource_exhaust",
		"timeout waiting for tx",
		"timed out",
		"connection reset by peer",
		"EOF",
		"tx_insufficient_fee",
	}
	for _, msg := range cases {
		err := errors.New(msg)
		if !IsRetryable(err) {
			t.Errorf("%q should be retryable", msg)
		}
	}
}

func TestIsRetryable_Deterministic(t *testing.T) {
	cases := []string{
		"insufficient_balance",
		"contract error: NotRegistered",
		"already filled",
		"AuctionNotFound",
		"unauthorized",
	}
	for _, msg := range cases {
		err := errors.New(msg)
		if IsRetryable(err) {
			t.Errorf("%q should NOT be retryable", msg)
		}
	}
}

func TestIsRetryable_Nil(t *testing.T) {
	if IsRetryable(nil) {
		t.Fatal("nil error should not be retryable")
	}
}

func TestRetryWith_Success_FirstAttempt(t *testing.T) {
	calls := 0
	cfg := RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, BackoffFactor: 2.0}
	err := RetryWith(cfg, func() error {
		calls++
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if calls != 1 {
		t.Errorf("expected 1 call, got %d", calls)
	}
}

func TestRetryWith_Success_AfterTransient(t *testing.T) {
	calls := 0
	cfg := RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, BackoffFactor: 2.0}
	err := RetryWith(cfg, func() error {
		calls++
		if calls < 3 {
			return errors.New("tx_too_late")
		}
		return nil
	})
	if err != nil {
		t.Fatalf("unexpected: %v", err)
	}
	if calls != 3 {
		t.Errorf("expected 3 calls, got %d", calls)
	}
}

func TestRetryWith_StopsOnNonRetryable(t *testing.T) {
	calls := 0
	cfg := RetryConfig{MaxAttempts: 5, InitialDelay: time.Millisecond, BackoffFactor: 2.0}
	err := RetryWith(cfg, func() error {
		calls++
		return errors.New("insufficient_balance")
	})
	if err == nil {
		t.Fatal("expected error to propagate")
	}
	if calls != 1 {
		t.Errorf("non-retryable should fail fast; got %d calls", calls)
	}
}

func TestRetryWith_ExhaustsBudget(t *testing.T) {
	calls := 0
	cfg := RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, BackoffFactor: 2.0}
	err := RetryWith(cfg, func() error {
		calls++
		return errors.New("timeout: rpc unavailable")
	})
	if err == nil {
		t.Fatal("expected exhaustion error")
	}
	if calls != cfg.MaxAttempts {
		t.Errorf("expected %d attempts, got %d", cfg.MaxAttempts, calls)
	}
}

func TestRetryWith_BackoffGrows(t *testing.T) {
	cfg := RetryConfig{MaxAttempts: 4, InitialDelay: 5 * time.Millisecond, BackoffFactor: 2.0}
	start := time.Now()
	calls := 0
	_ = RetryWith(cfg, func() error {
		calls++
		return errors.New("timeout")
	})
	elapsed := time.Since(start)
	// Three sleeps between four attempts: 5 + 10 + 20 = 35ms minimum.
	min := 30 * time.Millisecond
	if elapsed < min {
		t.Errorf("expected at least %v elapsed, got %v", min, elapsed)
	}
}

func TestRetryWith_ZeroAttemptsCoercesToOne(t *testing.T) {
	calls := 0
	cfg := RetryConfig{MaxAttempts: 0}
	err := RetryWith(cfg, func() error {
		calls++
		return errors.New("timeout")
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if calls != 1 {
		t.Errorf("expected 1 call (coerced), got %d", calls)
	}
}

func TestDefaultRetry_HasReasonableValues(t *testing.T) {
	c := DefaultRetry()
	if c.MaxAttempts < 2 {
		t.Errorf("default MaxAttempts too low: %d", c.MaxAttempts)
	}
	if c.InitialDelay <= 0 {
		t.Error("default InitialDelay must be positive")
	}
	if c.BackoffFactor <= 1 {
		t.Errorf("default BackoffFactor should be >1, got %f", c.BackoffFactor)
	}
}

// Post-send ambiguity: the tx may still land, so a config that forbids
// ambiguous retries must stop after the first attempt even though the error
// string ("timed out") is otherwise retryable.
func TestRetryWith_AmbiguousNotRetriedWhenDisabled(t *testing.T) {
	attempts := 0
	amb := &TxStatusUnknownError{Hash: "deadbeefcafe", Err: errors.New("tx deadbeef timed out")}
	err := retryWith(RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, RetryAmbiguous: false}, func() error {
		attempts++
		return amb
	})
	if attempts != 1 {
		t.Fatalf("expected 1 attempt (no ambiguous retry), got %d", attempts)
	}
	if !IsTxStatusUnknown(err) {
		t.Fatalf("ambiguity must survive to the caller, got %v", err)
	}
}

func TestRetryWith_AmbiguousRetriedWhenEnabled(t *testing.T) {
	attempts := 0
	err := retryWith(RetryConfig{MaxAttempts: 3, InitialDelay: time.Millisecond, RetryAmbiguous: true}, func() error {
		attempts++
		if attempts < 2 {
			return &TxStatusUnknownError{Hash: "deadbeefcafe", Err: errors.New("tx deadbeef timed out")}
		}
		return nil
	})
	if err != nil || attempts != 2 {
		t.Fatalf("expected success on attempt 2, got attempts=%d err=%v", attempts, err)
	}
}

func TestIsTxStatusUnknown_Wrapping(t *testing.T) {
	amb := &TxStatusUnknownError{Hash: "deadbeefcafe", Err: errors.New("poll failed")}
	if !IsTxStatusUnknown(amb) {
		t.Fatal("bare ambiguous error must classify")
	}
	if !IsTxStatusUnknown(errors.Join(errors.New("outer"), amb)) {
		t.Fatal("joined ambiguous error must classify")
	}
	if IsTxStatusUnknown(errors.New("tx deadbeef timed out")) {
		t.Fatal("a plain string error must not classify as ambiguous")
	}
}
