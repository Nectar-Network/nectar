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

// CreateAuction calls new_liquidation_auction on the Blend pool. This only
// applies to user-liquidation auctions; interest and bad-debt auctions are
// triggered by the pool's internal accounting (no creation entry-point).
func CreateAuction(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, user string, pct int) error {
	userVal, err := soroban.ScvAddress(user)
	if err != nil {
		return err
	}
	pctVal := soroban.ScvU64(uint64(pct) * 1e7)

	_, err = rpc.InvokeWithRetry(horizonURL, kp, passphrase, poolAddr, "new_liquidation_auction",
		soroban.DefaultRetry(), userVal, pctVal)
	if err != nil {
		if isAuctionExists(err.Error()) {
			return nil
		}
		return fmt.Errorf("new_liquidation_auction: %w", err)
	}
	return nil
}

// GetAuctionByType fetches an auction of the given kind for the user/address.
// Returns (nil, nil) when no such auction exists (clean miss).
func GetAuctionByType(rpc *soroban.Client, passphrase, poolAddr, user string, kind AuctionType) (*Auction, error) {
	userVal, err := soroban.ScvAddress(user)
	if err != nil {
		return nil, err
	}
	auctType := soroban.ScvU64(uint64(kind))

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

// fillAuctionRequest builds the Soroban submit() arguments and invokes the
// pool. addr/from/spender are all the keeper. The request map is the same
// shape across all three fill paths — only the request_type constant changes.
func fillAuctionRequest(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, user string, kind AuctionType) error {
	fromVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return err
	}
	userVal, err := soroban.ScvAddress(user)
	if err != nil {
		return err
	}

	// Blend's Request struct: request_type:u32, address:Address, amount:i128.
	// Wrong scalar types make the pool reject the submit even when the keys
	// look right — see contracts/blend Request derive.
	reqTypeVal := soroban.ScvU32(kind.requestType())
	zeroAmt := soroban.ScvI128(0)

	// Keys MUST be in sorted lexicographic order for Soroban Map<Symbol, Val>.
	reqMap := xdr.ScMap{
		{Key: soroban.ScvSymbol("address"), Val: userVal},
		{Key: soroban.ScvSymbol("amount"), Val: zeroAmt},
		{Key: soroban.ScvSymbol("request_type"), Val: reqTypeVal},
	}
	reqMapPtr := &reqMap
	reqVec := xdr.ScVec{{Type: xdr.ScValTypeScvMap, Map: &reqMapPtr}}
	reqVecPtr := &reqVec
	requestsVal := xdr.ScVal{Type: xdr.ScValTypeScvVec, Vec: &reqVecPtr}

	_, err = rpc.InvokeWithRetry(horizonURL, kp, passphrase, poolAddr, "submit",
		soroban.DefaultRetry(),
		fromVal, fromVal, fromVal, requestsVal)
	if err != nil {
		if isAlreadyFilled(err.Error()) {
			return ErrAlreadyFilled
		}
		return fmt.Errorf("fill %s auction: %w", kind, err)
	}
	return nil
}

// FillAuction fills a user-liquidation auction (request_type 6).
// Backward-compat alias for FillUserLiquidationAuction.
func FillAuction(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, user string) error {
	return fillAuctionRequest(rpc, horizonURL, kp, passphrase, poolAddr, user, AuctionUserLiquidation)
}

// FillUserLiquidationAuction fills a user-liquidation auction (request_type 6).
func FillUserLiquidationAuction(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, user string) error {
	return fillAuctionRequest(rpc, horizonURL, kp, passphrase, poolAddr, user, AuctionUserLiquidation)
}

// FillBadDebtAuction fills a bad-debt auction (request_type 7). The bidder
// takes on socialized bad debt in exchange for the lot of bToken collateral.
func FillBadDebtAuction(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, addr string) error {
	return fillAuctionRequest(rpc, horizonURL, kp, passphrase, poolAddr, addr, AuctionBadDebt)
}

// FillInterestAuction fills an interest auction (request_type 8). The bidder
// pays BLND in exchange for accumulated backstop interest.
func FillInterestAuction(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, addr string) error {
	return fillAuctionRequest(rpc, horizonURL, kp, passphrase, poolAddr, addr, AuctionInterest)
}

// FillByType dispatches to the right Fill* function based on the auction kind.
func FillByType(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, poolAddr, addr string, kind AuctionType) error {
	return fillAuctionRequest(rpc, horizonURL, kp, passphrase, poolAddr, addr, kind)
}

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
		lotVal += (f / scalar) * lotPct * r.OraclePrice
	}
	for asset, amt := range auction.Bid {
		r, ok := pool.Reserves[asset]
		if !ok {
			continue
		}
		f, _ := new(big.Float).SetInt(amt).Float64()
		bidVal += (f / scalar) * bidPct * r.OraclePrice
	}
	if bidVal == 0 {
		return math.Inf(1)
	}
	return lotVal / bidVal
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
		v += (f / scalar) * bidPct * r.OraclePrice
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

func isNotFound(s string) bool {
	return contains(s, "AuctionNotFound") || contains(s, "NotFound") || contains(s, "#4")
}
func isAlreadyFilled(s string) bool {
	return contains(s, "AuctionNotFound") || contains(s, "AlreadyFilled") || contains(s, "#4")
}
func isAuctionExists(s string) bool {
	return contains(s, "AuctionExists") || contains(s, "#5")
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
