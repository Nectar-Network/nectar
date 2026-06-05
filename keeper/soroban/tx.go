package soroban

import (
	"fmt"
	"strconv"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/network"
	"github.com/stellar/go/strkey"
	"github.com/stellar/go/txnbuild"
	"github.com/stellar/go/xdr"
)

// Invoke builds, simulates, assembles, signs, sends, and awaits a contract call.
func (c *Client) Invoke(horizonURL string, kp *keypair.Full, passphrase, contractID, fn string, args ...xdr.ScVal) (*TxResult, error) {
	seq, err := c.GetAccount(horizonURL, kp.Address())
	if err != nil {
		return nil, fmt.Errorf("get account seq: %w", err)
	}

	txXDR, err := buildTx(contractID, fn, args, kp.Address(), seq+1, passphrase, nil)
	if err != nil {
		return nil, err
	}
	sim, err := c.Simulate(txXDR)
	if err != nil {
		return nil, err
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("%s sim: %s", fn, sim.Error)
	}

	signedXDR, err := assembleAndSign(contractID, fn, args, kp, seq+1, sim, passphrase)
	if err != nil {
		return nil, err
	}
	hash, err := c.Send(signedXDR)
	if err != nil {
		return nil, err
	}
	return c.AwaitTx(hash, 30*1e9) // 30s
}

// SimulateRead calls simulateTransaction for read-only functions (no signing).
func (c *Client) SimulateRead(passphrase, contractID, fn string, args ...xdr.ScVal) (*SimulateResult, error) {
	dummyKP, _ := keypair.Random()
	txXDR, err := buildTx(contractID, fn, args, dummyKP.Address(), 0, passphrase, nil)
	if err != nil {
		return nil, err
	}
	return c.Simulate(txXDR)
}

func buildTx(contractID, fn string, args []xdr.ScVal, source string, seq int64, passphrase string, sim *SimulateResult) (string, error) {
	contractAddr, err := contractScAddress(contractID)
	if err != nil {
		return "", err
	}
	op := &txnbuild.InvokeHostFunction{
		HostFunction: xdr.HostFunction{
			Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
			InvokeContract: &xdr.InvokeContractArgs{
				ContractAddress: contractAddr,
				FunctionName:    xdr.ScSymbol(fn),
				Args:            xdr.ScVec(args),
			},
		},
	}
	acct := txnbuild.SimpleAccount{AccountID: source, Sequence: seq}
	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &acct,
		IncrementSequenceNum: false,
		Operations:           []txnbuild.Operation{op},
		BaseFee:              txnbuild.MinBaseFee,
		Preconditions:        txnbuild.Preconditions{TimeBounds: txnbuild.NewInfiniteTimeout()},
	})
	if err != nil {
		return "", err
	}
	b, err := tx.MarshalBinary()
	if err != nil {
		return "", err
	}
	var env xdr.TransactionEnvelope
	if err := env.UnmarshalBinary(b); err != nil {
		return "", err
	}
	return xdr.MarshalBase64(env)
}

func assembleAndSign(contractID, fn string, args []xdr.ScVal, kp *keypair.Full, seq int64, sim *SimulateResult, passphrase string) (string, error) {
	contractAddr, err := contractScAddress(contractID)
	if err != nil {
		return "", err
	}

	var authEntries []xdr.SorobanAuthorizationEntry
	for _, r := range sim.Results {
		for _, ab64 := range r.Auth {
			var e xdr.SorobanAuthorizationEntry
			if err := xdr.SafeUnmarshalBase64(ab64, &e); err != nil {
				return "", err
			}
			authEntries = append(authEntries, e)
		}
	}

	op := &txnbuild.InvokeHostFunction{
		HostFunction: xdr.HostFunction{
			Type: xdr.HostFunctionTypeHostFunctionTypeInvokeContract,
			InvokeContract: &xdr.InvokeContractArgs{
				ContractAddress: contractAddr,
				FunctionName:    xdr.ScSymbol(fn),
				Args:            xdr.ScVec(args),
			},
		},
		Auth: authEntries,
	}

	minFee, _ := strconv.ParseInt(sim.MinResourceFee, 10, 64)
	acct := txnbuild.SimpleAccount{AccountID: kp.Address(), Sequence: seq}
	tx, err := txnbuild.NewTransaction(txnbuild.TransactionParams{
		SourceAccount:        &acct,
		IncrementSequenceNum: false,
		Operations:           []txnbuild.Operation{op},
		BaseFee:              txnbuild.MinBaseFee + minFee,
		Preconditions:        txnbuild.Preconditions{TimeBounds: txnbuild.NewTimeout(30)},
	})
	if err != nil {
		return "", err
	}

	b, err := tx.MarshalBinary()
	if err != nil {
		return "", err
	}
	var env xdr.TransactionEnvelope
	if err := env.UnmarshalBinary(b); err != nil {
		return "", err
	}

	var sorobanData xdr.SorobanTransactionData
	if err := xdr.SafeUnmarshalBase64(sim.TransactionData, &sorobanData); err != nil {
		return "", fmt.Errorf("parse soroban data: %w", err)
	}
	env.V1.Tx.Ext = xdr.TransactionExt{V: 1, SorobanData: &sorobanData}

	hash, err := network.HashTransactionInEnvelope(env, passphrase)
	if err != nil {
		return "", err
	}
	sig, err := kp.Sign(hash[:])
	if err != nil {
		return "", err
	}
	env.V1.Signatures = []xdr.DecoratedSignature{{
		Hint:      xdr.SignatureHint(kp.Hint()),
		Signature: xdr.Signature(sig),
	}}
	return xdr.MarshalBase64(env)
}

// ScvAddress encodes a Stellar address (G... or C...) as xdr.ScVal.
func ScvAddress(addr string) (xdr.ScVal, error) {
	var scAddr xdr.ScAddress
	var err error
	if len(addr) > 0 && addr[0] == 'C' {
		scAddr, err = contractScAddress(addr)
	} else {
		scAddr, err = accountScAddress(addr)
	}
	if err != nil {
		return xdr.ScVal{}, err
	}
	return xdr.ScVal{Type: xdr.ScValTypeScvAddress, Address: &scAddr}, nil
}

// ScvU32 wraps a uint32 as ScVal. Blend's Request.request_type is u32.
func ScvU32(n uint32) xdr.ScVal {
	v := xdr.Uint32(n)
	return xdr.ScVal{Type: xdr.ScValTypeScvU32, U32: &v}
}

// ScvU64 wraps a uint64 as ScVal.
func ScvU64(n uint64) xdr.ScVal {
	v := xdr.Uint64(n)
	return xdr.ScVal{Type: xdr.ScValTypeScvU64, U64: &v}
}

// ScvI128 wraps an int64 as ScVal (i128 low bits, hi=sign extended).
func ScvI128(n int64) xdr.ScVal {
	hi := xdr.Int64(0)
	if n < 0 {
		hi = -1
	}
	lo := xdr.Uint64(uint64(n))
	return xdr.ScVal{
		Type: xdr.ScValTypeScvI128,
		I128: &xdr.Int128Parts{Hi: hi, Lo: lo},
	}
}

// ScvSymbol wraps a string as ScVal symbol.
func ScvSymbol(s string) xdr.ScVal {
	sym := xdr.ScSymbol(s)
	return xdr.ScVal{Type: xdr.ScValTypeScvSymbol, Sym: &sym}
}

// ScvString wraps a string as ScVal.
func ScvString(s string) xdr.ScVal {
	str := xdr.ScString(s)
	return xdr.ScVal{Type: xdr.ScValTypeScvString, Str: &str}
}

// ScvVec wraps a slice of ScVals as a Soroban vector (e.g. Vec<Address> for a
// swap path). xdr.ScVal.Vec is a pointer-to-pointer, so the double indirection
// mirrors the hand-built vectors in blend/auction.go.
func ScvVec(vals ...xdr.ScVal) xdr.ScVal {
	vec := xdr.ScVec(vals)
	vecPtr := &vec
	return xdr.ScVal{Type: xdr.ScValTypeScvVec, Vec: &vecPtr}
}

// ScvVoid encodes the unit value, used for a Soroban Option::None argument.
func ScvVoid() xdr.ScVal {
	return xdr.ScVal{Type: xdr.ScValTypeScvVoid}
}

// ParseAddress extracts a string address from an xdr.ScAddress.
func ParseAddress(addr xdr.ScAddress) (string, error) {
	switch addr.Type {
	case xdr.ScAddressTypeScAddressTypeAccount:
		return addr.AccountId.Address(), nil
	case xdr.ScAddressTypeScAddressTypeContract:
		return strkey.Encode(strkey.VersionByteContract, addr.ContractId[:])
	}
	return "", fmt.Errorf("unknown address type %v", addr.Type)
}

func contractScAddress(contractID string) (xdr.ScAddress, error) {
	b, err := strkey.Decode(strkey.VersionByteContract, contractID)
	if err != nil {
		return xdr.ScAddress{}, fmt.Errorf("invalid contract %q: %w", contractID, err)
	}
	var hash xdr.Hash
	copy(hash[:], b)
	cid := xdr.ContractId(hash)
	return xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeContract, ContractId: &cid}, nil
}

func accountScAddress(addr string) (xdr.ScAddress, error) {
	var aid xdr.AccountId
	if err := aid.SetAddress(addr); err != nil {
		return xdr.ScAddress{}, fmt.Errorf("invalid account %q: %w", addr, err)
	}
	return xdr.ScAddress{Type: xdr.ScAddressTypeScAddressTypeAccount, AccountId: &aid}, nil
}
