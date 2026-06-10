"use client";

import { useEffect, useRef, useState } from "react";
import { useSSEEvents } from "../../lib/sse";

const STATIC_LOGS = [
  { tag: "keeper-alpha", msg: "pos GABC3…F2Q0 hf=1.847 — monitoring" },
  { tag: "keeper-beta", msg: "pool loaded reserves=2" },
  { tag: "keeper-alpha", msg: "pos GD7F9…2K1R hf=1.203 — monitoring" },
  { tag: "keeper-beta", msg: "pool heartbeat ledger=51247801" },
  { tag: "keeper-alpha", msg: "pos GD7F9…2K1R hf=0.847 — LIQUIDATABLE" },
  { tag: "keeper-alpha", msg: "creating auction for GD7F9…2K1R" },
  { tag: "keeper-beta", msg: "auction profit=1.0841 — filling" },
  { tag: "keeper-alpha", msg: "filled auction: GD7F9…2K1R" },
  { tag: "keeper-beta", msg: "already filled by another keeper" },
  { tag: "keeper-alpha", msg: "returned 1100000 to vault" },
];

interface LogEntry {
  tag: string;
  msg: string;
}

export default function Hero() {
  const liveEvents = useSSEEvents(20);
  const [visibleLogs, setVisibleLogs] = useState<LogEntry[]>(STATIC_LOGS.slice(0, 4));
  const [staticIdx, setStaticIdx] = useState(4);
  const containerRef = useRef<HTMLDivElement>(null);

  // If no live events, cycle static logs
  useEffect(() => {
    if (liveEvents.length > 0) return;
    const id = setInterval(() => {
      setStaticIdx((prev) => {
        const next = (prev + 1) % STATIC_LOGS.length;
        setVisibleLogs((logs) => [...logs, STATIC_LOGS[next]].slice(-8));
        return next;
      });
    }, 900);
    return () => clearInterval(id);
  }, [liveEvents.length]);

  // When live events arrive, append them
  useEffect(() => {
    if (liveEvents.length === 0) return;
    const latest = liveEvents[liveEvents.length - 1];
    setVisibleLogs((logs) =>
      [...logs, { tag: "keeper", msg: latest }].slice(-8)
    );
  }, [liveEvents]);

  useEffect(() => {
    if (containerRef.current) {
      containerRef.current.scrollTop = containerRef.current.scrollHeight;
    }
  }, [visibleLogs]);

  return (
    <section
      className="relative min-h-screen flex flex-col justify-center dot-grid overflow-hidden"
      style={{ paddingTop: "80px" }}
    >
      <div
        className="absolute inset-0 pointer-events-none"
        style={{
          background:
            "radial-gradient(ellipse 80% 60% at 50% 40%, rgba(0,229,160,0.04) 0%, transparent 70%)",
        }}
      />

      <div className="relative z-10 max-w-6xl mx-auto px-6 py-20 grid grid-cols-1 lg:grid-cols-2 gap-16 items-center">
        <div>
          <div
            className="stagger-1 text-xs font-mono mb-6 flex items-center gap-2"
            style={{ color: "var(--accent)" }}
          >
            <span className="status-dot" />
            <span>Testnet Live — Soroban Testnet</span>
          </div>

          <h1
            className="stagger-2 font-syne font-800 leading-none mb-6"
            style={{
              color: "var(--text)",
              fontSize: "clamp(2.5rem, 6vw, 4.5rem)",
              lineHeight: 1.05,
            }}
          >
            Keeper
            <br />
            Infrastructure
            <br />
            <span style={{ color: "var(--accent)" }}>for Soroban</span>
          </h1>

          <p
            className="stagger-3 text-sm leading-relaxed mb-8 max-w-md"
            style={{ color: "var(--text-dim)", fontFamily: "DM Mono, monospace" }}
          >
            Multi-operator liquidation network for Blend Protocol. Distributed
            across competing keepers. No single point of failure.
          </p>

          <div className="stagger-4 flex flex-wrap gap-3">
            <a
              href="/vault"
              className="px-5 py-2.5 text-sm font-mono transition-all duration-200 border"
              style={{
                background: "var(--accent)",
                color: "var(--bg)",
                borderColor: "var(--accent)",
                textDecoration: "none",
              }}
              onMouseEnter={(e) => {
                e.currentTarget.style.background = "transparent";
                e.currentTarget.style.color = "var(--accent)";
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.background = "var(--accent)";
                e.currentTarget.style.color = "var(--bg)";
              }}
            >
              Deposit USDC
            </a>
            <a
              href="/features"
              className="px-5 py-2.5 text-sm font-mono border transition-all duration-200"
              style={{ borderColor: "var(--border)", color: "var(--text-dim)", textDecoration: "none" }}
              onMouseEnter={(e) => {
                e.currentTarget.style.borderColor = "var(--text-dim)";
                e.currentTarget.style.color = "var(--text)";
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.borderColor = "var(--border)";
                e.currentTarget.style.color = "var(--text-dim)";
              }}
            >
              How It Works
            </a>
            <a
              href="https://github.com/Nectar-Network/nectar"
              target="_blank"
              rel="noopener noreferrer"
              className="px-5 py-2.5 text-sm font-mono border transition-all duration-200"
              style={{ borderColor: "var(--border)", color: "var(--text-dim)", textDecoration: "none" }}
              onMouseEnter={(e) => {
                e.currentTarget.style.borderColor = "var(--text-dim)";
                e.currentTarget.style.color = "var(--text)";
              }}
              onMouseLeave={(e) => {
                e.currentTarget.style.borderColor = "var(--border)";
                e.currentTarget.style.color = "var(--text-dim)";
              }}
            >
              GitHub
            </a>
          </div>
        </div>

        {/* live log stream */}
        <div
          className="stagger-5 border rounded-sm overflow-hidden"
          style={{ borderColor: "var(--border)", background: "var(--surface)" }}
        >
          <div
            className="flex items-center gap-2 px-4 py-3 border-b"
            style={{ borderColor: "var(--border)" }}
          >
            <div className="status-dot" style={{ background: liveEvents.length > 0 ? "var(--accent)" : "var(--amber)" }} />
            <span className="text-xs font-mono" style={{ color: "var(--text-dim)" }}>
              {liveEvents.length > 0 ? "keeper log stream — live" : "keeper log stream — demo"}
            </span>
          </div>
          <div
            ref={containerRef}
            className="overflow-y-auto p-4 space-y-1"
            style={{ height: "280px", scrollBehavior: "smooth" }}
          >
            {visibleLogs.map((entry, i) => {
              const isLiquidatable = entry.msg.includes("LIQUIDATABLE") || entry.msg.includes("underwater");
              const isFill = entry.msg.includes("filled auction") || entry.msg.includes("fill success");
              const isAlready = entry.msg.includes("already filled");
              const isVault = entry.msg.includes("vault") || entry.msg.includes("drew") || entry.msg.includes("returned");
              return (
                <div
                  key={i}
                  className="text-xs font-mono"
                  style={{
                    animation: "stream 0.3s ease-out forwards",
                    color: isLiquidatable
                      ? "var(--amber)"
                      : isFill
                      ? "var(--accent)"
                      : isVault
                      ? "hsl(200, 80%, 60%)"
                      : "var(--text-dim)",
                  }}
                >
                  <span style={{ color: "var(--text-dim)" }}>[</span>
                  <span style={{ color: "var(--accent)" }}>{entry.tag}</span>
                  <span style={{ color: "var(--text-dim)" }}>]</span>{" "}
                  <span style={{ color: isAlready ? "var(--text-dim)" : undefined }}>{entry.msg}</span>
                </div>
              );
            })}
          </div>
        </div>
      </div>

      <div
        className="stagger-7 absolute bottom-8 left-1/2 -translate-x-1/2 text-xs font-mono"
        style={{ color: "var(--text-dim)" }}
      >
        scroll ↓
      </div>
    </section>
  );
}
