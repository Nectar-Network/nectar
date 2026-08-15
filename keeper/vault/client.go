package vault

import (
	"fmt"
	"log/slog"
	"math/big"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

type VaultState struct {
	TotalUSDC   int64 `json:"total_usdc"`
	TotalShares int64 `json:"total_shares"`
	TotalProfit int64 `json:"total_profit"`
	ActiveLiq   int64 `json:"active_liq"`
}

type BalanceResult struct {
	Shares    int64
	USDCValue int64
}

// Draw requests capital from NectarVault for a liquidation. Retries on
// transient pre-send failures (sequence/fee/simulation); does not retry on
// deterministic contract errors (insufficient balance, draw limit, etc.) and
// — because the vault ACCUMULATES per-keeper draws — never re-broadcasts after
// a post-send-ambiguous failure, where the first draw may still land and a
// retry would double-draw. RetryAmbiguous stays false.
// asset is the collateral asset this draw targets (contract DECISION F-2a) —
// the vault checks its global and per-asset liquidation pauses against it
// on-chain, so a paused draw fails with a deterministic contract error
// (LiquidationsPaused=14 / AssetPaused=15) and is not retried.
func Draw(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, vaultAddr string, amount int64, asset string) error {
	if amount <= 0 {
		return fmt.Errorf("draw: amount must be > 0, got %d", amount)
	}
	keeperVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return err
	}
	assetVal, err := soroban.ScvAddress(asset)
	if err != nil {
		return fmt.Errorf("draw: bad asset address %q: %w", asset, err)
	}
	amtVal := soroban.ScvI128(amount)
	tx, err := rpc.InvokeWithRetry(horizonURL, kp, passphrase, vaultAddr, "draw",
		soroban.RetryConfig{MaxAttempts: 2, InitialDelay: time.Second, BackoffFactor: 2.0, RetryAmbiguous: false},
		keeperVal, amtVal, assetVal)
	if err != nil {
		return fmt.Errorf("vault draw: %w", err)
	}
	slog.Info("vault draw landed", "amount", amount, "asset", asset, "tx", tx.Hash)
	return nil
}

// ReturnProceeds sends capital back to the vault after a liquidation. Higher
// retry budget than Draw because returning capital is the safer side: the
// keeper has the funds in hand and we want to ensure they reach the vault.
//
// Requires an OUTSTANDING DRAW: the vault rejects a return from a keeper with
// drawn == 0 (NoDraw, error 13 — the VLT-2 anti-donation guard). Proceeds from
// float-funded work (the bad-debt path) therefore cannot be credited to
// depositors through here at all.
//
// Post-send-ambiguous failures are NOT retried: if the first return actually
// landed and fully settled the draw, a re-broadcast would transfer the
// keeper's funds again — and against a now-zero draw it reverts NoDraw, so the
// retry is useless as well as unsafe. The next keeper cycle's stale-draw
// recovery is the safe retry path — it re-reads get_keeper_draw first, so it
// is a no-op when the first return landed.
//
// responseTimeMs is the keeper-observed elapsed time from draw → fill → here.
// Pass 0 when the keeper did not actually execute (e.g. another bot beat it
// to the auction); the registry will skip the response-time update.
func ReturnProceeds(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, vaultAddr string, amount int64, responseTimeMs int64) error {
	if amount <= 0 {
		return fmt.Errorf("return_proceeds: amount must be > 0, got %d", amount)
	}
	keeperVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return err
	}
	amtVal := soroban.ScvI128(amount)
	if responseTimeMs < 0 {
		responseTimeMs = 0
	}
	respVal := soroban.ScvU64(uint64(responseTimeMs))
	retry := soroban.DefaultRetry()
	retry.RetryAmbiguous = false
	tx, err := rpc.InvokeWithRetry(horizonURL, kp, passphrase, vaultAddr, "return_proceeds",
		retry,
		keeperVal, amtVal, respVal)
	if err != nil {
		return fmt.Errorf("vault return_proceeds: %w", err)
	}
	slog.Info("vault return_proceeds landed", "amount", amount, "tx", tx.Hash)
	return nil
}

// GetState reads current vault state.
func GetState(rpc *soroban.Client, passphrase, vaultAddr string) (*VaultState, error) {
	sim, err := rpc.SimulateRead(passphrase, vaultAddr, "get_state")
	if err != nil {
		return nil, fmt.Errorf("get_state: %w", err)
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("get_state: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return nil, fmt.Errorf("get_state: no result")
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil, err
	}
	return parseState(val), nil
}

// Balance returns the shares and current USDC value for a depositor.
func Balance(rpc *soroban.Client, passphrase, vaultAddr, userAddr string) (*BalanceResult, error) {
	addrVal, err := soroban.ScvAddress(userAddr)
	if err != nil {
		return nil, err
	}
	sim, err := rpc.SimulateRead(passphrase, vaultAddr, "balance", addrVal)
	if err != nil {
		return nil, fmt.Errorf("balance: %w", err)
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("balance: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return &BalanceResult{}, nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil, err
	}
	// contract returns (i128, i128) as a Soroban vec
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		return &BalanceResult{}, nil
	}
	vec := **val.Vec
	if len(vec) < 2 {
		return &BalanceResult{}, nil
	}
	return &BalanceResult{
		Shares:    scI128(vec[0]),
		USDCValue: scI128(vec[1]),
	}, nil
}

// GetKeeperDraw reads a keeper's outstanding (drawn-but-unreturned) capital and
// the ledger timestamp of its most recent draw — (0, 0) when there is none.
// The contract returns the tuple (i128 amount, u64 since) (F4): the timestamp
// lets a restarted keeper age its own stale draw from chain state alone.
// Returns an error on vaults deployed before the getter existed, which the
// caller treats as "no recovery possible".
func GetKeeperDraw(rpc *soroban.Client, passphrase, vaultAddr, keeper string) (int64, uint64, error) {
	addrVal, err := soroban.ScvAddress(keeper)
	if err != nil {
		return 0, 0, err
	}
	sim, err := rpc.SimulateRead(passphrase, vaultAddr, "get_keeper_draw", addrVal)
	if err != nil {
		return 0, 0, fmt.Errorf("get_keeper_draw: %w", err)
	}
	if sim.Error != "" {
		return 0, 0, fmt.Errorf("get_keeper_draw: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return 0, 0, nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return 0, 0, err
	}
	// (i128, u64) arrives as a 2-element Soroban vec.
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		return 0, 0, fmt.Errorf("get_keeper_draw: unexpected result shape %v", val.Type)
	}
	vec := **val.Vec
	if len(vec) < 2 {
		return 0, 0, fmt.Errorf("get_keeper_draw: tuple has %d elements, want 2", len(vec))
	}
	var since uint64
	if vec[1].Type == xdr.ScValTypeScvU64 && vec[1].U64 != nil {
		since = uint64(*vec[1].U64)
	}
	return scI128(vec[0]), since, nil
}

func parseState(val xdr.ScVal) *VaultState {
	s := &VaultState{}
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return s
	}
	for _, e := range **val.Map {
		k := scSymbol(e.Key)
		v := scI128Big(e.Val)
		if v == nil {
			continue
		}
		n := v.Int64()
		switch k {
		case "total_usdc":
			s.TotalUSDC = n
		case "total_shares":
			s.TotalShares = n
		case "total_profit":
			s.TotalProfit = n
		case "active_liq":
			s.ActiveLiq = n
		}
	}
	return s
}

func scSymbol(val xdr.ScVal) string {
	if val.Type == xdr.ScValTypeScvSymbol && val.Sym != nil {
		return string(*val.Sym)
	}
	return ""
}

func scI128(val xdr.ScVal) int64 {
	n, _ := soroban.I128ToInt64(val) // out-of-int64-range decodes as 0
	return n
}

func scI128Big(val xdr.ScVal) *big.Int {
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return nil
	}
	hi := new(big.Int).SetInt64(int64(val.I128.Hi))
	lo := new(big.Int).SetUint64(uint64(val.I128.Lo))
	result := new(big.Int).Lsh(hi, 64)
	result.Add(result, lo)
	return result
}
