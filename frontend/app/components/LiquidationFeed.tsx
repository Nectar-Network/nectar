"use client";

import { LiquidationRecord, formatUSDC, shortAddress } from "../../lib/api";

const EXPLORER_ACCOUNT = "https://stellar.expert/explorer/testnet/account/";

const cell: React.CSSProperties = {
  padding: "8px 12px",
  borderBottom: "1px solid var(--border)",
  fontFamily: "monospace",
  fontSize: "13px",
  color: "var(--text)",
};

const headerCell: React.CSSProperties = {
  ...cell,
  color: "var(--text-dim)",
  fontSize: "11px",
  textTransform: "uppercase",
  letterSpacing: "0.08em",
};

/**
 * Liquidation event feed. Presentational — the parent polls fetchPerformance and
 * passes the latest liquidations; this renders them newest-first. Each
 * liquidated position links to Stellar Expert (the LiquidationRecord carries no
 * tx hash, so we link the position account).
 */
export default function LiquidationFeed({
  liquidations,
  limit = 20,
}: {
  liquidations: LiquidationRecord[];
  limit?: number;
}) {
  const rows = [...(liquidations ?? [])].reverse().slice(0, limit);

  if (rows.length === 0) {
    return (
      <div
        style={{
          border: "1px solid var(--border)",
          borderRadius: 4,
          padding: 24,
          textAlign: "center",
          color: "var(--text-dim)",
          fontFamily: "monospace",
          fontSize: 12,
        }}
      >
        No liquidations recorded yet.
      </div>
    );
  }

  return (
    <div style={{ border: "1px solid var(--border)", borderRadius: 4, overflow: "hidden" }}>
      <table style={{ width: "100%", borderCollapse: "collapse" }}>
        <thead>
          <tr style={{ background: "rgba(255,255,255,0.03)" }}>
            <th style={{ ...headerCell, textAlign: "left" }}>Position</th>
            <th style={{ ...headerCell, textAlign: "right" }}>Block</th>
            <th style={{ ...headerCell, textAlign: "right" }}>Drew</th>
            <th style={{ ...headerCell, textAlign: "right" }}>Proceeds</th>
            <th style={{ ...headerCell, textAlign: "right" }}>Profit</th>
            <th style={{ ...headerCell, textAlign: "right" }}>Time</th>
          </tr>
        </thead>
        <tbody>
          {rows.map((liq, idx) => {
            const profit = liq.proceeds - liq.drew;
            return (
              <tr
                key={`${liq.user}-${liq.block}-${idx}`}
                style={{ background: idx % 2 === 0 ? "transparent" : "rgba(255,255,255,0.01)" }}
              >
                <td style={cell}>
                  <a
                    href={`${EXPLORER_ACCOUNT}${liq.user}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    title={liq.user}
                    style={{ color: "var(--accent)", textDecoration: "none" }}
                  >
                    {shortAddress(liq.user)}
                  </a>
                </td>
                <td style={{ ...cell, textAlign: "right", color: "var(--text-dim)" }}>
                  {liq.block.toLocaleString()}
                </td>
                <td style={{ ...cell, textAlign: "right" }}>${formatUSDC(liq.drew)}</td>
                <td
                  style={{
                    ...cell,
                    textAlign: "right",
                    color: liq.proceeds > liq.drew ? "var(--accent)" : "var(--text)",
                  }}
                >
                  ${formatUSDC(liq.proceeds)}
                </td>
                <td
                  style={{
                    ...cell,
                    textAlign: "right",
                    color: profit > 0 ? "var(--accent)" : "var(--text-dim)",
                  }}
                >
                  {profit >= 0 ? "+" : ""}${formatUSDC(profit)}
                </td>
                <td style={{ ...cell, textAlign: "right", color: "var(--text-dim)" }}>
                  {new Date(liq.ts).toLocaleString()}
                </td>
              </tr>
            );
          })}
        </tbody>
      </table>
    </div>
  );
}
