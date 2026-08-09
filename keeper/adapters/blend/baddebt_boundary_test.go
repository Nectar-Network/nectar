package blend

// Regression for the live-caught t=400 boundary artifact (#1219
// InvalidDTokenBurnAmount): a repay-carrying bad-debt fill built in the last
// ledgers of the curve can land after the bid scales to empty, where the
// repay leg has nothing to burn and the submit reverts. Inside the margin
// the adapter must WAIT (neither fill path may run); at t>=400 the free
// capture runs as usual.

import (
	"strings"
	"testing"
)

func TestExecuteBadDebt_BoundaryWaitsForFreeCapture(t *testing.T) {
	for _, elapsed := range []int64{398, 399} {
		bal := newBalTable()
		bal.m[tUSDC] = 300_000_000
		pcts, _, free, _ := installBadDebtSeams(t, bal, backstopAuction(), elapsed)

		res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
		if err != nil {
			t.Fatal(err)
		}
		if res.Success || len(*pcts) != 0 || *free != 0 {
			t.Fatalf("elapsed %d: must wait at the boundary, got success=%v fills=%v free=%d",
				elapsed, res.Success, *pcts, *free)
		}
		if !strings.Contains(res.Note, "t=400 boundary") {
			t.Errorf("elapsed %d: note must explain the boundary wait, got %q", elapsed, res.Note)
		}
	}
	// One ledger before the margin the repay path still runs.
	bal := newBalTable()
	bal.m[tUSDC] = 300_000_000
	pcts, _, _, _ := installBadDebtSeams(t, bal, backstopAuction(), 397)
	res, err := badDebtAdapter().Execute(nil, mustKPExec(t), badDebtTask(), &fakeVault{bal: bal})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Success || len(*pcts) != 1 {
		t.Fatalf("elapsed 397: repay path must still run, got success=%v fills=%v note=%q", res.Success, *pcts, res.Note)
	}
}
