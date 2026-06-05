"use client";

import { useEffect, useState } from "react";
import Link from "next/link";
import {
  PerformanceData,
  fetchPerformance,
  formatUSDC,
  shortAddress,
  sharePriceSeries,
  vaultReturn,
} from "../../lib/api";
import ApyChart from "../components/ApyChart";
import LiquidationFeed from "../components/LiquidationFeed";

const EMPTY: PerformanceData = { vault: null, depositors: [], keeper_stats: {}, liquidations: [] };

const statCard: React.CSSProperties = {
  padding: 20,
  border: "1px solid var(--border)",
  borderRadius: 4,
  background: "rgba(255,255,255,0.02)",
};
const statLabel: React.CSSProperties = {
  fontSize: 11,
  color: "var(--text-dim)",
  letterSpacing: "0.08em",
  textTransform: "uppercase",
  marginBottom: 8,
};
const sectionTitle: React.CSSProperties = {
  fontSize: 12,
  letterSpacing: "0.1em",
  textTransform: "uppercase",
  color: "var(--text-dim)",
  marginBottom: 12,
};
const link: React.CSSProperties = { color: "var(--accent)", textDecoration: "none", fontFamily: "monospace", fontSize: 12 };

export default function DashboardOverview({ initialData }: { initialData: PerformanceData | null }) {
  const [perf, setPerf] = useState<PerformanceData>(initialData ?? EMPTY);
  const [live, setLive] = useState(!!initialData);
  const [lastUpdate, setLastUpdate] = useState<Date>(new Date());

  useEffect(() => {
    const poll = async () => {
      const fresh = await fetchPerformance();
      if (fresh) {
        setPerf(fresh);
        setLive(true);
        setLastUpdate(new Date());
      } else {
        setLive(false);
      }
    };
    poll();
    const t = setInterval(poll, 15_000);
    return () => clearInterval(t);
  }, []);

  const vault = perf.vault;
  const tvl = vault?.total_usdc ?? 0;
  const profit = vault?.total_profit ?? 0;
  const depositors = perf.depositors ?? [];
  const keeperStats = perf.keeper_stats ?? {};
  const liquidations = perf.liquidations ?? [];
  const ret = vaultReturn(sharePriceSeries(perf));

  const topKeepers = Object.values(keeperStats)
    .slice()
    .sort((a, b) => b.total_profit - a.total_profit)
    .slice(0, 5);

  return (
    <div style={{ maxWidth: 1100, margin: "0 auto", padding: "32px 24px" }}>
      {/* Header */}
      <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 32 }}>
        <div>
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
            Dashboard
          </h1>
          <p style={{ fontSize: 12, color: "var(--text-dim)", fontFamily: "monospace" }}>
            Vault analytics · keeper performance · liquidation activity
          </p>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <span
            style={{
              width: 8,
              height: 8,
              borderRadius: "50%",
              background: live ? "var(--accent)" : "var(--amber)",
              boxShadow: live ? "0 0 6px var(--accent)" : "0 0 6px var(--amber)",
              animation: "pulse2 2s ease-in-out infinite",
            }}
          />
          <span style={{ fontSize: 11, color: "var(--text-dim)", fontFamily: "monospace" }}>
            {live ? `LIVE · ${lastUpdate.toLocaleTimeString()}` : "OFFLINE · awaiting keeper API"}
          </span>
        </div>
      </div>

      {/* Top stats */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))",
          gap: 16,
          marginBottom: 32,
        }}
      >
        {[
          { label: "TVL", value: `$${formatUSDC(tvl)}`, accent: false },
          { label: "Cumulative Profit", value: `+$${formatUSDC(profit)}`, accent: profit > 0 },
          {
            label: ret.annualized ? "Trailing APY" : "Return to date",
            value: `${ret.pct >= 0 ? "+" : ""}${ret.pct.toFixed(2)}%`,
            accent: ret.pct > 0,
          },
          { label: "Depositors", value: `${depositors.length}`, accent: false },
          { label: "Active Keepers", value: `${Object.keys(keeperStats).length}`, accent: false },
          { label: "Liquidations", value: `${liquidations.length}`, accent: false },
        ].map(({ label, value, accent }) => (
          <div key={label} style={statCard}>
            <div style={statLabel}>{label}</div>
            <div
              style={{
                fontSize: 22,
                fontFamily: "monospace",
                fontWeight: 600,
                color: accent ? "var(--accent)" : "var(--text)",
              }}
            >
              {value}
            </div>
          </div>
        ))}
      </div>

      {/* APY chart */}
      <section style={{ marginBottom: 32 }}>
        <h2 style={sectionTitle}>Vault Returns</h2>
        <ApyChart perf={perf} />
      </section>

      {/* Top keepers */}
      <section style={{ marginBottom: 32 }}>
        <div style={{ display: "flex", justifyContent: "space-between", alignItems: "baseline" }}>
          <h2 style={sectionTitle}>Top Keepers</h2>
          <Link href="/dashboard/keepers" style={link}>
            view leaderboard →
          </Link>
        </div>
        <div style={{ border: "1px solid var(--border)", borderRadius: 4, overflow: "hidden" }}>
          <table style={{ width: "100%", borderCollapse: "collapse" }}>
            <tbody>
              {topKeepers.length === 0 && (
                <tr>
                  <td
                    style={{
                      padding: "12px",
                      color: "var(--text-dim)",
                      fontFamily: "monospace",
                      fontSize: 13,
                      textAlign: "center",
                    }}
                  >
                    No keepers registered.
                  </td>
                </tr>
              )}
              {topKeepers.map((k, idx) => (
                <tr key={k.address} style={{ borderBottom: "1px solid var(--border)" }}>
                  <td style={{ padding: "8px 12px", color: "var(--text-dim)", fontFamily: "monospace", fontSize: 13, width: 32 }}>
                    {idx + 1}
                  </td>
                  <td style={{ padding: "8px 12px", color: "var(--accent)", fontFamily: "monospace", fontSize: 13 }}>{k.name}</td>
                  <td style={{ padding: "8px 12px", color: "var(--text-dim)", fontFamily: "monospace", fontSize: 12 }} title={k.address}>
                    {shortAddress(k.address)}
                  </td>
                  <td style={{ padding: "8px 12px", textAlign: "right", color: "var(--text-dim)", fontFamily: "monospace", fontSize: 13 }}>
                    {k.liquidations} fills
                  </td>
                  <td style={{ padding: "8px 12px", textAlign: "right", color: "var(--accent)", fontFamily: "monospace", fontSize: 13 }}>
                    ${formatUSDC(k.total_profit)}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      {/* Recent liquidations */}
      <section>
        <h2 style={sectionTitle}>Recent Liquidations</h2>
        <LiquidationFeed liquidations={liquidations} limit={5} />
      </section>
    </div>
  );
}
