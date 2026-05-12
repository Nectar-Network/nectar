package vault

import (
	"fmt"
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
// transient infra failures (sequence/fee/timeout); does not retry on
// deterministic contract errors (insufficient balance, draw limit, etc.).
func Draw(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, vaultAddr string, amount int64) error {
	if amount <= 0 {
		return fmt.Errorf("draw: amount must be > 0, got %d", amount)
	}
	keeperVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return err
	}
	amtVal := soroban.ScvI128(amount)
	_, err = rpc.InvokeWithRetry(horizonURL, kp, passphrase, vaultAddr, "draw",
		soroban.RetryConfig{MaxAttempts: 2, InitialDelay: time.Second, BackoffFactor: 2.0},
		keeperVal, amtVal)
	if err != nil {
		return fmt.Errorf("vault draw: %w", err)
	}
	return nil
}

// ReturnProceeds sends capital back to the vault after a liquidation. Higher
// retry budget than Draw because returning capital is the safer side: the
// keeper has the funds in hand and we want to ensure they reach the vault.
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
	_, err = rpc.InvokeWithRetry(horizonURL, kp, passphrase, vaultAddr, "return_proceeds",
		soroban.DefaultRetry(),
		keeperVal, amtVal, respVal)
	if err != nil {
		return fmt.Errorf("vault return_proceeds: %w", err)
	}
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
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return 0
	}
	return int64(val.I128.Lo)
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
