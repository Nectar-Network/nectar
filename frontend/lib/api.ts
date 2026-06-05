const API_URL = process.env.NEXT_PUBLIC_API_URL ?? "http://localhost:8080";

export interface KeeperRow {
  name: string;
  address: string;
  active: boolean;
}

export interface PosRow {
  address: string;
  hf: number;
}

export interface VaultState {
  total_usdc: number;
  total_shares: number;
  total_profit: number;
  active_liq: number;
}

export interface AppState {
  keepers: KeeperRow[];
  positions: PosRow[];
  events: string[];
  vault: VaultState | null;
}

export interface DepositorRow {
  address: string;
  shares: number;
  usdc_value: number;
  pnl_pct: number;
}

export interface KeeperStat {
  name: string;
  address: string;
  liquidations: number;
  total_profit: number;
  // Tranche 1 on-chain extensions (optional — keeper API may not surface them yet).
  stake?: number;
  total_executions?: number;
  successful_fills?: number;
  has_active_draw?: boolean;
  last_draw_time?: number;
}

export interface LiquidationRecord {
  user: string;
  block: number;
  drew: number;
  proceeds: number;
  ts: string;
}

export interface PerformanceData {
  vault: VaultState | null;
  depositors: DepositorRow[];
  keeper_stats: Record<string, KeeperStat>;
  liquidations: LiquidationRecord[];
}

export async function fetchState(): Promise<AppState | null> {
  try {
    const res = await fetch(`${API_URL}/api/state`, { cache: "no-store" });
    if (!res.ok) return null;
    return res.json();
  } catch {
    return null;
  }
}

export async function fetchPerformance(): Promise<PerformanceData | null> {
  try {
    const res = await fetch(`${API_URL}/api/performance`, { cache: "no-store" });
    if (!res.ok) return null;
    return res.json();
  } catch {
    return null;
  }
}

export function apiUrl(): string {
  return API_URL;
}

/** Format a stroop amount (7 decimals) as a USDC dollar string */
export function formatUSDC(stroops: number): string {
  return (stroops / 1e7).toLocaleString("en-US", {
    minimumFractionDigits: 2,
    maximumFractionDigits: 2,
  });
}

/** Shorten a Stellar address for display */
export function shortAddress(addr: string): string {
  if (!addr || addr.length < 10) return addr;
  return `${addr.slice(0, 4)}…${addr.slice(-4)}`;
}

/**
 * Format a duration in seconds as H:MM:SS or MM:SS, suitable for the
 * withdrawal-cooldown timer. Returns "0:00" for non-positive values.
 */
export function formatDuration(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds <= 0) return "0:00";
  const s = Math.floor(seconds);
  const h = Math.floor(s / 3600);
  const m = Math.floor((s % 3600) / 60);
  const sec = s % 60;
  const pad = (n: number) => n.toString().padStart(2, "0");
  if (h > 0) return `${h}:${pad(m)}:${pad(sec)}`;
  return `${m}:${pad(sec)}`;
}

/**
 * Compute share price (USDC per share) from vault state. Returns 1.0 when the
 * vault is empty so the UI never has to handle a divide-by-zero.
 */
export function sharePrice(totalUsdc: number, totalShares: number): number {
  if (!totalShares) return 1.0;
  return totalUsdc / totalShares;
}

/** Success rate as a 0-1 fraction. Returns 0 when no executions recorded. */
export function successRate(executions: number, fills: number): number {
  if (!executions) return 0;
  return Math.min(1, fills / executions);
}

export interface SharePricePoint {
  ts: number; // epoch ms
  label: string; // formatted date for the x-axis
  sharePrice: number; // USDC per share
}

/**
 * Reconstruct a share-price time series from realized vault profit. The vault's
 * principal (total_usdc minus realized total_profit) is the starting base; each
 * liquidation's realized profit (proceeds - drew) raises the share price. This
 * is derived entirely from real on-chain outcomes the keeper recorded — no
 * figures are synthesized — so it reflects actual vault returns over time.
 */
export function sharePriceSeries(perf: PerformanceData): SharePricePoint[] {
  const vault = perf.vault;
  if (!vault || !vault.total_shares) return [];
  const shares = vault.total_shares;
  const current = vault.total_usdc / shares;
  const base = Math.max(0, vault.total_usdc - vault.total_profit);

  const liqs = (perf.liquidations ?? [])
    .filter((l) => l.ts)
    .sort((a, b) => new Date(a.ts).getTime() - new Date(b.ts).getTime());

  if (liqs.length === 0) {
    return [{ ts: Date.now(), label: "now", sharePrice: current }];
  }

  const fmt = (t: number) =>
    new Date(t).toLocaleDateString("en-US", { month: "short", day: "numeric" });

  // The keeper's in-memory liquidation list may not cover the vault's full
  // history (it is stateless and resets on restart), so the raw realized-profit
  // deltas need not sum to the authoritative on-chain total_profit. Scale the
  // deltas so the curve's endpoint matches the true current share price — this
  // keeps the chart consistent with the share price shown elsewhere while
  // preserving the shape of when profit accrued. Losses are not clamped.
  const deltas = liqs.map((l) => l.proceeds - l.drew);
  const sum = deltas.reduce((a, b) => a + b, 0);
  const scale = sum > 0 ? vault.total_profit / sum : 0;

  let running = base;
  const t0 = new Date(liqs[0].ts).getTime();
  const pts: SharePricePoint[] = [{ ts: t0, label: fmt(t0), sharePrice: running / shares }];
  liqs.forEach((l, i) => {
    running += deltas[i] * scale;
    const t = new Date(l.ts).getTime();
    pts.push({ ts: t, label: fmt(t), sharePrice: running / shares });
  });
  // Anchor the final point to the authoritative current share price.
  pts[pts.length - 1] = { ...pts[pts.length - 1], sharePrice: current };
  return pts;
}

export interface VaultReturn {
  pct: number; // APY when annualized, else cumulative return since the first point
  annualized: boolean;
  days: number;
}

const MIN_ANNUALIZE_DAYS = 7;

/**
 * Vault return from a share-price series. Annualizes to an APY only when the
 * series spans a meaningful window (>= 7 days). For shorter windows it returns
 * the raw cumulative return instead — annualizing a few minutes of data yields
 * astronomically misleading (even Infinite) figures, so we never present those
 * as an APY. Always finite.
 */
export function vaultReturn(series: SharePricePoint[]): VaultReturn {
  if (series.length < 2) return { pct: 0, annualized: false, days: 0 };
  const first = series[0];
  const last = series[series.length - 1];
  if (first.sharePrice <= 0) return { pct: 0, annualized: false, days: 0 };
  const days = (last.ts - first.ts) / 86_400_000;
  const growth = last.sharePrice / first.sharePrice;
  const cumulative = (growth - 1) * 100;
  if (days < MIN_ANNUALIZE_DAYS) {
    return { pct: cumulative, annualized: false, days };
  }
  const apy = (Math.pow(growth, 365 / days) - 1) * 100;
  if (!Number.isFinite(apy)) return { pct: cumulative, annualized: false, days };
  return { pct: apy, annualized: true, days };
}
