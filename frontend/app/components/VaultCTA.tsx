"use client";

import { useEffect, useState } from "react";
import {
  PerformanceData,
  fetchPerformance,
  sharePrice,
  sharePriceSeries,
  vaultReturn,
} from "../../lib/api";
import { Btn, Eyebrow, Card, Sparkline } from "./ds";

export default function VaultCTA() {
  const [perf, setPerf] = useState<PerformanceData | null>(null);

  useEffect(() => {
    const poll = async () => {
      const fresh = await fetchPerformance();
      if (fresh) setPerf(fresh);
    };
    poll();
    const t = setInterval(poll, 15_000);
    return () => clearInterval(t);
  }, []);

  const vault = perf?.vault ?? null;
  const price = vault ? sharePrice(vault.total_usdc, vault.total_shares) : 1.0;
  const ret = perf ? vaultReturn(sharePriceSeries(perf)) : { pct: 0, annualized: false, days: 0 };
  const series = perf ? sharePriceSeries(perf).slice(-30).map((p) => ({ value: p.sharePrice })) : [];

  return (
    <section style={{ padding: "96px 24px", borderTop: "1px solid var(--border)" }}>
      <div className="home-wrap" style={{ padding: 0 }}>
        <Card accent style={{ padding: 0 }}>
          <div className="vault-cta-grid">
            <div style={{ padding: "40px 40px" }}>
              <Eyebrow style={{ marginBottom: 14 }}>The vault</Eyebrow>
              <h2 style={{ fontFamily: "var(--font-display)", fontWeight: 700, fontSize: "clamp(1.5rem, 3vw, 2.1rem)", color: "var(--text)", margin: "0 0 16px", letterSpacing: "-0.01em" }}>
                Deposit USDC. Earn keeper yield.
              </h2>
              <p style={{ fontFamily: "var(--font-mono)", fontSize: 13, lineHeight: 1.75, color: "var(--text-dim)", maxWidth: 420, margin: "0 0 28px" }}>
                Deposit into the shared vault and receive LP shares. Every liquidation returns its profit to the
                vault — your shares appreciate against a rising share price. Withdraw anytime.
              </p>
              <div style={{ display: "flex", flexWrap: "wrap", gap: 12 }}>
                <Btn primary href="/vault">Open the vault</Btn>
                <Btn href="/dashboard" small>See the dashboard →</Btn>
              </div>
            </div>
            <div style={{ borderLeft: "1px solid var(--border)", padding: "32px 28px", display: "flex", flexDirection: "column", justifyContent: "center", gap: 20 }}>
              <div style={{ display: "flex", gap: 28, flexWrap: "wrap" }}>
                <div>
                  <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--text-mute)", marginBottom: 6 }}>
                    {ret.annualized ? "30d APY" : "Return"}
                  </div>
                  <div style={{ fontFamily: "var(--font-display)", fontWeight: 700, fontSize: 34, color: "var(--accent)", fontVariantNumeric: "tabular-nums", lineHeight: 1 }}>
                    {ret.pct >= 0 ? "+" : ""}{ret.pct.toFixed(2)}%
                  </div>
                </div>
                <div>
                  <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--text-mute)", marginBottom: 6 }}>Share price</div>
                  <div style={{ fontFamily: "var(--font-display)", fontWeight: 700, fontSize: 34, color: "var(--text)", fontVariantNumeric: "tabular-nums", lineHeight: 1 }}>${price.toFixed(4)}</div>
                </div>
              </div>
              <div><Sparkline data={series} width={320} height={56} /></div>
              <div style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--text-dim)" }}>share price · trailing 30 days</div>
            </div>
          </div>
        </Card>
      </div>
    </section>
  );
}
