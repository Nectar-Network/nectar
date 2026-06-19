"use client";

import { useEffect, useMemo, useState } from "react";
import {
  PerformanceData,
  HistoryEvent,
  fetchPerformance,
  formatUSDC,
  shortAddress,
  sharePriceSeries,
} from "../../../lib/api";
import {
  connectWallet,
  queryDepositor,
  queryVaultBalance,
} from "../../../lib/stellar";
import {
  Btn,
  Card,
  CardHead,
  Eyebrow,
  LineChart,
  StatTile,
  StatusDot,
  fmtPct,
  fmtUSD,
} from "../../components/ds";

const STROOPS = 1e7;
const G_ADDR = /^G[A-Z2-7]{55}$/;
const EXPLORER_TX = "https://stellar.expert/explorer/testnet/tx/";

function historyDateLabel(iso: string): string {
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t) || t <= 0) return "—";
  return new Date(t).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

// 5-col grid: date · type · amount · shares · tx
const HIST_COLS = "1.1fr 0.8fr 1fr 1fr 60px";

interface Position {
  address: string;
  shares: number; // stroops
  value: number; // stroops (current USDC value of shares)
  depositedAt: number; // unix seconds (0 if unknown)
}

const mono = (size: number, color = "var(--text-dim)"): React.CSSProperties => ({
  fontFamily: "var(--font-mono)",
  fontSize: size,
  color,
});

function joinedLabel(unixSeconds: number): string | null {
  if (!unixSeconds) return null;
  return new Date(unixSeconds * 1000).toLocaleDateString("en-US", {
    month: "short",
    day: "numeric",
    year: "numeric",
  });
}

export default function DepositorAnalytics({
  initialData,
  initialAddress,
}: {
  initialData: PerformanceData | null;
  // Pre-filled by the /dashboard/<address> deep-link route; looked up on mount.
  initialAddress?: string;
}) {
  const [perf, setPerf] = useState<PerformanceData | null>(initialData);
  const [input, setInput] = useState(initialAddress ?? "");
  const [pos, setPos] = useState<Position | null>(null);
  const [looked, setLooked] = useState(false); // a lookup has completed (for empty-state)
  const [loading, setLoading] = useState(false);
  const [err, setErr] = useState<string | null>(null);

  // Poll vault performance for the share-price series.
  useEffect(() => {
    const poll = async () => {
      const f = await fetchPerformance();
      if (f) setPerf(f);
    };
    poll();
    const t = setInterval(poll, 15_000);
    return () => clearInterval(t);
  }, []);

  // Deep-link: resolve the address from the route once on mount.
  useEffect(() => {
    if (initialAddress) void lookup(initialAddress);
    // lookup is stable for the component's lifetime; run once per deep-link.
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [initialAddress]);

  // Re-read the looked-up position on the on-chain cadence so value/shares stay fresh.
  useEffect(() => {
    if (!pos) return;
    const addr = pos.address;
    let cancelled = false;
    const refresh = async () => {
      const [bal, dep] = await Promise.all([
        queryVaultBalance(addr),
        queryDepositor(addr),
      ]);
      if (cancelled) return;
      const shares = bal?.shares ?? dep?.shares ?? 0;
      if (shares <= 0) return; // keep last good position rather than blank it
      setPos({
        address: addr,
        shares,
        value: bal?.usdcValue ?? shares,
        depositedAt: dep?.depositedAt ?? 0,
      });
    };
    const t = setInterval(refresh, 30_000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
    // Re-subscribe only when the looked-up address changes (not on every setPos).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [pos?.address]);

  async function lookup(address: string) {
    const addr = address.trim().toUpperCase();
    if (!G_ADDR.test(addr)) {
      setErr("Enter a valid Stellar address (G… 56 chars).");
      return;
    }
    setErr(null);
    setLoading(true);
    try {
      const [bal, dep] = await Promise.all([
        queryVaultBalance(addr),
        queryDepositor(addr),
      ]);
      const shares = bal?.shares ?? dep?.shares ?? 0;
      setPos({
        address: addr,
        shares,
        value: bal?.usdcValue ?? shares,
        depositedAt: dep?.depositedAt ?? 0,
      });
      setLooked(true);
    } catch {
      setErr("Lookup failed — the chain read is unavailable right now.");
    } finally {
      setLoading(false);
    }
  }

  async function onConnect() {
    setErr(null);
    try {
      const w = await connectWallet();
      if (w?.address) {
        setInput(w.address);
        await lookup(w.address);
      }
    } catch {
      setErr("Wallet connection failed.");
    }
  }

  const shares = pos?.shares ?? 0;
  const value = pos?.value ?? 0;
  // Per-depositor cost basis isn't on-chain, so net-deposited is approximated at
  // the 1:1 mint price (par entry). Yield and return both derive from this basis
  // so the cards stay consistent — they are labeled estimates.
  const yieldStroops = value - shares;
  const yieldPct = shares > 0 ? (yieldStroops / shares) * 100 : 0;
  const yieldPos = yieldStroops >= 0;

  // Value-over-time: scale the vault share-price series to this depositor's shares.
  const series = useMemo(() => {
    if (!perf || shares <= 0) return [];
    return sharePriceSeries(perf).map((p) => ({
      label: p.label,
      value: (shares * p.sharePrice) / STROOPS,
    }));
  }, [perf, shares]);

  // Deposit/withdraw history for the looked-up address, indexed from vault
  // events by the keeper (newest first). Sourced from the performance payload's
  // matching depositor row; absent when the keeper has not indexed this address.
  const history = useMemo<HistoryEvent[]>(() => {
    if (!perf || !pos) return [];
    const row = perf.depositors.find(
      (d) => d.address.toUpperCase() === pos.address.toUpperCase(),
    );
    return row?.history ?? [];
  }, [perf, pos]);

  const hasPosition = !!pos && shares > 0;
  const joined = pos ? joinedLabel(pos.depositedAt) : null;

  return (
    <div style={{ maxWidth: 1180, margin: "0 auto", padding: "34px 24px 64px" }}>
      {/* ── Header + address picker ─────────────────────────────────────── */}
      <div
        style={{
          marginBottom: 24,
          display: "flex",
          justifyContent: "space-between",
          alignItems: "flex-end",
          flexWrap: "wrap",
          gap: 16,
        }}
      >
        <div>
          <Eyebrow style={{ marginBottom: 10 }}>Per-depositor analytics</Eyebrow>
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
            Your Position
          </h1>
        </div>

        <div style={{ display: "flex", flexDirection: "column", alignItems: "flex-end", gap: 8 }}>
          <span
            style={{
              ...mono(10, "var(--text-mute)"),
              letterSpacing: "0.08em",
              textTransform: "uppercase",
            }}
          >
            Lookup address
          </span>
          <form
            onSubmit={(e) => {
              e.preventDefault();
              lookup(input);
            }}
            style={{ display: "flex", gap: 8, alignItems: "center", flexWrap: "wrap", justifyContent: "flex-end" }}
          >
            <input
              value={input}
              onChange={(e) => setInput(e.target.value)}
              placeholder="G…"
              spellCheck={false}
              autoCapitalize="characters"
              style={{
                ...mono(12, "var(--accent)"),
                background: "var(--surface)",
                border: "1px solid var(--border)",
                borderRadius: 2,
                padding: "8px 12px",
                width: 240,
                maxWidth: "60vw",
                outline: "none",
              }}
            />
            <button
              type="submit"
              disabled={loading}
              style={{
                ...mono(12, "var(--bg)"),
                background: "var(--accent)",
                border: "1px solid var(--accent)",
                borderRadius: 2,
                padding: "8px 14px",
                cursor: loading ? "default" : "pointer",
                opacity: loading ? 0.6 : 1,
              }}
            >
              {loading ? "…" : "Look up"}
            </button>
            <button
              type="button"
              onClick={onConnect}
              style={{
                ...mono(12, "var(--text-dim)"),
                background: "transparent",
                border: "1px solid var(--border)",
                borderRadius: 2,
                padding: "8px 14px",
                cursor: "pointer",
              }}
            >
              Connect wallet
            </button>
          </form>
          {err ? (
            <span style={mono(11, "var(--red)")}>{err}</span>
          ) : pos ? (
            <span style={mono(11, "var(--text-dim)")} title={pos.address}>
              {shortAddress(pos.address)}
              {joined ? ` · joined ${joined}` : ""}
            </span>
          ) : null}
        </div>
      </div>

      {/* ── Prompt / empty / position ───────────────────────────────────── */}
      {!pos && !looked ? (
        <Card style={{ padding: "40px 24px", textAlign: "center" }}>
          <div style={{ display: "flex", justifyContent: "center", marginBottom: 14 }}>
            <StatusDot color="var(--text-mute)" glow={false} />
          </div>
          <div style={{ ...mono(13, "var(--text)"), marginBottom: 8 }}>
            Enter a Stellar address to view its vault position
          </div>
          <div style={mono(12, "var(--text-mute)")}>
            Paste a G-address above, or connect your wallet. Shares, current value, and an estimated
            return are read live from the vault contract.
          </div>
        </Card>
      ) : !hasPosition ? (
        <Card style={{ padding: "40px 24px", textAlign: "center" }}>
          <div style={{ ...mono(13, "var(--text)"), marginBottom: 8 }}>
            No vault position found for this address
          </div>
          <div style={mono(12, "var(--text-mute)")}>
            {pos ? shortAddress(pos.address) : ""} holds zero shares in the Nectar vault.
          </div>
        </Card>
      ) : (
        <>
          {/* Stat tiles */}
          <div
            style={{
              display: "grid",
              gridTemplateColumns: "repeat(auto-fit, minmax(220px, 1fr))",
              gap: 14,
              marginBottom: 20,
            }}
          >
            <StatTile label="Current shares" value={(shares / STROOPS).toFixed(2)} />
            <StatTile label="Current value" value={`$${formatUSDC(value)}`} />
            <StatTile label="Net deposited (est.)" value={`$${formatUSDC(shares)}`} sub="at par entry" />
            <StatTile
              label="Cumulative yield (est.)"
              value={`${yieldPos ? "+" : "-"}$${formatUSDC(Math.abs(yieldStroops))}`}
              accent
              delta={yieldPct}
            />
          </div>

          {/* Position value over time */}
          <Card style={{ padding: 0, marginBottom: 20 }}>
            <div
              style={{
                display: "flex",
                justifyContent: "space-between",
                alignItems: "flex-start",
                padding: "18px 20px 4px",
                flexWrap: "wrap",
                gap: 12,
              }}
            >
              <div>
                <div
                  style={{
                    ...mono(11, "var(--text-dim)"),
                    letterSpacing: "0.08em",
                    textTransform: "uppercase",
                    marginBottom: 10,
                  }}
                >
                  Position value over time
                </div>
                <div style={{ display: "flex", alignItems: "baseline", gap: 14 }}>
                  <span
                    style={{
                      fontFamily: "var(--font-display)",
                      fontWeight: 700,
                      fontSize: 30,
                      color: "var(--text)",
                      fontVariantNumeric: "tabular-nums",
                    }}
                  >
                    ${formatUSDC(value)}
                  </span>
                  <span style={mono(13, yieldPos ? "var(--accent)" : "var(--red)")}>
                    {fmtUSD(yieldStroops / STROOPS, { sign: true, dp: 0 })} ({fmtPct(yieldPct)})
                  </span>
                </div>
              </div>
              <div
                style={{
                  display: "flex",
                  alignItems: "center",
                  gap: 16,
                  ...mono(11, "var(--text-dim)"),
                }}
              >
                <span style={{ display: "flex", alignItems: "center", gap: 7 }}>
                  <span style={{ width: 14, height: 2, background: "var(--accent)", display: "inline-block" }} />
                  value
                </span>
              </div>
            </div>
            <div style={{ padding: "4px 12px 12px" }}>
              {series.length >= 2 ? (
                <LineChart
                  data={series}
                  height={244}
                  variant="area"
                  label="posval"
                  valueFmt={(v) => `$${Math.round(v).toLocaleString()}`}
                />
              ) : (
                <div
                  style={{
                    height: 244,
                    display: "flex",
                    alignItems: "center",
                    justifyContent: "center",
                    ...mono(12, "var(--text-mute)"),
                  }}
                >
                  {perf
                    ? "Not enough vault history yet to chart this position."
                    : "Awaiting keeper API — no series data yet."}
                </div>
              )}
            </div>
          </Card>

          {/* Deposit / withdraw history — indexed from vault events by the keeper */}
          <Card style={{ padding: 0, marginBottom: 20 }}>
            <CardHead>Deposit / withdraw history</CardHead>
            <div style={{ overflowX: "auto" }}>
              <div style={{ minWidth: 520 }}>
                {/* Column header */}
                <div
                  style={{
                    display: "grid",
                    gridTemplateColumns: HIST_COLS,
                    padding: "10px 20px",
                    ...mono(10, "var(--text-dim)"),
                    letterSpacing: "0.08em",
                    textTransform: "uppercase",
                    borderBottom: "1px solid var(--border)",
                  }}
                >
                  <span>Date</span>
                  <span>Type</span>
                  <span style={{ textAlign: "right" }}>Amount</span>
                  <span style={{ textAlign: "right" }}>Shares</span>
                  <span style={{ textAlign: "right" }}>Tx</span>
                </div>

                {history.length === 0 ? (
                  <div style={{ padding: "32px 20px", textAlign: "center", ...mono(12, "var(--text-mute)") }}>
                    No deposit/withdraw events in the indexed window.
                  </div>
                ) : (
                  history.map((h, i) => (
                    <div
                      key={`${h.tx_hash ?? h.ledger}-${h.type}-${i}`}
                      style={{
                        display: "grid",
                        gridTemplateColumns: HIST_COLS,
                        padding: "9px 20px",
                        ...mono(12, "var(--text-dim)"),
                        alignItems: "center",
                        borderBottom: i < history.length - 1 ? "1px solid var(--border)" : "none",
                      }}
                    >
                      <span>{historyDateLabel(h.ts)}</span>
                      <span style={{ color: h.type === "deposit" ? "var(--accent)" : "var(--text)" }}>
                        {h.type}
                      </span>
                      <span style={{ textAlign: "right", color: "var(--text)" }}>
                        ${formatUSDC(h.amount)}
                      </span>
                      <span style={{ textAlign: "right" }}>{(h.shares / STROOPS).toFixed(2)}</span>
                      {h.tx_hash ? (
                        <a
                          href={`${EXPLORER_TX}${h.tx_hash}`}
                          target="_blank"
                          rel="noopener noreferrer"
                          title={h.tx_hash}
                          style={{ textAlign: "right", color: "var(--accent)", textDecoration: "none" }}
                        >
                          tx ↗
                        </a>
                      ) : (
                        <span style={{ textAlign: "right", color: "var(--text-mute)" }}>—</span>
                      )}
                    </div>
                  ))
                )}
              </div>
            </div>
          </Card>

          {/* Estimate note + deposit/withdraw history honesty */}
          <Card style={{ padding: 0, marginBottom: 20 }}>
            <CardHead>Cost-basis note</CardHead>
            <div style={{ padding: "16px 20px", ...mono(12, "var(--text-dim)"), lineHeight: 1.55 }}>
              Yield and return are <strong style={{ color: "var(--text)" }}>estimates assuming a 1.0
              entry price</strong>. Per-depositor cost basis is not tracked on-chain, so a depositor
              who entered above par may see an overstated gain. The history above is indexed from
              recent vault events (deposits and withdrawals within the RPC retention window); events
              older than that window are not reconstructable from the keeper. Shares and current
              value are read directly from the vault contract.
            </div>
          </Card>

          <div style={{ display: "flex", gap: 12, flexWrap: "wrap" }}>
            <Btn href="/vault" small primary>
              Manage position
            </Btn>
            <Btn href="/dashboard" small>
              ← Back to overview
            </Btn>
          </div>
        </>
      )}
    </div>
  );
}
