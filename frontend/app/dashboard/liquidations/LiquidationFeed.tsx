"use client";

import { useEffect, useMemo, useRef, useState } from "react";
import {
  PerformanceData,
  LiquidationRecord,
  fetchPerformance,
  formatUSDC,
  shortAddress,
} from "../../../lib/api";
import { useSSEEvents } from "../../../lib/sse";
import {
  Card,
  CardHead,
  StatTile,
  StatusDot,
  Eyebrow,
  fmtClock,
  fmtUSD,
} from "../../components/ds";

const EMPTY: PerformanceData = { vault: null, depositors: [], keeper_stats: {}, liquidations: [] };

const EXPLORER_ACCOUNT = "https://stellar.expert/explorer/testnet/account/";
const EXPLORER_TX = "https://stellar.expert/explorer/testnet/tx/";

interface Row {
  key: string;
  ts: Date | null;
  user: string;
  block: number;
  drew: number; // stroops
  proceeds: number; // stroops
  profit: number; // stroops
  txHash?: string; // fill transaction (when the keeper recorded it)
  keeper?: string; // filling keeper's name (when recorded)
}

// 7-col grid mirroring the design's LiqTable: time · keeper · position · drew · proceeds · profit · tx
const COLS = "82px 0.9fr 1.1fr 1fr 1fr 1fr 70px";
const MIN_W = 820;

function toRows(liqs: LiquidationRecord[]): Row[] {
  // Newest-first. The API returns oldest-first, so reverse.
  return [...(liqs ?? [])]
    .map((l, i) => {
      const t = l.ts ? new Date(l.ts) : null;
      return {
        key: `${l.user}-${l.block}-${i}`,
        ts: t && !Number.isNaN(t.getTime()) ? t : null,
        user: l.user,
        block: l.block,
        drew: l.drew,
        proceeds: l.proceeds,
        profit: l.proceeds - l.drew,
        txHash: l.tx_hash,
        keeper: l.keeper,
      };
    })
    .reverse();
}

export default function LiquidationFeed({
  initialData,
}: {
  initialData: PerformanceData | null;
}) {
  const [perf, setPerf] = useState<PerformanceData>(initialData ?? EMPTY);
  const [live, setLive] = useState<boolean>(!!initialData);
  const [lastUpdate, setLastUpdate] = useState<Date>(new Date());

  // Live ticker from the keeper SSE stream.
  const events = useSSEEvents(8);

  // Poll the keeper API every 15s for the authoritative liquidation list.
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

  const rows = useMemo(() => toRows(perf.liquidations), [perf.liquidations]);

  // Track newly-arrived rows so we can flash them when streaming.
  const seen = useRef<Set<string>>(new Set());
  const [flash, setFlash] = useState<Set<string>>(new Set());
  useEffect(() => {
    const fresh = new Set<string>();
    for (const r of rows) {
      if (!seen.current.has(r.key)) fresh.add(r.key);
      seen.current.add(r.key);
    }
    if (fresh.size) {
      setFlash(fresh);
      const t = setTimeout(() => setFlash(new Set()), 1400);
      return () => clearTimeout(t);
    }
    return undefined;
  }, [rows]);

  // Window stats — derived entirely from real records. No fabrication.
  const count = rows.length;
  const totalProfit = rows.reduce((s, r) => s + r.profit, 0) / 1e7;
  const avgProfit = count ? totalProfit / count : 0;
  const largest = count ? Math.max(...rows.map((r) => r.profit)) / 1e7 : 0;

  const tiles: { label: string; value: string; accent?: boolean }[] = [
    { label: "Fills in feed", value: count ? String(count) : "—" },
    { label: "Profit captured", value: count ? fmtUSD(totalProfit, { dp: 0 }) : "—", accent: count > 0 },
    { label: "Avg profit / fill", value: count ? fmtUSD(avgProfit, { dp: 0 }) : "—" },
    { label: "Largest fill", value: count ? fmtUSD(largest, { dp: 0 }) : "—" },
  ];

  return (
    <div style={{ maxWidth: 1180, margin: "0 auto", padding: "0 24px" }}>
      <div style={{ paddingTop: 34, paddingBottom: 64 }}>
        {/* Page head */}
        <div
          style={{
            marginBottom: 26,
            display: "flex",
            justifyContent: "space-between",
            alignItems: "flex-end",
            flexWrap: "wrap",
            gap: 16,
          }}
        >
          <div>
            <Eyebrow style={{ marginBottom: 10 }}>return_proceeds · vault events</Eyebrow>
            <h1
              style={{
                fontFamily: "var(--font-display)",
                fontWeight: 700,
                fontSize: "clamp(2rem, 4vw, 3rem)",
                letterSpacing: "-0.01em",
                color: "var(--text)",
                margin: 0,
              }}
            >
              Liquidation Feed
            </h1>
          </div>
          <div
            style={{
              display: "flex",
              alignItems: "center",
              gap: 10,
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: live ? "var(--accent)" : "var(--text-dim)",
              padding: "7px 12px",
              border: "1px solid var(--border)",
              borderRadius: 2,
              whiteSpace: "nowrap",
            }}
          >
            <StatusDot color={live ? "var(--accent)" : "var(--amber)"} />
            <span>
              {live ? `streaming · SSE · ${lastUpdate.toLocaleTimeString()}` : "offline — awaiting keeper API"}
            </span>
          </div>
        </div>

        {/* Window stat tiles */}
        <div
          style={{
            display: "grid",
            gridTemplateColumns: "repeat(4, 1fr)",
            gap: 14,
            marginBottom: 24,
          }}
          className="liq-grid-4"
        >
          {tiles.map((ti) => (
            <StatTile key={ti.label} label={ti.label} value={ti.value} accent={ti.accent} />
          ))}
        </div>

        {/* Live ticker — honest: shows real keeper SSE log lines, or a muted note */}
        <Card style={{ marginBottom: 24, padding: 0 }}>
          <CardHead
            right={
              <span style={{ textTransform: "none", letterSpacing: 0, color: "var(--text-mute)" }}>
                live keeper log
              </span>
            }
          >
            Activity stream
          </CardHead>
          <div
            style={{
              padding: "10px 16px",
              fontFamily: "var(--font-mono)",
              fontSize: 11,
              color: "var(--text-dim)",
              maxHeight: 132,
              overflowY: "auto",
              display: "flex",
              flexDirection: "column",
              gap: 5,
            }}
            className="thin-scroll"
          >
            {events.length === 0 ? (
              <span style={{ color: "var(--text-mute)" }}>awaiting keeper API stream…</span>
            ) : (
              [...events].reverse().map((e, i) => (
                <div key={i} style={{ display: "flex", gap: 8, alignItems: "baseline" }}>
                  <StatusDot size={5} color="var(--accent)" glow={false} style={{ flexShrink: 0 }} />
                  <span style={{ color: i === 0 ? "var(--text)" : "var(--text-dim)" }}>{e}</span>
                </div>
              ))
            )}
          </div>
        </Card>

        {/* Feed table */}
        <Card style={{ padding: 0 }}>
          <CardHead
            right={
              <span style={{ textTransform: "none", letterSpacing: 0, color: "var(--text-mute)" }}>
                {count} {count === 1 ? "event" : "events"}
              </span>
            }
          >
            All liquidations · newest first
          </CardHead>

          <div style={{ overflowX: "auto" }} className="thin-scroll">
            <div style={{ minWidth: MIN_W }}>
              {/* Column header */}
              <div
                style={{
                  display: "grid",
                  gridTemplateColumns: COLS,
                  padding: "10px 16px",
                  fontFamily: "var(--font-mono)",
                  fontSize: 10,
                  letterSpacing: "0.08em",
                  textTransform: "uppercase",
                  color: "var(--text-dim)",
                  borderBottom: "1px solid var(--border)",
                }}
              >
                <span>Time</span>
                <span>Keeper</span>
                <span>Position</span>
                <span style={{ textAlign: "right" }}>Drew</span>
                <span style={{ textAlign: "right" }}>Proceeds</span>
                <span style={{ textAlign: "right" }}>Profit</span>
                <span style={{ textAlign: "right" }}>Tx</span>
              </div>

              {rows.length === 0 ? (
                <div
                  style={{
                    padding: "40px 16px",
                    textAlign: "center",
                    fontFamily: "var(--font-mono)",
                    fontSize: 12,
                    color: "var(--text-mute)",
                  }}
                >
                  No liquidations yet — the feed populates as keepers fill auctions and return
                  proceeds to the vault.
                </div>
              ) : (
                rows.map((r, i) => (
                  <div
                    key={r.key}
                    className={flash.has(r.key) && live ? "flash-up" : undefined}
                    style={{
                      display: "grid",
                      gridTemplateColumns: COLS,
                      padding: "9px 16px",
                      fontFamily: "var(--font-mono)",
                      fontSize: 12,
                      alignItems: "center",
                      borderBottom: i < rows.length - 1 ? "1px solid var(--border)" : "none",
                    }}
                  >
                    {/* Time */}
                    <span style={{ color: "var(--text-dim)" }}>
                      {r.ts ? fmtClock(r.ts) : "—"}
                    </span>
                    {/* Keeper — attribution recorded by the filling keeper */}
                    {r.keeper ? (
                      <span
                        style={{
                          color: "var(--text)",
                          whiteSpace: "nowrap",
                          overflow: "hidden",
                          textOverflow: "ellipsis",
                        }}
                        title={r.keeper}
                      >
                        {r.keeper}
                      </span>
                    ) : (
                      <span style={{ color: "var(--text-mute)" }} title="recorded before keeper attribution existed">
                        —
                      </span>
                    )}
                    {/* Position */}
                    <a
                      href={`${EXPLORER_ACCOUNT}${r.user}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      title={r.user}
                      style={{ color: "var(--accent)", textDecoration: "none" }}
                    >
                      {shortAddress(r.user)}
                    </a>
                    {/* Drew */}
                    <span style={{ textAlign: "right", color: "var(--text-dim)" }}>
                      ${formatUSDC(r.drew)}
                    </span>
                    {/* Proceeds */}
                    <span
                      style={{
                        textAlign: "right",
                        color: r.proceeds > r.drew ? "var(--text)" : "var(--text-dim)",
                      }}
                    >
                      ${formatUSDC(r.proceeds)}
                    </span>
                    {/* Profit */}
                    <span
                      style={{
                        textAlign: "right",
                        color: r.profit > 0 ? "var(--accent)" : r.profit < 0 ? "var(--red)" : "var(--text-dim)",
                      }}
                    >
                      {r.profit >= 0 ? "+" : "-"}${formatUSDC(Math.abs(r.profit))}
                    </span>
                    {/* Tx → the fill transaction when recorded, else the position account */}
                    <a
                      href={r.txHash ? `${EXPLORER_TX}${r.txHash}` : `${EXPLORER_ACCOUNT}${r.user}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      title={r.txHash ?? r.user}
                      style={{
                        textAlign: "right",
                        color: r.txHash ? "var(--accent)" : "var(--text-mute)",
                        textDecoration: "none",
                      }}
                    >
                      {r.txHash ? "tx ↗" : "view ↗"}
                    </a>
                  </div>
                ))
              )}
            </div>
          </div>
        </Card>

        <p
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 11,
            color: "var(--text-mute)",
            marginTop: 14,
            lineHeight: 1.6,
          }}
        >
          Each fill returns realized profit (proceeds − drawn capital) to the shared vault. The tx
          link opens the fill transaction on stellar.expert (testnet); rows recorded before the
          keeper surfaced tx hashes fall back to the liquidated position account.
        </p>
      </div>
    </div>
  );
}
