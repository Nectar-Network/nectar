//! Property-based tests for the vault's core value invariants.
//!
//! These exercise the pure share-math functions (`to_shares` / `to_assets`) over
//! wide randomized input ranges — the invariants an auditor most cares about:
//! no depositor can ever extract more value than they put in (rounding always
//! favors the pool), shares are monotonic in the deposit, and the virtual-offset
//! math never over-issues shares. Amounts are 7-decimal stroops.
#![cfg(test)]

use crate::{to_assets, to_shares};
use proptest::prelude::*;

// Bounds kept well within realistic vault sizes (≤ ~100k USDC per amount, ≤ ~1M
// USDC total) so products stay far below i128::MAX while still stress-testing
// the ratio math across many orders of magnitude.
const MAX_TOTAL: i128 = 10_000_000_000_000; // ~1,000,000 USDC in stroops
const MAX_AMT: i128 = 1_000_000_000_000; //    ~100,000 USDC in stroops

proptest! {
    #![proptest_config(ProptestConfig::with_cases(2000))]

    /// SOLVENCY / no-free-value: a deposit immediately followed by withdrawing
    /// exactly the shares it minted never returns more than was deposited. This
    /// is the invariant that guarantees the pool can always cover redemptions —
    /// rounding must always favor the pool.
    #[test]
    fn deposit_then_withdraw_never_profits(
        s in 0i128..=MAX_TOTAL,
        u in 0i128..=MAX_TOTAL,
        a in 1i128..=MAX_AMT,
    ) {
        let shares = to_shares(a, s, u);
        prop_assert!(shares >= 0);
        // State after the deposit, then redeem exactly those shares.
        let out = to_assets(shares, s + shares, u + a);
        prop_assert!(out <= a, "round-trip out {out} exceeded deposit {a} (s={s}, u={u})");
        prop_assert!(out >= 0);
    }

    /// The inverse loop must also not create value: withdraw some shares, then
    /// re-deposit exactly the proceeds — you never get more shares back.
    #[test]
    fn withdraw_then_deposit_never_profits(
        s in 1i128..=MAX_TOTAL,
        u in 1i128..=MAX_TOTAL,
        sh in 1i128..=MAX_AMT,
    ) {
        prop_assume!(sh <= s);
        let assets = to_assets(sh, s, u);
        prop_assume!(assets <= u);
        let back = to_shares(assets, s - sh, u - assets);
        prop_assert!(back <= sh, "share round-trip {back} exceeded {sh} (s={s}, u={u})");
    }

    /// Shares minted are monotonic non-decreasing in the deposit amount — a
    /// larger deposit never yields fewer shares (no discontinuity an attacker
    /// could exploit).
    #[test]
    fn shares_monotonic_in_amount(
        s in 0i128..=MAX_TOTAL,
        u in 0i128..=MAX_TOTAL,
        a in 1i128..=MAX_AMT,
        d in 0i128..=MAX_AMT,
    ) {
        prop_assert!(to_shares(a + d, s, u) >= to_shares(a, s, u));
    }

    /// Share-inflation resistance (VLT-1): the first-depositor inflation attack
    /// is never PROFITABLE. An attacker seeds 1 share for 1 stroop, donates a
    /// large amount to inflate the price, then a victim deposits — the attacker
    /// can never extract more than it contributed (deposit + donation). The
    /// virtual offset dilutes the attacker's single share so heavily that the
    /// donation is unrecoverable, making the attack a guaranteed loss.
    #[test]
    fn inflation_attack_is_unprofitable(
        donation in 0i128..=MAX_TOTAL,
        victim in 1i128..=MAX_AMT,
    ) {
        let (s0, u0) = (1i128, 1i128 + donation);      // attacker's seed + donation
        let vic_shares = to_shares(victim, s0, u0);    // victim deposits
        let (s1, u1) = (s0 + vic_shares, u0 + victim);
        let attacker_out = to_assets(1, s1, u1);       // attacker redeems its 1 share
        let attacker_in = 1i128 + donation;
        prop_assert!(
            attacker_out <= attacker_in,
            "inflation attack profited: out {attacker_out} > in {attacker_in} (donation={donation}, victim={victim})"
        );
    }
}
