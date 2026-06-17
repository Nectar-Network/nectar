"use client";

import { useEffect, useState } from "react";
import { fetchState, PosRow } from "../../lib/api";
import { hfColor } from "./ds";

function hfLabel(hf: number): string {
  if (hf < 1.0) return "LIQUIDATABLE";
  if (hf < 1.2) return "AT RISK";
  return "healthy";
}

function shortAddr(addr: string): string {
  if (addr.length > 10) return addr.slice(0, 6) + "…" + addr.slice(-4);
  return addr;
}

export default function MonitorFeed() {
  const [positions, setPositions] = useState<PosRow[]>([]);
  const [live, setLive] = useState(false);

  useEffect(() => {
    // Real positions only — when no keeper is streaming, show an honest empty
    // state (never fabricated/animated health factors).
    const load = () =>
      fetchState().then((s) => {
        if (s?.positions && s.positions.length > 0) {
          setPositions(s.positions);
          setLive(true);
        } else {
          setPositions([]);
          setLive(false);
        }
      });
    load();
    const id = setInterval(load, 10000);
    return () => clearInterval(id);
  }, []);

  return (
    <section className="py-24 px-6" style={{ borderTop: "1px solid var(--border)" }}>
      <div className="max-w-6xl mx-auto">
        <div className="flex flex-wrap items-end justify-between gap-4 mb-12">
          <div>
            <p className="text-xs font-mono mb-2" style={{ color: "var(--text-dim)", letterSpacing: "0.12em" }}>
              MONITOR
            </p>
            <h2 className="font-syne font-700" style={{ fontSize: "clamp(1.4rem, 3vw, 2rem)", color: "var(--text)" }}>
              Pool Position Health
            </h2>
          </div>
          <div className="text-xs font-mono flex items-center gap-2" style={{ color: "var(--text-dim)" }}>
            <span className="status-dot" style={{ background: live ? "var(--accent)" : "var(--text-mute)" }} />
            <span>{live ? "live data · polling 10s" : "awaiting keeper API"}</span>
          </div>
        </div>

        {positions.length === 0 ? (
          <div
            className="border"
            style={{ borderColor: "var(--border)", background: "var(--card-fill)", padding: "40px 24px", textAlign: "center", borderRadius: 4 }}
          >
            <span className="text-xs font-mono" style={{ color: "var(--text-dim)" }}>
              No positions monitored — connect a running keeper to stream live Blend pool health here.
            </span>
          </div>
        ) : (
          <div className="overflow-x-auto">
            <div className="border" style={{ borderColor: "var(--border)", minWidth: "520px" }}>
              <div
                className="grid grid-cols-3 px-4 py-3 text-xs font-mono border-b"
                style={{ borderColor: "var(--border)", background: "var(--surface)", color: "var(--text-dim)", letterSpacing: "0.08em" }}
              >
                <span>ADDRESS</span>
                <span>HEALTH FACTOR</span>
                <span>STATUS</span>
              </div>
              {positions.map((p, i) => (
                <div
                  key={i}
                  className="grid grid-cols-3 px-4 py-4 text-xs font-mono border-b last:border-b-0 transition-all duration-500"
                  style={{ borderColor: "var(--border)", background: p.hf < 1.0 ? "var(--red-fill)" : "transparent" }}
                >
                  <span style={{ color: "var(--accent)" }}>{shortAddr(p.address)}</span>
                  <span className="flex items-center gap-2">
                    <span style={{ color: hfColor(p.hf), fontVariantNumeric: "tabular-nums", transition: "color 0.5s ease" }}>
                      {p.hf.toFixed(4)}
                    </span>
                    {p.hf < 1.2 && (
                      <span
                        className="px-1.5 py-0.5 text-xs"
                        style={{
                          background: p.hf < 1.0
                            ? "color-mix(in srgb, var(--red) 15%, transparent)"
                            : "color-mix(in srgb, var(--amber) 15%, transparent)",
                          color: hfColor(p.hf),
                        }}
                      >
                        {hfLabel(p.hf)}
                      </span>
                    )}
                  </span>
                  <span style={{ color: "var(--text-dim)" }}>{hfLabel(p.hf)}</span>
                </div>
              ))}
            </div>
          </div>
        )}
      </div>
    </section>
  );
}
