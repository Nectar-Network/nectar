"use client";

import { useEffect, useRef, useState } from "react";
import {
  PerformanceData,
  fetchPerformance,
  sharePriceSeries,
  vaultReturn,
} from "../../lib/api";
import { useSSEEvents } from "../../lib/sse";
import { Btn, StatusDot, fmtUSD, fmtNum } from "./ds";

// Color a live keeper-log line by its content (real SSE messages have no kind tag).
function logColor(msg: string): string {
  const m = msg.toLowerCase();
  if (/fail|error|slash|liquidatable|outstanding/.test(m)) return "var(--amber)";
  if (/filled|returned|swapped|executed|registered/.test(m)) return "var(--accent)";
  if (/underwater|drew|draw/.test(m)) return "var(--info)";
  return "var(--text-dim)";
}

function LogStream({ operators }: { operators: number }) {
  const events = useSSEEvents(9);
  const ref = useRef<HTMLDivElement>(null);
  useEffect(() => {
    if (ref.current) ref.current.scrollTop = ref.current.scrollHeight;
  }, [events]);
  // "reconnecting…" lines are emitted while the keeper API is unreachable — they
  // don't count as a live stream.
  const live = events.some((e) => !/reconnect/i.test(e));
  return (
    <div style={{ border: "1px solid var(--border)", background: "var(--card-fill)", borderRadius: 4, overflow: "hidden" }}>
      <div style={{ display: "flex", alignItems: "center", gap: 8, padding: "11px 16px", borderBottom: "1px solid var(--border)" }}>
        <StatusDot glow={live} color={live ? "var(--accent)" : "var(--text-mute)"} />
        <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--text-dim)" }}>
          keeper log stream — {live ? "live testnet" : "awaiting keeper API"}
        </span>
        <span style={{ marginLeft: "auto", fontFamily: "var(--font-mono)", fontSize: 10, color: "var(--text-mute)" }}>
          {operators} operators
        </span>
      </div>
      <div ref={ref} className="thin-scroll" style={{ height: 296, overflowY: "auto", padding: 16, display: "flex", flexDirection: "column", gap: 5 }}>
        {!live && (
          <div style={{ fontFamily: "var(--font-mono)", fontSize: 11.5, color: "var(--text-mute)" }}>
            connect a running keeper (SSE /api/events) to stream live liquidation activity here…
          </div>
        )}
        {events.map((msg, idx) => (
          <div key={idx} className="logline" style={{ fontFamily: "var(--font-mono)", fontSize: 11.5, color: logColor(msg), wordBreak: "break-word" }}>
            <span style={{ color: "var(--text-mute)" }}>{">"} </span>{msg}
          </div>
        ))}
      </div>
    </div>
  );
}

export default function HomeHero() {
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
  const tvl = vault ? vault.total_usdc / 1e7 : 0;
  const ret = perf ? vaultReturn(sharePriceSeries(perf)) : { pct: 0, annualized: false, days: 0 };
  const keeperStats = perf?.keeper_stats ?? {};
  const operators = Object.keys(keeperStats).length;
  const lifetimeFills =
    Object.values(keeperStats).reduce((s, k) => s + (k.liquidations ?? 0), 0) ||
    (perf?.liquidations?.length ?? 0);

  const strip: [string, string][] = [
    ["TVL", fmtUSD(tvl)],
    [ret.annualized ? "30d APY" : "Return", `${ret.pct >= 0 ? "+" : ""}${ret.pct.toFixed(2)}%`],
    ["Lifetime fills", fmtNum(lifetimeFills)],
  ];

  return (
    <section
      style={{
        position: "relative", minHeight: "100vh", display: "flex", alignItems: "center",
        paddingTop: 80, overflow: "hidden",
        backgroundImage: "radial-gradient(var(--border) 1px, transparent 1px)", backgroundSize: "24px 24px",
      }}
    >
      <div style={{ position: "absolute", inset: 0, pointerEvents: "none",
        background: "radial-gradient(ellipse 80% 60% at 50% 38%, color-mix(in srgb, var(--accent) 6%, transparent) 0%, transparent 70%)" }} />
      <div className="home-wrap" style={{ position: "relative", zIndex: 1, padding: "72px 24px" }}>
        <div className="hero-grid">
          <div>
            <div style={{ display: "inline-flex", alignItems: "center", gap: 8, fontFamily: "var(--font-mono)",
              fontSize: 12, color: "var(--accent)", marginBottom: 26, padding: "5px 12px",
              border: "1px solid var(--accent)", borderRadius: 2, background: "var(--accent-fill)", whiteSpace: "nowrap" }}>
              <StatusDot /><span>Soroban Testnet — live</span>
            </div>
            <h1 style={{ fontFamily: "var(--font-display)", fontWeight: 800, fontSize: "clamp(2.6rem, 6vw, 4.4rem)",
              lineHeight: 1.04, letterSpacing: "-0.015em", color: "var(--text)", margin: "0 0 24px" }}>
              One vault.<br />Many keepers.<br /><span style={{ color: "var(--accent)" }}>No single point<br />of failure.</span>
            </h1>
            <p style={{ fontFamily: "var(--font-mono)", fontSize: 14, lineHeight: 1.75, color: "var(--text-dim)", maxWidth: 460, margin: "0 0 30px" }}>
              Nectar replaces single-operator liquidation bots on Blend Protocol with a distributed network of
              competing keepers — funded by a shared USDC vault. Depositors earn the profit from every liquidation.
            </p>
            <div style={{ display: "flex", flexWrap: "wrap", gap: 12, marginBottom: 38 }}>
              <Btn primary href="/vault">Deposit USDC</Btn>
              <Btn href="/dashboard">Live dashboard →</Btn>
              <Btn href="https://github.com/Nectar-Network/nectar" target="_blank">GitHub</Btn>
            </div>
            <div style={{ display: "flex", gap: 0, borderTop: "1px solid var(--border)", paddingTop: 22 }}>
              {strip.map(([k, v], i) => (
                <div key={k} style={{ paddingRight: 32, paddingLeft: i ? 32 : 0, borderRight: i < 2 ? "1px solid var(--border)" : "none" }}>
                  <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--text-mute)", marginBottom: 7 }}>{k}</div>
                  <div style={{ fontFamily: "var(--font-display)", fontWeight: 700, fontSize: 22, color: "var(--text)", fontVariantNumeric: "tabular-nums" }}>{v}</div>
                </div>
              ))}
            </div>
          </div>
          <LogStream operators={operators} />
        </div>
      </div>
    </section>
  );
}
