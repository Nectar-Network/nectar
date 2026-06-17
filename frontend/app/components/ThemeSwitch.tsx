"use client";

import { useEffect, useState } from "react";

const VIBES: [string, string][] = [
  ["terminal", "Terminal"],
  ["bloomberg", "Bloomberg"],
  ["clinical", "Clinical"],
];
const KEY = "nectar.vibe";

/** Set the active theme: updates <html data-vibe>, persists, and syncs instances. */
export function setVibe(v: string) {
  if (typeof document !== "undefined") document.documentElement.dataset.vibe = v;
  try {
    localStorage.setItem(KEY, v);
  } catch {
    /* storage unavailable — non-fatal */
  }
  if (typeof window !== "undefined") {
    window.dispatchEvent(new CustomEvent("nectar:vibe", { detail: v }));
  }
}

/** Segmented Terminal / Bloomberg / Clinical theme toggle, persisted across pages. */
export default function ThemeSwitch({ compact = false }: { compact?: boolean }) {
  const [vibe, setV] = useState("terminal");

  useEffect(() => {
    const current =
      (typeof document !== "undefined" && document.documentElement.dataset.vibe) ||
      (typeof localStorage !== "undefined" && localStorage.getItem(KEY)) ||
      "terminal";
    setV(current);
    const h = (e: Event) => setV((e as CustomEvent).detail as string);
    window.addEventListener("nectar:vibe", h);
    return () => window.removeEventListener("nectar:vibe", h);
  }, []);

  return (
    <div
      title="Theme"
      style={{
        display: "inline-flex", alignItems: "center", border: "1px solid var(--border)",
        borderRadius: 2, overflow: "hidden", background: "var(--surface)",
      }}
    >
      {VIBES.map(([id, label], i) => {
        const active = vibe === id;
        return (
          <button
            key={id}
            onClick={() => setVibe(id)}
            title={`${label} theme`}
            aria-pressed={active}
            style={{
              fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.07em",
              textTransform: "uppercase", padding: compact ? "5px 8px" : "5px 10px",
              cursor: "pointer", whiteSpace: "nowrap", border: "none",
              borderLeft: i ? "1px solid var(--border)" : "none",
              background: active ? "var(--accent)" : "transparent",
              color: active ? "var(--bg)" : "var(--text-dim)", transition: "all 150ms",
            }}
          >
            {compact ? label.slice(0, 1) : label}
          </button>
        );
      })}
    </div>
  );
}
