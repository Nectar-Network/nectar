"use client";

// Nectar design-system primitives, ported from the Claude Design handoff
// (shared/ui.jsx + shared/charts.jsx) to React/TSX. Pure DS vocabulary:
// hairline borders, mono type, mint accent, status dots.

import React, { useState, useRef, useEffect } from "react";

type CSS = React.CSSProperties;

// ── formatters ────────────────────────────────────────────────────────────────
export const fmtUSD = (n: number, opts: { sign?: boolean; dp?: number } = {}): string => {
  const { sign = false, dp = 0 } = opts;
  const s = `$${Math.abs(n).toLocaleString(undefined, { minimumFractionDigits: dp, maximumFractionDigits: dp })}`;
  if (sign) return (n < 0 ? "-" : "+") + s;
  return (n < 0 ? "-" : "") + s;
};
export const fmtNum = (n: number): string => n.toLocaleString();
export const fmtPct = (n: number, sign = true): string => `${sign && n >= 0 ? "+" : ""}${n.toFixed(2)}%`;
export const fmtClock = (ts: Date): string =>
  `${String(ts.getHours()).padStart(2, "0")}:${String(ts.getMinutes()).padStart(2, "0")}:${String(ts.getSeconds()).padStart(2, "0")}`;
export const fmtDate = (ts: Date): string => ts.toLocaleDateString("en-US", { month: "short", day: "numeric" });
export const fmtAgo = (ts: Date): string => {
  const s = Math.floor((Date.now() - ts.getTime()) / 1000);
  if (s < 60) return `${s}s ago`;
  const m = Math.floor(s / 60);
  if (m < 60) return `${m}m ago`;
  const h = Math.floor(m / 60);
  if (h < 24) return `${h}h ago`;
  return `${Math.floor(h / 24)}d ago`;
};
export const hfColor = (hf: number): string =>
  hf < 1.0 ? "var(--red)" : hf < 1.2 ? "var(--amber)" : "var(--text-dim)";

// ── keeper / success colors (ported from ui.jsx) ────────────────────────────────
export const keeperColor = (name: string): string => {
  const map: Record<string, string> = {
    alpha: "var(--accent)", beta: "var(--info)", gamma: "#9b8cff",
    delta: "var(--amber)", epsilon: "#f06595", zeta: "var(--text-dim)",
  };
  return map[name.replace("keeper-", "")] || "var(--text)";
};
export const successColor = (r: number): string =>
  r >= 95 ? "var(--accent)" : r >= 80 ? "var(--amber)" : "var(--red)";

// ── StatusDot ─────────────────────────────────────────────────────────────────
export const StatusDot = ({ color = "var(--accent)", size = 6, glow = true, style }:
  { color?: string; size?: number; glow?: boolean; style?: CSS }) => (
  <span
    style={{
      width: size, height: size, borderRadius: "50%", background: color, display: "inline-block",
      boxShadow: glow ? `0 0 6px ${color}` : "none",
      animation: "pulse2 2s ease-in-out infinite", ...style,
    }}
  />
);

// ── Pill — tiny state tag ───────────────────────────────────────────────────────
export const Pill = ({ color = "var(--text-dim)", fill, children, style }:
  { color?: string; fill?: string; children: React.ReactNode; style?: CSS }) => (
  <span style={{
    fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.06em",
    padding: "2px 7px", color, background: fill || "transparent",
    border: fill ? "none" : `1px solid ${color === "var(--text-dim)" ? "var(--border)" : color}`,
    borderRadius: 2, whiteSpace: "nowrap", textTransform: "uppercase",
    display: "inline-flex", alignItems: "center", gap: 5, ...style,
  }}>{children}</span>
);

// ── Eyebrow + SectionHead ───────────────────────────────────────────────────────
export const Eyebrow = ({ children, color = "var(--text-dim)", style }:
  { children: React.ReactNode; color?: string; style?: CSS }) => (
  <p style={{ fontFamily: "var(--font-mono)", fontSize: 11, letterSpacing: "0.12em",
    textTransform: "uppercase", color, margin: 0, ...style }}>{children}</p>
);

export const SectionHead = ({ eyebrow, title, right, style }:
  { eyebrow?: React.ReactNode; title: React.ReactNode; right?: React.ReactNode; style?: CSS }) => (
  <div style={{ display: "flex", alignItems: "flex-end", justifyContent: "space-between",
    gap: 24, flexWrap: "wrap", marginBottom: 28, ...style }}>
    <div>
      {eyebrow && <Eyebrow style={{ marginBottom: 10 }}>{eyebrow}</Eyebrow>}
      <h2 style={{ fontFamily: "var(--font-display)", fontWeight: 700,
        fontSize: "clamp(1.3rem, 2.6vw, 1.9rem)", color: "var(--text)", margin: 0, letterSpacing: "-0.01em" }}>{title}</h2>
    </div>
    {right}
  </div>
);

// ── Card — hairline panel ───────────────────────────────────────────────────────
export const Card = ({ children, accent = false, radius = 4, style, ...rest }:
  { children: React.ReactNode; accent?: boolean; radius?: number; style?: CSS } & React.HTMLAttributes<HTMLDivElement>) => (
  <div {...rest} style={{
    border: `1px solid ${accent ? "var(--accent)" : "var(--border)"}`,
    background: accent ? "var(--card-fill-accent)" : "var(--card-fill)",
    borderRadius: radius, overflow: "hidden", ...style,
  }}>{children}</div>
);

// ── Btn — primary/secondary, hover-invert ────────────────────────────────────────
export const Btn = ({ primary, href, target, onClick, children, small, style }:
  { primary?: boolean; href?: string; target?: string; onClick?: () => void;
    children: React.ReactNode; small?: boolean; style?: CSS }) => {
  const [hover, setHover] = useState(false);
  const base: CSS = {
    fontFamily: "var(--font-mono)", fontSize: small ? 12 : 13, letterSpacing: "0.03em",
    padding: small ? "7px 14px" : "10px 20px", border: "1px solid", borderRadius: 2,
    textDecoration: "none", cursor: "pointer", display: "inline-flex", alignItems: "center",
    gap: 8, transition: "all 200ms var(--ease-out)", whiteSpace: "nowrap", ...style,
  };
  const s: CSS = primary
    ? hover
      ? { ...base, background: "transparent", color: "var(--accent)", borderColor: "var(--accent)" }
      : { ...base, background: "var(--accent)", color: "var(--bg)", borderColor: "var(--accent)" }
    : hover
      ? { ...base, background: "transparent", color: "var(--text)", borderColor: "var(--text-dim)" }
      : { ...base, background: "transparent", color: "var(--text-dim)", borderColor: "var(--border)" };
  return (
    <a href={href} target={target} rel={target ? "noopener noreferrer" : undefined}
      onClick={onClick} style={s}
      onMouseEnter={() => setHover(true)} onMouseLeave={() => setHover(false)}>{children}</a>
  );
};

// ── Sparkline — inline SVG, no axes (ported from charts.jsx) ─────────────────────
export const Sparkline = ({ data, width = 120, height = 34, color = "var(--accent)", fill = true, strokeWidth = 1.4 }:
  { data: { value: number }[]; width?: number; height?: number; color?: string; fill?: boolean; strokeWidth?: number }) => {
  const pts = data.length ? data : [{ value: 0 }, { value: 0 }];
  const vals = pts.map((p) => p.value);
  let min = Math.min(...vals), max = Math.max(...vals);
  if (max === min) max = min + 1;
  const X = (i: number) => (pts.length === 1 ? width / 2 : (i / (pts.length - 1)) * width);
  const Y = (v: number) => height - 2 - ((v - min) / (max - min)) * (height - 4);
  const line = pts.map((p, i) => `${i ? "L" : "M"}${X(i).toFixed(1)},${Y(p.value).toFixed(1)}`).join(" ");
  const gid = "sp" + Math.round(min * 1000) + pts.length;
  return (
    <svg width={width} height={height} style={{ display: "block", maxWidth: "100%" }}>
      <defs>
        <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
          <stop offset="0%" stopColor={color} stopOpacity="0.2" />
          <stop offset="100%" stopColor={color} stopOpacity="0" />
        </linearGradient>
      </defs>
      {fill && <path d={`${line} L${width},${height} L0,${height} Z`} fill={`url(#${gid})`} />}
      <path d={line} fill="none" stroke={color} strokeWidth={strokeWidth} strokeLinejoin="round" strokeLinecap="round" />
    </svg>
  );
};

// ── CardHead — uppercase hairline header row ─────────────────────────────────────
export const CardHead = ({ children, right, style }:
  { children: React.ReactNode; right?: React.ReactNode; style?: CSS }) => (
  <div style={{ padding: "12px 16px", borderBottom: "1px solid var(--border)", display: "flex",
    alignItems: "center", justifyContent: "space-between", gap: 12, fontFamily: "var(--font-mono)",
    fontSize: 11, letterSpacing: "0.08em", textTransform: "uppercase", color: "var(--text-dim)", ...style }}>
    <span style={{ whiteSpace: "nowrap" }}>{children}</span>{right}
  </div>
);

// ── StatTile — big number cell ──────────────────────────────────────────────────
export const StatTile = ({ label, value, sub, accent = false, delta, style }:
  { label: React.ReactNode; value: React.ReactNode; sub?: React.ReactNode; accent?: boolean; delta?: number | null; style?: CSS }) => (
  <Card style={{ padding: "18px 20px", ...style }}>
    <div style={{ fontFamily: "var(--font-mono)", fontSize: 10, letterSpacing: "0.08em",
      textTransform: "uppercase", color: "var(--text-dim)", marginBottom: 10 }}>{label}</div>
    <div style={{ fontFamily: "var(--font-display)", fontWeight: 700, fontSize: 26, lineHeight: 1,
      color: accent ? "var(--accent)" : "var(--text)", fontVariantNumeric: "tabular-nums" }}>{value}</div>
    {(sub || delta != null) && (
      <div style={{ marginTop: 9, fontFamily: "var(--font-mono)", fontSize: 11, color: "var(--text-dim)",
        display: "flex", alignItems: "center", gap: 8 }}>
        {delta != null && <span style={{ color: delta >= 0 ? "var(--accent)" : "var(--red)" }}>{fmtPct(delta)}</span>}
        {sub && <span>{sub}</span>}
      </div>
    )}
  </Card>
);

// ── MiniBars — horizontal share bar ──────────────────────────────────────────────
export const MiniBars = ({ value, max, color = "var(--accent)", height = 4 }:
  { value: number; max: number; color?: string; height?: number }) => (
  <div style={{ background: "var(--border)", borderRadius: 2, height, width: "100%", overflow: "hidden" }}>
    <div style={{ width: `${Math.min(100, max > 0 ? (value / max) * 100 : 0)}%`, height: "100%", background: color, borderRadius: 2 }} />
  </div>
);

// ── LineChart — self-measuring SVG chart (hairline grid, mint line + area, hover) ──
function useWidth(): [React.RefObject<HTMLDivElement>, number] {
  const ref = useRef<HTMLDivElement>(null);
  const [w, setW] = useState(720);
  useEffect(() => {
    if (!ref.current) return;
    const ro = new ResizeObserver((entries) => {
      const cw = entries[0].contentRect.width;
      if (cw > 0) setW(cw);
    });
    ro.observe(ref.current);
    return () => ro.disconnect();
  }, []);
  return [ref, w];
}

export const LineChart = ({
  data, height = 240, color = "var(--accent)", variant = "area",
  valueFmt = (v: number) => v.toFixed(2), yTicks = 4, xTickEvery,
  padL = 56, padR = 16, padT = 14, padB = 26, baselineZero = false, label = "value",
}: {
  data: { value: number; label?: string }[]; height?: number; color?: string;
  variant?: "area" | "line" | "bars"; valueFmt?: (v: number) => string; yTicks?: number;
  xTickEvery?: number; padL?: number; padR?: number; padT?: number; padB?: number;
  baselineZero?: boolean; label?: string;
}) => {
  const [ref, width] = useWidth();
  const pts = data.map((d, i) => ({ value: d.value, label: d.label ?? String(i) }));
  const [hover, setHover] = useState<number | null>(null);
  if (pts.length === 0) return <div ref={ref} style={{ height }} />;

  const innerW = Math.max(10, width - padL - padR);
  const innerH = height - padT - padB;
  const vals = pts.map((p) => p.value);
  let min = Math.min(...vals), max = Math.max(...vals);
  if (baselineZero) min = Math.min(0, min);
  const padv = (max - min) * 0.12 || 1;
  max += padv; min -= baselineZero ? 0 : padv;
  if (max === min) max = min + 1;

  const X = (i: number) => padL + (pts.length === 1 ? innerW / 2 : (i / (pts.length - 1)) * innerW);
  const Y = (v: number) => padT + innerH - ((v - min) / (max - min)) * innerH;
  const linePath = pts.map((p, i) => `${i ? "L" : "M"}${X(i).toFixed(1)},${Y(p.value).toFixed(1)}`).join(" ");
  const areaPath = `${linePath} L${X(pts.length - 1).toFixed(1)},${(padT + innerH).toFixed(1)} L${X(0).toFixed(1)},${(padT + innerH).toFixed(1)} Z`;

  const yLines: { v: number; y: number }[] = [];
  for (let i = 0; i <= yTicks; i++) { const v = min + (i / yTicks) * (max - min); yLines.push({ v, y: Y(v) }); }
  const every = xTickEvery || Math.ceil(pts.length / 7);
  const gid = "g" + Math.abs(label.split("").reduce((a, c) => a + c.charCodeAt(0), 0)) + Math.round(min);
  const barW = variant === "bars" ? Math.max(2, (innerW / pts.length) * 0.6) : 0;

  const onMove = (e: React.MouseEvent<SVGSVGElement>) => {
    const rect = e.currentTarget.getBoundingClientRect();
    const x = e.clientX - rect.left;
    const i = Math.round(((x - padL) / innerW) * (pts.length - 1));
    if (i >= 0 && i < pts.length) setHover(i);
  };

  return (
    <div ref={ref} style={{ width: "100%", position: "relative" }}>
      <svg width={width} height={height} onMouseMove={onMove} onMouseLeave={() => setHover(null)}
        style={{ display: "block", fontVariantNumeric: "tabular-nums" }}>
        <defs>
          <linearGradient id={gid} x1="0" y1="0" x2="0" y2="1">
            <stop offset="0%" stopColor={color} stopOpacity="0.16" />
            <stop offset="100%" stopColor={color} stopOpacity="0" />
          </linearGradient>
        </defs>
        {yLines.map((g, i) => (
          <g key={i}>
            <line x1={padL} y1={g.y} x2={width - padR} y2={g.y} stroke="var(--border)" strokeWidth="1" />
            <text x={padL - 10} y={g.y + 3} textAnchor="end" fontFamily="var(--font-mono)" fontSize="10" fill="var(--text-mute)">{valueFmt(g.v)}</text>
          </g>
        ))}
        {pts.map((p, i) => (i % every === 0 || i === pts.length - 1) ? (
          <text key={i} x={X(i)} y={height - 8} textAnchor="middle" fontFamily="var(--font-mono)" fontSize="10" fill="var(--text-mute)">{p.label}</text>
        ) : null)}
        {variant === "bars" ? (
          pts.map((p, i) => (
            <rect key={i} x={X(i) - barW / 2} y={Y(p.value)} width={barW} height={padT + innerH - Y(p.value)} fill={color} fillOpacity={hover === i ? 0.95 : 0.55} />
          ))
        ) : (
          <>
            {variant === "area" && <path d={areaPath} fill={`url(#${gid})`} />}
            <path d={linePath} fill="none" stroke={color} strokeWidth="1.5" strokeLinejoin="round" strokeLinecap="round" />
          </>
        )}
        {hover != null && (
          <g>
            <line x1={X(hover)} y1={padT} x2={X(hover)} y2={padT + innerH} stroke="var(--text-dim)" strokeWidth="1" strokeDasharray="2 3" />
            <circle cx={X(hover)} cy={Y(pts[hover].value)} r="3.5" fill="var(--bg)" stroke={color} strokeWidth="1.5" />
          </g>
        )}
      </svg>
      {hover != null && (
        <div style={{ position: "absolute", top: padT, pointerEvents: "none",
          left: Math.min(Math.max(X(hover) + 8, padL), width - 150),
          background: "var(--surface-2)", border: "1px solid var(--border)", borderRadius: 2,
          padding: "6px 9px", fontFamily: "var(--font-mono)", fontSize: 11, whiteSpace: "nowrap" }}>
          <span style={{ color: "var(--text-mute)" }}>{pts[hover].label}</span>{"  "}
          <span style={{ color, fontWeight: 500 }}>{valueFmt(pts[hover].value)}</span>
        </div>
      )}
    </div>
  );
};
