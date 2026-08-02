# FACTS.md — Verified Blend Protocol Facts

Single source of truth for versions, contract IDs, and decoded shapes used by
Nectar Network's Blend integration. **Every entry must carry a date and a
source** (URL, repo-path@commit, command, or tx hash). Entries without a source
are invalid — do not add them.

Entry format: `claim — value — date — source`.

Status: SKELETON — populated by verification Gates 0.1–0.7 (see
VERIFICATION-REPORT.md once complete).

---

## Reference baselines

| Claim | Value | Date | Source |
|---|---|---|---|
| blend-contracts-v2 pinned commit | `ba22b487b2c5057a4ecc28b05b5193c28e4bd117` (authored 2025-08-14 11:26:04 -0400) | 2026-08-03 | `git rev-parse HEAD` / `git log -1 --format=%ci` in local clone of https://github.com/blend-capital/blend-contracts-v2 |
| blend-utils pinned commit | `b05242df30b6b6caf9d317646f754541824a5a8b` (authored 2025-12-18 08:22:46 -0500) | 2026-08-03 | `git rev-parse HEAD` / `git log -1 --format=%ci` in local clone of https://github.com/blend-capital/blend-utils |
| Clones live in `reference/` (gitignored) | pinned by hash above, not vendored | 2026-08-03 | `.gitignore` `reference/` entry |

## Request struct & RequestType integers

| Variant | Integer | Date | Source (file:line @ commit) |
|---|---|---|---|
| _to be filled by Gate 0.2_ | | | |

## Auction fill price curve

| Claim | Value | Date | Source (file:line @ commit) |
|---|---|---|---|
| _to be filled by Gate 0.3_ | | | |

## Auction asset flows

| Auction type | Lot asset(s) | Bid asset(s) | Filler provides | Filler receives | Backstop LP involved? | Source |
|---|---|---|---|---|---|---|
| _to be filled by Gate 0.4_ | | | | | | |

## Testnet addresses

| Claim | Value | Date | Source (command / URL) |
|---|---|---|---|
| _to be filled by Gate 0.5_ | | | |

## Our testnet USDC

| Claim | Value | Date | Source |
|---|---|---|---|
| _to be filled by Gate 0.5_ | | | |

## blend-utils pool deploy

| Claim | Value | Date | Source |
|---|---|---|---|
| _to be filled by Gate 0.6_ | | | |

## Decisions

| Decision | Rationale | Date | Source / evidence |
|---|---|---|---|
| _decisions recorded as they are made_ | | | |
