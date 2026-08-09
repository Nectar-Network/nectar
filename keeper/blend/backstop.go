package blend

import (
	"fmt"
	"math/big"

	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// The pool exposes NO public backstop getter (docs/FACTS.md "Pool public read
// interface") — the address lives in instance storage under Symbol("Backstop")
// (blend-contracts-v2 pool/src/storage.rs:104,268-273 @ ba22b48). GetBackstop
// reads it straight from the ledger entry so the keeper stays chain-derived:
// no per-pool backstop env var to drift out of sync with the deployment.
func GetBackstop(rpc *soroban.Client, poolAddr string) (string, error) {
	addrVal, err := soroban.ScvAddress(poolAddr)
	if err != nil {
		return "", err
	}
	key := xdr.LedgerKey{
		Type: xdr.LedgerEntryTypeContractData,
		ContractData: &xdr.LedgerKeyContractData{
			Contract:   *addrVal.Address,
			Key:        xdr.ScVal{Type: xdr.ScValTypeScvLedgerKeyContractInstance},
			Durability: xdr.ContractDataDurabilityPersistent,
		},
	}
	keyB64, err := xdr.MarshalBase64(key)
	if err != nil {
		return "", fmt.Errorf("marshal instance key: %w", err)
	}
	entries, err := rpc.GetLedgerEntries([]string{keyB64})
	if err != nil {
		return "", fmt.Errorf("get instance entry: %w", err)
	}
	if len(entries) == 0 {
		return "", fmt.Errorf("pool %s has no instance entry", poolAddr)
	}
	var data xdr.LedgerEntryData
	if err := xdr.SafeUnmarshalBase64(entries[0].XDR, &data); err != nil {
		return "", fmt.Errorf("decode instance entry: %w", err)
	}
	backstop, err := backstopFromInstance(data)
	if err != nil {
		return "", fmt.Errorf("pool %s: %w", poolAddr, err)
	}
	return backstop, nil
}

// backstopFromInstance extracts the Symbol("Backstop") address from a decoded
// contract-instance ledger entry. Split out for testability.
func backstopFromInstance(data xdr.LedgerEntryData) (string, error) {
	if data.Type != xdr.LedgerEntryTypeContractData || data.ContractData == nil {
		return "", fmt.Errorf("instance entry is not contract data")
	}
	val := data.ContractData.Val
	if val.Type != xdr.ScValTypeScvContractInstance || val.Instance == nil || val.Instance.Storage == nil {
		return "", fmt.Errorf("instance entry has no storage map")
	}
	for _, e := range *val.Instance.Storage {
		if scSymbol(e.Key) != "Backstop" {
			continue
		}
		if e.Val.Type != xdr.ScValTypeScvAddress || e.Val.Address == nil {
			return "", fmt.Errorf("Backstop storage entry is not an address")
		}
		return soroban.ParseAddress(*e.Val.Address)
	}
	return "", fmt.Errorf("no Backstop key in instance storage")
}

// GetBackstopToken reads backstop_token() from the backstop contract — the
// BLND:USDC comet LP token that bad-debt lots and interest bids are
// denominated in (backstop/src/contract.rs:76 @ ba22b48).
func GetBackstopToken(rpc *soroban.Client, passphrase, backstopAddr string) (string, error) {
	sim, err := rpc.SimulateRead(passphrase, backstopAddr, "backstop_token")
	if err != nil {
		return "", err
	}
	if sim.Error != "" {
		return "", fmt.Errorf("backstop_token sim: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return "", fmt.Errorf("backstop_token: empty result")
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return "", err
	}
	if val.Type != xdr.ScValTypeScvAddress || val.Address == nil {
		return "", fmt.Errorf("backstop_token: not an address")
	}
	return soroban.ParseAddress(*val.Address)
}

// BackstopPoolData is the slice of backstop.pool_data() the bad-debt path
// needs: how many LP tokens back this pool and what the backstop itself
// values one whole LP token at (7-dec USD — the SAME price the pool uses to
// size bad-debt lots, bad_debt_auction.rs:85-96 @ ba22b48).
type BackstopPoolData struct {
	Tokens    *big.Int // LP tokens deposited for this pool, 7-dec
	SpotPrice float64  // USD per whole LP token (token_spot_price / 1e7)
}

// GetBackstopPoolData reads pool_data(pool) from the backstop contract.
func GetBackstopPoolData(rpc *soroban.Client, passphrase, backstopAddr, poolAddr string) (*BackstopPoolData, error) {
	poolVal, err := soroban.ScvAddress(poolAddr)
	if err != nil {
		return nil, err
	}
	sim, err := rpc.SimulateRead(passphrase, backstopAddr, "pool_data", poolVal)
	if err != nil {
		return nil, err
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("pool_data sim: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return nil, fmt.Errorf("pool_data: empty result")
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil, err
	}
	return parseBackstopPoolData(val)
}

func parseBackstopPoolData(val xdr.ScVal) (*BackstopPoolData, error) {
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return nil, fmt.Errorf("pool_data: not a map")
	}
	out := &BackstopPoolData{}
	for _, e := range **val.Map {
		switch scSymbol(e.Key) {
		case "tokens":
			out.Tokens = scI128(e.Val)
		case "token_spot_price":
			if p := scI128(e.Val); p != nil {
				f, _ := new(big.Float).SetInt(p).Float64()
				out.SpotPrice = f / scalar
			}
		}
	}
	if out.Tokens == nil || out.SpotPrice <= 0 {
		return nil, fmt.Errorf("pool_data: missing tokens or non-positive token_spot_price")
	}
	return out, nil
}

// LoadPosition reads one address's pool positions directly (no event scan) —
// used for the backstop, which never appears in position-discovery events as
// a borrower but accumulates dToken liabilities when liquidations leave bad
// debt behind.
func LoadPosition(rpc *soroban.Client, passphrase, poolAddr, user string) (*Position, error) {
	return loadPosition(rpc, passphrase, poolAddr, user)
}
