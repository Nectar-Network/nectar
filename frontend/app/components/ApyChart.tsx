"use client";

import {
  Area,
  AreaChart,
  CartesianGrid,
  ResponsiveContainer,
  Tooltip,
  XAxis,
  YAxis,
} from "recharts";
import { PerformanceData, sharePriceSeries, vaultReturn } from "../../lib/api";

/**
 * APY history chart. Plots the vault share price reconstructed from realized
 * liquidation profit (lib/api.sharePriceSeries) and shows the trailing return.
 * Short windows report cumulative return rather than a misleading annualized
 * figure. All figures derive from real on-chain outcomes — nothing is synthesized.
 */
export default function ApyChart({ perf }: { perf: PerformanceData }) {
  const series = sharePriceSeries(perf);
  const ret = vaultReturn(series);
  const hasSeries = series.length >= 2;

  return (
    <div
      style={{
        border: "1px solid var(--border)",
        borderRadius: 4,
        background: "rgba(255,255,255,0.02)",
        padding: 20,
      }}
    >
      <div
        style={{
          display: "flex",
          justifyContent: "space-between",
          alignItems: "baseline",
          marginBottom: 16,
        }}
      >
        <div>
          <div
            style={{
              fontSize: 11,
              color: "var(--text-dim)",
              letterSpacing: "0.08em",
              textTransform: "uppercase",
            }}
          >
            {ret.annualized ? "Trailing APY" : "Return to date"}
          </div>
          <div
            style={{
              fontSize: 28,
              fontFamily: "monospace",
              fontWeight: 600,
              color: ret.pct >= 0 ? "var(--accent)" : "#ff6b6b",
            }}
          >
            {ret.pct >= 0 ? "+" : ""}
            {ret.pct.toFixed(2)}%
          </div>
        </div>
        <div style={{ fontSize: 11, color: "var(--text-dim)", fontFamily: "monospace" }}>
          {ret.annualized ? "annualized · from realized profit" : "cumulative · not annualized"}
        </div>
      </div>

      {hasSeries ? (
        <ResponsiveContainer width="100%" height={240}>
          <AreaChart data={series} margin={{ top: 8, right: 8, bottom: 0, left: -8 }}>
            <defs>
              <linearGradient id="apyFill" x1="0" y1="0" x2="0" y2="1">
                <stop offset="0%" stopColor="var(--accent)" stopOpacity={0.35} />
                <stop offset="100%" stopColor="var(--accent)" stopOpacity={0} />
              </linearGradient>
            </defs>
            <CartesianGrid stroke="var(--border)" strokeDasharray="3 3" vertical={false} />
            <XAxis
              dataKey="label"
              tick={{ fill: "var(--text-dim)", fontSize: 10, fontFamily: "monospace" }}
              stroke="var(--border)"
              minTickGap={24}
            />
            <YAxis
              domain={["auto", "auto"]}
              tick={{ fill: "var(--text-dim)", fontSize: 10, fontFamily: "monospace" }}
              stroke="var(--border)"
              tickFormatter={(v) => Number(v).toFixed(3)}
              width={48}
            />
            <Tooltip
              contentStyle={{
                background: "var(--bg)",
                border: "1px solid var(--border)",
                borderRadius: 4,
                fontFamily: "monospace",
                fontSize: 12,
              }}
              labelStyle={{ color: "var(--text-dim)" }}
              formatter={(v) => [Number(v as number).toFixed(5), "share price"]}
            />
            <Area
              type="monotone"
              dataKey="sharePrice"
              stroke="var(--accent)"
              strokeWidth={2}
              fill="url(#apyFill)"
            />
          </AreaChart>
        </ResponsiveContainer>
      ) : (
        <div
          style={{
            height: 240,
            display: "flex",
            alignItems: "center",
            justifyContent: "center",
            color: "var(--text-dim)",
            fontFamily: "monospace",
            fontSize: 12,
          }}
        >
          Not enough liquidation history yet to chart returns.
        </div>
      )}
    </div>
  );
}
