"use client";

import { useEffect, useState } from "react";
import { fetchState, KeeperRow } from "../../lib/api";

const FALLBACK: KeeperRow[] = [
  { address: "GCR36Y5AHRAMJGHJLA4EFORJKR3E4D4QVIMPFM26MWAP77DAQ463ZTGZ", name: "keeper-alpha", active: true },
  { address: "GBOE5QCNDXNSVSEMU3GJ3INAJITX44UOK5D5YXRIX523DYBSTPCS546F", name: "keeper-beta", active: true },
];

function shortAddr(addr: string): string {
  if (addr.length > 10) return addr.slice(0, 6) + "…" + addr.slice(-4);
  return addr;
}

export default function KeeperRegistry() {
  const [keepers, setKeepers] = useState<KeeperRow[]>(FALLBACK);

  useEffect(() => {
    fetchState().then((s) => {
      if (s?.keepers && s.keepers.length > 0) setKeepers(s.keepers);
    });
    const id = setInterval(() => {
      fetchState().then((s) => {
        if (s?.keepers && s.keepers.length > 0) setKeepers(s.keepers);
      });
    }, 15000);
    return () => clearInterval(id);
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
