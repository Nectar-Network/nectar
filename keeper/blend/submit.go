package blend

import (
	"fmt"
	"log/slog"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// Blend v2 RequestType integers used by the keeper. Full enum in
// docs/FACTS.md (blend-contracts-v2 pool/src/pool/actions.rs:21-34 @ ba22b48).
const (
	ReqWithdrawCollateral  uint32 = 3
	ReqRepay               uint32 = 5
	ReqFillUserLiquidation uint32 = 6
)

// MaxWithdraw asks the pool for more collateral than can exist;
// apply_withdraw_collateral burns bTokens ceil-clamped to the actual balance
// (actions.rs:363-384), so this withdraws the full position.
const MaxWithdraw int64 = 1 << 62

// PoolRequest is one entry of Blend's submit() request vector:
// `Request { request_type: u32, address: Address, amount: i128 }`.
// For fill types (6/7/8) Amount is a whole fill PERCENTAGE (1..=100), for
// asset actions it is a token amount.
type PoolRequest struct {
	Type    uint32
	Address string
	Amount  int64
}

// requestsVal encodes []PoolRequest as Vec<Request>. Map keys must be in
// sorted lexicographic order for Soroban Map<Symbol, Val>.
func requestsVal(reqs []PoolRequest) (xdr.ScVal, error) {
	entries := make([]xdr.ScVal, 0, len(reqs))
	for _, r := range reqs {
		addrVal, err := soroban.ScvAddress(r.Address)
		if err != nil {
			return xdr.ScVal{}, fmt.Errorf("request address %s: %w", r.Address, err)
		}
		m := xdr.ScMap{
			{Key: soroban.ScvSymbol("address"), Val: addrVal},
			{Key: soroban.ScvSymbol("amount"), Val: soroban.ScvI128(r.Amount)},
			{Key: soroban.ScvSymbol("request_type"), Val: soroban.ScvU32(r.Type)},
		}
		mp := &m
		entries = append(entries, xdr.ScVal{Type: xdr.ScValTypeScvMap, Map: &mp})
	}
	vec := xdr.ScVec(entries)
	vecPtr := &vec
	return xdr.ScVal{Type: xdr.ScValTypeScvVec, Vec: &vecPtr}, nil
}

// SubmitRequests invokes pool.submit(from, spender, to, requests) with the
// keeper as all three addresses, returning the landed transaction hash.
func SubmitRequests(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr string, reqs []PoolRequest) (string, error) {
	if len(reqs) == 0 {
		return "", fmt.Errorf("no requests")
	}
	fromVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return "", err
	}
	reqVal, err := requestsVal(reqs)
	if err != nil {
		return "", err
	}
	tx, err := rpc.InvokeWithRetry(horizonURL, kp, passphrase, poolAddr, "submit",
		soroban.DefaultRetry(), fromVal, fromVal, fromVal, reqVal)
	if err != nil {
		if isAlreadyFilled(err.Error()) {
			return "", ErrAlreadyFilled
		}
		return "", fmt.Errorf("submit: %w", err)
	}
	slog.Info("pool submit landed", "requests", len(reqs), "tx", tx.Hash)
	return tx.Hash, nil
}

// RepayLeg is one Repay request inside a fill-and-unwind submit. Amount is in
// raw underlying tokens of Asset and must cover the full assumed dToken debt
// of that reserve — the pool refunds any overpayment (actions.rs:415-441
// @ ba22b48), so callers pass their whole earmarked holding of the asset.
type RepayLeg struct {
	Asset  string
	Amount int64
}

// FillAndUnwind fills a user-liquidation auction and immediately unwinds the
// resulting position in ONE atomic submit:
//
//	[6] FillUserLiquidationAuction — assume the liquidatee's dToken debt and
//	    receive their bToken collateral (no tokens move: docs/FACTS.md
//	    "Auction asset flows")
//	[5] Repay per debt asset — burns the assumed dTokens; overpayment is
//	    refunded (actions.rs:415-441). One leg per bid asset; the keeper must
//	    already hold each leg's tokens (drawn USDC for the settle asset,
//	    DEX-acquired for any other debt asset).
//	[3] WithdrawCollateral (clamped) per seized asset — turns the bToken
//	    positions into real tokens the keeper holds
//
// Atomicity is the point: submit applies the requests in order against the
// same position and runs ONE health check at the end (submit.rs:159-196), so
// either the keeper ends holding tokens and no debt, or nothing happened at
// all. A separate fill-then-unwind sequence could strand an assumed debt if
// the second transaction failed.
func FillAndUnwind(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, user string,
	fillPct int64, repays []RepayLeg, collateralAssets []string) (string, error) {
	if fillPct < 1 || fillPct > 100 {
		return "", fmt.Errorf("fill percent %d out of range [1,100]", fillPct)
	}
	// Filling without repaying every assumed debt leg (or without freeing the
	// seized collateral) would defeat the atomic unwind — reject up front.
	if len(repays) == 0 {
		return "", fmt.Errorf("at least one repay leg required")
	}
	if len(collateralAssets) == 0 {
		return "", fmt.Errorf("at least one collateral withdrawal required")
	}
	reqs := []PoolRequest{
		{Type: ReqFillUserLiquidation, Address: user, Amount: fillPct},
	}
	for _, leg := range repays {
		if leg.Asset == "" || leg.Amount <= 0 {
			return "", fmt.Errorf("repay leg needs an asset and a positive amount, got %q/%d", leg.Asset, leg.Amount)
		}
		reqs = append(reqs, PoolRequest{Type: ReqRepay, Address: leg.Asset, Amount: leg.Amount})
	}
	for _, asset := range collateralAssets {
		reqs = append(reqs, PoolRequest{Type: ReqWithdrawCollateral, Address: asset, Amount: MaxWithdraw})
	}
	return SubmitRequests(rpc, horizonURL, kp, passphrase, poolAddr, reqs)
}
