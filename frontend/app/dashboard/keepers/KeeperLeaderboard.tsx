"use client";

import { useEffect, useMemo, useState } from "react";
import {
  PerformanceData,
  fetchPerformance,
  formatUSDC,
  shortAddress,
  successRate,
} from "../../../lib/api";
import { KeeperInfoOnchain, queryKeeper, queryKeepers } from "../../../lib/stellar";
import {
  Card,
  Eyebrow,
  MiniBars,
  Pill,
  SectionHead,
  StatTile,
  StatusDot,
  fmtNum,
  fmtUSD,
  keeperColor,
  successColor,
} from "../../components/ds";

type SortKey = "profit" | "executions" | "success" | "stake" | "response";

interface Row {
  address: string;
  name: string;
  info: KeeperInfoOnchain | null;
  // keeper API fallback when the chain read is unavailable
  liquidations: number;
  apiProfit: number;
}

const REGISTRY_CONTRACT = process.env.NEXT_PUBLIC_REGISTRY_CONTRACT ?? "";
const EXPLORER_ACCOUNT = "https://stellar.expert/explorer/testnet/account/";

interface Col {
  // `null` key = a presentational column (rank / operator name) that is not user-sortable.
  key: SortKey | null;
  label: string;
  w: string;
  align: "left" | "right";
}

const COLS: Col[] = [
  { key: null, label: "#", w: "34px", align: "left" },
  { key: null, label: "Operator", w: "1.6fr", align: "left" },
  { key: "executions", label: "Executions", w: "1fr", align: "right" },
  { key: "success", label: "Win rate", w: "0.9fr", align: "right" },
  { key: "response", label: "Avg response", w: "1fr", align: "right" },
  { key: "stake", label: "Stake", w: "0.9fr", align: "right" },
  { key: "profit", label: "Total profit", w: "1.1fr", align: "right" },
];
const GRID = COLS.map((c) => c.w).join(" ");

export default function KeeperLeaderboard({ initialData }: { initialData: PerformanceData | null }) {
  const [perf, setPerf] = useState<PerformanceData | null>(initialData);
  const [chain, setChain] = useState<Record<string, KeeperInfoOnchain>>({});
  const [sortKey, setSortKey] = useState<SortKey>("profit");
  const [dir, setDir] = useState<1 | -1>(-1);
  const [live, setLive] = useState<boolean>(!!initialData);

  // Poll the keeper API for names/fallback stats.
  useEffect(() => {
    const poll = async () => {
      const fresh = await fetchPerformance();
      if (fresh) {
        setPerf(fresh);
        setLive(true);
      } else {
        setLive(false);
      }
    };
    poll();
    const t = setInterval(poll, 15_000);
    return () => clearInterval(t);
  }, []);

  // Stable key of API-known keeper addresses, so the registry-read effect below
  // only re-subscribes when the address set actually changes — not on every poll
  // (fetchPerformance returns a fresh object each time, which would otherwise
  // reset the 30s interval and double the RPC load).
  const apiAddrKey = useMemo(
    () =>
      Object.values(perf?.keeper_stats ?? {})
        .map((k) => k.address)
        .filter(Boolean)
        .sort()
        .join(","),
    [perf?.keeper_stats],
  );

  // Read authoritative keeper info from the registry: get_keepers, then get_keeper
  // for each, unioned with any addresses the keeper API knows about.
  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      const fromApi = apiAddrKey ? apiAddrKey.split(",") : [];
      const onChain = await queryKeepers();
      const addrs = Array.from(new Set([...onChain, ...fromApi]));
      if (!addrs.length) return;
      const infos = await Promise.all(addrs.map((a) => queryKeeper(a)));
      if (cancelled) return;
      const next: Record<string, KeeperInfoOnchain> = {};
      infos.forEach((info, i) => {
        if (info) next[addrs[i]] = info;
      });
      setChain(next);
    };
    refresh();
    const t = setInterval(refresh, 30_000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, [apiAddrKey]);

  const rows: Row[] = useMemo(() => {
    const stats = perf?.keeper_stats ?? {};
    const nameByAddr = new Map<string, string>();
    Object.values(stats).forEach((k) => nameByAddr.set(k.address, k.name));
    const addrs = Array.from(new Set([...Object.keys(chain), ...Object.values(stats).map((k) => k.address)]));
    const apiByAddr = new Map(Object.values(stats).map((k) => [k.address, k]));
    return addrs.filter(Boolean).map((address) => {
      const api = apiByAddr.get(address);
      const chainName = chain[address]?.name;
      return {
        address,
        name: (chainName && chainName.trim()) || nameByAddr.get(address) || shortAddress(address),
        info: chain[address] ?? null,
        liquidations: api?.liquidations ?? 0,
        apiProfit: api?.total_profit ?? 0,
      };
    });
  }, [perf?.keeper_stats, chain]);

  const profitOf = (r: Row) => r.info?.totalProfit ?? r.apiProfit;
  const execOf = (r: Row) => r.info?.totalExecutions ?? r.liquidations;
  const fillsOf = (r: Row) => r.info?.successfulFills ?? r.liquidations;
  const stakeOf = (r: Row) => r.info?.stake ?? 0;
  const respOf = (r: Row) => r.info?.avgResponseTimeMs ?? 0;
  // Win-rate fraction 0..1; meaningless with zero executions (rendered as "—").
  const rateOf = (r: Row) => successRate(execOf(r), fillsOf(r));

  const sorted = useMemo(() => {
    const arr = [...rows];
    const cmp = (a: Row, b: Row): number => {
      switch (sortKey) {
        case "executions":
          return execOf(a) - execOf(b);
        case "success":
          return rateOf(a) - rateOf(b);
        case "stake":
          return stakeOf(a) - stakeOf(b);
        case "response":
          return respOf(a) - respOf(b);
        case "profit":
        default:
          return profitOf(a) - profitOf(b);
      }
    };
    arr.sort((a, b) => cmp(a, b) * dir);
    return arr;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, sortKey, dir]);

  const onSort = (k: SortKey) => {
    if (k === sortKey) {
      setDir((d) => (d === -1 ? 1 : -1));
    } else {
      setSortKey(k);
      // response: lower (faster) is better → ascending first; others descending.
      setDir(k === "response" ? 1 : -1);
    }
  };

  const maxExec = Math.max(1, ...rows.map((r) => execOf(r)));

  // ── network aggregates (honest: "—" when there is nothing to average) ──────
  const totExec = rows.reduce((s, r) => s + execOf(r), 0);
  const totFills = rows.reduce((s, r) => s + fillsOf(r), 0);
  const totStake = rows.reduce((s, r) => s + stakeOf(r), 0);
  const activeDrawCount = rows.filter((r) => r.info?.hasActiveDraw).length;
  const activeOperators = rows.filter((r) => r.info?.active).length;
  const netWin = totExec > 0 ? `${((totFills / totExec) * 100).toFixed(1)}%` : "—";

  const contractLabel = REGISTRY_CONTRACT
    ? `${REGISTRY_CONTRACT.slice(0, 6)}…${REGISTRY_CONTRACT.slice(-4)}`
    : "registry";

  return (
    <div style={{ maxWidth: 1180, margin: "0 auto", padding: "34px 24px 64px" }}>
      {/* ── page header ─────────────────────────────────────────────────── */}
      <div style={{ marginBottom: 26 }}>
        <div style={{ display: "flex", alignItems: "center", gap: 10, marginBottom: 10, flexWrap: "wrap" }}>
          <Eyebrow>
            KeeperRegistry ·{" "}
            <span style={{ color: "var(--accent)" }}>{contractLabel}</span>
          </Eyebrow>
          <span style={{ display: "inline-flex", alignItems: "center", gap: 7 }}>
            <StatusDot size={6} color={live ? "var(--accent)" : "var(--amber)"} />
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--text-dim)" }}>
              {live ? "live" : "awaiting keeper API"}
            </span>
          </span>
        </div>
        <h1
          style={{
            fontFamily: "var(--font-display)",
            fontWeight: 700,
            fontSize: "clamp(2rem, 4vw, 3rem)",
            color: "var(--text)",
            letterSpacing: "-0.01em",
            margin: "0 0 10px",
          }}
        >
          Keeper Leaderboard
        </h1>
        <p
          style={{
            fontFamily: "var(--font-mono)",
            fontSize: 13,
            lineHeight: 1.6,
            color: "var(--text-dim)",
            margin: 0,
            maxWidth: 580,
          }}
        >
          Competing operators race to fill Dutch auctions. The loser handles{" "}
          <span style={{ color: "var(--text)" }}>ErrAlreadyFilled</span> gracefully. Ranked by
          realized profit returned to the vault.
        </p>
      </div>

      {/* ── network stat tiles ──────────────────────────────────────────── */}
      <div
        style={{
          display: "grid",
          gridTemplateColumns: "repeat(auto-fit, minmax(180px, 1fr))",
          gap: 14,
          marginBottom: 24,
        }}
      >
        <StatTile
          label="Registered operators"
          value={rows.length === 0 ? "—" : `${activeOperators} / ${rows.length}`}
          sub={rows.length === 0 ? "no keepers" : "active"}
        />
        <StatTile label="Total executions" value={totExec === 0 ? "—" : fmtNum(totExec)} />
        <StatTile label="Network win rate" value={netWin} accent={totExec > 0} />
        <StatTile label="Total stake bonded" value={totStake === 0 ? "—" : fmtUSD(totStake / 1e7)} />
      </div>

      {/* ── section head ─────────────────────────────────────────────────── */}
      <SectionHead
        eyebrow="On-chain from KeeperRegistry · click a column to re-sort"
        title="All operators"
        right={
          activeDrawCount > 0 ? (
            <div
              style={{
                display: "flex",
                alignItems: "center",
                gap: 8,
                fontFamily: "var(--font-mono)",
                fontSize: 11,
                color: "var(--text-dim)",
              }}
            >
              <StatusDot color="var(--amber)" />
              <span>
                {activeDrawCount} operator{activeDrawCount === 1 ? "" : "s"} drawing
              </span>
            </div>
          ) : undefined
        }
      />

      {/* ── leaderboard table ───────────────────────────────────────────── */}
      <Card style={{ padding: 0 }}>
        <div style={{ overflowX: "auto" }} className="thin-scroll">
          <div style={{ minWidth: 880 }}>
            {/* header row */}
            <div
              style={{
                display: "grid",
                gridTemplateColumns: GRID,
                padding: "12px 18px",
                fontFamily: "var(--font-mono)",
                fontSize: 10,
                letterSpacing: "0.08em",
                color: "var(--text-dim)",
                borderBottom: "1px solid var(--border)",
                textTransform: "uppercase",
                background: "var(--surface)",
              }}
            >
              {COLS.map((c, i) => {
                const sortable = c.key !== null;
                const active = sortable && sortKey === c.key;
                return (
                  <span
                    key={i}
                    onClick={() => c.key && onSort(c.key)}
                    style={{
                      textAlign: c.align,
                      cursor: sortable ? "pointer" : "default",
                      color: active ? "var(--accent)" : "var(--text-dim)",
                      userSelect: "none",
                      whiteSpace: "nowrap",
                    }}
                  >
                    {c.label}
                    {active ? (dir === -1 ? " ↓" : " ↑") : ""}
                  </span>
                );
              })}
            </div>

            {/* empty state */}
            {sorted.length === 0 && (
              <div
                style={{
                  padding: "32px 18px",
                  fontFamily: "var(--font-mono)",
                  fontSize: 13,
                  color: "var(--text-dim)",
                  textAlign: "center",
                }}
              >
                No keepers registered yet.
              </div>
            )}

            {/* data rows */}
            {sorted.map((r, idx) => {
              const exec = execOf(r);
              const rate = rateOf(r);
              const hasExec = exec > 0;
              const resp = respOf(r);
              const stake = stakeOf(r);
              const profit = profitOf(r);
              const active = r.info?.active ?? false;
              const drawing = r.info?.hasActiveDraw ?? false;
              const kColor = keeperColor(r.name);
              return (
                <div
                  key={r.address}
                  className="nrow"
                  style={{
                    display: "grid",
                    gridTemplateColumns: GRID,
                    padding: "16px 18px",
                    fontFamily: "var(--font-mono)",
                    fontSize: 13,
                    alignItems: "center",
                    borderBottom: idx < sorted.length - 1 ? "1px solid var(--border)" : "none",
                    fontVariantNumeric: "tabular-nums",
                  }}
                >
                  {/* rank */}
                  <span style={{ textAlign: "left", color: "var(--text-mute)" }}>{idx + 1}</span>

                  {/* operator */}
                  <span style={{ display: "flex", alignItems: "center", gap: 9, minWidth: 0 }}>
                    <StatusDot
                      size={7}
                      color={active ? kColor : "var(--text-mute)"}
                      glow={active}
                    />
                    <a
                      href={`${EXPLORER_ACCOUNT}${r.address}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      title={r.address}
                      style={{
                        color: kColor,
                        textDecoration: "none",
                        whiteSpace: "nowrap",
                        overflow: "hidden",
                        textOverflow: "ellipsis",
                      }}
                    >
                      {r.name}
                    </a>
                    {drawing && (
                      <Pill color="var(--amber)" style={{ fontSize: 9 }}>
                        drawing
                      </Pill>
                    )}
                    {r.info && !active && (
                      <Pill color="var(--red)" style={{ fontSize: 9 }}>
                        offline
                      </Pill>
                    )}
                  </span>

                  {/* executions + mini-bar */}
                  <span
                    style={{
                      display: "inline-flex",
                      flexDirection: "column",
                      alignItems: "flex-end",
                      gap: 5,
                      width: "100%",
                    }}
                  >
                    <span style={{ color: "var(--text)" }}>{hasExec ? fmtNum(exec) : "—"}</span>
                    {hasExec && (
                      <span style={{ width: 64 }}>
                        <MiniBars value={exec} max={maxExec} color={kColor} />
                      </span>
                    )}
                  </span>

                  {/* win rate — "—" with zero executions, never 100% */}
                  <span
                    style={{
                      textAlign: "right",
                      color: hasExec ? successColor(rate * 100) : "var(--text-mute)",
                    }}
                  >
                    {hasExec ? `${(rate * 100).toFixed(1)}%` : "—"}
                  </span>

                  {/* avg response */}
                  <span
                    style={{
                      textAlign: "right",
                      color: resp > 0 ? (resp < 350 ? "var(--text)" : "var(--text-dim)") : "var(--text-mute)",
                    }}
                  >
                    {resp > 0 ? `${resp}ms` : "—"}
                  </span>

                  {/* stake */}
                  <span style={{ textAlign: "right", color: stake > 0 ? "var(--text-dim)" : "var(--text-mute)" }}>
                    {stake > 0 ? fmtUSD(stake / 1e7) : "—"}
                  </span>

                  {/* total profit */}
                  <span style={{ textAlign: "right", color: profit > 0 ? "var(--accent)" : "var(--text)" }}>
                    {profit !== 0 ? `$${formatUSDC(profit)}` : "$0.00"}
                  </span>
                </div>
              );
            })}
          </div>
        </div>
      </Card>

      {/* ── color key ────────────────────────────────────────────────────── */}
      <div
        style={{
          marginTop: 16,
          fontFamily: "var(--font-mono)",
          fontSize: 11,
          color: "var(--text-dim)",
          display: "flex",
          alignItems: "center",
          gap: 18,
          flexWrap: "wrap",
        }}
      >
        <span style={{ color: "var(--text-mute)" }}>Win-rate key:</span>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <span style={{ color: "var(--accent)" }}>●</span> ≥95%
        </span>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <span style={{ color: "var(--amber)" }}>●</span> 80–95%
        </span>
        <span style={{ display: "inline-flex", alignItems: "center", gap: 6 }}>
          <span style={{ color: "var(--red)" }}>●</span> &lt;80%
        </span>
        <span style={{ color: "var(--text-mute)" }}>· zero executions render as —</span>
      </div>
    </div>
  );
}
