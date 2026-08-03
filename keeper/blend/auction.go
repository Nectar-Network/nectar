package blend

import (
	"errors"
	"fmt"
	"math"
	"math/big"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// AuctionType matches Blend v2's on-chain enum for auction storage.
// The mapping to RequestType (used by submit() to fill) is:
//
//	UserLiquidation (0) → RequestType 6 (FillUserLiquidationAuction)
//	BadDebtAuction  (1) → RequestType 7 (FillBadDebtAuction)
//	InterestAuction (2) → RequestType 8 (FillInterestAuction)
type AuctionType uint64

const (
	AuctionUserLiquidation AuctionType = 0
	AuctionBadDebt         AuctionType = 1
	AuctionInterest        AuctionType = 2
)

// AllAuctionTypes is the canonical scan order for DetectAuctions.
var AllAuctionTypes = []AuctionType{
	AuctionUserLiquidation,
	AuctionBadDebt,
	AuctionInterest,
}

// requestTypeFor returns the Blend submit() request_type that fills the given
// auction kind. Blend's Request.request_type is u32 on-chain.
func (t AuctionType) requestType() uint32 {
	switch t {
	case AuctionUserLiquidation:
		return 6
	case AuctionBadDebt:
		return 7
	case AuctionInterest:
		return 8
	default:
		return 6
	}
}

// String pretty-prints the auction kind for log lines.
func (t AuctionType) String() string {
	switch t {
	case AuctionUserLiquidation:
		return "user_liquidation"
	case AuctionBadDebt:
		return "bad_debt"
	case AuctionInterest:
		return "interest"
	default:
		return fmt.Sprintf("unknown(%d)", uint64(t))
	}
}

type Auction struct {
	User       string
	Type       AuctionType
	StartBlock int64
	Lot        map[string]*big.Int
	Bid        map[string]*big.Int
}

var ErrAlreadyFilled = errors.New("auction already filled by another keeper")

// CreateAuction creates a user-liquidation auction via the blend-contracts-v2
// entry point `new_auction(auction_type: u32, user: Address, bid: Vec<Address>,
// lot: Vec<Address>, percent: u32)` (pool/src/contract.rs:277-283 @ ba22b48 —
// v2 has no `new_liquidation_auction`). bid lists the user's liability
// underlyings to auction, lot the collateral underlyings; percent is a whole
// percentage 1..=100 (NOT 7-dec scaled — user_liquidation_auction.rs:92-105).
func CreateAuction(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, user string, pct int, bid, lot []string) error {
	userVal, err := soroban.ScvAddress(user)
	if err != nil {
		return err
	}
	bidVec, err := addressVecVal(bid)
	if err != nil {
		return fmt.Errorf("bid vec: %w", err)
	}
	lotVec, err := addressVecVal(lot)
	if err != nil {
		return fmt.Errorf("lot vec: %w", err)
	}

	_, err = rpc.InvokeWithRetry(horizonURL, kp, passphrase, poolAddr, "new_auction",
		soroban.DefaultRetry(),
		soroban.ScvU32(uint32(AuctionUserLiquidation)), userVal, bidVec, lotVec, soroban.ScvU32(uint32(pct)))
	if err != nil {
		if isAuctionExists(err.Error()) {
			return nil
		}
		return fmt.Errorf("new_auction: %w", err)
	}
	return nil
}

// Blend pool error codes relevant to sizing a liquidation
// (pool/src/errors.rs @ ba22b48).
const (
	errInvalidLiqTooLarge = 1213
	errInvalidLiqTooSmall = 1214
)

// ErrNoValidLiqPercent means no liquidation percentage satisfies the pool's
// post-liquidation health-factor band for this position.
var ErrNoValidLiqPercent = errors.New("no valid liquidation percent")

// FindLiquidationPercent picks the liquidation percentage the pool will
// accept. Blend requires the post-liquidation health factor to land in
// [1.03, 1.15] — too small panics InvalidLiqTooSmall (1214), too large
// InvalidLiqTooLarge (1213) — with a special case where >95% on a fully
// included position is treated as a full liquidation
// (user_liquidation_auction.rs:92-205 @ ba22b48).
//
// Rather than duplicating the contract's fixed-point incentive math (which
// would silently drift from upstream), this binary-searches the percentage
// using read-only `new_auction` SIMULATIONS: the pool itself judges each
// candidate, and the error code says which way to move. Nothing is submitted.
func FindLiquidationPercent(rpc *soroban.Client, passphrase, poolAddr, user string, bid, lot []string) (int, error) {
	userVal, err := soroban.ScvAddress(user)
	if err != nil {
		return 0, err
	}
	bidVec, err := addressVecVal(bid)
	if err != nil {
		return 0, err
	}
	lotVec, err := addressVecVal(lot)
	if err != nil {
		return 0, err
	}

	simulate := func(pct int) (ok bool, code uint32, err error) {
		sim, err := rpc.SimulateRead(passphrase, poolAddr, "new_auction",
			soroban.ScvU32(uint32(AuctionUserLiquidation)), userVal, bidVec, lotVec, soroban.ScvU32(uint32(pct)))
		if err != nil {
			return false, 0, err
		}
		if sim.Error == "" {
			return true, 0, nil
		}
		if c, found := soroban.ParseContractCode(sim.Error); found {
			return false, c, nil
		}
		return false, 0, fmt.Errorf("new_auction sim: %s", sim.Error)
	}

	// A full liquidation is valid whenever the position is deeply enough
	// underwater, and it is the most profitable outcome — try it first.
	if ok, _, err := simulate(100); err != nil {
		return 0, err
	} else if ok {
		return 100, nil
	}

	lo, hi := 1, 99
	best := 0
	for lo <= hi {
		mid := (lo + hi) / 2
		ok, code, err := simulate(mid)
		if err != nil {
			return 0, err
		}
		switch {
		case ok:
			best = mid
			hi = mid - 1 // prefer the smallest sufficient liquidation
		case code == errInvalidLiqTooSmall:
			lo = mid + 1
		case code == errInvalidLiqTooLarge:
			hi = mid - 1
		default:
			return 0, fmt.Errorf("new_auction rejected at %d%%: contract error %d", mid, code)
		}
	}
	if best == 0 {
		return 0, ErrNoValidLiqPercent
	}
	return best, nil
}

// addressVecVal builds a Vec<Address> ScVal from contract/account addresses.
func addressVecVal(addrs []string) (xdr.ScVal, error) {
	vals := make([]xdr.ScVal, 0, len(addrs))
	for _, a := range addrs {
		v, err := soroban.ScvAddress(a)
		if err != nil {
			return xdr.ScVal{}, err
		}
		vals = append(vals, v)
	}
	return soroban.ScvVec(vals...), nil
}

// GetAuctionByType fetches an auction of the given kind for the user/address.
// Returns (nil, nil) when no such auction exists (clean miss).
func GetAuctionByType(rpc *soroban.Client, passphrase, poolAddr, user string, kind AuctionType) (*Auction, error) {
	userVal, err := soroban.ScvAddress(user)
	if err != nil {
		return nil, err
	}
	// get_auction(auction_type: u32, user: Address) — u32, not u64
	// (pool/src/contract.rs:295 @ ba22b48)
	auctType := soroban.ScvU32(uint32(kind))

	sim, err := rpc.SimulateRead(passphrase, poolAddr, "get_auction", auctType, userVal)
	if err != nil {
		return nil, err
	}
	if sim.Error != "" {
		if isNotFound(sim.Error) {
			return nil, nil
		}
		return nil, fmt.Errorf("get_auction: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return nil, nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil, err
	}
	a := parseAuction(val, user)
	a.Type = kind
	return a, nil
}

// GetAuction is the legacy single-type lookup, kept for backward compat. It
// only checks for user-liquidation auctions; new code should prefer
// GetAuctionByType or DetectAuctions.
func GetAuction(rpc *soroban.Client, passphrase, poolAddr, user string) (*Auction, error) {
	return GetAuctionByType(rpc, passphrase, poolAddr, user, AuctionUserLiquidation)
}

// DetectAuctions scans all three auction kinds for the given address and
// returns whichever (if any) currently exist. RPC errors on any single kind
// are wrapped and returned, but a clean "not found" on one kind doesn't stop
// the scan.
func DetectAuctions(rpc *soroban.Client, passphrase, poolAddr, user string) ([]*Auction, error) {
	out := make([]*Auction, 0, 3)
	for _, kind := range AllAuctionTypes {
		a, err := GetAuctionByType(rpc, passphrase, poolAddr, user, kind)
		if err != nil {
			return out, fmt.Errorf("detect %s: %w", kind, err)
		}
		if a != nil {
			out = append(out, a)
		}
	}
	return out, nil
}

// FullFillPct fills 100% of an auction's scaled amounts.
const FullFillPct int64 = 100

// NOTE: the old standalone fill entry points (FillAuction / FillByType /
// FillBadDebtAuction / FillInterestAuction) were DELETED. A bare fill of a
// user-liquidation or bad-debt auction moves no tokens — the filler assumes
// dToken debt (docs/FACTS.md "Auction asset flows") — so "fill, then swap the
// collateral" was operating on collateral the keeper never held, and a bare
// interest fill would revert without the backstop-LP pre-approval it never
// performed. The only supported fill path is the atomic FillAndUnwind in
// submit.go. Auction DETECTION for all three kinds (DetectAuctions,
// GetAuctionByType) remains.

// AuctionPhase describes which scaling phase a Blend Dutch auction is in.
type AuctionPhase int

const (
	PhaseLotScaling AuctionPhase = iota // 0–200: lot grows 0→100 %, bid stays 100 %
	PhaseBidScaling                     // 200–400: lot stays 100 %, bid shrinks 100→0 %
	PhaseExpired                        // > 400: lot 100 %, bid 0 %
)

// PhaseAt reports the auction phase and the scaled lot/bid percentages.
// Block boundaries are inclusive on the lower side (elapsed=200 → phase 2 at
// the boundary, with lotPct=1.0 and bidPct=1.0).
func PhaseAt(elapsed int64) (AuctionPhase, float64, float64) {
	if elapsed < 0 {
		elapsed = 0
	}
	switch {
	case elapsed <= 200:
		return PhaseLotScaling, float64(elapsed) / 200.0, 1.0
	case elapsed <= 400:
		return PhaseBidScaling, 1.0, float64(400-elapsed) / 200.0
	default:
		return PhaseExpired, 1.0, 0.0
	}
}

// Profitability computes lot_value/bid_cost for a Blend Dutch auction at
// currentBlock. The auction follows Blend v2's two-phase Dutch model: the
// "fair price" point is at elapsed=200 where both legs are at 100 %. Profit
// math is identical across the three auction kinds — the only difference is
// what the lot/bid maps contain (collateral vs. backstop interest vs. bad
// debt), which the caller has already populated by the time this runs.
func Profitability(auction Auction, pool *PoolState, currentBlock int64) float64 {
	elapsed := currentBlock - auction.StartBlock
	_, lotPct, bidPct := PhaseAt(elapsed)

	var lotVal, bidVal float64
	for asset, amt := range auction.Lot {
		r, ok := pool.Reserves[asset]
		if !ok {
			continue
		}
		f, _ := new(big.Float).SetInt(amt).Float64()
		lotVal += (f / r.TokenScalar()) * legRate(auction.Type, true, r) * lotPct * r.OraclePrice
	}
	for asset, amt := range auction.Bid {
		r, ok := pool.Reserves[asset]
		if !ok || r.OraclePrice <= 0 {
			// An unpriced bid asset means the COST is unknown, not zero.
			// Returning +Inf here would read as infinite profit and green-light
			// a fill whose true cost we cannot see.
			return 0
		}
		f, _ := new(big.Float).SetInt(amt).Float64()
		bidVal += (f / r.TokenScalar()) * legRate(auction.Type, false, r) * bidPct * r.OraclePrice
	}
	// A genuinely zero-cost bid (t >= 400: bid scaled to 0) is free money, but
	// only when the lot is actually worth something we could price.
	if bidVal == 0 {
		if lotVal > 0 {
			return math.Inf(1)
		}
		return 0
	}
	return lotVal / bidVal
}

// legRate converts a raw auction map amount to underlying tokens. Per
// docs/FACTS.md ("Auction asset flows", verified @ ba22b48): user-liquidation
// lots are bTokens and bids are dTokens; bad-debt bids are dTokens (lot is
// backstop LP, not a reserve — callers skip unpriced assets); interest lots
// are underlying already (bid is backstop LP, likewise skipped).
func legRate(kind AuctionType, isLot bool, r *Reserve) float64 {
	switch kind {
	case AuctionUserLiquidation:
		if isLot {
			return r.BRate
		}
		return r.DRate
	case AuctionBadDebt:
		if !isLot {
			return r.DRate
		}
	case AuctionInterest:
		// lot is underlying tokens; no conversion
	}
	return 1.0
}

// BidValueUSD totals the bid leg's USD value at the current scaling.
func BidValueUSD(auction Auction, pool *PoolState, currentBlock int64) float64 {
	elapsed := currentBlock - auction.StartBlock
	_, _, bidPct := PhaseAt(elapsed)
	var v float64
	for asset, amt := range auction.Bid {
		r, ok := pool.Reserves[asset]
		if !ok {
			continue
		}
		f, _ := new(big.Float).SetInt(amt).Float64()
		v += (f / r.TokenScalar()) * legRate(auction.Type, false, r) * bidPct * r.OraclePrice
	}
	return v
}

func parseAuction(val xdr.ScVal, user string) *Auction {
	a := &Auction{User: user, Lot: make(map[string]*big.Int), Bid: make(map[string]*big.Int)}
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return a
	}
	for _, entry := range **val.Map {
		switch scSymbol(entry.Key) {
		case "block":
			if entry.Val.Type == xdr.ScValTypeScvU32 && entry.Val.U32 != nil {
				a.StartBlock = int64(*entry.Val.U32)
			}
		case "lot":
			a.Lot = parseAssetMap(entry.Val)
		case "bid":
			a.Bid = parseAssetMap(entry.Val)
		}
	}
	return a
}

func parseAssetMap(val xdr.ScVal) map[string]*big.Int {
	m := make(map[string]*big.Int)
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return m
	}
	for _, e := range **val.Map {
		if e.Key.Type != xdr.ScValTypeScvAddress || e.Key.Address == nil {
			continue
		}
		asset, err := soroban.ParseAddress(*e.Key.Address)
		if err != nil {
			continue
		}
		if amt := scI128(e.Val); amt != nil {
			m[asset] = amt
		}
	}
	return m
}

// Blend pool contract error codes. Matched on the parsed Error(Contract, #N)
// integer code rather than a loose substring, so a "#42" id or a base64 result
// blob can't accidentally match and an adversarial RPC message is far harder to
// spoof. The variant-name checks remain only as a fallback for RPCs that surface
// the name when no numeric code is present. (Codes inferred from Blend v2's
// PoolError ordering; the structural matching itself is the fix.)
const (
	blendErrAuctionNotFound uint32 = 4 // auction absent: never created, or already filled/consumed
	blendErrAuctionExists   uint32 = 5 // creating an auction that already exists
)

func isNotFound(s string) bool {
	if code, ok := soroban.ParseContractCode(s); ok {
		return code == blendErrAuctionNotFound
	}
	return contains(s, "AuctionNotFound") || contains(s, "NotFound")
}

// isAlreadyFilled reports that the auction is no longer fillable. At fill time a
// missing auction (code #4) means another keeper consumed it first — the safe
// "lost the race" outcome, where drawn capital is returned unspent. It shares
// code #4 with isNotFound by design; the two differ only in call-site intent.
func isAlreadyFilled(s string) bool {
	if code, ok := soroban.ParseContractCode(s); ok {
		return code == blendErrAuctionNotFound
	}
	return contains(s, "AuctionNotFound") || contains(s, "AlreadyFilled")
}
func isAuctionExists(s string) bool {
	if code, ok := soroban.ParseContractCode(s); ok {
		return code == blendErrAuctionExists
	}
	return contains(s, "AuctionExists")
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
