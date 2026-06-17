"use client";

import { useEffect, useState } from "react";
import {
  PerformanceData,
  KeeperStat,
  fetchPerformance,
  formatUSDC,
  shortAddress,
  successRate,
  sharePriceSeries,
  vaultReturn,
} from "../../lib/api";
import { queryKeeper } from "../../lib/stellar";
import {
  Btn,
  Card,
  CardHead,
  StatTile,
  StatusDot,
  Pill,
  LineChart,
  MiniBars,
  Eyebrow,
  keeperColor,
  successColor,
} from "../components/ds";

interface Props {
  initialData: PerformanceData | null;
}

const EMPTY: PerformanceData = {
  vault: null,
  depositors: [],
  keeper_stats: {},
  liquidations: [],
};

// On-chain keeper overrides read directly from KeeperRegistry. These are the
// authoritative figures (stake, executions, fills, active draw, profit) and win
// over whatever the keeper API reports.
type ChainKeeper = {
  stake: number;
  total_executions: number;
  successful_fills: number;
  has_active_draw: boolean;
  total_profit: number;
};

export default function PerformanceDashboard({ initialData }: Props) {
  const [perf, setPerf] = useState<PerformanceData>(initialData ?? EMPTY);
  const [live, setLive] = useState(!!initialData);
  const [lastUpdate, setLastUpdate] = useState<Date>(new Date());
  const [chainKeepers, setChainKeepers] = useState<Record<string, ChainKeeper>>({});

  // Poll the keeper performance API every 15s.
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

  const keeperStats = perf.keeper_stats ?? {};

  // Pull authoritative keeper info from the registry contract directly (~30s).
  useEffect(() => {
    let cancelled = false;
    const refreshChain = async () => {
      const keeperAddrs = Object.values(keeperStats)
        .map((k) => k.address)
        .filter(Boolean);
      if (!keeperAddrs.length) return;
      const results = await Promise.all(keeperAddrs.map((a) => queryKeeper(a)));
      if (cancelled) return;
      const next: Record<string, ChainKeeper> = {};
      results.forEach((r, i) => {
        if (r) {
          next[keeperAddrs[i]] = {
            stake: r.stake,
            total_executions: r.totalExecutions,
            successful_fills: r.successfulFills,
            has_active_draw: r.hasActiveDraw,
            total_profit: r.totalProfit,
          };
        }
      });
      if (Object.keys(next).length > 0) setChainKeepers(next);
    };
    refreshChain();
    const t = setInterval(refreshChain, 30_000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [JSON.stringify(Object.values(keeperStats).map((k) => k.address))]);

  const vault = perf.vault;
  const depositors = perf.depositors ?? [];
  const liquidations = perf.liquidations ?? [];

  const tvl = vault?.total_usdc ?? 0;
  const totalProfit = vault?.total_profit ?? 0;

  // Vault returns series (reconstructed from realized on-chain profit).
  const series = sharePriceSeries(perf);
  const ret = vaultReturn(series);
  const hasSeries = series.length >= 2;

  // Merge each keeper's on-chain figures over the API figures for display.
  const mergedKeepers = Object.values(keeperStats).map((ks: KeeperStat) => {
    const chain = chainKeepers[ks.address];
    const stake = chain?.stake ?? ks.stake ?? 0;
    const exec = chain?.total_executions ?? ks.total_executions ?? ks.liquidations;
    const fills = chain?.successful_fills ?? ks.successful_fills ?? ks.liquidations;
    const profit = chain?.total_profit ?? ks.total_profit;
    const active = chain?.has_active_draw ?? ks.has_active_draw ?? false;
    const rate = exec > 0 ? successRate(exec, fills) : null; // null when no executions
    return { name: ks.name, address: ks.address, stake, exec, fills, profit, active, rate };
  });

  const sortedKeepers = [...mergedKeepers].sort((a, b) => b.profit - a.profit);

  // Top depositors by USDC value (for the side panel summary).
  const topDepositors = [...depositors]
    .sort((a, b) => b.usdc_value - a.usdc_value)
    .slice(0, 5);
  const maxDepValue = topDepositors.reduce((m, d) => Math.max(m, d.usdc_value), 0);

  const recentLiqs = [...liquidations]
    .sort((a, b) => new Date(b.ts).getTime() - new Date(a.ts).getTime())
    .slice(0, 20);

  const wrap: React.CSSProperties = { maxWidth: 1100, margin: "0 auto", padding: "32px 24px 8px" };
  const th: React.CSSProperties = {
    padding: "10px 16px",
    textAlign: "left",
    fontFamily: "var(--font-mono)",
    fontSize: 10,
    letterSpacing: "0.08em",
    textTransform: "uppercase",
    color: "var(--text-dim)",
    borderBottom: "1px solid var(--border)",
    whiteSpace: "nowrap",
  };
  const td: React.CSSProperties = {
    padding: "11px 16px",
    fontFamily: "var(--font-mono)",
    fontSize: 12,
    color: "var(--text)",
    borderBottom: "1px solid var(--border)",
    fontVariantNumeric: "tabular-nums",
  };
  const emptyCell: React.CSSProperties = {
    padding: "20px 16px",
    fontFamily: "var(--font-mono)",
    fontSize: 12,
    color: "var(--text-mute)",
    textAlign: "center",
  };

  return (
    <div style={wrap}>
      {/* ── Header ─────────────────────────────────────────────────────── */}
      <div
        style={{
          display: "flex",
          flexWrap: "wrap",
          gap: 16,
          alignItems: "flex-end",
          justifyContent: "space-between",
          marginBottom: 28,
        }}
      >
        <div>
          <Eyebrow style={{ marginBottom: 10 }}>
            {live ? "LIVE — UPDATED EVERY 15s" : "VAULT PERFORMANCE"}
          </Eyebrow>
          <h1
            style={{
              fontFamily: "var(--font-display)",
              fontSize: "clamp(1.5rem, 3vw, 2rem)",
              fontWeight: 700,
              letterSpacing: "-0.01em",
              color: "var(--text)",
              margin: 0,
            }}
          >
            Performance Dashboard
          </h1>
        </div>
        <div style={{ display: "flex", alignItems: "center", gap: 8 }}>
          <StatusDot color={live ? "var(--accent)" : "var(--amber)"} />
          <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--text-dim)" }}>
            {live ? `LIVE · ${lastUpdate.toLocaleTimeString()}` : "OFFLINE · awaiting keeper API"}
          </span>
        </div>
      </div>

      {/* ── Summary stat tiles ─────────────────────────────────────────── */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))",
          gap: 16,
          marginBottom: 24,
        }}
      >
        <StatTile label="TVL" value={`$${formatUSDC(tvl)}`} sub="total value locked" />
        <StatTile
          label="Cumulative Profit"
          value={`${totalProfit >= 0 ? "+" : "-"}$${formatUSDC(Math.abs(totalProfit))}`}
          accent={totalProfit > 0}
          sub="realized to depositors"
        />
        <StatTile label="Depositors" value={`${depositors.length}`} sub="active accounts" />
        <StatTile label="Liquidations" value={`${liquidations.length}`} sub="recorded fills" />
      </div>

      {/* ── Vault returns chart ────────────────────────────────────────── */}
      <Card style={{ marginBottom: 24 }}>
        <CardHead
          right={
            <span style={{ color: ret.pct >= 0 ? "var(--accent)" : "var(--red)", textTransform: "none", letterSpacing: 0 }}>
              {ret.pct >= 0 ? "+" : ""}
              {ret.pct.toFixed(2)}% {ret.annualized ? "APY" : "to date"}
            </span>
          }
        >
          Vault Returns
        </CardHead>
        <div style={{ padding: 16 }}>
          {hasSeries ? (
            <LineChart
              data={series.map((p) => ({ value: p.sharePrice, label: p.label }))}
              height={240}
              variant="area"
              label="sharePrice"
              valueFmt={(v) => v.toFixed(4)}
            />
          ) : (
            <div
              style={{
                height: 240,
                display: "flex",
                alignItems: "center",
                justifyContent: "center",
                color: "var(--text-mute)",
                fontFamily: "var(--font-mono)",
                fontSize: 12,
              }}
            >
              Not enough liquidation history yet to chart returns.
            </div>
          )}
        </div>
      </Card>

      {/* ── Keepers + Top depositors (left) / Recent liquidations (right) ─ */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(340px, 1fr))",
          gap: 24,
          marginBottom: 24,
        }}
      >
        {/* left column */}
        <div style={{ display: "flex", flexDirection: "column", gap: 24 }}>
          {/* Keepers */}
          <Card>
            <CardHead
              right={
                Object.keys(keeperStats).length > 0 ? (
                  <span style={{ display: "flex", alignItems: "center", gap: 6, color: "var(--accent)", textTransform: "none", letterSpacing: 0 }}>
                    <StatusDot size={6} />
                    <span style={{ fontSize: 10 }}>
                      {mergedKeepers.filter((k) => !k.active).length === mergedKeepers.length ? "all idle" : "active"}
                    </span>
                  </span>
                ) : undefined
              }
            >
              Keepers ({Object.keys(keeperStats).length})
            </CardHead>
            {sortedKeepers.length === 0 ? (
              <div style={emptyCell}>No keepers registered yet.</div>
            ) : (
              sortedKeepers.map((k, i) => (
                <div
                  key={k.address}
                  style={{
                    padding: 16,
                    borderBottom: i < sortedKeepers.length - 1 ? "1px solid var(--border)" : "none",
                  }}
                >
                  <div style={{ display: "flex", justifyContent: "space-between", alignItems: "center", marginBottom: 12, gap: 12 }}>
                    <div style={{ display: "flex", alignItems: "center", gap: 8, minWidth: 0 }}>
                      <span style={{ fontFamily: "var(--font-mono)", fontSize: 13, color: keeperColor(k.name) }}>{k.name}</span>
                      <span
                        title={k.address}
                        style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--accent)", overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}
                      >
                        {shortAddress(k.address)}
                      </span>
                    </div>
                    <Pill color={k.active ? "var(--amber)" : "var(--text-dim)"}>
                      {k.active ? "ACTIVE DRAW" : "IDLE"}
                    </Pill>
                  </div>
                  <div style={{ display: "grid", gridTemplateColumns: "repeat(4, 1fr)", gap: 12 }}>
                    <KeeperMetric label="Stake" value={k.stake > 0 ? `$${formatUSDC(k.stake)}` : "—"} />
                    <KeeperMetric label="Fills" value={`${k.fills}/${k.exec}`} />
                    <KeeperMetric
                      label="Success"
                      value={k.rate == null ? "—" : `${(k.rate * 100).toFixed(0)}%`}
                      color={k.rate == null ? "var(--text-mute)" : successColor(k.rate * 100)}
                    />
                    <KeeperMetric label="Profit" value={`$${formatUSDC(k.profit)}`} color={k.profit > 0 ? "var(--accent)" : "var(--text)"} />
                  </div>
                </div>
              ))
            )}
          </Card>

          {/* Top depositors */}
          <Card>
            <CardHead>Top Depositors</CardHead>
            {topDepositors.length === 0 ? (
              <div style={emptyCell}>No depositors yet.</div>
            ) : (
              <table style={{ width: "100%", borderCollapse: "collapse" }}>
                <thead>
                  <tr>
                    <th style={th}>Address</th>
                    <th style={{ ...th, textAlign: "right" }}>USDC Value</th>
                    <th style={{ ...th, textAlign: "right", width: 90 }}>Share</th>
                  </tr>
                </thead>
                <tbody>
                  {topDepositors.map((d, i) => (
                    <tr key={d.address}>
                      <td
                        style={{ ...td, color: "var(--accent)", borderBottom: i < topDepositors.length - 1 ? td.borderBottom : "none" }}
                        title={d.address}
                      >
                        {shortAddress(d.address)}
                      </td>
                      <td style={{ ...td, textAlign: "right", borderBottom: i < topDepositors.length - 1 ? td.borderBottom : "none" }}>
                        ${formatUSDC(d.usdc_value)}
                      </td>
                      <td style={{ ...td, textAlign: "right", borderBottom: i < topDepositors.length - 1 ? td.borderBottom : "none" }}>
                        <MiniBars value={d.usdc_value} max={maxDepValue} />
                      </td>
                    </tr>
                  ))}
                </tbody>
              </table>
            )}
          </Card>
        </div>

        {/* right column — recent liquidations */}
        <Card>
          <CardHead>Recent Liquidations</CardHead>
          {recentLiqs.length === 0 ? (
            <div style={emptyCell}>No liquidations recorded yet.</div>
          ) : (
            <table style={{ width: "100%", borderCollapse: "collapse" }}>
              <thead>
                <tr>
                  <th style={th}>Time</th>
                  <th style={th}>User</th>
                  <th style={{ ...th, textAlign: "right" }}>Amount</th>
                  <th style={{ ...th, textAlign: "right" }}>Profit</th>
                </tr>
              </thead>
              <tbody>
                {recentLiqs.map((l, i) => {
                  const profit = l.proceeds - l.drew;
                  const last = i === recentLiqs.length - 1;
                  const bb = last ? "none" : td.borderBottom;
                  return (
                    <tr key={`${l.ts}-${l.block}-${i}`}>
                      <td style={{ ...td, color: "var(--text-dim)", borderBottom: bb }}>
                        {new Date(l.ts).toLocaleTimeString()}
                      </td>
                      <td style={{ ...td, color: "var(--accent)", borderBottom: bb }} title={l.user}>
                        {shortAddress(l.user)}
                      </td>
                      <td style={{ ...td, textAlign: "right", borderBottom: bb }}>${formatUSDC(l.drew)}</td>
                      <td
                        style={{
                          ...td,
                          textAlign: "right",
                          color: profit > 0 ? "var(--accent)" : profit < 0 ? "var(--red)" : "var(--text-dim)",
                          borderBottom: bb,
                        }}
                      >
                        {profit >= 0 ? "+" : "-"}${formatUSDC(Math.abs(profit))}
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </Card>
      </div>

      {/* ── Full depositors table ──────────────────────────────────────── */}
      <Card style={{ marginBottom: 24 }}>
        <CardHead>Depositors ({depositors.length})</CardHead>
        <div style={{ overflowX: "auto" }}>
          {depositors.length === 0 ? (
            <div style={emptyCell}>No depositors yet.</div>
          ) : (
            <table style={{ width: "100%", borderCollapse: "collapse", minWidth: 640 }}>
              <thead>
                <tr>
                  <th style={{ ...th, width: 40 }}>#</th>
                  <th style={th}>Address</th>
                  <th style={{ ...th, textAlign: "right" }}>Shares</th>
                  <th style={{ ...th, textAlign: "right" }}>USDC Value</th>
                  <th style={{ ...th, textAlign: "right" }}>PnL</th>
                </tr>
              </thead>
              <tbody>
                {depositors.map((d, i) => {
                  const last = i === depositors.length - 1;
                  const bb = last ? "none" : td.borderBottom;
                  return (
                    <tr key={d.address}>
                      <td style={{ ...td, color: "var(--text-dim)", borderBottom: bb }}>{i + 1}</td>
                      <td style={{ ...td, color: "var(--accent)", borderBottom: bb }} title={d.address}>
                        {shortAddress(d.address)}
                      </td>
                      <td style={{ ...td, textAlign: "right", borderBottom: bb }}>{(d.shares / 1e7).toFixed(2)}</td>
                      <td style={{ ...td, textAlign: "right", borderBottom: bb }}>${formatUSDC(d.usdc_value)}</td>
                      <td
                        style={{
                          ...td,
                          textAlign: "right",
                          color: d.pnl_pct > 0 ? "var(--accent)" : d.pnl_pct < 0 ? "var(--red)" : "var(--text-dim)",
                          borderBottom: bb,
                        }}
                      >
                        {d.pnl_pct >= 0 ? "+" : ""}
                        {d.pnl_pct.toFixed(2)}%
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      </Card>

      {/* ── Full keepers table ─────────────────────────────────────────── */}
      <Card style={{ marginBottom: 24 }}>
        <CardHead>Keeper Stats ({Object.keys(keeperStats).length})</CardHead>
        <div style={{ overflowX: "auto" }}>
          {sortedKeepers.length === 0 ? (
            <div style={emptyCell}>No keepers registered yet.</div>
          ) : (
            <table style={{ width: "100%", borderCollapse: "collapse", minWidth: 680 }}>
              <thead>
                <tr>
                  <th style={th}>Name</th>
                  <th style={th}>Address</th>
                  <th style={{ ...th, textAlign: "right" }}>Stake</th>
                  <th style={{ ...th, textAlign: "right" }}>Fills</th>
                  <th style={{ ...th, textAlign: "right" }}>Success</th>
                  <th style={{ ...th, textAlign: "right" }}>Profit</th>
                  <th style={{ ...th, textAlign: "center" }}>Status</th>
                </tr>
              </thead>
              <tbody>
                {sortedKeepers.map((k, i) => {
                  const last = i === sortedKeepers.length - 1;
                  const bb = last ? "none" : td.borderBottom;
                  return (
                    <tr key={k.address}>
                      <td style={{ ...td, color: keeperColor(k.name), borderBottom: bb }}>{k.name}</td>
                      <td style={{ ...td, color: "var(--accent)", borderBottom: bb }} title={k.address}>
                        {shortAddress(k.address)}
                      </td>
                      <td style={{ ...td, textAlign: "right", borderBottom: bb }}>
                        {k.stake > 0 ? `$${formatUSDC(k.stake)}` : "—"}
                      </td>
                      <td style={{ ...td, textAlign: "right", borderBottom: bb }}>
                        {k.fills}/{k.exec}
                      </td>
                      <td
                        style={{
                          ...td,
                          textAlign: "right",
                          color: k.rate == null ? "var(--text-mute)" : successColor(k.rate * 100),
                          borderBottom: bb,
                        }}
                      >
                        {k.rate == null ? "—" : `${(k.rate * 100).toFixed(1)}%`}
                      </td>
                      <td
                        style={{
                          ...td,
                          textAlign: "right",
                          color: k.profit > 0 ? "var(--accent)" : "var(--text)",
                          borderBottom: bb,
                        }}
                      >
                        ${formatUSDC(k.profit)}
                      </td>
                      <td style={{ ...td, textAlign: "center", borderBottom: bb }}>
                        <Pill color={k.active ? "var(--amber)" : "var(--text-dim)"}>
                          {k.active ? "ACTIVE DRAW" : "IDLE"}
                        </Pill>
                      </td>
                    </tr>
                  );
                })}
              </tbody>
            </table>
          )}
        </div>
      </Card>

      {/* ── CTAs ───────────────────────────────────────────────────────── */}
      <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
        <Btn href="/dashboard" small primary>
          Open full dashboard →
        </Btn>
        <Btn href="/dashboard/keepers" small>
          Keeper leaderboard
        </Btn>
      </div>
    </div>
  );
}

// ── small keeper metric cell ──────────────────────────────────────────────
function KeeperMetric({ label, value, color }: { label: string; value: React.ReactNode; color?: string }) {
  return (
    <div>
      <div
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          letterSpacing: "0.06em",
          textTransform: "uppercase",
          color: "var(--text-dim)",
          marginBottom: 4,
        }}
      >
        {label}
      </div>
      <div
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 15,
          color: color ?? "var(--text)",
          fontVariantNumeric: "tabular-nums",
        }}
      >
        {value}
      </div>
    </div>
  );
}
