package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/strkey"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/dex"
	"github.com/nectar-network/keeper/soroban"
)

// Faucet pays a fixed amount of the vault's USDC from a pre-funded treasury
// account to a requesting wallet. This is a PAYMENT, not a mint: the vault
// settles Circle testnet USDC (SAC over classic USDC:GBBD47IF…), which we
// cannot issue — the treasury is refilled manually from faucet.circle.com.
// Capacity is therefore treasuryBalance/amount claims; /api/faucet/info
// reports the live balance so the UI never promises more than exists.
type Faucet struct {
	rpc         *soroban.Client
	kp          *keypair.Full
	horizonURL  string
	passphrase  string
	usdc        string // SAC contract
	assetCode   string // classic code, from SAC name()
	assetIssuer string // classic issuer, from SAC name()
	amount      int64
	cooldown    time.Duration

	mu        sync.Mutex
	lastClaim map[string]time.Time // recipient -> last successful claim
}

// faucet is nil when FAUCET_SECRET is unset (endpoints report disabled).
var faucet *Faucet

// initFaucet builds the faucet if configured. The asset's classic code:issuer
// is derived from the SAC's on-chain name() ("USDC:G...") rather than trusted
// from config.
func initFaucet(rpc *soroban.Client, cfg Config) {
	if cfg.FaucetSecret == "" {
		return
	}
	if cfg.UsdcAddr == "" {
		logWarn("faucet disabled: USDC_CONTRACT not set")
		return
	}
	kp, err := keypair.ParseFull(cfg.FaucetSecret)
	if err != nil {
		logWarn("faucet disabled: bad FAUCET_SECRET", "err", err)
		return
	}
	code, issuer, err := sacClassicAsset(rpc, cfg.Passphrase, cfg.UsdcAddr)
	if err != nil {
		logWarn("faucet disabled: cannot resolve SAC classic asset", "err", err)
		return
	}
	faucet = &Faucet{
		rpc:         rpc,
		kp:          kp,
		horizonURL:  cfg.HorizonURL,
		passphrase:  cfg.Passphrase,
		usdc:        cfg.UsdcAddr,
		assetCode:   code,
		assetIssuer: issuer,
		amount:      cfg.FaucetAmount,
		cooldown:    time.Duration(cfg.FaucetCooldownSecs) * time.Second,
		lastClaim:   make(map[string]time.Time),
	}
	logInfo("faucet enabled", "treasury", short(kp.Address()), "asset", code+":"+short(issuer),
		"amount", cfg.FaucetAmount, "cooldown_s", cfg.FaucetCooldownSecs)
}

// sacClassicAsset reads name() from a Stellar Asset Contract and splits the
// canonical "CODE:ISSUER" form. A pure Soroban token (name without an issuer
// suffix) errors — the faucet only supports classic-asset SACs.
func sacClassicAsset(rpc *soroban.Client, passphrase, sac string) (code, issuer string, err error) {
	sim, err := rpc.SimulateRead(passphrase, sac, "name")
	if err != nil {
		return "", "", err
	}
	if sim.Error != "" || len(sim.Results) == 0 {
		return "", "", fmt.Errorf("name() sim failed: %s", sim.Error)
	}
	name, err := decodeScString(sim.Results[0].XDR)
	if err != nil {
		return "", "", err
	}
	parts := strings.SplitN(name, ":", 2)
	if len(parts) != 2 || !strkey.IsValidEd25519PublicKey(parts[1]) {
		return "", "", fmt.Errorf("token %s is not a classic-asset SAC (name=%q)", short(sac), name)
	}
	return parts[0], parts[1], nil
}

type faucetInfoResponse struct {
	Enabled         bool       `json:"enabled"`
	Amount          string     `json:"amount,omitempty"`
	CooldownSecs    int64      `json:"cooldownSecs,omitempty"`
	Asset           *assetInfo `json:"asset,omitempty"`
	TreasuryBalance string     `json:"treasuryBalance,omitempty"`
}

type assetInfo struct {
	Code     string `json:"code"`
	Issuer   string `json:"issuer"`
	Contract string `json:"contract"`
}

func handleFaucetInfo(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if faucet == nil {
		_ = json.NewEncoder(w).Encode(faucetInfoResponse{Enabled: false})
		return
	}
	bal, err := dex.TokenBalance(faucet.rpc, faucet.passphrase, faucet.usdc, faucet.kp.Address())
	if err != nil {
		bal = 0
	}
	_ = json.NewEncoder(w).Encode(faucetInfoResponse{
		Enabled:      true,
		Amount:       fmt.Sprintf("%d", faucet.amount),
		CooldownSecs: int64(faucet.cooldown / time.Second),
		Asset: &assetInfo{
			Code:     faucet.assetCode,
			Issuer:   faucet.assetIssuer,
			Contract: faucet.usdc,
		},
		TreasuryBalance: fmt.Sprintf("%d", bal),
	})
}

func faucetError(w http.ResponseWriter, status int, code string, extra map[string]any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	body := map[string]any{"error": code}
	for k, v := range extra {
		body[k] = v
	}
	_ = json.NewEncoder(w).Encode(body)
}

func handleFaucetClaim(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		faucetError(w, http.StatusMethodNotAllowed, "method_not_allowed", nil)
		return
	}
	if faucet == nil {
		faucetError(w, http.StatusServiceUnavailable, "disabled", nil)
		return
	}
	var req struct {
		Address string `json:"address"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || !strkey.IsValidEd25519PublicKey(req.Address) {
		faucetError(w, http.StatusBadRequest, "bad_address", nil)
		return
	}
	addr := req.Address

	// Per-address cooldown. In-memory: resets on keeper restart, which is an
	// accepted testnet trade-off; the treasury balance is the hard cap.
	faucet.mu.Lock()
	if last, ok := faucet.lastClaim[addr]; ok {
		if wait := faucet.cooldown - time.Since(last); wait > 0 {
			faucet.mu.Unlock()
			faucetError(w, http.StatusTooManyRequests, "cooldown",
				map[string]any{"retryAfterSecs": int64(wait / time.Second)})
			return
		}
	}
	faucet.mu.Unlock()

	// The SAC transfer would fail without a recipient trustline; pre-check via
	// Horizon for a precise error the UI can act on.
	hasTrust, err := hasTrustline(faucet.horizonURL, addr, faucet.assetCode, faucet.assetIssuer)
	if err != nil {
		faucetError(w, http.StatusBadGateway, "horizon_unavailable", nil)
		return
	}
	if !hasTrust {
		faucetError(w, http.StatusConflict, "no_trustline", nil)
		return
	}

	bal, err := dex.TokenBalance(faucet.rpc, faucet.passphrase, faucet.usdc, faucet.kp.Address())
	if err != nil || bal < faucet.amount {
		faucetError(w, http.StatusConflict, "insufficient_treasury", map[string]any{"treasuryBalance": bal})
		return
	}

	fromVal, err := soroban.ScvAddress(faucet.kp.Address())
	if err != nil {
		faucetError(w, http.StatusInternalServerError, "payment_failed", nil)
		return
	}
	toVal, err := soroban.ScvAddress(addr)
	if err != nil {
		faucetError(w, http.StatusBadRequest, "bad_address", nil)
		return
	}
	// Deliberately NOT retried: re-broadcasting a payment after an ambiguous
	// timeout could double-pay. A failed claim can simply be retried by the
	// user (cooldown only records successes).
	tx, err := faucet.rpc.Invoke(faucet.horizonURL, faucet.kp, faucet.passphrase, faucet.usdc,
		"transfer", fromVal, toVal, soroban.ScvI128(faucet.amount))
	if err != nil {
		logWarn("faucet payment failed", "to", short(addr), "err", err)
		faucetError(w, http.StatusBadGateway, "payment_failed", nil)
		return
	}

	faucet.mu.Lock()
	faucet.lastClaim[addr] = time.Now()
	faucet.mu.Unlock()

	logInfo("faucet paid", "to", short(addr), "amount", faucet.amount, "tx", tx.Hash)
	state.addEvent(fmt.Sprintf("faucet: paid %d.%07d USDC to %s", faucet.amount/1_0000000, faucet.amount%1_0000000, short(addr)))

	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]string{
		"txHash": tx.Hash,
		"amount": fmt.Sprintf("%d", faucet.amount),
	})
}

// decodeScString unmarshals a base64 ScVal that is a string or symbol.
func decodeScString(b64 string) (string, error) {
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(b64, &val); err != nil {
		return "", err
	}
	switch val.Type {
	case xdr.ScValTypeScvString:
		if val.Str != nil {
			return string(*val.Str), nil
		}
	case xdr.ScValTypeScvSymbol:
		if val.Sym != nil {
			return string(*val.Sym), nil
		}
	}
	return "", fmt.Errorf("scval is not a string")
}

// hasTrustline reports whether the account holds a trustline for code:issuer.
// A 404 (account not funded) is reported as no-trustline, not an error.
func hasTrustline(horizonURL, account, code, issuer string) (bool, error) {
	resp, err := http.Get(strings.TrimRight(horizonURL, "/") + "/accounts/" + account)
	if err != nil {
		return false, err
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return false, nil
	}
	if resp.StatusCode != http.StatusOK {
		return false, fmt.Errorf("horizon status %d", resp.StatusCode)
	}
	var acct struct {
		Balances []struct {
			AssetCode   string `json:"asset_code"`
			AssetIssuer string `json:"asset_issuer"`
		} `json:"balances"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&acct); err != nil {
		return false, err
	}
	for _, b := range acct.Balances {
		if b.AssetCode == code && b.AssetIssuer == issuer {
			return true, nil
		}
	}
	return false, nil
}
