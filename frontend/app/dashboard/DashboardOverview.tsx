"use client";

import { useEffect, useState } from "react";
import {
  PerformanceData,
  KeeperStat,
  fetchPerformance,
  formatUSDC,
  shortAddress,
  sharePriceSeries,
  vaultReturn,
  successRate,
} from "../../lib/api";
import {
  Card,
  Btn,
  StatusDot,
  Pill,
  Eyebrow,
  StatTile,
  CardHead,
  LineChart,
  fmtClock,
  keeperColor,
  successColor,
} from "../components/ds";

const EMPTY: PerformanceData = { vault: null, depositors: [], keeper_stats: {}, liquidations: [] };

const EXPLORER_ACCOUNT = "https://stellar.expert/explorer/testnet/account/";
const EXPLORER_TX = "https://stellar.expert/explorer/testnet/tx/";

// ── Page header — Syne hero + live pill ───────────────────────────────────────
function PageHeader({ live, liqCount, lastUpdate }: { live: boolean; liqCount: number; lastUpdate: Date }) {
  return (
    <div
      style={{
        display: "flex",
        alignItems: "flex-end",
        justifyContent: "space-between",
        gap: 20,
        flexWrap: "wrap",
        marginBottom: 28,
      }}
    >
      <div>
        <Eyebrow style={{ marginBottom: 10 }}>Dashboard · Soroban Testnet</Eyebrow>
        <h1
          style={{
            fontFamily: "var(--font-display)",
            fontWeight: 700,
            fontSize: "clamp(2rem, 4vw, 3rem)",
            lineHeight: 1.02,
            letterSpacing: "-0.01em",
            color: "var(--text)",
            margin: 0,
          }}
        >
          Network Overview
        </h1>
      </div>
      <div
        style={{
          display: "flex",
          alignItems: "center",
          gap: 10,
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          color: "var(--text-dim)",
          padding: "7px 12px",
          border: "1px solid var(--border)",
          borderRadius: 2,
          whiteSpace: "nowrap",
        }}
      >
        <StatusDot color={live ? "var(--accent)" : "var(--amber)"} />
        <span>
          {live ? `live · ${lastUpdate.toLocaleTimeString()}` : "awaiting keeper API"} · {liqCount} lifetime fills
        </span>
      </div>
    </div>
  );
}

// ── APY feature — big trailing return + 30D/90D LineChart ─────────────────────
function ApyFeature({ perf }: { perf: PerformanceData }) {
  const [range, setRange] = useState<30 | 90>(30);

  const fullSeries = sharePriceSeries(perf);
  // sharePriceSeries points carry epoch-ms `ts`; slice by trailing window.
  const cutoff = Date.now() - range * 86_400_000;
  const windowed = fullSeries.filter((p) => p.ts >= cutoff);
  // Keep at least the last two points so a short on-chain history still draws.
  const sliced = windowed.length >= 2 ? windowed : fullSeries.slice(-2);

  const ret = vaultReturn(sliced.length >= 2 ? sliced : fullSeries);
  const hasSeries = fullSeries.length >= 2;
  const chartData = sliced.map((p) => ({ value: p.sharePrice, label: p.label }));

  const current = fullSeries.length ? fullSeries[fullSeries.length - 1].sharePrice : 1;
  const sinceInception = (current - 1) * 100;

  const retLabel = ret.annualized ? `Trailing ${range}d APY` : "Return to date";
  const retColor = ret.pct >= 0 ? "var(--accent)" : "var(--red)";

  const RangeBtn = ({ v, children }: { v: 30 | 90; children: React.ReactNode }) => {
    const on = range === v;
    return (
      <button
        type="button"
        onClick={() => setRange(v)}
        style={{
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          padding: "4px 10px",
          borderRadius: 2,
          cursor: "pointer",
          background: on ? "var(--accent-fill)" : "transparent",
          color: on ? "var(--accent)" : "var(--text-dim)",
          border: `1px solid ${on ? "var(--accent)" : "var(--border)"}`,
        }}
      >
        {children}
      </button>
    );
  };

  const stats: [string, string][] = [
    ["Share price", `$${current.toFixed(4)}`],
    [ret.annualized ? "Window" : "Sample", ret.days >= 1 ? `${ret.days.toFixed(0)}d` : "<1d"],
    ["Since inception", `${sinceInception >= 0 ? "+" : ""}${sinceInception.toFixed(2)}%`],
  ];

  return (
    <Card style={{ padding: 0 }}>
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-start",
          padding: "20px 22px 8px",
          flexWrap: "wrap",
          gap: 16,
        }}
      >
        <div>
          <div
            style={{
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              letterSpacing: "0.08em",
              textTransform: "uppercase",
              color: "var(--text-dim)",
              marginBottom: 12,
            }}
          >
            {retLabel}
          </div>
          <div style={{ display: "flex", alignItems: "baseline", gap: 14 }}>
            <span
              style={{
                fontFamily: "var(--font-display)",
                fontWeight: 700,
                fontSize: 52,
                lineHeight: 1,
                color: hasSeries ? retColor : "var(--text-mute)",
                fontVariantNumeric: "tabular-nums",
              }}
            >
              {hasSeries ? `${ret.pct >= 0 ? "+" : ""}${ret.pct.toFixed(2)}%` : "—"}
            </span>
            <span
              style={{
                fontFamily: "var(--font-mono)",
                fontSize: 13,
                whiteSpace: "nowrap",
                color: "var(--text-mute)",
              }}
            >
              {hasSeries ? (ret.annualized ? "annualized" : "cumulative · not annualized") : "no data yet"}
            </span>
          </div>
          <div style={{ display: "flex", gap: 28, marginTop: 18, flexWrap: "wrap" }}>
            {stats.map(([k, v]) => (
              <div key={k}>
                <div
                  style={{
                    fontFamily: "var(--font-mono)",
                    fontSize: 10,
                    letterSpacing: "0.06em",
                    whiteSpace: "nowrap",
                    textTransform: "uppercase",
                    color: "var(--text-mute)",
                    marginBottom: 5,
                  }}
                >
                  {k}
                </div>
                <div style={{ fontFamily: "var(--font-mono)", fontSize: 15, color: "var(--text)", fontVariantNumeric: "tabular-nums" }}>
                  {hasSeries ? v : "—"}
                </div>
              </div>
            ))}
          </div>
        </div>
        <div style={{ display: "flex", gap: 6 }}>
          <RangeBtn v={30}>30D</RangeBtn>
          <RangeBtn v={90}>90D</RangeBtn>
        </div>
      </div>
      <div style={{ padding: "4px 12px 12px" }}>
        {chartData.length >= 2 ? (
          <LineChart
            data={chartData}
            height={208}
            variant="area"
            label="apy"
            valueFmt={(v) => v.toFixed(4)}
            baselineZero={false}
          />
        ) : (
          <div
            style={{
              height: 208,
              display: "flex",
              alignItems: "center",
              justifyContent: "center",
              fontFamily: "var(--font-mono)",
              fontSize: 12,
              color: "var(--text-dim)",
            }}
          >
            Not enough liquidation history yet to chart returns.
          </div>
        )}
      </div>
    </Card>
  );
}

// ── Top keepers — compact leaderboard preview ─────────────────────────────────
function TopKeepers({ stats }: { stats: Record<string, KeeperStat> }) {
  const rows = Object.values(stats)
    .slice()
    .sort((a, b) => b.total_profit - a.total_profit)
    .slice(0, 5);

  const cols = "26px 1fr 0.7fr 0.9fr";

  return (
    <Card style={{ padding: 0 }}>
      <CardHead
        right={
          <a
            href="/dashboard/keepers"
            style={{ color: "var(--accent)", textDecoration: "none", textTransform: "none", letterSpacing: 0 }}
          >
            Leaderboard →
          </a>
        }
      >
        Top Keepers
      </CardHead>
      <div
        style={{
          display: "grid",
          gridTemplateColumns: cols,
          padding: "10px 16px",
          fontFamily: "var(--font-mono)",
          fontSize: 10,
          letterSpacing: "0.08em",
          color: "var(--text-dim)",
          borderBottom: "1px solid var(--border)",
          textTransform: "uppercase",
        }}
      >
        <span>#</span>
        <span>Operator</span>
        <span style={{ textAlign: "right" }}>Win%</span>
        <span style={{ textAlign: "right" }}>Profit</span>
      </div>
      {rows.length === 0 && (
        <div
          style={{
            padding: "20px 16px",
            fontFamily: "var(--font-mono)",
            fontSize: 12,
            color: "var(--text-dim)",
            textAlign: "center",
          }}
        >
          No keepers registered yet.
        </div>
      )}
      {rows.map((k, i) => {
        // Win rate only when the API actually surfaces execution counts —
        // falling back to liquidations for both terms would always fabricate
        // 100% (the full leaderboard reads the real rate on-chain).
        const exec = k.total_executions;
        const fills = k.successful_fills ?? 0;
        const rate = exec != null && exec > 0 ? successRate(exec, fills) * 100 : null;
        const color = keeperColor(k.name);
        return (
          <div
            key={k.address || k.name}
            style={{
              display: "grid",
              gridTemplateColumns: cols,
              padding: "13px 16px",
              fontFamily: "var(--font-mono)",
              fontSize: 12,
              alignItems: "center",
              borderBottom: i < rows.length - 1 ? "1px solid var(--border)" : "none",
            }}
          >
            <span style={{ color: "var(--text-mute)" }}>{i + 1}</span>
            <a
              href={`${EXPLORER_ACCOUNT}${k.address}`}
              target="_blank"
              rel="noopener noreferrer"
              title={k.address}
              style={{ display: "flex", alignItems: "center", gap: 8, color, textDecoration: "none", minWidth: 0 }}
            >
              <StatusDot size={6} color={color} glow={!!k.has_active_draw} />
              <span style={{ overflow: "hidden", textOverflow: "ellipsis", whiteSpace: "nowrap" }}>
                {k.name || shortAddress(k.address)}
              </span>
              {k.has_active_draw && (
                <Pill color="var(--amber)" style={{ fontSize: 9 }}>
                  drawing
                </Pill>
              )}
            </a>
            <span style={{ textAlign: "right", color: rate == null ? "var(--text-mute)" : successColor(rate) }}>
              {rate == null ? "—" : `${rate.toFixed(0)}%`}
            </span>
            <span style={{ textAlign: "right", color: "var(--text)" }}>${formatUSDC(k.total_profit)}</span>
          </div>
        );
      })}
    </Card>
  );
}

// ── Recent liquidations — DS table (real LiquidationRecord shape) ─────────────
function RecentLiquidations({ perf }: { perf: PerformanceData }) {
  const rows = [...(perf.liquidations ?? [])].reverse().slice(0, 6);
  const cols = "84px 1.2fr 0.9fr 1fr 1fr 96px";

  return (
    <Card style={{ padding: 0, marginBottom: 28 }}>
      <CardHead
        right={
          <a
            href="/dashboard/liquidations"
            style={{ color: "var(--accent)", textDecoration: "none", textTransform: "none", letterSpacing: 0 }}
          >
            Full feed →
          </a>
        }
      >
        Recent Liquidations
      </CardHead>

      {rows.length === 0 ? (
        <div
          style={{
            padding: "28px 16px",
            textAlign: "center",
            fontFamily: "var(--font-mono)",
            fontSize: 12,
            color: "var(--text-dim)",
          }}
        >
          No liquidations recorded yet.
        </div>
      ) : (
        <div style={{ overflowX: "auto" }} className="thin-scroll">
          <div style={{ minWidth: 640 }}>
            <div
              style={{
                display: "grid",
                gridTemplateColumns: cols,
                padding: "10px 16px",
                fontFamily: "var(--font-mono)",
                fontSize: 10,
                letterSpacing: "0.08em",
                color: "var(--text-dim)",
                borderBottom: "1px solid var(--border)",
                textTransform: "uppercase",
              }}
            >
              <span>Time</span>
              <span>Position</span>
              <span style={{ textAlign: "right" }}>Block</span>
              <span style={{ textAlign: "right" }}>Drew</span>
              <span style={{ textAlign: "right" }}>Profit</span>
              <span style={{ textAlign: "right" }}>Tx</span>
            </div>
            {rows.map((l, i) => {
              const profit = l.proceeds - l.drew;
              const ts = new Date(l.ts);
              return (
                <div
                  key={`${l.user}-${l.block}-${i}`}
                  style={{
                    display: "grid",
                    gridTemplateColumns: cols,
                    padding: "13px 16px",
                    fontFamily: "var(--font-mono)",
                    fontSize: 12,
                    alignItems: "center",
                    borderBottom: i < rows.length - 1 ? "1px solid var(--border)" : "none",
                  }}
                >
                  <span style={{ color: "var(--text-dim)" }}>{fmtClock(ts)}</span>
                  <a
                    href={`${EXPLORER_ACCOUNT}${l.user}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    title={l.user}
                    style={{ color: "var(--accent)", textDecoration: "none" }}
                  >
                    {shortAddress(l.user)}
                  </a>
                  <span style={{ textAlign: "right", color: "var(--text-dim)" }}>{l.block.toLocaleString()}</span>
                  <span style={{ textAlign: "right", color: "var(--text)" }}>${formatUSDC(l.drew)}</span>
                  <span style={{ textAlign: "right", color: profit >= 0 ? "var(--accent)" : "var(--red)" }}>
                    {profit >= 0 ? "+" : "-"}${formatUSDC(Math.abs(profit))}
                  </span>
                  <a
                    href={l.tx_hash ? `${EXPLORER_TX}${l.tx_hash}` : `${EXPLORER_ACCOUNT}${l.user}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    title={l.tx_hash ?? l.user}
                    style={{
                      textAlign: "right",
                      color: l.tx_hash ? "var(--accent)" : "var(--text-mute)",
                      textDecoration: "none",
                    }}
                  >
                    {l.tx_hash ? "tx↗" : "view↗"}
                  </a>
                </div>
              );
            })}
          </div>
        </div>
      )}
    </Card>
  );
}

// ── Page ──────────────────────────────────────────────────────────────────────
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

  const keeperList = Object.values(keeperStats);
  const activeKeepers = keeperList.filter((k) => k.has_active_draw).length;
  const ret = vaultReturn(sharePriceSeries(perf));
  const apyHas = sharePriceSeries(perf).length >= 2;

  return (
    <div style={{ maxWidth: 1180, margin: "0 auto", padding: "34px 24px 64px" }}>
      <PageHeader live={live} liqCount={liquidations.length} lastUpdate={lastUpdate} />

      {/* Top stat tiles */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(170px, 1fr))",
          gap: 14,
          marginBottom: 20,
        }}
      >
        <StatTile label="TVL" value={`$${formatUSDC(tvl)}`} />
        <StatTile label="Depositors" value={String(depositors.length)} />
        <StatTile
          label="Active keepers"
          value={keeperList.length ? `${activeKeepers} / ${keeperList.length}` : "—"}
        />
        <StatTile label="Liquidations" value={String(liquidations.length)} />
        <StatTile label="Cumulative profit" value={`+$${formatUSDC(profit)}`} accent={profit > 0} />
        <StatTile
          label={ret.annualized ? "Trailing APY" : "Return to date"}
          value={apyHas ? `${ret.pct >= 0 ? "+" : ""}${ret.pct.toFixed(2)}%` : "—"}
          accent={apyHas && ret.pct > 0}
          sub={apyHas ? (ret.annualized ? "annualized" : "cumulative") : "no data yet"}
        />
      </div>

      {/* APY feature + top keepers */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "minmax(0, 1.3fr) minmax(0, 1fr)",
          gap: 20,
          marginBottom: 20,
        }}
        className="dash-split"
      >
        <ApyFeature perf={perf} />
        <TopKeepers stats={keeperStats} />
      </div>

      {/* Recent liquidations */}
      <RecentLiquidations perf={perf} />

      {/* Footer CTAs */}
      <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
        <Btn href="/dashboard/keepers" small>
          Keeper leaderboard →
        </Btn>
        <Btn href="/dashboard/depositor" small>
          Your position →
        </Btn>
        <Btn href="/vault" small primary>
          Deposit USDC
        </Btn>
      </div>

      <style>{`
        @media (max-width: 920px) {
          .dash-split { grid-template-columns: 1fr !important; }
        }
      `}</style>
    </div>
  );
}
