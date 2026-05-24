package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	RpcURL          string
	HorizonURL      string
	Passphrase      string
	SecretKey       string
	KeeperName      string
	RegistryID      string
	VaultID         string
	USDCContract    string // for keeper balance reads when computing real proceeds
	BlendPool       string
	APIPort         string
	PollInterval    int
	MinProfit       float64
	DemoProfitBPS   int64    // 0 = production (return real USDC delta); >0 = lab/demo mode tops up profit from keeper's own balance
	SlashScanEvery  int      // every N cycles, sweep the registry and slash any keeper past its timeout (0 = off)
	KnownDepositors []string // comma-separated G-addresses for performance page
}

func LoadConfig() Config {
	c := Config{
		RpcURL:       envOr("SOROBAN_RPC", "https://soroban-testnet.stellar.org:443"),
		HorizonURL:   envOr("HORIZON_URL", "https://horizon-testnet.stellar.org"),
		Passphrase:   envOr("NETWORK_PASSPHRASE", "Test SDF Network ; September 2015"),
		SecretKey:    mustEnv("KEEPER_SECRET"),
		KeeperName:   envOr("KEEPER_NAME", "nectar-keeper-1"),
		RegistryID:   mustEnv("REGISTRY_CONTRACT"),
		VaultID:      mustEnv("VAULT_CONTRACT"),
		USDCContract: envOr("USDC_CONTRACT", ""),
		BlendPool:    envOr("BLEND_POOL", ""),
		APIPort:      envOr("API_PORT", "8080"),
	}

	pollStr := envOr("POLL_INTERVAL", "10")
	poll, err := strconv.Atoi(pollStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "POLL_INTERVAL=%q is not a valid integer\n", pollStr)
		os.Exit(1)
	}
	if poll < 3 || poll > 300 {
		fmt.Fprintf(os.Stderr, "POLL_INTERVAL=%d out of range [3,300]\n", poll)
		os.Exit(1)
	}
	c.PollInterval = poll

	profitStr := envOr("MIN_PROFIT", "1.02")
	profit, err := strconv.ParseFloat(profitStr, 64)
	if err != nil {
		fmt.Fprintf(os.Stderr, "MIN_PROFIT=%q is not a valid float\n", profitStr)
		os.Exit(1)
	}
	if profit <= 0 {
		fmt.Fprintf(os.Stderr, "MIN_PROFIT must be > 0, got %.4f\n", profit)
		os.Exit(1)
	}
	c.MinProfit = profit

	if raw := os.Getenv("KNOWN_DEPOSITORS"); raw != "" {
		for _, addr := range strings.Split(raw, ",") {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				c.KnownDepositors = append(c.KnownDepositors, addr)
			}
		}
	}

	// DEMO_PROFIT_BPS: opt-in synthetic profit (in basis points) the keeper
	// tops up from its own USDC. 0 = production (return real proceeds only).
	// Set 1000 (= 10%) when running against LiquidationLab for the headline
	// demo flow; never set this against a real Blend pool.
	if raw := strings.TrimSpace(os.Getenv("DEMO_PROFIT_BPS")); raw != "" {
		bps, err := strconv.ParseInt(raw, 10, 64)
		if err != nil {
			fmt.Fprintf(os.Stderr, "DEMO_PROFIT_BPS=%q is not a valid integer\n", raw)
			os.Exit(1)
		}
		if bps < 0 || bps > 10_000 {
			fmt.Fprintf(os.Stderr, "DEMO_PROFIT_BPS=%d out of range [0,10000]\n", bps)
			os.Exit(1)
		}
		c.DemoProfitBPS = bps
	}

	// SLASH_SCAN_EVERY: every N cycles, sweep the registry and call slash()
	// on any keeper past its slash_timeout. 0 disables (admin-driven slashing).
	scanStr := envOr("SLASH_SCAN_EVERY", "0")
	scan, err := strconv.Atoi(scanStr)
	if err != nil || scan < 0 {
		fmt.Fprintf(os.Stderr, "SLASH_SCAN_EVERY=%q must be a non-negative integer\n", scanStr)
		os.Exit(1)
	}
	c.SlashScanEvery = scan

	return c
}

func mustEnv(key string) string {
	v := strings.TrimSpace(os.Getenv(key))
	if v == "" {
		fmt.Fprintf(os.Stderr, "missing required env: %s\n", key)
		os.Exit(1)
	}
	return v
}

func envOr(key, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(key)); v != "" {
		return v
	}
	return fallback
}
