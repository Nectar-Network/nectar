# Tranche submission wording — Blend auction-type scope

Verified 2026-08-09 against blend-contracts-v2 @ `ba22b48` (docs/FACTS.md
"Auction asset flows"); live evidence in docs/evidence/b-full-cycle.md and
docs/evidence/c-bad-debt.md.

## The honest claim (use this wording in tranche submissions)

> The keeper supports Blend's three auction kinds to the level their verified
> mechanics allow. **User liquidations are fully supported and proven live on
> testnet**: atomic fill + repay + collateral withdrawal in one health-checked
> submit, collateral swapped to USDC, capital and profit returned to the vault.
> **Bad-debt auctions are filled fill-and-hold**: the keeper creates and fills
> the backstop's auction atomically with the debt repay from its own operator
> float (never vault capital), and holds the backstop-LP lot it receives,
> valued at the backstop's spot price minus a configurable haircut; unwinding
> the LP is deferred to mainnet, where the comet's USDC leg is the vault's
> settlement asset and the exit is a single verified call. Bad-debt fills
> therefore use operator capital and return operator profit — crediting that
> profit to depositors additionally requires a vault entry point that accepts
> proceeds without a prior draw, which is not built. **Interest auctions are detected but
> deliberately not filled**: their bid is backstop LP tokens (120% of the
> auctioned interest at spot) that a vault-USDC keeper does not hold — the
> keeper logs the deferral and never attempts a fill.

## Why the scope is shaped this way (one paragraph)

Prior wording claimed all three auction kinds worked via "same logic,
different request type". That was wrong: the three kinds move different
assets (user liquidations exchange positions only; bad-debt fills pay out
backstop LP; interest fills demand backstop LP as payment), which was
verified from the pinned contract source and recorded in docs/FACTS.md before
this scope was implemented. The current claims match exactly what the code
does and what the on-chain evidence shows.
