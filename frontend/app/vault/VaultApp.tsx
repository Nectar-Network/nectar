"use client";

import { useState, useEffect, useCallback, type CSSProperties } from "react";
import {
  formatUSDC,
  formatDuration,
  sharePrice,
  fetchPerformance,
  fetchFaucetInfo,
  requestFaucet,
  type FaucetInfo,
} from "../../lib/api";
import {
  connectWallet,
  disconnectWallet,
  depositToVault,
  withdrawFromVault,
  queryVaultBalance,
  queryVaultConfig,
  queryVaultState,
  queryDepositor,
  queryKeeper,
  queryRegistryConfig,
  registerKeeper,
  deregisterKeeper,
  getUsdcTrustline,
  addUsdcTrustline,
  fundWithFriendbot,
  shortAddr,
  USDC_ISSUER,
  type WalletState,
  type VaultConfig,
  type VaultStateOnchain,
  type DepositorOnchain,
  type KeeperInfoOnchain,
  type TrustlineStatus,
} from "../../lib/stellar";
import { Card, Btn, Pill, StatusDot, Eyebrow } from "../components/ds";

type Tab = "deposit" | "withdraw";
type TxStatus = "idle" | "simulating" | "signing" | "submitted" | "confirmed" | "error";

const VAULT_CONTRACT = process.env.NEXT_PUBLIC_VAULT_CONTRACT ?? "";
const REGISTRY_CONTRACT = process.env.NEXT_PUBLIC_REGISTRY_CONTRACT ?? "";

function walletDisplayName(id: string | undefined): string {
  switch (id) {
    case "freighter": return "Freighter";
    case "albedo": return "Albedo";
    case "xbull": return "xBull";
    case "lobstr": return "Lobstr";
    case "hana": return "Hana";
    case "rabet": return "Rabet";
    default: return "wallet";
  }
}

const mono: CSSProperties = { fontFamily: "var(--font-mono)" };

// ── StatLabel / StatRow — design's small uppercase label + big mono value ──────
function StatRow({ label, value, accent }: { label: string; value: React.ReactNode; accent?: boolean }) {
  return (
    <div>
      <div style={{ ...mono, fontSize: 10, color: "var(--text-dim)", letterSpacing: "0.06em", textTransform: "uppercase", marginBottom: 4 }}>
        {label}
      </div>
      <div style={{ ...mono, fontSize: 18, fontWeight: 600, color: accent ? "var(--accent)" : "var(--text)", fontVariantNumeric: "tabular-nums" }}>
        {value}
      </div>
    </div>
  );
}

function PanelLabel({ children, accent, right }: { children: React.ReactNode; accent?: boolean; right?: React.ReactNode }) {
  return (
    <div style={{ display: "flex", alignItems: "center", justifyContent: "space-between", marginBottom: 16 }}>
      <Eyebrow color={accent ? "var(--accent)" : "var(--text-dim)"} style={{ fontSize: 11 }}>{children}</Eyebrow>
      {right}
    </div>
  );
}

export default function VaultApp() {
  const [tab, setTab] = useState<Tab>("deposit");
  const [amount, setAmount] = useState("");
  const [wallet, setWallet] = useState<WalletState | null>(null);
  const [txStatus, setTxStatus] = useState<TxStatus>("idle");
  const [txHash, setTxHash] = useState("");
  const [error, setError] = useState("");
  const [vaultShares, setVaultShares] = useState<number>(0);
  const [vaultUsdcValue, setVaultUsdcValue] = useState<number>(0);
  const [vaultCfg, setVaultCfg] = useState<VaultConfig | null>(null);
  const [vaultState, setVaultState] = useState<VaultStateOnchain | null>(null);
  const [depositor, setDepositor] = useState<DepositorOnchain | null>(null);
  const [keeperInfo, setKeeperInfo] = useState<KeeperInfoOnchain | null>(null);
  const [registryMinStake, setRegistryMinStake] = useState<number | null>(null);
  const [keeperName, setKeeperName] = useState("");
  const [keeperBusy, setKeeperBusy] = useState(false);
  const [keeperError, setKeeperError] = useState("");
  const [depositorCount, setDepositorCount] = useState<number | null>(null);
  const [trustline, setTrustline] = useState<{ status: TrustlineStatus; balance: string } | null>(null);
  const [faucetInfo, setFaucetInfo] = useState<FaucetInfo | null>(null);
  const [fundingBusy, setFundingBusy] = useState<"" | "friendbot" | "trustline" | "faucet">("");
  const [fundingError, setFundingError] = useState("");
  const [fundingTx, setFundingTx] = useState("");
  const [now, setNow] = useState<number>(() => Math.floor(Date.now() / 1000));

  // Tick once a second so the cooldown countdown updates without re-querying chain.
  useEffect(() => {
    const t = setInterval(() => setNow(Math.floor(Date.now() / 1000)), 1000);
    return () => clearInterval(t);
  }, []);

  // Read on-chain config + state once on mount (cheap, refreshable on tx).
  const refreshVaultMeta = useCallback(async () => {
    if (!VAULT_CONTRACT) return;
    const [cfg, state] = await Promise.all([queryVaultConfig(), queryVaultState()]);
    if (cfg) setVaultCfg(cfg);
    if (state) setVaultState(state);
  }, []);

  // Query vault balance + depositor + keeper status when connected
  const refreshVaultBalance = useCallback(async () => {
    if (!wallet?.address || !VAULT_CONTRACT) return;
    const [bal, dep, keeper] = await Promise.all([
      queryVaultBalance(wallet.address),
      queryDepositor(wallet.address),
      REGISTRY_CONTRACT ? queryKeeper(wallet.address) : Promise.resolve(null),
    ]);
    if (bal) {
      setVaultShares(bal.shares);
      setVaultUsdcValue(bal.usdcValue);
    }
    setDepositor(dep);
    setKeeperInfo(keeper);
  }, [wallet?.address]);

  // Depositor count is sourced from the keeper performance API; on-chain state
  // exposes share totals but not the depositor roster. Honest null when offline.
  const refreshDepositorCount = useCallback(async () => {
    const perf = await fetchPerformance();
    setDepositorCount(perf ? perf.depositors.length : null);
  }, []);

  // Wallet funding status: trustline + faucet availability. When the trustline
  // is live this also refreshes the wallet's displayed USDC balance straight
  // from Horizon — no wallet-modal re-connect needed.
  const refreshFunding = useCallback(async () => {
    if (!wallet?.address) return;
    const [tl, info] = await Promise.all([
      getUsdcTrustline(wallet.address).catch(() => null),
      fetchFaucetInfo(),
    ]);
    setTrustline(tl);
    setFaucetInfo(info);
    if (tl?.status === "ok") {
      const bal = parseFloat(tl.balance).toFixed(2);
      setWallet((w) => (w ? { ...w, usdcBalance: bal } : w));
    }
  }, [wallet?.address]);

  useEffect(() => {
    refreshFunding();
  }, [refreshFunding]);

  // Read registry minStake once if registry is configured.
  useEffect(() => {
    if (!REGISTRY_CONTRACT) return;
    queryRegistryConfig().then((c) => {
      if (c) setRegistryMinStake(c.minStake);
    });
  }, []);

  useEffect(() => {
    refreshVaultMeta();
  }, [refreshVaultMeta]);

  useEffect(() => {
    refreshVaultBalance();
  }, [refreshVaultBalance]);

  useEffect(() => {
    refreshDepositorCount();
  }, [refreshDepositorCount]);

  // Poll: keeper API every 15s, on-chain reads every 30s (rate-limit friendly).
  useEffect(() => {
    const apiTimer = setInterval(refreshDepositorCount, 15_000);
    const chainTimer = setInterval(() => {
      refreshVaultMeta();
      refreshVaultBalance();
    }, 30_000);
    return () => {
      clearInterval(apiTimer);
      clearInterval(chainTimer);
    };
  }, [refreshDepositorCount, refreshVaultMeta, refreshVaultBalance]);

  const handleConnect = async () => {
    setError("");
    try {
      const w = await connectWallet();
      if (w) setWallet(w);
    } catch (err) {
      setError(err instanceof Error ? err.message : "Failed to connect wallet");
    }
  };

  const handleDisconnect = async () => {
    await disconnectWallet();
    setWallet(null);
    setVaultShares(0);
    setVaultUsdcValue(0);
    setDepositor(null);
    setKeeperInfo(null);
    setTrustline(null);
    setFaucetInfo(null);
    setFundingError("");
    setFundingTx("");
    resetTx();
  };

  const handleFriendbot = async () => {
    if (!wallet) return;
    setFundingError("");
    setFundingBusy("friendbot");
    try {
      await fundWithFriendbot(wallet.address);
      await refreshFunding();
    } catch (err) {
      setFundingError(err instanceof Error ? err.message : "Friendbot funding failed");
    } finally {
      setFundingBusy("");
    }
  };

  const handleAddTrustline = async () => {
    if (!wallet) return;
    setFundingError("");
    setFundingBusy("trustline");
    try {
      const hash = await addUsdcTrustline(wallet.address);
      setFundingTx(hash);
      await refreshFunding();
    } catch (err) {
      const msg = err instanceof Error ? err.message : "Trustline transaction failed";
      // User closed the wallet dialog — not an error worth shouting about.
      if (msg !== "cancelled") setFundingError(msg);
    } finally {
      setFundingBusy("");
    }
  };

  const handleFaucet = async () => {
    if (!wallet) return;
    setFundingError("");
    setFundingBusy("faucet");
    try {
      const res = await requestFaucet(wallet.address);
      if (res.ok) {
        setFundingTx(res.txHash);
        await refreshFunding();
      } else if (res.error === "cooldown") {
        const mins = Math.max(1, Math.ceil((res.retryAfterSecs ?? 0) / 60));
        setFundingError(`Faucet cooldown — retry in ${mins}m`);
      } else {
        setFundingError(faucetErrorText(res.error));
      }
    } finally {
      setFundingBusy("");
    }
  };

  const handleRegisterKeeper = async () => {
    if (!wallet) return;
    if (!keeperName.trim()) {
      setKeeperError("Choose a keeper name first.");
      return;
    }
    setKeeperError("");
    setKeeperBusy(true);
    try {
      await registerKeeper(wallet.address, keeperName.trim());
      const fresh = await queryKeeper(wallet.address);
      setKeeperInfo(fresh);
    } catch (err) {
      setKeeperError(err instanceof Error ? err.message : "Register failed");
    } finally {
      setKeeperBusy(false);
    }
  };

  const handleDeregisterKeeper = async () => {
    if (!wallet) return;
    setKeeperError("");
    setKeeperBusy(true);
    try {
      await deregisterKeeper(wallet.address);
      setKeeperInfo(null);
    } catch (err) {
      setKeeperError(err instanceof Error ? err.message : "Deregister failed");
    } finally {
      setKeeperBusy(false);
    }
  };

  const handleSubmit = async () => {
    if (!amount || parseFloat(amount) <= 0 || !wallet) return;
    setError("");

    // Pre-flight checks against on-chain config so we fail fast with a clear
    // message instead of letting Soroban's simulator return a cryptic error.
    if (tab === "deposit" && cap > 0) {
      const planned = parseFloat(amount) * 1e7;
      if (liveTvl + planned > cap) {
        setError(
          `Deposit would exceed the vault cap. Capacity remaining: $${((capRemaining ?? 0) / 1e7).toLocaleString(undefined, { maximumFractionDigits: 2 })}.`,
        );
        return;
      }
    }
    if (tab === "withdraw" && cooldownRemaining > 0) {
      setError(`Withdrawal cooldown active. Available in ${formatDuration(cooldownRemaining)}.`);
      return;
    }

    setTxStatus("simulating");

    try {
      const stroops = BigInt(Math.floor(parseFloat(amount) * 1e7));

      setTxStatus("signing");
      let result;
      if (tab === "deposit") {
        result = await depositToVault(wallet.address, stroops);
      } else {
        result = await withdrawFromVault(wallet.address, stroops);
      }

      setTxStatus("submitted");
      setTxHash(result.txHash);

      if (result.success) {
        setTxStatus("confirmed");
        // Refresh balances (incl. trustline/faucet panel state)
        await Promise.all([refreshVaultBalance(), refreshVaultMeta(), refreshFunding()]);
        // Refresh wallet balances
        const updated = await connectWallet();
        if (updated) setWallet(updated);
      } else {
        setTxStatus("error");
        setError("Transaction failed on-chain. Check explorer for details.");
      }
    } catch (err) {
      setTxStatus("error");
      setError(err instanceof Error ? err.message : "Transaction failed");
    }
  };

  const resetTx = () => {
    setTxStatus("idle");
    setTxHash("");
    setAmount("");
    setError("");
  };

  const connected = wallet?.connected;

  // ── Derived UI bits from on-chain state ─────────────────────────────
  const liveTvl = vaultState?.totalUsdc ?? 0;
  const liveTotalShares = vaultState?.totalShares ?? 0;
  const liveTotalProfit = vaultState?.totalProfit ?? 0;
  const liveActiveLiq = vaultState?.activeLiq ?? 0;
  const livePrice = sharePrice(liveTvl, liveTotalShares);
  const haveState = vaultState !== null;

  const cap = vaultCfg?.depositCap ?? 0;
  const capRemaining = cap > 0 ? Math.max(cap - liveTvl, 0) : null;
  const capPctUsed = cap > 0 ? Math.min(1, liveTvl / cap) : 0;

  const cooldownSec = vaultCfg?.withdrawCooldown ?? 0;
  const lastDeposit = depositor?.lastDepositTime ?? 0;
  const cooldownRemaining =
    cooldownSec > 0 && lastDeposit > 0
      ? Math.max(lastDeposit + cooldownSec - now, 0)
      : 0;
  const withdrawReady = cooldownRemaining === 0;

  const isKeeper = !!keeperInfo;
  const stakeUsdc = (keeperInfo?.stake ?? 0) / 1e7;
  const minStakeUsdc = (registryMinStake ?? 0) / 1e7;

  // ── Wallet funding (account → trustline → faucet) ───────────────────
  const tlStatus = trustline?.status ?? null;
  const tlBalance = trustline ? parseFloat(trustline.balance) : 0;
  // Deposits need spendable USDC. Prefer the authoritative trustline read;
  // before it lands, fall back to the balance connectWallet fetched.
  const usdcReady = trustline
    ? tlStatus === "ok" && tlBalance > 0
    : parseFloat((wallet?.usdcBalance ?? "0").replace(/,/g, "")) > 0;
  const depositGated = tab === "deposit" && !usdcReady;
  const faucetAmountUsdc = faucetInfo ? Number(faucetInfo.amount) / 1e7 : 0;

  const explainCap = (() => {
    if (cap <= 0) return "Unlimited";
    const remainingDollars = (capRemaining ?? 0) / 1e7;
    const capDollars = cap / 1e7;
    return `$${remainingDollars.toLocaleString(undefined, { maximumFractionDigits: 0 })} / $${capDollars.toLocaleString(undefined, { maximumFractionDigits: 0 })} remaining`;
  })();

  // Honest values: dash placeholders before the first chain read lands.
  const tvlDisplay = haveState ? `$${formatUSDC(liveTvl)}` : "—";
  const priceDisplay = haveState ? `$${livePrice.toFixed(4)}` : "—";
  const sharesDisplay = haveState
    ? (liveTotalShares / 1e7).toLocaleString(undefined, { maximumFractionDigits: 0 })
    : "—";
  const profitDisplay = haveState ? `+$${formatUSDC(liveTotalProfit)}` : "—";
  const activeDisplay = haveState ? `$${formatUSDC(liveActiveLiq)}` : "—";
  const depositorsDisplay = depositorCount === null ? "—" : depositorCount.toLocaleString();

  return (
    <div style={{ maxWidth: 1100, margin: "0 auto", padding: "48px 24px" }}>
      {/* Header */}
      <div style={{ display: "flex", flexWrap: "wrap", gap: 16, justifyContent: "space-between", alignItems: "flex-start", marginBottom: 32 }}>
        <div>
          <h1 style={{
            fontFamily: "var(--font-display)", fontSize: 20, fontWeight: 700,
            letterSpacing: "0.15em", color: "var(--text)", textTransform: "uppercase", margin: "0 0 4px",
          }}>
            Nectar Vault
          </h1>
          <p style={{ ...mono, fontSize: 12, color: "var(--text-dim)", margin: 0 }}>
            Deposit USDC to fund liquidations and earn yield from keeper profits
          </p>
        </div>
        {connected && wallet ? (
          <div style={{ display: "flex", alignItems: "center", gap: 12 }}>
            <div style={{ textAlign: "right" }}>
              <div style={{ ...mono, fontSize: 12, color: "var(--accent)" }}>
                {shortAddr(wallet.address)}
              </div>
              <div style={{ ...mono, fontSize: 10, color: "var(--text-dim)" }}>
                {wallet.balance} XLM · {wallet.network}
              </div>
            </div>
            <button
              onClick={handleDisconnect}
              style={{
                ...mono, padding: "4px 10px", fontSize: 10, background: "transparent",
                border: "1px solid var(--border)", borderRadius: "var(--r-sharp)",
                color: "var(--text-dim)", cursor: "pointer",
              }}
            >
              Disconnect
            </button>
          </div>
        ) : (
          <Pill color="var(--text-dim)">
            <StatusDot color="var(--text-mute)" glow={false} size={5} /> Not connected
          </Pill>
        )}
      </div>

      <div className="grid grid-cols-1 lg:grid-cols-2" style={{ gap: 24, alignItems: "start" }}>
        {/* Left column: Vault Overview + Position + Keeper + How It Works */}
        <div style={{ display: "flex", flexDirection: "column", gap: 16 }}>
          {/* Vault Overview */}
          <Card style={{ padding: 24 }}>
            <PanelLabel
              right={
                <Pill color={haveState ? "var(--accent)" : "var(--text-dim)"}>
                  <StatusDot color={haveState ? "var(--accent)" : "var(--text-mute)"} glow={haveState} size={5} />
                  {haveState ? "On-chain" : "Awaiting chain"}
                </Pill>
              }
            >
              Vault Overview
            </PanelLabel>
            <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
              <StatRow label="TVL" value={tvlDisplay} />
              <StatRow label="Share Price" value={priceDisplay} accent={haveState && livePrice > 1.0} />
              <StatRow label="Total Profit" value={profitDisplay} accent={haveState && liveTotalProfit > 0} />
              <StatRow label="Active Deployed" value={activeDisplay} />
              <StatRow label="Total Shares" value={sharesDisplay} />
              <StatRow label="Depositors" value={depositorsDisplay} />
            </div>

            {/* Capacity bar — only shown when a deposit cap is configured */}
            {cap > 0 && (
              <div style={{ marginTop: 20 }}>
                <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 6 }}>
                  <span style={{ ...mono, fontSize: 10, color: "var(--text-dim)", letterSpacing: "0.06em", textTransform: "uppercase" }}>
                    Capacity
                  </span>
                  <span style={{ ...mono, fontSize: 11, color: "var(--text)", fontVariantNumeric: "tabular-nums" }}>
                    {explainCap}
                  </span>
                </div>
                <div style={{ height: 6, background: "var(--surface)", borderRadius: "var(--r-sharp)", overflow: "hidden" }}>
                  <div style={{
                    width: `${capPctUsed * 100}%`, height: "100%",
                    background: capPctUsed > 0.95 ? "var(--amber)" : "var(--accent)",
                    transition: "width 0.5s var(--ease-out)",
                  }} />
                </div>
              </div>
            )}
          </Card>

          {/* Your Position (when connected) */}
          {connected && (
            <Card accent style={{ padding: 24 }}>
              <PanelLabel accent>Your Position</PanelLabel>
              <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 16 }}>
                <StatRow label="Vault Shares" value={(vaultShares / 1e7).toFixed(2)} />
                <StatRow label="USDC Value" value={`$${formatUSDC(vaultUsdcValue)}`} accent />
                <StatRow label="USDC Balance" value={wallet?.usdcBalance ?? "0.00"} />
                <StatRow label="XLM Balance" value={wallet?.balance ?? "0.00"} />
              </div>

              {cooldownSec > 0 && depositor && (
                <div style={{
                  marginTop: 16, padding: "10px 12px",
                  border: `1px solid ${withdrawReady ? "var(--accent)" : "var(--amber)"}`,
                  borderRadius: "var(--r-sharp)",
                  background: withdrawReady ? "var(--card-fill-accent)" : "var(--amber-fill)",
                  display: "flex", alignItems: "center", justifyContent: "space-between",
                }}>
                  <span style={{ ...mono, fontSize: 11, color: "var(--text-dim)", letterSpacing: "0.06em", textTransform: "uppercase" }}>
                    Withdrawal
                  </span>
                  <span style={{ ...mono, fontSize: 13, color: withdrawReady ? "var(--accent)" : "var(--amber)", fontVariantNumeric: "tabular-nums" }}>
                    {withdrawReady ? "Available now" : `Available in ${formatDuration(cooldownRemaining)}`}
                  </span>
                </div>
              )}
            </Card>
          )}

          {/* Keeper Operator panel — only when registry is wired in */}
          {connected && REGISTRY_CONTRACT && (
            <Card style={{ padding: 20 }}>
              <PanelLabel
                right={
                  <Pill color={isKeeper ? "var(--accent)" : "var(--text-dim)"}>
                    <StatusDot color={isKeeper ? "var(--accent)" : "var(--text-mute)"} glow={isKeeper} size={5} />
                    {isKeeper ? "Registered" : "Not registered"}
                  </Pill>
                }
              >
                Keeper Operator
              </PanelLabel>

              {isKeeper ? (
                <>
                  <div style={{ display: "grid", gridTemplateColumns: "1fr 1fr", gap: 12, marginBottom: 12 }}>
                    <StatRow label="Name" value={keeperInfo?.name || "—"} />
                    <StatRow label="Stake" value={`$${stakeUsdc.toLocaleString(undefined, { maximumFractionDigits: 2 })} USDC`} accent />
                    <StatRow
                      label="Executions"
                      value={`${keeperInfo?.totalExecutions ?? 0} (${keeperInfo?.successfulFills ?? 0} filled)`}
                    />
                    <div>
                      <div style={{ ...mono, fontSize: 10, color: "var(--text-dim)", letterSpacing: "0.06em", textTransform: "uppercase", marginBottom: 4 }}>
                        Active Draw
                      </div>
                      <div style={{ ...mono, fontSize: 18, fontWeight: 600, color: keeperInfo?.hasActiveDraw ? "var(--amber)" : "var(--text-dim)" }}>
                        {keeperInfo?.hasActiveDraw ? "Outstanding" : "None"}
                      </div>
                    </div>
                  </div>
                  <button
                    onClick={handleDeregisterKeeper}
                    disabled={keeperBusy || keeperInfo?.hasActiveDraw}
                    title={keeperInfo?.hasActiveDraw ? "Cannot deregister with an outstanding draw" : ""}
                    style={{
                      ...mono, width: "100%", padding: 10, background: "transparent",
                      border: "1px solid var(--border)", borderRadius: "var(--r-sharp)",
                      color: keeperInfo?.hasActiveDraw ? "var(--text-dim)" : "var(--text)",
                      fontSize: 12, cursor: keeperBusy || keeperInfo?.hasActiveDraw ? "not-allowed" : "pointer",
                      letterSpacing: "0.05em", textTransform: "uppercase",
                    }}
                  >
                    {keeperBusy ? "Submitting..." : "Deregister & Withdraw Stake"}
                  </button>
                </>
              ) : (
                <>
                  <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)", marginBottom: 12, lineHeight: 1.6 }}>
                    Operate a keeper to liquidate underwater Blend positions. Registration locks
                    {minStakeUsdc > 0 ? ` $${minStakeUsdc.toLocaleString()} USDC ` : " "}
                    as stake — slashable on draw timeout.
                  </div>
                  <input
                    type="text"
                    placeholder="keeper name"
                    value={keeperName}
                    onChange={(e) => setKeeperName(e.target.value)}
                    style={{
                      ...mono, width: "100%", padding: 10, background: "var(--surface)",
                      color: "var(--text)", border: "1px solid var(--border)", borderRadius: "var(--r-sharp)",
                      fontSize: 13, outline: "none", marginBottom: 12,
                    }}
                  />
                  {keeperError && (
                    <div style={{ ...mono, fontSize: 11, color: "var(--red)", marginBottom: 8 }}>
                      {keeperError}
                    </div>
                  )}
                  <button
                    onClick={handleRegisterKeeper}
                    disabled={keeperBusy || !keeperName.trim()}
                    style={{
                      ...mono, width: "100%", padding: 10,
                      background: keeperBusy || !keeperName.trim() ? "var(--surface)" : "var(--accent)",
                      color: keeperBusy || !keeperName.trim() ? "var(--text-dim)" : "var(--bg)",
                      border: "none", borderRadius: "var(--r-sharp)", fontSize: 12, fontWeight: 600,
                      cursor: keeperBusy || !keeperName.trim() ? "not-allowed" : "pointer",
                      letterSpacing: "0.05em", textTransform: "uppercase",
                    }}
                  >
                    {keeperBusy ? "Submitting..." : minStakeUsdc > 0 ? `Register (Stake $${minStakeUsdc.toLocaleString()})` : "Register Keeper"}
                  </button>
                </>
              )}
            </Card>
          )}

          {/* How It Works */}
          <Card style={{ padding: 24 }}>
            <PanelLabel>How Vault Deposits Work</PanelLabel>
            <div style={{ display: "flex", flexDirection: "column", gap: 12 }}>
              {[
                { step: "1", title: "Deposit USDC", desc: "Your USDC is pooled in the NectarVault smart contract on Soroban. You receive LP shares proportional to your deposit." },
                { step: "2", title: "Keepers Draw Capital", desc: "When a liquidation opportunity is found, keepers draw USDC from the vault to fill Blend Protocol Dutch auctions." },
                { step: "3", title: "Profits Returned", desc: "After a successful liquidation, the capital + profit is returned to the vault. Your shares appreciate in value." },
                { step: "4", title: "Withdraw Anytime", desc: "Redeem your LP shares for USDC at the current share price, which reflects accumulated profits." },
              ].map(({ step, title, desc }) => (
                <div key={step} style={{ display: "flex", gap: 12, alignItems: "flex-start" }}>
                  <div style={{
                    width: 24, height: 24, borderRadius: "50%", border: "1px solid var(--accent)",
                    display: "flex", alignItems: "center", justifyContent: "center", flexShrink: 0,
                    ...mono, fontSize: 11, color: "var(--accent)",
                  }}>
                    {step}
                  </div>
                  <div>
                    <div style={{ ...mono, fontSize: 13, color: "var(--text)", marginBottom: 2 }}>{title}</div>
                    <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)", lineHeight: 1.6 }}>{desc}</div>
                  </div>
                </div>
              ))}
            </div>
          </Card>
        </div>

        {/* Right column: Deposit/Withdraw Form + Contract Info */}
        <div>
          {/* Wallet Funding — testnet onboarding: account → trustline → faucet */}
          {connected && wallet && (
            <Card style={{ padding: 20, marginBottom: 16 }}>
              <PanelLabel
                right={
                  <Pill color={tlStatus === "ok" ? "var(--accent)" : tlStatus === null ? "var(--text-dim)" : "var(--amber)"}>
                    <StatusDot
                      color={tlStatus === "ok" ? "var(--accent)" : tlStatus === null ? "var(--text-mute)" : "var(--amber)"}
                      glow={tlStatus === "ok"}
                      size={5}
                    />
                    {tlStatus === null
                      ? "Checking"
                      : tlStatus === "no_account"
                      ? "Unfunded"
                      : tlStatus === "no_trustline"
                      ? "No trustline"
                      : "Ready"}
                  </Pill>
                }
              >
                Wallet Funding
              </PanelLabel>

              <div style={{ display: "flex", flexDirection: "column", gap: 14 }}>
                {/* Step 1 — account exists on-chain */}
                <FundingStep
                  state={tlStatus === null ? "pending" : tlStatus === "no_account" ? "active" : "done"}
                  title="1 · Account"
                >
                  {tlStatus === null && (
                    <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)" }}>
                      Checking account on Horizon...
                    </div>
                  )}
                  {tlStatus === "no_account" && (
                    <>
                      <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)", lineHeight: 1.6 }}>
                        This wallet does not exist on-chain yet — it needs XLM before it can hold USDC.
                      </div>
                      {wallet.network !== "PUBLIC" ? (
                        <button
                          onClick={handleFriendbot}
                          disabled={fundingBusy !== ""}
                          style={fundingBtnStyle(fundingBusy !== "")}
                        >
                          {fundingBusy === "friendbot" ? "Funding..." : "Fund with Friendbot (testnet)"}
                        </button>
                      ) : (
                        <div style={{ ...mono, fontSize: 11, color: "var(--amber)", marginTop: 6 }}>
                          Send XLM to this address to activate it.
                        </div>
                      )}
                    </>
                  )}
                </FundingStep>

                {/* Step 2 — USDC trustline */}
                <FundingStep
                  state={tlStatus === "ok" ? "done" : tlStatus === "no_trustline" ? "active" : "pending"}
                  title="2 · USDC Trustline"
                >
                  {tlStatus === "no_trustline" && (
                    <>
                      <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)", lineHeight: 1.6 }}>
                        A trustline to USDC ({shortAddr(USDC_ISSUER)}) lets this wallet hold the
                        vault&apos;s settlement asset.
                      </div>
                      <button
                        onClick={handleAddTrustline}
                        disabled={fundingBusy !== ""}
                        style={fundingBtnStyle(fundingBusy !== "")}
                      >
                        {fundingBusy === "trustline"
                          ? `Sign in ${walletDisplayName(wallet.walletId)}...`
                          : "Add USDC Trustline"}
                      </button>
                    </>
                  )}
                  {tlStatus === "ok" && (
                    <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)" }}>
                      USDC · {shortAddr(USDC_ISSUER)}
                    </div>
                  )}
                </FundingStep>

                {/* Step 3 — USDC balance via faucet */}
                <FundingStep
                  state={tlStatus === "ok" ? (tlBalance > 0 ? "done" : "active") : "pending"}
                  title="3 · USDC Balance"
                >
                  {tlStatus === "ok" && (
                    <>
                      <div style={{ ...mono, fontSize: 13, color: tlBalance > 0 ? "var(--accent)" : "var(--text)", fontVariantNumeric: "tabular-nums" }}>
                        {tlBalance.toLocaleString(undefined, { minimumFractionDigits: 2, maximumFractionDigits: 2 })} USDC
                      </div>
                      {faucetInfo?.enabled ? (
                        <button
                          onClick={handleFaucet}
                          disabled={fundingBusy !== ""}
                          style={fundingBtnStyle(fundingBusy !== "")}
                        >
                          {fundingBusy === "faucet"
                            ? "Requesting..."
                            : `Get ${faucetAmountUsdc.toLocaleString(undefined, { maximumFractionDigits: 2 })} USDC`}
                        </button>
                      ) : (
                        <div style={{ ...mono, fontSize: 10, color: "var(--text-dim)", marginTop: 4 }}>
                          {faucetInfo === null
                            ? "Faucet unavailable — keeper API offline."
                            : "Faucet not configured on this keeper API."}
                        </div>
                      )}
                    </>
                  )}
                </FundingStep>
              </div>

              {fundingTx && (
                <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)", marginTop: 12 }}>
                  tx:{" "}
                  <a
                    href={`https://stellar.expert/explorer/testnet/tx/${fundingTx}`}
                    target="_blank"
                    rel="noopener noreferrer"
                    style={{ color: "var(--accent)", textDecoration: "underline" }}
                  >
                    {fundingTx.slice(0, 8)}…{fundingTx.slice(-8)}
                  </a>
                </div>
              )}

              {fundingError && (
                <div style={{
                  ...mono, fontSize: 11, color: "var(--red)", marginTop: 12,
                  padding: 8, background: "var(--red-fill)", borderRadius: "var(--r-sharp)",
                }}>
                  {fundingError}
                </div>
              )}
            </Card>
          )}

          <Card style={{ padding: 0 }}>
            {/* Tabs */}
            <div style={{ display: "flex", borderBottom: "1px solid var(--border)" }}>
              {(["deposit", "withdraw"] as Tab[]).map((t) => (
                <button
                  key={t}
                  onClick={() => { setTab(t); resetTx(); }}
                  style={{
                    ...mono, flex: 1, padding: 12, border: "none",
                    background: tab === t ? "var(--surface-2)" : "transparent",
                    color: tab === t ? "var(--accent)" : "var(--text-dim)",
                    fontSize: 12, textTransform: "uppercase", letterSpacing: "0.1em", cursor: "pointer",
                    borderBottom: tab === t ? "2px solid var(--accent)" : "2px solid transparent",
                    transition: "all 200ms var(--ease-out)",
                  }}
                >
                  {t}
                </button>
              ))}
            </div>

            {/* Form Content */}
            <div style={{ padding: 24 }}>
              {!connected ? (
                <div style={{ textAlign: "center", padding: "32px 0" }}>
                  <div style={{ ...mono, fontSize: 13, color: "var(--text-dim)", marginBottom: 16 }}>
                    Connect your Stellar wallet to {tab}
                  </div>
                  <button
                    onClick={handleConnect}
                    style={{
                      ...mono, padding: "12px 32px", background: "var(--accent)", color: "var(--bg)",
                      border: "none", borderRadius: "var(--r-sharp)", fontSize: 13, fontWeight: 600,
                      cursor: "pointer", letterSpacing: "0.05em",
                    }}
                  >
                    Connect Wallet
                  </button>
                  <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)", marginTop: 12 }}>
                    Freighter · Albedo · xBull · Lobstr · Hana · Rabet
                  </div>
                  {error && (
                    <div style={{ ...mono, fontSize: 11, color: "var(--red)", marginTop: 8 }}>
                      {error}
                    </div>
                  )}
                </div>
              ) : txStatus === "confirmed" ? (
                <div style={{ textAlign: "center", padding: "24px 0" }}>
                  <div style={{ display: "flex", justifyContent: "center", marginBottom: 16 }}>
                    <StatusDot color="var(--accent)" size={10} />
                  </div>
                  <div style={{ ...mono, fontSize: 14, color: "var(--accent)", marginBottom: 8 }}>
                    {tab === "deposit" ? "Deposit" : "Withdrawal"} Confirmed
                  </div>
                  <div style={{ ...mono, fontSize: 12, color: "var(--text-dim)", marginBottom: 4 }}>
                    {amount} USDC {tab === "deposit" ? "deposited into" : "withdrawn from"} vault
                  </div>
                  <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)", marginBottom: 16 }}>
                    tx:{" "}
                    <a
                      href={`https://stellar.expert/explorer/testnet/tx/${txHash}`}
                      target="_blank"
                      rel="noopener noreferrer"
                      style={{ color: "var(--accent)", textDecoration: "underline" }}
                    >
                      {txHash.slice(0, 8)}…{txHash.slice(-8)}
                    </a>
                  </div>
                  <button
                    onClick={resetTx}
                    style={{
                      ...mono, padding: "8px 24px", background: "transparent", color: "var(--accent)",
                      border: "1px solid var(--accent)", borderRadius: "var(--r-sharp)", fontSize: 12, cursor: "pointer",
                    }}
                  >
                    New {tab}
                  </button>
                </div>
              ) : txStatus === "error" ? (
                <div style={{ textAlign: "center", padding: "24px 0" }}>
                  <div style={{ display: "flex", justifyContent: "center", marginBottom: 16 }}>
                    <StatusDot color="var(--red)" size={10} glow={false} />
                  </div>
                  <div style={{ ...mono, fontSize: 14, color: "var(--red)", marginBottom: 8 }}>
                    Transaction Failed
                  </div>
                  <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)", maxWidth: 300, margin: "0 auto 16px" }}>
                    {error}
                  </div>
                  {txHash && (
                    <div style={{ ...mono, fontSize: 11, color: "var(--text-dim)", marginBottom: 12 }}>
                      tx:{" "}
                      <a
                        href={`https://stellar.expert/explorer/testnet/tx/${txHash}`}
                        target="_blank"
                        rel="noopener noreferrer"
                        style={{ color: "var(--accent)", textDecoration: "underline" }}
                      >
                        {txHash.slice(0, 8)}…{txHash.slice(-8)}
                      </a>
                    </div>
                  )}
                  <button
                    onClick={resetTx}
                    style={{
                      ...mono, padding: "8px 24px", background: "transparent", color: "var(--accent)",
                      border: "1px solid var(--accent)", borderRadius: "var(--r-sharp)", fontSize: 12, cursor: "pointer",
                    }}
                  >
                    Try Again
                  </button>
                </div>
              ) : (
                <>
                  {/* Amount Input */}
                  <div style={{ marginBottom: 16 }}>
                    <div style={{ display: "flex", justifyContent: "space-between", marginBottom: 6 }}>
                      <label style={{ ...mono, fontSize: 11, color: "var(--text-dim)", textTransform: "uppercase", letterSpacing: "0.06em" }}>
                        {tab === "deposit" ? "USDC Amount" : "Shares to Redeem"}
                      </label>
                      <span style={{ ...mono, fontSize: 11, color: "var(--text-dim)", fontVariantNumeric: "tabular-nums" }}>
                        {tab === "deposit"
                          ? `Balance: ${wallet?.usdcBalance ?? "0.00"} USDC`
                          : `Shares: ${(vaultShares / 1e7).toFixed(2)}`}
                      </span>
                    </div>
                    <div style={{ display: "flex", border: "1px solid var(--border)", borderRadius: "var(--r-sharp)", overflow: "hidden" }}>
                      <input
                        type="number"
                        value={amount}
                        onChange={(e) => setAmount(e.target.value)}
                        placeholder="0.00"
                        style={{
                          ...mono, flex: 1, padding: 12, background: "var(--surface)",
                          color: "var(--text)", border: "none", fontSize: 16, outline: "none",
                          fontVariantNumeric: "tabular-nums",
                        }}
                      />
                      <button
                        onClick={() => setAmount(
                          tab === "deposit"
                            ? (wallet?.usdcBalance ?? "0").replace(/,/g, "")
                            : (vaultShares / 1e7).toFixed(2)
                        )}
                        style={{
                          ...mono, padding: "12px 16px", background: "var(--surface-2)", color: "var(--accent)",
                          border: "none", borderLeft: "1px solid var(--border)", fontSize: 11,
                          cursor: "pointer", letterSpacing: "0.05em",
                        }}
                      >
                        MAX
                      </button>
                    </div>
                  </div>

                  {/* Summary */}
                  {amount && parseFloat(amount) > 0 && (
                    <div style={{ padding: 12, background: "var(--surface)", borderRadius: "var(--r-sharp)", marginBottom: 16 }}>
                      {tab === "deposit" ? (
                        <>
                          <SummaryLine label="You deposit" value={`${parseFloat(amount).toLocaleString()} USDC`} />
                          <SummaryLine label="You receive" value={`~${estDepositShares(amount, livePrice).toFixed(2)} shares`} />
                          <SummaryLine label="Share price" value={priceDisplay} accent last />
                        </>
                      ) : (
                        <>
                          <SummaryLine label="You redeem" value={`${parseFloat(amount).toLocaleString()} shares`} />
                          <SummaryLine label="You receive" value={`~${estWithdrawUsdc(amount, livePrice).toFixed(2)} USDC`} accent last />
                        </>
                      )}
                    </div>
                  )}

                  {/* Error display */}
                  {error && (
                    <div style={{
                      ...mono, fontSize: 11, color: "var(--red)", marginBottom: 12,
                      padding: 8, background: "var(--red-fill)", borderRadius: "var(--r-sharp)",
                    }}>
                      {error}
                    </div>
                  )}

                  {/* Submit */}
                  <button
                    onClick={handleSubmit}
                    disabled={!amount || parseFloat(amount) <= 0 || depositGated || txStatus !== "idle"}
                    style={{
                      ...mono, width: "100%", padding: 14,
                      background: !amount || parseFloat(amount) <= 0 || depositGated ? "var(--surface)" : "var(--accent)",
                      color: !amount || parseFloat(amount) <= 0 || depositGated ? "var(--text-dim)" : "var(--bg)",
                      border: "none", borderRadius: "var(--r-sharp)", fontSize: 13, fontWeight: 600,
                      cursor: !amount || parseFloat(amount) <= 0 || depositGated ? "not-allowed" : "pointer",
                      letterSpacing: "0.05em", textTransform: "uppercase",
                    }}
                  >
                    {txStatus === "simulating"
                      ? "Simulating..."
                      : txStatus === "signing"
                      ? `Sign in ${walletDisplayName(wallet?.walletId)}...`
                      : txStatus === "submitted"
                      ? "Confirming on Soroban..."
                      : tab === "deposit"
                      ? "Deposit USDC"
                      : "Withdraw USDC"}
                  </button>

                  {depositGated && (
                    <div style={{ ...mono, fontSize: 10, color: "var(--amber)", marginTop: 8, textAlign: "center" }}>
                      No USDC — use the faucet above.
                    </div>
                  )}

                  {!VAULT_CONTRACT && (
                    <div style={{ ...mono, fontSize: 10, color: "var(--amber)", marginTop: 8, textAlign: "center" }}>
                      Vault contract not deployed yet. Set NEXT_PUBLIC_VAULT_CONTRACT to enable transactions.
                    </div>
                  )}
                </>
              )}
            </div>
          </Card>

          {/* Contract Info */}
          <Card style={{ padding: 16, marginTop: 16 }}>
            <Eyebrow style={{ fontSize: 11, marginBottom: 8 }}>Contract Info</Eyebrow>
            <div style={{ display: "flex", flexDirection: "column", gap: 6 }}>
              {[
                { label: "Network", value: "Soroban Testnet" },
                { label: "Asset", value: "USDC (test token)" },
                { label: "Vault Contract", value: VAULT_CONTRACT ? shortAddr(VAULT_CONTRACT) : "Not deployed", addr: !!VAULT_CONTRACT },
                { label: "Min Deposit", value: "1.00 USDC" },
              ].map(({ label, value, addr }) => (
                <div key={label} style={{ display: "flex", justifyContent: "space-between" }}>
                  <span style={{ ...mono, fontSize: 11, color: "var(--text-dim)" }}>{label}</span>
                  <span style={{ ...mono, fontSize: 11, color: addr ? "var(--accent)" : "var(--text)", fontVariantNumeric: "tabular-nums" }}>{value}</span>
                </div>
              ))}
            </div>
          </Card>
        </div>
      </div>
    </div>
  );
}

// ── small helpers (presentation-only) ──────────────────────────────────────────
function FundingStep({ state, title, children }: {
  state: "done" | "active" | "pending";
  title: string;
  children?: React.ReactNode;
}) {
  const color = state === "done" ? "var(--accent)" : state === "active" ? "var(--amber)" : "var(--text-mute)";
  return (
    <div style={{ display: "flex", gap: 10, alignItems: "flex-start" }}>
      <StatusDot color={color} glow={state !== "pending"} size={6} style={{ marginTop: 4, flexShrink: 0 }} />
      <div style={{ flex: 1, minWidth: 0 }}>
        <div style={{
          ...mono, fontSize: 11, color: state === "pending" ? "var(--text-dim)" : "var(--text)",
          letterSpacing: "0.06em", textTransform: "uppercase", marginBottom: 2,
        }}>
          {title}
        </div>
        {children}
      </div>
    </div>
  );
}

// Shared style for the funding-panel action buttons (mirrors the submit button).
function fundingBtnStyle(busy: boolean): CSSProperties {
  return {
    ...mono, width: "100%", padding: 10, marginTop: 8,
    background: busy ? "var(--surface)" : "var(--accent)",
    color: busy ? "var(--text-dim)" : "var(--bg)",
    border: "none", borderRadius: "var(--r-sharp)", fontSize: 12, fontWeight: 600,
    cursor: busy ? "not-allowed" : "pointer",
    letterSpacing: "0.05em", textTransform: "uppercase",
  };
}

// Map faucet API error codes to human-readable inline messages.
function faucetErrorText(code: string): string {
  switch (code) {
    case "no_trustline": return "Add the USDC trustline first.";
    case "insufficient_treasury": return "Faucet treasury is empty — try again later.";
    case "disabled": return "Faucet is not enabled on this keeper API.";
    case "bad_address": return "Faucet rejected the address.";
    case "payment_failed": return "Faucet payment failed — try again.";
    case "network": return "Keeper API unreachable — is it running?";
    default: return `Faucet error: ${code}`;
  }
}

function SummaryLine({ label, value, accent, last }: { label: string; value: string; accent?: boolean; last?: boolean }) {
  return (
    <div style={{ display: "flex", justifyContent: "space-between", marginBottom: last ? 0 : 4 }}>
      <span style={{ ...mono, fontSize: 11, color: "var(--text-dim)" }}>{label}</span>
      <span style={{ ...mono, fontSize: 11, color: accent ? "var(--accent)" : "var(--text)", fontVariantNumeric: "tabular-nums" }}>{value}</span>
    </div>
  );
}

// Estimate shares received for a deposit at the live share price. Falls back to
// a 1:1 estimate when price is unavailable; never fabricates a fixed rate.
function estDepositShares(amount: string, price: number): number {
  const usdc = parseFloat(amount);
  if (!Number.isFinite(usdc) || usdc <= 0) return 0;
  return price > 0 ? usdc / price : usdc;
}

// Estimate USDC received for a share redemption at the live share price.
function estWithdrawUsdc(amount: string, price: number): number {
  const shares = parseFloat(amount);
  if (!Number.isFinite(shares) || shares <= 0) return 0;
  return shares * (price > 0 ? price : 1);
}
