"use client";

import { useEffect, useMemo, useState } from "react";
import {
  DepositorRow,
  PerformanceData,
  fetchPerformance,
  formatUSDC,
  shortAddress,
  sharePriceSeries,
} from "../../../lib/api";
import { queryDepositor } from "../../../lib/stellar";
import { LineChart } from "../../components/ds";

const statCard: React.CSSProperties = {
  padding: 20,
  border: "1px solid var(--border)",
  borderRadius: 4,
  background: "var(--card-fill)",
};
const statLabel: React.CSSProperties = {
  fontSize: 11,
  color: "var(--text-dim)",
  letterSpacing: "0.08em",
  textTransform: "uppercase",
  marginBottom: 8,
};

export default function DepositorAnalytics({
  address,
  initialData,
}: {
  address: string;
  initialData: PerformanceData | null;
}) {
  const [perf, setPerf] = useState<PerformanceData | null>(initialData);
  const [onchainShares, setOnchainShares] = useState<number | null>(null);

  useEffect(() => {
    const poll = async () => {
      const f = await fetchPerformance();
      if (f) setPerf(f);
    };
    poll();
    const t = setInterval(poll, 15_000);
    return () => clearInterval(t);
  }, []);

  useEffect(() => {
    let cancelled = false;
    queryDepositor(address).then((d) => {
      if (!cancelled && d) setOnchainShares(d.shares);
    });
    return () => {
      cancelled = true;
    };
  }, [address]);

  const dep: DepositorRow | undefined = (perf?.depositors ?? []).find((d) => d.address === address);
  const shares = onchainShares ?? dep?.shares ?? 0;
  const value = dep?.usdc_value ?? 0;
  // Per-depositor cost basis isn't tracked on-chain, so net-deposited is
  // approximated at the 1:1 mint price. Both yield and return derive from this
  // same basis so the cards stay consistent; they are labeled as estimates. (The
  // keeper API never populates pnl_pct, so we don't use it.)
  const yieldStroops = value - shares;
  const pnlPct = shares > 0 ? (yieldStroops / shares) * 100 : 0;

  const series = useMemo(() => {
    if (!perf || !shares) return [];
    return sharePriceSeries(perf).map((p) => ({
      label: p.label,
      value: (shares * p.sharePrice) / 1e7,
    }));
  }, [perf, shares]);

  const isDepositor = !!dep || shares > 0;

  return (
    <div style={{ maxWidth: 1100, margin: "0 auto", padding: "32px 24px" }}>
      <div style={{ marginBottom: 24 }}>
        <h1
          style={{
            fontFamily: "var(--font-syne)",
            fontSize: 20,
            fontWeight: 700,
            letterSpacing: "0.15em",
            textTransform: "uppercase",
            color: "var(--text)",
            marginBottom: 4,
          }}
        >
          Depositor Analytics
        </h1>
        <p style={{ fontSize: 12, color: "var(--text-dim)", fontFamily: "monospace" }} title={address}>
          {shortAddress(address)}
        </p>
      </div>

      {!isDepositor ? (
        <div
          style={{
            border: "1px solid var(--border)",
            borderRadius: 4,
            padding: 24,
            color: "var(--text-dim)",
            fontFamily: "monospace",
            fontSize: 13,
          }}
        >
          No vault position found for this address.
        </div>
      ) : (
        <>
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))",
              gap: 16,
              marginBottom: 32,
            }}
          >
            <div style={statCard}>
              <div style={statLabel}>Current Value</div>
              <div style={{ fontSize: 22, fontFamily: "monospace", fontWeight: 600, color: "var(--text)" }}>
                ${formatUSDC(value)}
              </div>
            </div>
            <div style={statCard}>
              <div style={statLabel}>Shares</div>
              <div style={{ fontSize: 22, fontFamily: "monospace", fontWeight: 600, color: "var(--text)" }}>
                {(shares / 1e7).toFixed(2)}
              </div>
            </div>
            <div style={statCard}>
              <div style={statLabel}>Cumulative Yield (est.)</div>
              <div
                style={{
                  fontSize: 22,
                  fontFamily: "monospace",
                  fontWeight: 600,
                  color: yieldStroops >= 0 ? "var(--accent)" : "var(--red)",
                }}
              >
                {yieldStroops >= 0 ? "+" : "-"}${formatUSDC(Math.abs(yieldStroops))}
              </div>
            </div>
            <div style={statCard}>
              <div style={statLabel}>Return (est.)</div>
              <div
                style={{
                  fontSize: 22,
                  fontFamily: "monospace",
                  fontWeight: 600,
                  color: pnlPct >= 0 ? "var(--accent)" : "var(--red)",
                }}
              >
                {pnlPct >= 0 ? "+" : ""}
                {pnlPct.toFixed(2)}%
              </div>
            </div>
          </div>

          <p
            style={{
              fontSize: 11,
              color: "var(--text-dim)",
              fontFamily: "monospace",
              marginBottom: 28,
            }}
          >
            Yield &amp; return are estimates assuming a 1.0 entry price — per-depositor cost basis
            is not tracked on-chain, so a depositor who entered above par may see an overstated gain.
          </p>

          <section>
            <h2
              style={{
                fontSize: 12,
                letterSpacing: "0.1em",
                textTransform: "uppercase",
                color: "var(--text-dim)",
                marginBottom: 12,
              }}
            >
              Position Value Over Time
            </h2>
            <div style={{ border: "1px solid var(--border)", borderRadius: 4, background: "var(--card-fill)", padding: 20 }}>
              {series.length >= 2 ? (
                <LineChart data={series} height={240} variant="area" label="position" valueFmt={(v) => `$${v.toFixed(0)}`} />
              ) : (
                <div style={{ height: 240, display: "flex", alignItems: "center", justifyContent: "center", color: "var(--text-dim)", fontFamily: "monospace", fontSize: 12 }}>
                  Not enough history yet to chart this position.
                </div>
              )}
            </div>
          </section>
        </>
      )}
    </div>
  );
}
