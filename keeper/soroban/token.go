package soroban

import (
	"fmt"
	"math/big"

	"github.com/stellar/go/xdr"
)

// TokenBalance reads a Soroban-token balance for the given holder.
// Works for both Stellar Asset Contracts (SAC) and custom token contracts that
// implement the standard `balance(address) -> i128` view.
//
// Returns 0 (no error) when the contract reports the holder has never been
// touched (some token impls return ScvVoid in that case).
func (c *Client) TokenBalance(passphrase, tokenAddr, holder string) (int64, error) {
	holderVal, err := ScvAddress(holder)
	if err != nil {
		return 0, fmt.Errorf("encode holder: %w", err)
	}
	sim, err := c.SimulateRead(passphrase, tokenAddr, "balance", holderVal)
	if err != nil {
		return 0, fmt.Errorf("balance sim: %w", err)
	}
	if sim.Error != "" {
		return 0, fmt.Errorf("balance: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return 0, nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return 0, fmt.Errorf("balance decode: %w", err)
	}
	switch val.Type {
	case xdr.ScValTypeScvI128:
		if val.I128 == nil {
			return 0, nil
		}
		hi := new(big.Int).SetInt64(int64(val.I128.Hi))
		lo := new(big.Int).SetUint64(uint64(val.I128.Lo))
		result := new(big.Int).Lsh(hi, 64)
		result.Add(result, lo)
		// i128 balances above MaxInt64 (≈9.2e18 stroops ≈ 9.2e11 USDC) are
		// well beyond anything realistic — clamp rather than overflow silently.
		if !result.IsInt64() {
			return 0, fmt.Errorf("balance overflows int64: %s", result.String())
		}
		return result.Int64(), nil
	case xdr.ScValTypeScvVoid:
		return 0, nil
	default:
		return 0, fmt.Errorf("unexpected balance type %s", val.Type.String())
	}
}
