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

type SortKey = "profit" | "executions" | "success" | "stake" | "response";

interface Row {
  address: string;
  name: string;
  info: KeeperInfoOnchain | null;
  // keeper API fallback when the chain read is unavailable
  liquidations: number;
  apiProfit: number;
}

const cell: React.CSSProperties = {
  padding: "8px 12px",
  borderBottom: "1px solid var(--border)",
  fontFamily: "monospace",
  fontSize: "13px",
  color: "var(--text)",
};

function headerCell(active: boolean): React.CSSProperties {
  return {
    ...cell,
    color: active ? "var(--accent)" : "var(--text-dim)",
    fontSize: "11px",
    textTransform: "uppercase",
    letterSpacing: "0.08em",
    cursor: "pointer",
    userSelect: "none",
  };
}

const EXPLORER_ACCOUNT = "https://stellar.expert/explorer/testnet/account/";

export default function KeeperLeaderboard({ initialData }: { initialData: PerformanceData | null }) {
  const [perf, setPerf] = useState<PerformanceData | null>(initialData);
  const [chain, setChain] = useState<Record<string, KeeperInfoOnchain>>({});
  const [sortKey, setSortKey] = useState<SortKey>("profit");

  // Poll the keeper API for names/fallback stats.
  useEffect(() => {
    const poll = async () => {
      const fresh = await fetchPerformance();
      if (fresh) setPerf(fresh);
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
      return {
        address,
        name: nameByAddr.get(address) ?? shortAddress(address),
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
  const rateOf = (r: Row) => successRate(execOf(r), fillsOf(r));

  const sorted = useMemo(() => {
    const arr = [...rows];
    arr.sort((a, b) => {
      switch (sortKey) {
        case "executions":
          return execOf(b) - execOf(a);
        case "success":
          return rateOf(b) - rateOf(a);
        case "stake":
          return stakeOf(b) - stakeOf(a);
        case "response":
          return respOf(a) - respOf(b); // faster (lower) first
        case "profit":
        default:
          return profitOf(b) - profitOf(a);
      }
    });
    return arr;
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [rows, sortKey]);

  return (
    <div style={{ maxWidth: 1100, margin: "0 auto", padding: "32px 24px" }}>
      <div style={{ marginBottom: 24 }}>
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
          Keeper Leaderboard
        </h1>
        <p style={{ fontSize: 12, color: "var(--text-dim)", fontFamily: "monospace" }}>
          Ranked by realized profit · on-chain from KeeperRegistry · click a column to re-sort
        </p>
      </div>

      <div style={{ border: "1px solid var(--border)", borderRadius: 4, overflow: "hidden" }}>
        <table style={{ width: "100%", borderCollapse: "collapse" }}>
          <thead>
            <tr style={{ background: "rgba(255,255,255,0.03)" }}>
              <th style={{ ...cell, color: "var(--text-dim)", fontSize: 11, textAlign: "left" }}>#</th>
              <th style={{ ...headerCell(false), textAlign: "left", cursor: "default" }}>Keeper</th>
              <th style={{ ...headerCell(sortKey === "executions"), textAlign: "right" }} onClick={() => setSortKey("executions")}>
                Executions
              </th>
              <th style={{ ...headerCell(sortKey === "success"), textAlign: "right" }} onClick={() => setSortKey("success")}>
                Success
              </th>
              <th style={{ ...headerCell(sortKey === "profit"), textAlign: "right" }} onClick={() => setSortKey("profit")}>
                Profit
              </th>
              <th style={{ ...headerCell(sortKey === "stake"), textAlign: "right" }} onClick={() => setSortKey("stake")}>
                Stake
              </th>
              <th style={{ ...headerCell(sortKey === "response"), textAlign: "right" }} onClick={() => setSortKey("response")}>
                Avg Resp
              </th>
              <th style={{ ...cell, color: "var(--text-dim)", fontSize: 11, textAlign: "center" }}>Status</th>
            </tr>
          </thead>
          <tbody>
            {sorted.length === 0 && (
              <tr>
                <td style={{ ...cell, color: "var(--text-dim)", textAlign: "center" }} colSpan={8}>
                  No keepers registered.
                </td>
              </tr>
            )}
            {sorted.map((r, idx) => {
              const rate = rateOf(r);
              const active = r.info?.hasActiveDraw ?? false;
              const resp = respOf(r);
              return (
                <tr key={r.address} style={{ background: idx % 2 === 0 ? "transparent" : "rgba(255,255,255,0.01)" }}>
                  <td style={{ ...cell, color: "var(--text-dim)" }}>{idx + 1}</td>
                  <td style={cell}>
                    <a
                      href={`${EXPLORER_ACCOUNT}${r.address}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      title={r.address}
                      style={{ color: "var(--accent)", textDecoration: "none" }}
                    >
                      {r.name}
                    </a>
                  </td>
                  <td style={{ ...cell, textAlign: "right" }}>
                    {fillsOf(r)}/{execOf(r)}
                  </td>
                  <td
                    style={{
                      ...cell,
                      textAlign: "right",
                      color: rate >= 0.95 ? "var(--accent)" : rate >= 0.8 ? "var(--amber)" : "#ff6b6b",
                    }}
                  >
                    {(rate * 100).toFixed(1)}%
                  </td>
                  <td style={{ ...cell, textAlign: "right", color: profitOf(r) > 0 ? "var(--accent)" : "var(--text)" }}>
                    ${formatUSDC(profitOf(r))}
                  </td>
                  <td style={{ ...cell, textAlign: "right" }}>{stakeOf(r) > 0 ? `$${formatUSDC(stakeOf(r))}` : "—"}</td>
                  <td style={{ ...cell, textAlign: "right", color: "var(--text-dim)" }}>{resp > 0 ? `${resp}ms` : "—"}</td>
                  <td style={{ ...cell, textAlign: "center" }}>
                    <span
                      style={{
                        padding: "2px 8px",
                        borderRadius: 10,
                        fontSize: 10,
                        fontFamily: "monospace",
                        background: active ? "rgba(230,172,47,0.12)" : "rgba(0,229,160,0.08)",
                        color: active ? "var(--amber)" : "var(--accent)",
                        border: `1px solid ${active ? "var(--amber)" : "var(--accent)"}`,
                      }}
                    >
                      {active ? "ACTIVE DRAW" : "IDLE"}
                    </span>
                  </td>
                </tr>
              );
            })}
          </tbody>
        </table>
      </div>
    </div>
  );
}
