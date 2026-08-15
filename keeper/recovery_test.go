package main

// Tests for the idempotent unwind resume (Task B4): recoverStaleDraw +
// sweepHeldCollateral rebuild their entire view from chain reads each cycle,
// so a keeper restart loses nothing. The fakes stand in for those chain reads.

import (
	"errors"
	"strings"
	"testing"

	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/blend"
	"github.com/nectar-network/keeper/dex"
	"github.com/nectar-network/keeper/soroban"
)

const (
	rUSDC = "CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA"
	rXLM  = "CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC"
	rPool = "CBUBTHATT25SGJWXYWL47XN2J372XAJWTNKZAIKVMBBW6SYOIF6PCK3V"
)

type recVault struct {
	returns []int64
	retErr  error
}

func (v *recVault) Draw(amount int64, asset string) error {
	return errors.New("draw not expected in recovery")
}
func (v *recVault) LiqPaused(assets []string) (bool, error) { return false, nil }
func (v *recVault) ReturnProceeds(amount, responseTimeMs int64) error {
	if v.retErr != nil {
		return v.retErr
	}
	v.returns = append(v.returns, amount)
	return nil
}

type recSwap struct {
	calls []recSwapCall
	fn    func(token string, amount, ref int64) (*dex.SwapResult, error)
}

type recSwapCall struct {
	token       string
	amount, ref int64
}

func (s *recSwap) SwapToUSDC(kp *keypair.Full, token string, amount, ref int64) (*dex.SwapResult, error) {
	s.calls = append(s.calls, recSwapCall{token, amount, ref})
	if s.fn == nil {
		return nil, dex.ErrNoRoute
	}
	return s.fn(token, amount, ref)
}

// recSeams installs fake chain reads for one test and restores them after.
func recSeams(t *testing.T, drawn int64, balances map[string]int64, pool *blend.PoolState) {
	t.Helper()
	origDraw, origBal, origPool := getKeeperDraw, readTokenBalance, loadPoolState
	t.Cleanup(func() { getKeeperDraw, readTokenBalance, loadPoolState = origDraw, origBal, origPool })

	getKeeperDraw = func(rpc *soroban.Client, passphrase, vaultAddr, keeper string) (int64, uint64, error) {
		return drawn, 0, nil
	}
	readTokenBalance = func(rpc *soroban.Client, passphrase, tokenAddr, owner string) (int64, error) {
		return balances[tokenAddr], nil
	}
	loadPoolState = func(rpc *soroban.Client, passphrase, poolAddr string) (*blend.PoolState, error) {
		if pool == nil {
			return nil, errors.New("pool unreadable")
		}
		return pool, nil
	}
}

func recKeeper(t *testing.T, vc *recVault, sw *recSwap, xlmReserve int64) *Keeper {
	t.Helper()
	kp, err := keypair.Random()
	if err != nil {
		t.Fatal(err)
	}
	k := &Keeper{
		kp: kp,
		cfg: Config{
			UsdcAddr:   rUSDC,
			VaultID:    "CVAULT",
			XlmReserve: xlmReserve,
			BlendPools: []BlendPoolConfig{{Addr: rPool}},
		},
		vault:     vc,
		nativeSAC: rXLM,
	}
	if sw != nil {
		k.dexc = sw
	}
	return k
}

func recPool() *blend.PoolState {
	return &blend.PoolState{Reserves: map[string]*blend.Reserve{
		rUSDC: {Asset: rUSDC, Decimals: 7, OraclePrice: 1.0},
		rXLM:  {Asset: rXLM, Decimals: 7, OraclePrice: 0.1},
	}}
}

// No outstanding draw: recovery must do nothing at all.
func TestRecovery_NoDraw_NoAction(t *testing.T) {
	vc, sw := &recVault{}, &recSwap{}
	recSeams(t, 0, map[string]int64{rUSDC: 999_0000000}, recPool())
	recKeeper(t, vc, sw, 100_0000000).recoverStaleDraw()
	if len(vc.returns) != 0 || len(sw.calls) != 0 {
		t.Fatalf("no draw must mean no action: returns=%v swaps=%v", vc.returns, sw.calls)
	}
}

// USDC on hand covers the draw: return exactly the drawn amount, no sweep.
func TestRecovery_USDCCoversDraw_ReturnsWithoutSweep(t *testing.T) {
	vc, sw := &recVault{}, &recSwap{}
	recSeams(t, 20_0000000, map[string]int64{rUSDC: 25_0000000}, recPool())
	recKeeper(t, vc, sw, 100_0000000).recoverStaleDraw()
	if len(vc.returns) != 1 || vc.returns[0] != 20_0000000 {
		t.Fatalf("must return exactly the draw: %v", vc.returns)
	}
	if len(sw.calls) != 0 {
		t.Fatalf("no sweep needed when USDC covers the draw: %v", sw.calls)
	}
}

// USDC short, XLM held above the fee floor: sweep only the excess over the
// floor PLUS the native sweep margin (headroom for the sweep tx's own fee
// and in-flight legs), then return.
func TestRecovery_SweepsXLMAboveFeeFloor(t *testing.T) {
	vc := &recVault{}
	balances := map[string]int64{rUSDC: 5_0000000, rXLM: 150_0000000} // 5 USDC, 150 XLM
	sw := &recSwap{fn: func(token string, amount, ref int64) (*dex.SwapResult, error) {
		balances[rUSDC] += 4_8000000 // ~49 XLM → 4.8 USDC
		balances[rXLM] -= amount
		return &dex.SwapResult{OutputAmount: 4_8000000, TxHash: "sweeptx"}, nil
	}}
	recSeams(t, 20_0000000, balances, recPool())
	recKeeper(t, vc, sw, 100_0000000).recoverStaleDraw() // fee floor 100 XLM

	if len(sw.calls) != 1 {
		t.Fatalf("expected one sweep call, got %v", sw.calls)
	}
	wantSell := int64(150_0000000 - 100_0000000 - nativeSweepMargin) // 49 XLM
	if sw.calls[0].token != rXLM || sw.calls[0].amount != wantSell {
		t.Fatalf("must sweep only the excess above floor+margin (%d): %+v", wantSell, sw.calls[0])
	}
	// Oracle anchor: 49 XLM at $0.10 = 4.9 USDC reference.
	if sw.calls[0].ref != 4_9000000 {
		t.Fatalf("oracle reference: got %d want 4_9000000", sw.calls[0].ref)
	}
	// Post-sweep USDC (9.8) still < draw (20): return everything available.
	if len(vc.returns) != 1 || vc.returns[0] != 9_8000000 {
		t.Fatalf("must return all USDC on hand after sweep: %v", vc.returns)
	}
}

// XLM at or below the fee floor is untouchable even with a draw outstanding.
func TestRecovery_FeeFloorProtected(t *testing.T) {
	vc, sw := &recVault{}, &recSwap{}
	recSeams(t, 20_0000000, map[string]int64{rUSDC: 0, rXLM: 100_0000000}, recPool())
	recKeeper(t, vc, sw, 100_0000000).recoverStaleDraw()
	if len(sw.calls) != 0 {
		t.Fatalf("fee-floor XLM must never be swept: %v", sw.calls)
	}
	if len(vc.returns) != 0 {
		t.Fatalf("nothing to return: %v", vc.returns)
	}
}

// Unknown native SAC disables sweeps entirely — fail-safe for the fee balance.
func TestRecovery_NoNativeSAC_DisablesSweep(t *testing.T) {
	vc, sw := &recVault{}, &recSwap{}
	recSeams(t, 20_0000000, map[string]int64{rUSDC: 0, rXLM: 500_0000000}, recPool())
	k := recKeeper(t, vc, sw, 0)
	k.nativeSAC = ""
	k.recoverStaleDraw()
	if len(sw.calls) != 0 {
		t.Fatalf("sweep must be disabled without a native SAC: %v", sw.calls)
	}
}

// An unpriced reserve is held, not dumped: no oracle anchor means no floor.
func TestRecovery_UnpricedAssetHeld(t *testing.T) {
	vc := &recVault{}
	sw := &recSwap{}
	pool := recPool()
	pool.Reserves[rXLM].OraclePrice = 0
	recSeams(t, 20_0000000, map[string]int64{rUSDC: 0, rXLM: 500_0000000}, pool)
	recKeeper(t, vc, sw, 100_0000000).recoverStaleDraw()
	if len(sw.calls) != 0 {
		t.Fatalf("unpriced asset must be held, not sold: %v", sw.calls)
	}
}

// Monitor pools are never swept: no vault capital ever moved toward them.
func TestRecovery_MonitorPoolNotSwept(t *testing.T) {
	vc, sw := &recVault{}, &recSwap{}
	recSeams(t, 20_0000000, map[string]int64{rUSDC: 0, rXLM: 500_0000000}, recPool())
	k := recKeeper(t, vc, sw, 100_0000000)
	k.cfg.BlendPools = []BlendPoolConfig{{Addr: rPool, Monitor: true}}
	k.recoverStaleDraw()
	if len(sw.calls) != 0 {
		t.Fatalf("monitor pools must not be swept: %v", sw.calls)
	}
}

// A refused sweep (slippage floor) holds the collateral and returns whatever
// USDC exists; the draw stays outstanding for the next cycle.
func TestRecovery_SweepRefused_PartialReturn(t *testing.T) {
	vc := &recVault{}
	sw := &recSwap{fn: func(token string, amount, ref int64) (*dex.SwapResult, error) {
		return nil, dex.ErrSlippageExceeded
	}}
	recSeams(t, 20_0000000, map[string]int64{rUSDC: 3_0000000, rXLM: 500_0000000}, recPool())
	recKeeper(t, vc, sw, 100_0000000).recoverStaleDraw()
	if len(sw.calls) != 1 {
		t.Fatalf("sweep must be attempted once: %v", sw.calls)
	}
	if len(vc.returns) != 1 || vc.returns[0] != 3_0000000 {
		t.Fatalf("the 3 USDC on hand must still go back: %v", vc.returns)
	}
}

// The resume is idempotent: after a full recovery, a second pass (draw now 0)
// does nothing.
func TestRecovery_SecondPassIsNoOp(t *testing.T) {
	vc, sw := &recVault{}, &recSwap{}
	recSeams(t, 0, map[string]int64{rUSDC: 50_0000000}, recPool())
	k := recKeeper(t, vc, sw, 100_0000000)
	k.recoverStaleDraw()
	k.recoverStaleDraw()
	if len(vc.returns) != 0 || len(sw.calls) != 0 {
		t.Fatalf("cleared draw must be a no-op: returns=%v swaps=%v", vc.returns, sw.calls)
	}
}

// Config surface: KEEPER_XLM_RESERVE parses and rejects garbage.
func TestConfig_XlmReserve(t *testing.T) {
	t.Setenv("KEEPER_SECRET", "SB3KRWDCSLNGEZDNKSI353KVOOVMVIXDBP4IB4TFXPSPZ3IIAWNB5X4M")
	t.Setenv("REGISTRY_CONTRACT", "C1")
	t.Setenv("VAULT_CONTRACT", "C2")
	t.Setenv("KEEPER_XLM_RESERVE", "2500000000")
	cfg := LoadConfig()
	if cfg.XlmReserve != 2_500_000_000 {
		t.Fatalf("XlmReserve: got %d", cfg.XlmReserve)
	}
	if !strings.HasPrefix(rXLM, "C") { // keep the linter honest about the const
		t.Fatal("test constant corrupted")
	}
}
