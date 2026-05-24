export default function Architecture() {
  return (
    <section
      id="architecture"
      className="py-24 px-6"
      style={{ borderTop: "1px solid var(--border)" }}
    >
      <div className="max-w-6xl mx-auto">
        <p
          className="text-xs font-mono mb-12"
          style={{ color: "var(--text-dim)", letterSpacing: "0.12em" }}
        >
          ARCHITECTURE
        </p>

        <div className="grid grid-cols-1 lg:grid-cols-2 gap-16 items-start">
          {/* SVG Diagram */}
          <div>
            <svg
              viewBox="0 0 480 320"
              width="100%"
              xmlns="http://www.w3.org/2000/svg"
              style={{ maxWidth: "560px" }}
            >
              <defs>
                <marker
                  id="arrow"
                  viewBox="0 0 10 10"
                  refX="10"
                  refY="5"
                  markerWidth="6"
                  markerHeight="6"
                  orient="auto"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill="hsl(220, 15%, 30%)" />
                </marker>
                <marker
                  id="arrow-accent"
                  viewBox="0 0 10 10"
                  refX="10"
                  refY="5"
                  markerWidth="6"
                  markerHeight="6"
                  orient="auto"
                >
                  <path d="M 0 0 L 10 5 L 0 10 z" fill="hsl(160, 90%, 52%)" />
                </marker>
              </defs>

              {/* On-chain label */}
              <text x="10" y="18" fill="hsl(220,10%,40%)" fontSize="9" fontFamily="DM Mono, monospace" letterSpacing="1.5">
                SOROBAN TESTNET
              </text>
              <rect x="8" y="24" width="462" height="180" rx="2" fill="none" stroke="hsl(220,15%,16%)" strokeWidth="1" />

              {/* KeeperRegistry box */}
              <rect x="20" y="38" width="110" height="88" rx="1" fill="hsl(220,12%,10%)" stroke="hsl(220,15%,20%)" strokeWidth="1" />
              <text x="75" y="58" textAnchor="middle" fill="hsl(220,20%,75%)" fontSize="9" fontFamily="DM Mono, monospace" fontWeight="500">KeeperRegistry</text>
              <text x="75" y="74" textAnchor="middle" fill="hsl(160,90%,52%)" fontSize="8" fontFamily="DM Mono, monospace">register(stake)</text>
              <text x="75" y="87" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">mark_draw / clear_draw</text>
              <text x="75" y="100" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">record_execution()</text>
              <text x="75" y="113" textAnchor="middle" fill="hsl(38,95%,55%)" fontSize="8" fontFamily="DM Mono, monospace">slash(keeper)</text>

              {/* NectarVault box (NEW) */}
              <rect x="155" y="38" width="110" height="88" rx="1" fill="hsl(220,12%,10%)" stroke="hsl(160,40%,22%)" strokeWidth="1" />
              <text x="210" y="58" textAnchor="middle" fill="hsl(220,20%,75%)" fontSize="9" fontFamily="DM Mono, monospace" fontWeight="500">NectarVault</text>
              <text x="210" y="74" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">deposit / withdraw</text>
              <text x="210" y="87" textAnchor="middle" fill="hsl(160,90%,52%)" fontSize="8" fontFamily="DM Mono, monospace">draw / return_proceeds</text>
              <text x="210" y="100" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">cap · cooldown</text>
              <text x="210" y="113" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">share_price()</text>

              {/* Vault → Registry cross-contract arrow (mark_draw / clear_draw) */}
              <line x1="155" y1="100" x2="130" y2="100" stroke="hsl(160,90%,52%)" strokeWidth="1" strokeDasharray="2 2" markerEnd="url(#arrow-accent)" />
              <text x="142" y="95" textAnchor="middle" fill="hsl(160,90%,40%)" fontSize="6.5" fontFamily="DM Mono, monospace">mark / clear</text>

              {/* Blend Pool box (right of vault) */}
              <rect x="290" y="38" width="170" height="88" rx="1" fill="hsl(220,12%,10%)" stroke="hsl(220,15%,20%)" strokeWidth="1" />
              <text x="375" y="58" textAnchor="middle" fill="hsl(220,20%,75%)" fontSize="9" fontFamily="DM Mono, monospace" fontWeight="500">Blend Pool</text>
              <text x="375" y="74" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">new_liquidation_auction()</text>
              <text x="375" y="87" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">submit() — fill</text>
              <text x="375" y="100" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">get_positions()</text>
              <text x="375" y="113" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">Dutch · 0–400 ledgers</text>

              {/* Mock Oracle box (feeds Blend Pool) */}
              <rect x="305" y="156" width="140" height="42" rx="1" fill="hsl(220,12%,10%)" stroke="hsl(38,40%,20%)" strokeWidth="1" />
              <text x="375" y="172" textAnchor="middle" fill="hsl(38,95%,55%)" fontSize="9" fontFamily="DM Mono, monospace">Mock Oracle</text>
              <text x="375" y="186" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">set_price(asset, price)</text>

              {/* Oracle → Blend arrow */}
              <line x1="375" y1="156" x2="375" y2="127" stroke="hsl(220,15%,30%)" strokeWidth="1" markerEnd="url(#arrow)" />
              <text x="381" y="145" fill="hsl(220,10%,35%)" fontSize="7" fontFamily="DM Mono, monospace">prices</text>

              {/* USDC SAC token (under Vault — pays stake/draws) */}
              <rect x="155" y="156" width="110" height="42" rx="1" fill="hsl(220,12%,10%)" stroke="hsl(220,15%,20%)" strokeWidth="1" />
              <text x="210" y="172" textAnchor="middle" fill="hsl(220,20%,75%)" fontSize="9" fontFamily="DM Mono, monospace">USDC (SAC)</text>
              <text x="210" y="186" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">transfer · balance</text>
              {/* USDC ↔ Vault */}
              <line x1="210" y1="156" x2="210" y2="127" stroke="hsl(220,15%,30%)" strokeWidth="1" markerEnd="url(#arrow)" />
              {/* USDC ↔ Registry (stake transfers) */}
              <line x1="155" y1="172" x2="130" y2="100" stroke="hsl(38,95%,55%)" strokeWidth="1" strokeDasharray="2 2" markerEnd="url(#arrow)" />
              <text x="135" y="160" fill="hsl(38,95%,40%)" fontSize="6.5" fontFamily="DM Mono, monospace">stake</text>

              {/* Off-chain label */}
              <text x="10" y="226" fill="hsl(220,10%,40%)" fontSize="9" fontFamily="DM Mono, monospace" letterSpacing="1.5">
                OFF-CHAIN
              </text>
              <rect x="8" y="232" width="462" height="80" rx="2" fill="none" stroke="hsl(220,15%,16%)" strokeWidth="1" />

              {/* Keeper A */}
              <rect x="20" y="246" width="200" height="56" rx="1" fill="hsl(220,12%,10%)" stroke="hsl(160,40%,22%)" strokeWidth="1" />
              <text x="120" y="262" textAnchor="middle" fill="hsl(160,90%,52%)" fontSize="9" fontFamily="DM Mono, monospace">Keeper #1 (keeper-alpha)</text>
              <text x="120" y="276" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">monitor → draw → fill → return</text>
              <text x="120" y="290" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">staked operator · slashable</text>

              {/* Keeper B */}
              <rect x="248" y="246" width="200" height="56" rx="1" fill="hsl(220,12%,10%)" stroke="hsl(160,40%,22%)" strokeWidth="1" />
              <text x="348" y="262" textAnchor="middle" fill="hsl(160,90%,52%)" fontSize="9" fontFamily="DM Mono, monospace">Keeper #2 (keeper-beta)</text>
              <text x="348" y="276" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">monitor → draw → fill → return</text>
              <text x="348" y="290" textAnchor="middle" fill="hsl(220,10%,40%)" fontSize="8" fontFamily="DM Mono, monospace">staked operator · slashable</text>

              {/* Keeper A → Registry (register/stake) */}
              <line x1="75" y1="246" x2="75" y2="127" stroke="hsl(160,90%,52%)" strokeWidth="1" strokeDasharray="4 3" markerEnd="url(#arrow-accent)" />

              {/* Keeper A → Vault (draw / return) */}
              <line x1="160" y1="246" x2="200" y2="127" stroke="hsl(160,90%,52%)" strokeWidth="1" strokeDasharray="4 3" markerEnd="url(#arrow-accent)" />

              {/* Keeper B → Blend Pool (fill) */}
              <line x1="340" y1="246" x2="370" y2="127" stroke="hsl(160,90%,52%)" strokeWidth="1" strokeDasharray="4 3" markerEnd="url(#arrow-accent)" />
            </svg>
          </div>

          {/* Description */}
          <div className="space-y-6">
            {[
              {
                title: "KeeperRegistry — staking & slashing",
                desc: "Operators register by staking USDC; the registry tracks executions, success rate, and outstanding draws. Slashing transfers a configurable share of the stake to the vault when a draw isn't returned within the timeout.",
              },
              {
                title: "NectarVault — pooled liquidation capital",
                desc: "Depositors mint shares against pooled USDC. Deposit caps and per-account withdrawal cooldowns protect against capacity attacks; share price reflects accumulated profit. draw() / return_proceeds() cross-call the registry to mark and clear the operator's outstanding draw.",
              },
              {
                title: "Blend Pool — Dutch auctions",
                desc: "Liquidations run as two-phase Dutch auctions: lot scales 0→100 % over the first 200 ledgers, bid scales 100→0 % over the next 200. Keepers evaluate profitability live and submit a fill when lot/bid crosses their threshold.",
              },
              {
                title: "Multi-operator race",
                desc: "Both keepers can simulate and submit fills concurrently. One wins; the other receives an AlreadyFilled error, returns the drawn capital to the vault, and logs the loss.",
              },
            ].map((item, i) => (
              <div key={i} className="border-l-2 pl-4" style={{ borderColor: "var(--border)" }}>
                <div
                  className="text-sm font-syne font-600 mb-1"
                  style={{ color: "var(--text)" }}
                >
                  {item.title}
                </div>
                <div
                  className="text-xs font-mono leading-relaxed"
                  style={{ color: "var(--text-dim)" }}
                >
                  {item.desc}
                </div>
              </div>
            ))}
          </div>
        </div>
      </div>
    </section>
  );
}
