package soroban

import (
	"fmt"

	"github.com/stellar/go/strkey"
	"github.com/stellar/go/xdr"
)

// NativeContractID derives the Stellar Asset Contract address of the NATIVE
// asset (XLM) for a network passphrase. The SAC address is a deterministic
// hash of (network id, asset), so this needs no RPC and cannot drift from
// chain reality. Used to recognize which pool reserve is the keeper's own fee
// asset — that balance must never be swept by recovery.
func NativeContractID(passphrase string) (string, error) {
	id, err := xdr.MustNewNativeAsset().ContractID(passphrase)
	if err != nil {
		return "", fmt.Errorf("native contract id: %w", err)
	}
	return strkey.Encode(strkey.VersionByteContract, id[:])
}
