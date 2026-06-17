"use client";

import { useEffect, useState } from "react";
import { fetchPerformance, successRate } from "../../lib/api";
import { KeeperInfoOnchain, queryKeeper, queryKeepers } from "../../lib/stellar";
import { Card, Pill, StatusDot, SectionHead, Btn, keeperColor, successColor, fmtUSD, fmtNum } from "./ds";

interface KeeperCard {
  address: string;
  name: string;
  winRate: number | null; // 0–100; null = no track record yet
  fills: number; // successful fills (not attempts)
  profit: number; // USDC dollars
  hasActiveDraw: boolean;
  active: boolean;
}

export default function NetworkNow() {
  const [cards, setCards] = useState<KeeperCard[]>([]);

  useEffect(() => {
    let cancelled = false;
    const refresh = async () => {
      const perf = await fetchPerformance();
      const stats = perf?.keeper_stats ?? {};
      const nameByAddr = new Map<string, string>();
      Object.values(stats).forEach((k) => nameByAddr.set(k.address, k.name));

      const onChain = await queryKeepers();
      const addrs = Array.from(new Set([...onChain, ...Object.values(stats).map((k) => k.address)])).filter(Boolean);
      const infos = await Promise.all(addrs.map((a) => queryKeeper(a)));
      if (cancelled) return;

      const statByAddr = new Map(Object.values(stats).map((k) => [k.address, k]));
      const rows: KeeperCard[] = addrs.map((address, i) => {
        const info: KeeperInfoOnchain | null = infos[i];
        const api = statByAddr.get(address);
        const executions = info?.totalExecutions ?? api?.liquidations ?? 0;
        const fills = info?.successfulFills ?? api?.liquidations ?? 0;
        const profitStroops = info?.totalProfit ?? api?.total_profit ?? 0;
        return {
          address,
          name: nameByAddr.get(address) ?? `keeper-${address.slice(0, 4).toLowerCase()}`,
          // No track record yet → null (rendered as "—"), never a fabricated 100%.
          winRate: executions > 0 ? Math.round(successRate(executions, fills) * 1000) / 10 : null,
          fills,
          profit: profitStroops / 1e7,
          hasActiveDraw: info?.hasActiveDraw ?? api?.has_active_draw ?? false,
          active: info?.active ?? true,
        };
      });
      rows.sort((a, b) => b.profit - a.profit);
      setCards(rows.slice(0, 6));
    };
    refresh();
    const t = setInterval(refresh, 20_000);
    return () => {
      cancelled = true;
      clearInterval(t);
    };
  }, []);

  return (
    <section style={{ padding: "96px 24px", borderTop: "1px solid var(--border)" }}>
      <div className="home-wrap" style={{ padding: 0 }}>
        <SectionHead
          eyebrow="The network · right now"
          title="Competing keepers, one shared vault"
          right={<Btn href="/dashboard/keepers" small>Full leaderboard →</Btn>}
        />
        {cards.length === 0 ? (
          <Card style={{ padding: 28, textAlign: "center" }}>
            <span style={{ fontFamily: "var(--font-mono)", fontSize: 13, color: "var(--text-dim)" }}>
              No keepers registered yet — connect the keeper API / registry to populate the network.
            </span>
          </Card>
        ) : (
          <div className="net-grid">
            {cards.map((k) => (
              <Card key={k.address} style={{ padding: 18 }}>
                <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 16 }}>
                  <span style={{ display: "flex", alignItems: "center", gap: 8, fontFamily: "var(--font-mono)", fontSize: 13, color: keeperColor(k.name) }}>
                    <StatusDot size={7} color={keeperColor(k.name)} glow={k.active} />{k.name}
                  </span>
                  {k.hasActiveDraw ? <Pill color="var(--amber)">drawing</Pill>
                    : k.active ? <Pill color="var(--text-dim)">idle</Pill>
                    : <Pill color="var(--red)">offline</Pill>}
                </div>
                <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr 1fr", gap: 10 }}>
                  {([
                    ["Win", k.winRate === null ? "—" : k.winRate + "%", k.winRate === null ? "var(--text-dim)" : successColor(k.winRate)],
                    ["Fills", fmtNum(k.fills), "var(--text)"],
                    ["Profit", fmtUSD(k.profit), "var(--accent)"],
                  ] as [string, string, string][]).map(([l, v, c]) => (
                    <div key={l}>
                      <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.06em", textTransform: "uppercase", color: "var(--text-mute)", marginBottom: 5 }}>{l}</div>
                      <div style={{ fontFamily: "var(--font-mono)", fontSize: 14, color: c, fontVariantNumeric: "tabular-nums" }}>{v}</div>
                    </div>
                  ))}
                </div>
              </Card>
            ))}
          </div>
        )}
      </div>
    </section>
  );
}
