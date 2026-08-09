package blend

import (
	"fmt"
	"log/slog"
	"math"
	"math/big"

	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/soroban"
)

// CreateBadDebtAuction creates the pool's bad-debt auction via the
// permissionless `new_auction(1, backstop, bid, lot, 100)` entry point.
// Contract requirements (bad_debt_auction.rs:14-96 @ ba22b48, docs/FACTS.md):
//   - user MUST be the backstop and percent MUST be exactly 100
//   - every bid asset MUST carry a positive backstop liability (a subset of
//     the backstop's debt assets is allowed — the lot is sized to the listed
//     legs only, at debt_value × 1.2 / token_spot_price)
//   - lot MUST be exactly [backstop_token]
//
// Idempotent on AuctionInProgress: an existing auction is not an error.
func CreateBadDebtAuction(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, backstop, backstopToken string, bidAssets []string) error {
	if len(bidAssets) == 0 {
		return fmt.Errorf("bad-debt auction needs at least one bid asset")
	}
	backstopVal, err := soroban.ScvAddress(backstop)
	if err != nil {
		return err
	}
	bidVec, err := addressVecVal(bidAssets)
	if err != nil {
		return fmt.Errorf("bid vec: %w", err)
	}
	lotVec, err := addressVecVal([]string{backstopToken})
	if err != nil {
		return fmt.Errorf("lot vec: %w", err)
	}
	tx, err := rpc.InvokeWithRetry(horizonURL, kp, passphrase, poolAddr, "new_auction",
		soroban.DefaultRetry(),
		soroban.ScvU32(uint32(AuctionBadDebt)), backstopVal, bidVec, lotVec, soroban.ScvU32(100))
	if err != nil {
		if isAuctionExists(err.Error()) {
			return nil
		}
		return fmt.Errorf("new_auction(bad_debt): %w", err)
	}
	slog.Info("bad-debt auction created", "backstop", backstop, "bid_assets", len(bidAssets), "tx", tx.Hash)
	return nil
}

// FillBadDebtAndRepay fills the backstop's bad-debt auction and repays the
// assumed debt in ONE atomic submit:
//
//	[7] FillBadDebtAuction — the scaled dToken debt moves from the backstop's
//	    positions onto the keeper's, and the scaled backstop-LP lot is drawn
//	    to the keeper (backstop.draw) in the same call. No repayment happens
//	    here (docs/FACTS.md "Auction asset flows").
//	[5] Repay per debt asset — burns the assumed dTokens. The keeper must
//	    HOLD each leg's full amount (repay settles GROSS and refunds the
//	    excess in the same tx — actions.rs:415-441).
//
// Atomicity matters the same way it does for user liquidations: a type-7
// fill IS health-checked (actions.rs:227-247), so a fill whose repay leg
// comes up short reverts entirely instead of stranding assumed debt on a
// collateral-less keeper.
func FillBadDebtAndRepay(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, backstop string,
	fillPct int64, repays []RepayLeg) (string, error) {
	if fillPct < 1 || fillPct > 100 {
		return "", fmt.Errorf("fill percent %d out of range [1,100]", fillPct)
	}
	if len(repays) == 0 {
		return "", fmt.Errorf("at least one repay leg required (use FillBadDebtFree only past the price curve)")
	}
	reqs := []PoolRequest{
		{Type: ReqFillBadDebt, Address: backstop, Amount: fillPct},
	}
	for _, leg := range repays {
		if leg.Asset == "" || leg.Amount <= 0 {
			return "", fmt.Errorf("repay leg needs an asset and a positive amount, got %q/%d", leg.Asset, leg.Amount)
		}
		reqs = append(reqs, PoolRequest{Type: ReqRepay, Address: leg.Asset, Amount: leg.Amount})
	}
	return SubmitRequests(rpc, horizonURL, kp, passphrase, poolAddr, reqs)
}

// FillBadDebtFree fills a bad-debt auction whose scaled bid is EMPTY — past
// t=400 the bid modifier is 0 (auction.rs:217-221), so a 100% fill assumes no
// debt and draws the full LP lot for nothing but the tx fee. Callers must
// only use this past the curve; earlier, the bare fill would assume real
// dToken debt with no repay leg and the submit's health check would revert it
// (the contract, not this guard, is the backstop against misuse).
func FillBadDebtFree(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, backstop string) (string, error) {
	return SubmitRequests(rpc, horizonURL, kp, passphrase, poolAddr, []PoolRequest{
		{Type: ReqFillBadDebt, Address: backstop, Amount: FullFillPct},
	})
}

// BackstopLPValueUSD values a raw 7-dec backstop-LP amount at the backstop's
// own token_spot_price, discounted by haircutBps. The haircut is the honesty
// margin on an asset the keeper cannot yet liquidate: spot assumes the comet
// pool's implied BLND price holds for the whole position, and the unwind
// (single-sided comet exit, then only on mainnet where the comet's USDC leg
// IS the vault asset) pays swap fees and slippage. haircutBps=5000 values LP
// at half of spot; 0 takes spot at face value; >=10000 values it at zero.
func BackstopLPValueUSD(lpAmount *big.Int, spotPrice float64, haircutBps int) float64 {
	if lpAmount == nil || lpAmount.Sign() <= 0 || spotPrice <= 0 {
		return 0
	}
	if haircutBps >= 10000 {
		return 0
	}
	if haircutBps < 0 {
		haircutBps = 0
	}
	f, _ := new(big.Float).SetInt(lpAmount).Float64()
	return (f / scalar) * spotPrice * float64(10000-haircutBps) / 10000
}

// BadDebtProfitability computes (haircut LP lot value) / (bid debt cost) for
// a bad-debt auction at currentBlock, both legs scaled by the verified
// two-phase Dutch curve. The generic Profitability cannot price this auction
// kind: its lot is the backstop LP token, which is not a pool reserve, so it
// deliberately reports 0 there. Here the lot is valued at the backstop's own
// token_spot_price minus haircutBps, and the bid cost is the assumed dToken
// debt in underlying at oracle prices. Any unpriced bid asset makes the COST
// unknowable and returns 0 — never a green light.
func BadDebtProfitability(a Auction, pool *PoolState, backstopToken string, spotPrice float64, haircutBps int, currentBlock int64) float64 {
	elapsed := currentBlock - a.StartBlock
	_, lotPct, bidPct := PhaseAt(elapsed)

	lotVal := BackstopLPValueUSD(a.Lot[backstopToken], spotPrice, haircutBps) * lotPct

	var bidVal float64
	for asset, amt := range a.Bid {
		if amt == nil || amt.Sign() <= 0 {
			continue
		}
		r, ok := pool.Reserves[asset]
		if !ok || r.OraclePrice <= 0 || r.DRate <= 0 {
			return 0
		}
		f, _ := new(big.Float).SetInt(amt).Float64()
		bidVal += (f / r.TokenScalar()) * r.DRate * bidPct * r.OraclePrice
	}
	if bidVal == 0 {
		if lotVal > 0 {
			return math.Inf(1) // past-curve free capture: full lot, empty bid
		}
		return 0
	}
	return lotVal / bidVal
}
