package main

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// BlendPoolConfig is one entry of BLEND_POOLS: a pool contract address plus
// its capital mode. Monitor pools are scanned and reported but never receive
// vault capital. PoolUsdc, when set, names the pool's own USDC contract for
// pools that settle in a different USDC than the vault; the keeper converts
// vault-USDC <-> pool-USDC through the DEX with a par-anchored slippage guard.
type BlendPoolConfig struct {
	Addr     string
	Monitor  bool
	PoolUsdc string
}

type Config struct {
	RpcURL         string
	HorizonURL     string
	Passphrase     string
	SecretKey      string
	KeeperName     string
	RegistryID     string
	VaultID        string
	BlendPools     []BlendPoolConfig // parsed from BLEND_POOLS (fallback: BLEND_POOL)
	UsdcAddr       string            // USDC token contract; collateral is swapped into this
	SoroswapRouter string            // Soroswap router contract (primary DEX); empty disables
	PhoenixRouter  string            // Phoenix XYK pool (pair) contract for the collateral/USDC pair (fallback DEX); empty disables
	DeFindexVault  string            // DeFindex vault to monitor for rebalancing; empty disables
	APIPort        string
	PollInterval   int
	MinProfit      float64
	SlippageBps    int // max swap slippage in basis points (100 = 1%)
	DriftBps       int // DeFindex allocation drift threshold in bps (500 = 5%)
	// BorrowerCache is where the event-indexed borrower set is persisted between
	// restarts. Empty disables persistence: every start then rebuilds the set by
	// backfilling the RPC's full retention window.
	//
	// That rebuild is complete for every borrower active within the window
	// (~7 days observed) but NOT for one idle longer than that — its events have
	// aged out of the RPC, so only the cache remembers it. Running without a
	// cache, or losing one, means long-idle borrowers stay invisible until they
	// next transact. Pin any that matter with WATCH_ADDRESSES.
	BorrowerCache string
	// WatchAddresses is an OPTIONAL additive list of addresses to probe every
	// cycle regardless of what events say. It is a supplement to event-driven
	// discovery, not the mechanism: leaving it empty is the normal case.
	WatchAddresses  []string
	KnownDepositors []string // comma-separated G-addresses for performance page
	// XlmReserve (stroops) is the native XLM the keeper always keeps for
	// transaction fees. Stale-draw recovery may sweep pool-reserve assets the
	// keeper holds back into USDC, and for native XLM only the balance ABOVE
	// this floor is ever considered sellable.
	XlmReserve int64

	// BadDebtMaxSpend (stroops) caps the keeper-float USDC committed to one
	// bad-debt auction fill; 0 disables bad-debt fills. Bad-debt fills never
	// use vault capital: the fill pays the assumed debt in USDC and receives
	// backstop LP tokens, which cannot be liquidated into the vault's
	// settlement asset before mainnet — vault draws would hit the registry
	// slash timeout. Fill-and-hold is therefore keeper-operator risk, bounded
	// by this cap, and operator REWARD too: the unwound USDC can never be
	// credited to the vault, since return_proceeds rejects a keeper with no
	// outstanding draw (NoDraw).
	BadDebtMaxSpend int64
	// BadDebtHaircutBps discounts the backstop LP spot valuation in the
	// bad-debt profitability check (5000 = value LP at 50% of spot).
	BadDebtHaircutBps int

	// Testnet USDC faucet (payments from a treasury account; NOT a mint —
	// Circle USDC is not mintable by us). Empty FaucetSecret disables.
	FaucetSecret       string
	FaucetAmount       int64 // stroops per claim (default 1000 USDC)
	FaucetCooldownSecs int64 // per-address cooldown (default 3600)
}

func LoadConfig() Config {
	c := Config{
		RpcURL:         envOr("SOROBAN_RPC", "https://soroban-testnet.stellar.org:443"),
		HorizonURL:     envOr("HORIZON_URL", "https://horizon-testnet.stellar.org"),
		Passphrase:     envOr("NETWORK_PASSPHRASE", "Test SDF Network ; September 2015"),
		SecretKey:      mustEnv("KEEPER_SECRET"),
		KeeperName:     envOr("KEEPER_NAME", "nectar-keeper-1"),
		RegistryID:     mustEnv("REGISTRY_CONTRACT"),
		VaultID:        mustEnv("VAULT_CONTRACT"),
		UsdcAddr:       envOr("USDC_CONTRACT", ""),
		SoroswapRouter: envOr("SOROSWAP_ROUTER", ""),
		PhoenixRouter:  envOr("PHOENIX_ROUTER", ""),
		DeFindexVault:  envOr("DEFINDEX_VAULT", ""),
		APIPort:        envOr("API_PORT", "8080"),
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

	slipStr := envOr("SLIPPAGE_BPS", "100")
	slip, err := strconv.Atoi(slipStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "SLIPPAGE_BPS=%q is not a valid integer\n", slipStr)
		os.Exit(1)
	}
	if slip < 0 || slip > 10000 {
		fmt.Fprintf(os.Stderr, "SLIPPAGE_BPS=%d out of range [0,10000]\n", slip)
		os.Exit(1)
	}
	c.SlippageBps = slip

	driftStr := envOr("DEFINDEX_DRIFT_BPS", "500")
	drift, err := strconv.Atoi(driftStr)
	if err != nil {
		fmt.Fprintf(os.Stderr, "DEFINDEX_DRIFT_BPS=%q is not a valid integer\n", driftStr)
		os.Exit(1)
	}
	if drift < 0 || drift > 10000 {
		fmt.Fprintf(os.Stderr, "DEFINDEX_DRIFT_BPS=%d out of range [0,10000]\n", drift)
		os.Exit(1)
	}
	c.DriftBps = drift

	c.BorrowerCache = envOr("BORROWER_CACHE", "")

	if raw := os.Getenv("WATCH_ADDRESSES"); raw != "" {
		for _, addr := range strings.Split(raw, ",") {
			addr = strings.TrimSpace(addr)
			if addr == "" {
				continue
			}
			if len(addr) != 56 || (addr[0] != 'G' && addr[0] != 'C') {
				fmt.Fprintf(os.Stderr, "WATCH_ADDRESSES entry %q is not a 56-char G/C address\n", addr)
				os.Exit(1)
			}
			c.WatchAddresses = append(c.WatchAddresses, addr)
		}
	}

	xlmResStr := envOr("KEEPER_XLM_RESERVE", "1000000000") // 100 XLM
	xlmRes, err := strconv.ParseInt(xlmResStr, 10, 64)
	if err != nil || xlmRes < 0 {
		fmt.Fprintf(os.Stderr, "KEEPER_XLM_RESERVE=%q must be a non-negative integer (stroops)\n", xlmResStr)
		os.Exit(1)
	}
	c.XlmReserve = xlmRes

	bdSpendStr := envOr("BAD_DEBT_MAX_SPEND", "1000000000") // 100.0000000 USDC
	bdSpend, err := strconv.ParseInt(bdSpendStr, 10, 64)
	if err != nil || bdSpend < 0 {
		fmt.Fprintf(os.Stderr, "BAD_DEBT_MAX_SPEND=%q must be a non-negative integer (stroops; 0 disables)\n", bdSpendStr)
		os.Exit(1)
	}
	c.BadDebtMaxSpend = bdSpend

	bdHairStr := envOr("BAD_DEBT_LP_HAIRCUT_BPS", "5000")
	bdHair, err := strconv.Atoi(bdHairStr)
	if err != nil || bdHair < 0 || bdHair > 10000 {
		fmt.Fprintf(os.Stderr, "BAD_DEBT_LP_HAIRCUT_BPS=%q out of range [0,10000]\n", bdHairStr)
		os.Exit(1)
	}
	c.BadDebtHaircutBps = bdHair

	if raw := os.Getenv("KNOWN_DEPOSITORS"); raw != "" {
		for _, addr := range strings.Split(raw, ",") {
			addr = strings.TrimSpace(addr)
			if addr != "" {
				c.KnownDepositors = append(c.KnownDepositors, addr)
			}
		}
	}

	c.BlendPools = parseBlendPools(os.Getenv("BLEND_POOLS"), os.Getenv("BLEND_POOL"))

	c.FaucetSecret = envOr("FAUCET_SECRET", "")
	amtStr := envOr("FAUCET_AMOUNT", "10000000000") // 1000.0000000 USDC
	amt, err := strconv.ParseInt(amtStr, 10, 64)
	if err != nil || amt <= 0 {
		fmt.Fprintf(os.Stderr, "FAUCET_AMOUNT=%q must be a positive integer (stroops)\n", amtStr)
		os.Exit(1)
	}
	c.FaucetAmount = amt
	cdStr := envOr("FAUCET_COOLDOWN_SECS", "3600")
	cd, err := strconv.ParseInt(cdStr, 10, 64)
	if err != nil || cd < 0 {
		fmt.Fprintf(os.Stderr, "FAUCET_COOLDOWN_SECS=%q must be a non-negative integer\n", cdStr)
		os.Exit(1)
	}
	c.FaucetCooldownSecs = cd

	return c
}

// parseBlendPools parses BLEND_POOLS, a comma-separated list of
// `POOL_ADDRESS[:mode[:POOL_USDC_ADDRESS]]` entries. mode is `active`
// (default — vault capital may be used to fill this pool's auctions) or
// `monitor` (scan and report only). The optional third field names the pool's
// own USDC contract when it differs from the vault's USDC; fills then convert
// through the DEX under a par-anchored slippage guard. When BLEND_POOLS is
// empty it falls back to the legacy single BLEND_POOL variable (active mode),
// preserving backward compatibility. Duplicate addresses keep their first
// entry. Invalid entries exit(1) rather than silently monitoring the wrong
// contract.
func parseBlendPools(multi, single string) []BlendPoolConfig {
	raw := strings.TrimSpace(multi)
	if raw == "" {
		if s := strings.TrimSpace(single); s != "" {
			return []BlendPoolConfig{{Addr: s}}
		}
		return nil
	}
	var pools []BlendPoolConfig
	seen := make(map[string]bool)
	for _, entry := range strings.Split(raw, ",") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		parts := strings.Split(entry, ":")
		if len(parts) > 3 {
			fmt.Fprintf(os.Stderr, "BLEND_POOLS entry %q: too many fields (want ADDR[:mode[:POOL_USDC]])\n", entry)
			os.Exit(1)
		}
		pc := BlendPoolConfig{Addr: strings.TrimSpace(parts[0])}
		if len(parts) >= 2 {
			switch mode := strings.ToLower(strings.TrimSpace(parts[1])); mode {
			case "active", "":
			case "monitor":
				pc.Monitor = true
			default:
				fmt.Fprintf(os.Stderr, "BLEND_POOLS entry %q: unknown mode %q (want active|monitor)\n", entry, mode)
				os.Exit(1)
			}
		}
		if len(parts) == 3 {
			pc.PoolUsdc = strings.TrimSpace(parts[2])
			if len(pc.PoolUsdc) != 56 || !strings.HasPrefix(pc.PoolUsdc, "C") {
				fmt.Fprintf(os.Stderr, "BLEND_POOLS entry %q: pool USDC %q is not a contract address\n", entry, pc.PoolUsdc)
				os.Exit(1)
			}
		}
		if len(pc.Addr) != 56 || !strings.HasPrefix(pc.Addr, "C") {
			fmt.Fprintf(os.Stderr, "BLEND_POOLS entry %q: %q is not a contract address\n", entry, pc.Addr)
			os.Exit(1)
		}
		if seen[pc.Addr] {
			continue
		}
		seen[pc.Addr] = true
		pools = append(pools, pc)
	}
	return pools
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
