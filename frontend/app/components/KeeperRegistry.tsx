"use client";

import { useEffect, useState } from "react";
import { KeeperRow } from "../../lib/api";
import { queryKeeper, queryKeepers } from "../../lib/stellar";

// Correct registered operators — used only until the on-chain read returns.
const FALLBACK: KeeperRow[] = [
  { address: "GCC52N6U63PWM4GVUJK7T54W3X2GW2YKWOLZWN7TX7LMDU6LCOVZ3YVF", name: "keeper-alpha", active: true },
  { address: "GDQ7VA37AB7YRQ6CNNKFFWTR2QQ5Z232GPHX5U6IQCQFENTASBAV6DCV", name: "keeper-beta", active: true },
  { address: "GA472SZPEXVDKEN7BAGJAFVBDB74G37GOAHYFWUPC4Q62DDPTAGIQQXT", name: "keeper-gamma", active: true },
];

function shortAddr(addr: string): string {
  if (addr.length > 10) return addr.slice(0, 6) + "…" + addr.slice(-4);
  return addr;
}

export default function KeeperRegistry() {
  const [keepers, setKeepers] = useState<KeeperRow[]>(FALLBACK);

  useEffect(() => {
    let cancelled = false;
    // Read the full operator set straight from the registry on-chain so every
    // registered keeper shows (the keeper API only reports its own identity).
    const refresh = async () => {
      const addrs = await queryKeepers();
      if (cancelled || addrs.length === 0) return;
      const infos = await Promise.all(addrs.map((a) => queryKeeper(a)));
      if (cancelled) return;
      setKeepers(
        addrs.map((address, i) => ({
          address,
          name: infos[i]?.name || `keeper-${address.slice(0, 4).toLowerCase()}`,
          active: infos[i]?.active ?? true,
        })),
      );
    };
    refresh();
    const id = setInterval(refresh, 15000);
    return () => {
      cancelled = true;
      clearInterval(id);
    };
  }, []);

  return (
    <section
      className="py-24 px-6"
      style={{ borderTop: "1px solid var(--border)" }}
    >
      <div className="max-w-6xl mx-auto">
        <div className="flex flex-wrap items-end justify-between gap-4 mb-12">
          <div>
            <p
              className="text-xs font-mono mb-2"
              style={{ color: "var(--text-dim)", letterSpacing: "0.12em" }}
            >
              ON-CHAIN
            </p>
            <h2
              className="font-syne font-700"
              style={{ fontSize: "clamp(1.4rem, 3vw, 2rem)", color: "var(--text)" }}
            >
              KeeperRegistry
            </h2>
          </div>
          <div
            className="text-xs font-mono px-3 py-1 border flex items-center gap-2"
            style={{ borderColor: "var(--border)", color: "var(--text-dim)" }}
          >
            <span className="status-dot" />
            <span>{keepers.length} operators registered</span>
          </div>
        </div>

        <div className="overflow-x-auto">
        <div
          className="border overflow-hidden"
          style={{ borderColor: "var(--border)", minWidth: "440px" }}
        >
          <div
            className="grid grid-cols-3 px-4 py-3 text-xs font-mono border-b"
            style={{
              borderColor: "var(--border)",
              background: "var(--surface)",
              color: "var(--text-dim)",
              letterSpacing: "0.08em",
            }}
          >
            <span>ADDRESS</span>
            <span>NAME</span>
            <span>STATUS</span>
          </div>

          {keepers.map((k, i) => (
            <div
              key={i}
              className="grid grid-cols-3 px-4 py-4 text-xs font-mono border-b last:border-b-0 transition-colors duration-150"
              style={{ borderColor: "var(--border)" }}
              onMouseEnter={(e) => (e.currentTarget.style.background = "var(--surface)")}
              onMouseLeave={(e) => (e.currentTarget.style.background = "transparent")}
            >
              <span style={{ color: "var(--accent)", fontFamily: "DM Mono, monospace" }}>
                {shortAddr(k.address)}
              </span>
              <span style={{ color: "var(--text)" }}>{k.name}</span>
              <span className="flex items-center gap-2">
                <span className="status-dot" style={{ background: k.active ? "var(--accent)" : "var(--red)" }} />
                <span style={{ color: k.active ? "var(--accent)" : "var(--red)" }}>
                  {k.active ? "ACTIVE" : "INACTIVE"}
                </span>
              </span>
            </div>
          ))}
        </div>
        </div>

        <div
          className="mt-4 text-xs font-mono"
          style={{ color: "var(--text-dim)" }}
        >
          Contract:{" "}
          <a
            href={`https://stellar.expert/explorer/testnet/contract/${process.env.NEXT_PUBLIC_REGISTRY_CONTRACT ?? ""}`}
            target="_blank"
            rel="noopener noreferrer"
            style={{ color: "var(--accent)", textDecoration: "none" }}
            onMouseEnter={(e) => (e.currentTarget.style.textDecoration = "underline")}
            onMouseLeave={(e) => (e.currentTarget.style.textDecoration = "none")}
          >
            {shortAddr(process.env.NEXT_PUBLIC_REGISTRY_CONTRACT ?? "not deployed")}
          </a>
          {" · "}
          <span style={{ color: "var(--text-dim)" }}>Soroban Testnet</span>
        </div>
      </div>
    </section>
  );
}
