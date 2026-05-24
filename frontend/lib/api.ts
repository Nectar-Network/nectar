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
