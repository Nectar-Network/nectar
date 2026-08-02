package main

import (
	"os"
	"testing"
)

func TestEnvOr_Fallback(t *testing.T) {
	os.Unsetenv("TEST_VAR_MISSING")
	got := envOr("TEST_VAR_MISSING", "default")
	if got != "default" {
		t.Fatalf("expected 'default', got %q", got)
	}
}

func TestEnvOr_Set(t *testing.T) {
	os.Setenv("TEST_VAR_SET", "hello")
	defer os.Unsetenv("TEST_VAR_SET")
	got := envOr("TEST_VAR_SET", "default")
	if got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

func TestKnownDepositors_Parsed(t *testing.T) {
	setRequiredEnvs(t)
	os.Setenv("KNOWN_DEPOSITORS", "GABC,GXYZ, GFOO ")
	defer os.Unsetenv("KNOWN_DEPOSITORS")

	cfg := LoadConfig()
	if len(cfg.KnownDepositors) != 3 {
		t.Fatalf("expected 3 depositors, got %d", len(cfg.KnownDepositors))
	}
	if cfg.KnownDepositors[0] != "GABC" {
		t.Fatalf("unexpected first depositor: %q", cfg.KnownDepositors[0])
	}
	// spaces trimmed
	if cfg.KnownDepositors[2] != "GFOO" {
		t.Fatalf("expected trimmed %q, got %q", "GFOO", cfg.KnownDepositors[2])
	}
}

func TestKnownDepositors_Empty(t *testing.T) {
	setRequiredEnvs(t)
	os.Unsetenv("KNOWN_DEPOSITORS")

	cfg := LoadConfig()
	if len(cfg.KnownDepositors) != 0 {
		t.Fatalf("expected empty depositor list, got %d", len(cfg.KnownDepositors))
	}
}

func TestPollIntervalDefault(t *testing.T) {
	setRequiredEnvs(t)
	os.Unsetenv("POLL_INTERVAL")

	cfg := LoadConfig()
	if cfg.PollInterval != 10 {
		t.Fatalf("expected default 10, got %d", cfg.PollInterval)
	}
}

func TestMinProfitDefault(t *testing.T) {
	setRequiredEnvs(t)
	os.Unsetenv("MIN_PROFIT")

	cfg := LoadConfig()
	if cfg.MinProfit != 1.02 {
		t.Fatalf("expected default 1.02, got %f", cfg.MinProfit)
	}
}

func TestSlippageBpsDefault(t *testing.T) {
	setRequiredEnvs(t)
	os.Unsetenv("SLIPPAGE_BPS")

	cfg := LoadConfig()
	if cfg.SlippageBps != 100 {
		t.Fatalf("expected default 100, got %d", cfg.SlippageBps)
	}
}

func TestSlippageBpsCustom(t *testing.T) {
	setRequiredEnvs(t)
	os.Setenv("SLIPPAGE_BPS", "250")
	defer os.Unsetenv("SLIPPAGE_BPS")

	cfg := LoadConfig()
	if cfg.SlippageBps != 250 {
		t.Fatalf("expected 250, got %d", cfg.SlippageBps)
	}
}

func TestDriftBpsDefault(t *testing.T) {
	setRequiredEnvs(t)
	os.Unsetenv("DEFINDEX_DRIFT_BPS")

	cfg := LoadConfig()
	if cfg.DriftBps != 500 {
		t.Fatalf("expected default 500, got %d", cfg.DriftBps)
	}
}

// setRequiredEnvs sets the minimum env vars so LoadConfig doesn't os.Exit.
func setRequiredEnvs(t *testing.T) {
	t.Helper()
	pairs := map[string]string{
		"KEEPER_SECRET":     "SABC123FAKE",
		"REGISTRY_CONTRACT": "CABC123FAKE",
		"VAULT_CONTRACT":    "CDEF456FAKE",
		"BLEND_POOL":        "CGHI789FAKE",
		"POLL_INTERVAL":     "10",
		"MIN_PROFIT":        "1.02",
	}
	for k, v := range pairs {
		os.Setenv(k, v)
		t.Cleanup(func() { os.Unsetenv(k) })
	}
}

func TestParseBlendPools_MultiWithModes(t *testing.T) {
	const a = "CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF"
	const b = "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
	pools := parseBlendPools(a+":monitor, "+b+":active", "")
	if len(pools) != 2 {
		t.Fatalf("expected 2 pools, got %d", len(pools))
	}
	if pools[0].Addr != a || !pools[0].Monitor {
		t.Errorf("pool 0: got %+v want %s monitor", pools[0], a)
	}
	if pools[1].Addr != b || pools[1].Monitor {
		t.Errorf("pool 1: got %+v want %s active", pools[1], b)
	}
}

func TestParseBlendPools_DefaultModeIsActive(t *testing.T) {
	const a = "CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF"
	pools := parseBlendPools(a, "")
	if len(pools) != 1 || pools[0].Monitor {
		t.Fatalf("bare address must default to active, got %+v", pools)
	}
}

func TestParseBlendPools_FallbackToLegacySingle(t *testing.T) {
	const a = "CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF"
	pools := parseBlendPools("", a)
	if len(pools) != 1 || pools[0].Addr != a || pools[0].Monitor {
		t.Fatalf("BLEND_POOL fallback broken: %+v", pools)
	}
}

func TestParseBlendPools_EmptyBoth(t *testing.T) {
	if pools := parseBlendPools("", ""); pools != nil {
		t.Fatalf("expected nil, got %+v", pools)
	}
}

func TestParseBlendPools_DedupesKeepingFirst(t *testing.T) {
	const a = "CCEBVDYM32YNYCVNRXQKDFFPISJJCV557CDZEIRBEE4NCV4KHPQ44HGF"
	pools := parseBlendPools(a+":monitor,"+a+":active", "")
	if len(pools) != 1 || !pools[0].Monitor {
		t.Fatalf("dedupe must keep first entry, got %+v", pools)
	}
}
