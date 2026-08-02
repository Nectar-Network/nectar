import * as StellarSdk from "@stellar/stellar-sdk";

// Network selection: NEXT_PUBLIC_NETWORK = "testnet" (default) | "mainnet".
// Drives RPC / Horizon / passphrase so the same build serves both
// testnet.nectar.monster (testnet) and nectarnetwork.fun (mainnet).
const IS_MAINNET =
  (process.env.NEXT_PUBLIC_NETWORK ?? "testnet").toLowerCase() === "mainnet";

const RPC_URL =
  process.env.NEXT_PUBLIC_SOROBAN_RPC ??
  (IS_MAINNET
    ? "https://mainnet.sorobanrpc.com"
    : "https://soroban-testnet.stellar.org");
const NETWORK_PASSPHRASE = IS_MAINNET
  ? StellarSdk.Networks.PUBLIC
  : StellarSdk.Networks.TESTNET;
const HORIZON_URL =
  process.env.NEXT_PUBLIC_HORIZON_URL ??
  (IS_MAINNET
    ? "https://horizon.stellar.org"
    : "https://horizon-testnet.stellar.org");

// Circle USDC issuer (classic asset). The vault settles the SAC that wraps
// USDC:<issuer>, so trustline + balance checks must pin this exact issuer.
export const USDC_ISSUER =
  process.env.NEXT_PUBLIC_USDC_ISSUER ??
  (IS_MAINNET
    ? "GA5ZSEJYB37JRC5AVCIA5MOP4RHTM335X2KGX3IHOJAPP5RE34K4KZVN"
    : "GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5");

// Vault contract address — set via env or fallback
const VAULT_CONTRACT =
  process.env.NEXT_PUBLIC_VAULT_CONTRACT ?? "";
const REGISTRY_CONTRACT =
  process.env.NEXT_PUBLIC_REGISTRY_CONTRACT ?? "";

const STORAGE_SELECTED_WALLET = "nectar.selectedWalletId";

export interface WalletState {
  connected: boolean;
  address: string;
  network: string;
  balance: string; // XLM balance for display
  usdcBalance: string;
  walletId?: string; // freighter / albedo / xbull / lobstr / hana / rabet
}

/* ──────────────────────────────────────────────────────────────────────────
 * Stellar Wallets Kit — multi-wallet integration
 *
 * Replaces the direct @stellar/freighter-api flow with a kit-based picker
 * that supports Freighter, Albedo, xBull, Lobstr, Hana, and Rabet. The kit
 * surfaces a modal where the user picks; subsequent sign calls go through
 * the same uniform API regardless of which wallet was chosen.
 *
 * Initialization is lazy + browser-only because the kit modules touch
 * `window`. SSR-safe: nothing runs at module load.
 * ────────────────────────────────────────────────────────────────────────── */

let kitInitialized = false;

async function getKit() {
  // Reuse the static class. Initialize once on first use.
  const { StellarWalletsKit, Networks } = await import(
    "@creit.tech/stellar-wallets-kit"
  );

  if (!kitInitialized) {
    const [
      { FreighterModule },
      { AlbedoModule },
      { xBullModule },
      { LobstrModule },
      { HanaModule },
      { RabetModule },
    ] = await Promise.all([
      import("@creit.tech/stellar-wallets-kit/modules/freighter"),
      import("@creit.tech/stellar-wallets-kit/modules/albedo"),
      import("@creit.tech/stellar-wallets-kit/modules/xbull"),
      import("@creit.tech/stellar-wallets-kit/modules/lobstr"),
      import("@creit.tech/stellar-wallets-kit/modules/hana"),
      import("@creit.tech/stellar-wallets-kit/modules/rabet"),
    ]);

    const previouslySelected =
      typeof window !== "undefined"
        ? window.localStorage.getItem(STORAGE_SELECTED_WALLET) ?? undefined
        : undefined;

    StellarWalletsKit.init({
      modules: [
        new FreighterModule(),
        new AlbedoModule(),
        new xBullModule(),
        new LobstrModule(),
        new HanaModule(),
        new RabetModule(),
      ],
      network: IS_MAINNET ? Networks.PUBLIC : Networks.TESTNET,
      selectedWalletId: previouslySelected,
    });
    kitInitialized = true;
  }

  return StellarWalletsKit;
}

/**
 * Open the wallet picker modal. The user selects a wallet, signs the
 * connection, and we return the resulting address + balances.
 */
export async function connectWallet(): Promise<WalletState | null> {
  if (typeof window === "undefined") return null;
  try {
    const kit = await getKit();
    const { KitEventType } = await import("@creit.tech/stellar-wallets-kit");

    // Listen for the user's wallet pick (fires once per modal cycle).
    const off = kit.on(KitEventType.WALLET_SELECTED, (event) => {
      const id = event?.payload?.id;
      if (id) window.localStorage.setItem(STORAGE_SELECTED_WALLET, id);
    });

    // Open the auth modal — picker for all configured wallets.
    let address: string;
    try {
      ({ address } = await kit.authModal());
    } finally {
      off();
    }
    if (!address) return null; // user closed the modal without picking

    // We initialized the kit with the configured network, so we know it
    // without asking the wallet — Albedo doesn't implement getNetwork() and
    // throws code -3, so we skip the call entirely.
    const walletId =
      window.localStorage.getItem(STORAGE_SELECTED_WALLET) ?? undefined;

    // Fetch XLM + classic-asset balances via Horizon.
    let xlmBalance = "0";
    let usdcBalance = "0";
    try {
      const server = new StellarSdk.Horizon.Server(HORIZON_URL);
      const account = await server.loadAccount(address);
      const native = account.balances.find(
        (b: StellarSdk.Horizon.HorizonApi.BalanceLine) => b.asset_type === "native"
      );
      if (native) {
        xlmBalance = parseFloat(native.balance).toFixed(2);
      }
      for (const b of account.balances) {
        // Pin the issuer — "USDC" tokens from any other issuer are not the
        // asset the vault settles and must not show as spendable balance.
        if (
          "asset_code" in b &&
          b.asset_code === "USDC" &&
          "asset_issuer" in b &&
          b.asset_issuer === USDC_ISSUER
        ) {
          usdcBalance = parseFloat(b.balance).toFixed(2);
        }
      }
    } catch {
      // Account may not exist yet on testnet (needs Friendbot) — that's fine.
    }

    return {
      connected: true,
      address,
      network: IS_MAINNET ? "PUBLIC" : "TESTNET",
      balance: xlmBalance,
      usdcBalance,
      walletId,
    };
  } catch (err) {
    console.error("Wallet connection failed:", err);
    // Albedo + xBull throw plain `{ code, message }` objects, not Error
    // instances. Normalize and quietly swallow user-cancellation cases so
    // the UI doesn't show a scary error for normal interactions.
    const obj = err as { code?: number; message?: string };
    const code = typeof obj?.code === "number" ? obj.code : undefined;
    const msg =
      err instanceof Error
        ? err.message
        : obj?.message ?? String(err);
    const userCancelled =
      code === -4 ||
      /closed/i.test(msg) ||
      /cancell?ed/i.test(msg) ||
      /user reject/i.test(msg) ||
      /declined/i.test(msg);
    if (userCancelled) return null;
    throw err instanceof Error ? err : new Error(msg || "Wallet connection failed");
  }
}

/** Disconnect the currently selected wallet and forget the choice. */
export async function disconnectWallet(): Promise<void> {
  if (typeof window === "undefined") return;
  try {
    const kit = await getKit();
    await kit.disconnect();
  } catch {
    /* fine — kit may not have an active session */
  }
  window.localStorage.removeItem(STORAGE_SELECTED_WALLET);
}

/**
 * Sign a base64 XDR via the connected wallet. Throws if no wallet is connected
 * or signing is rejected.
 */
async function signWithKit(
  xdr: string,
  address: string
): Promise<{ signedTxXdr: string }> {
  const kit = await getKit();
  const { signedTxXdr } = await kit.signTransaction(xdr, {
    networkPassphrase: NETWORK_PASSPHRASE,
    address,
  });
  return { signedTxXdr };
}

/* ──────────────────────────────────────────────────────────────────────────
 * USDC trustline + account funding (classic Stellar ops via Horizon)
 * ────────────────────────────────────────────────────────────────────────── */

export type TrustlineStatus = "no_account" | "no_trustline" | "ok";

/**
 * Check whether `address` exists on-chain and holds a trustline to the
 * vault's USDC (exact code + issuer). Distinguishes unfunded accounts
 * (Horizon 404) from funded accounts that haven't added the trustline yet.
 */
export async function getUsdcTrustline(
  address: string
): Promise<{ status: TrustlineStatus; balance: string }> {
  const server = new StellarSdk.Horizon.Server(HORIZON_URL);
  try {
    const account = await server.loadAccount(address);
    const line = account.balances.find(
      (b) =>
        "asset_code" in b &&
        b.asset_code === "USDC" &&
        "asset_issuer" in b &&
        b.asset_issuer === USDC_ISSUER
    );
    if (!line) return { status: "no_trustline", balance: "0" };
    return { status: "ok", balance: line.balance };
  } catch (err) {
    const status = (err as { response?: { status?: number } })?.response?.status;
    const name = (err as { name?: string })?.name;
    if (status === 404 || name === "NotFoundError") {
      return { status: "no_account", balance: "0" };
    }
    throw err instanceof Error ? err : new Error("Failed to load account");
  }
}

/**
 * Add a USDC trustline to the connected wallet via a classic changeTrust op.
 * Signed through the wallet kit, submitted to Horizon (NOT Soroban RPC).
 * Returns the transaction hash. User-cancels throw Error("cancelled") so the
 * UI can quietly ignore them — same normalization as connectWallet.
 */
export async function addUsdcTrustline(address: string): Promise<string> {
  try {
    const server = new StellarSdk.Horizon.Server(HORIZON_URL);
    const account = await server.loadAccount(address);
    const tx = new StellarSdk.TransactionBuilder(account, {
      fee: StellarSdk.BASE_FEE,
      networkPassphrase: NETWORK_PASSPHRASE,
    })
      .addOperation(
        StellarSdk.Operation.changeTrust({
          asset: new StellarSdk.Asset("USDC", USDC_ISSUER),
        })
      )
      .setTimeout(120)
      .build();

    const { signedTxXdr } = await signWithKit(tx.toXDR(), address);
    const signed = StellarSdk.TransactionBuilder.fromXDR(
      signedTxXdr,
      NETWORK_PASSPHRASE
    );
    const res = await server.submitTransaction(signed);
    return res.hash;
  } catch (err) {
    // Albedo + xBull throw plain { code, message } objects, not Error
    // instances — normalize like connectWallet and tag user-cancellations.
    const obj = err as { code?: number; message?: string };
    const code = typeof obj?.code === "number" ? obj.code : undefined;
    const msg =
      err instanceof Error ? err.message : obj?.message ?? String(err);
    const userCancelled =
      code === -4 ||
      /closed/i.test(msg) ||
      /cancell?ed/i.test(msg) ||
      /user reject/i.test(msg) ||
      /declined/i.test(msg);
    if (userCancelled) throw new Error("cancelled");
    // Surface Horizon result codes when present (e.g. op_low_reserve).
    const extras = (
      err as {
        response?: { data?: { extras?: { result_codes?: Record<string, unknown> } } };
      }
    )?.response?.data?.extras;
    if (extras?.result_codes) {
      throw new Error(`Trustline failed: ${JSON.stringify(extras.result_codes)}`);
    }
    throw err instanceof Error ? err : new Error(msg || "Trustline transaction failed");
  }
}

/** Create + fund an account via Friendbot. Only meaningful on testnet. */
export async function fundWithFriendbot(address: string): Promise<void> {
  if (IS_MAINNET) {
    throw new Error("Friendbot is testnet-only — fund this account with XLM manually.");
  }
  const res = await fetch(
    "https://friendbot.stellar.org?addr=" + encodeURIComponent(address)
  );
  if (!res.ok) {
    throw new Error(`Friendbot funding failed (HTTP ${res.status})`);
  }
}

/** Build and submit a vault deposit transaction via Soroban */
export async function depositToVault(
  userAddress: string,
  amountStroops: bigint
): Promise<{ txHash: string; success: boolean }> {
  if (!VAULT_CONTRACT) {
    throw new Error("Vault contract address not configured. Set NEXT_PUBLIC_VAULT_CONTRACT.");
  }

  const server = new StellarSdk.rpc.Server(RPC_URL);
  const account = await server.getAccount(userAddress);

  const contract = new StellarSdk.Contract(VAULT_CONTRACT);

  const tx = new StellarSdk.TransactionBuilder(account, {
    fee: "1000000",
    networkPassphrase: NETWORK_PASSPHRASE,
  })
    .addOperation(
      contract.call(
        "deposit",
        StellarSdk.nativeToScVal(userAddress, { type: "address" }),
        StellarSdk.nativeToScVal(amountStroops, { type: "i128" })
      )
    )
    .setTimeout(60)
    .build();

  // Simulate first
  const simulated = await server.simulateTransaction(tx);
  if (StellarSdk.rpc.Api.isSimulationError(simulated)) {
    throw new Error(`Simulation failed: ${simulated.error}`);
  }

  const prepared = StellarSdk.rpc.assembleTransaction(tx, simulated).build();
  const xdr = prepared.toXDR();

  // Sign via the connected wallet (Freighter / Albedo / xBull / Lobstr / Hana / Rabet)
  const { signedTxXdr } = await signWithKit(xdr, userAddress);
  const signed = StellarSdk.TransactionBuilder.fromXDR(
    signedTxXdr,
    NETWORK_PASSPHRASE
  );

  const sendResult = await server.sendTransaction(signed);

  if (sendResult.status === "ERROR") {
    throw new Error("Transaction submission failed");
  }

  // Poll for confirmation
  let result = await server.getTransaction(sendResult.hash);
  while (result.status === "NOT_FOUND") {
    await new Promise((r) => setTimeout(r, 2000));
    result = await server.getTransaction(sendResult.hash);
  }

  return {
    txHash: sendResult.hash,
    success: result.status === "SUCCESS",
  };
}

/** Build and submit a vault withdraw transaction via Soroban */
export async function withdrawFromVault(
  userAddress: string,
  sharesStroops: bigint
): Promise<{ txHash: string; success: boolean }> {
  if (!VAULT_CONTRACT) {
    throw new Error("Vault contract address not configured. Set NEXT_PUBLIC_VAULT_CONTRACT.");
  }

  const server = new StellarSdk.rpc.Server(RPC_URL);
  const account = await server.getAccount(userAddress);

  const contract = new StellarSdk.Contract(VAULT_CONTRACT);

  const tx = new StellarSdk.TransactionBuilder(account, {
    fee: "1000000",
    networkPassphrase: NETWORK_PASSPHRASE,
  })
    .addOperation(
      contract.call(
        "withdraw",
        StellarSdk.nativeToScVal(userAddress, { type: "address" }),
        StellarSdk.nativeToScVal(sharesStroops, { type: "i128" })
      )
    )
    .setTimeout(60)
    .build();

  const simulated = await server.simulateTransaction(tx);
  if (StellarSdk.rpc.Api.isSimulationError(simulated)) {
    throw new Error(`Simulation failed: ${simulated.error}`);
  }

  const prepared = StellarSdk.rpc.assembleTransaction(tx, simulated).build();
  const xdr = prepared.toXDR();

  const { signedTxXdr } = await signWithKit(xdr, userAddress);
  const signed = StellarSdk.TransactionBuilder.fromXDR(
    signedTxXdr,
    NETWORK_PASSPHRASE
  );

  const sendResult = await server.sendTransaction(signed);

  if (sendResult.status === "ERROR") {
    throw new Error("Transaction submission failed");
  }

  let result = await server.getTransaction(sendResult.hash);
  while (result.status === "NOT_FOUND") {
    await new Promise((r) => setTimeout(r, 2000));
    result = await server.getTransaction(sendResult.hash);
  }

  return {
    txHash: sendResult.hash,
    success: result.status === "SUCCESS",
  };
}

/** Query vault balance for a user */
export async function queryVaultBalance(
  userAddress: string
): Promise<{ shares: number; usdcValue: number } | null> {
  if (!VAULT_CONTRACT) return null;

  try {
    const server = new StellarSdk.rpc.Server(RPC_URL);
    const account = await server.getAccount(userAddress);
    const contract = new StellarSdk.Contract(VAULT_CONTRACT);

    const tx = new StellarSdk.TransactionBuilder(account, {
      fee: "100",
      networkPassphrase: NETWORK_PASSPHRASE,
    })
      .addOperation(
        contract.call(
          "balance",
          StellarSdk.nativeToScVal(userAddress, { type: "address" })
        )
      )
      .setTimeout(30)
      .build();

    const simulated = await server.simulateTransaction(tx);
    if (StellarSdk.rpc.Api.isSimulationError(simulated)) {
      return null;
    }

    // Parse the result — returns (shares: i128, usdc_value: i128).
    // scValToNative on a tuple ScVal yields a [bigint, bigint] array.
    if (StellarSdk.rpc.Api.isSimulationSuccess(simulated) && simulated.result) {
      const native = StellarSdk.scValToNative(simulated.result.retval);
      if (Array.isArray(native) && native.length >= 2) {
        const toNum = (v: unknown): number => {
          if (typeof v === "bigint") return Number(v);
          if (typeof v === "number") return v;
          return 0;
        };
        return {
          shares: toNum(native[0]),
          usdcValue: toNum(native[1]),
        };
      }
    }
    return null;
  } catch {
    return null;
  }
}

export function shortAddr(addr: string): string {
  if (!addr || addr.length < 10) return addr;
  return `${addr.slice(0, 6)}...${addr.slice(-6)}`;
}

/* ──────────────────────────────────────────────────────────────────────────
 * Read-only Soroban queries (Tranche 1 contract additions)
 * ────────────────────────────────────────────────────────────────────────── */

export interface VaultConfig {
  depositCap: number;
  withdrawCooldown: number;
  maxDrawPerKeeper: number;
}

export interface VaultStateOnchain {
  totalUsdc: number;
  totalShares: number;
  totalProfit: number;
  activeLiq: number;
}

export interface DepositorOnchain {
  shares: number;
  depositedAt: number;
  lastDepositTime: number;
}

export interface KeeperInfoOnchain {
  addr: string;
  name: string;
  stake: number;
  registeredAt: number;
  active: boolean;
  totalExecutions: number;
  successfulFills: number;
  totalProfit: number;
  lastDrawTime: number;
  hasActiveDraw: boolean;
  avgResponseTimeMs: number;
}

/** Internal helper: simulate a read-only contract call and return retval. */
async function simulateRead(
  contractAddr: string,
  fn: string,
  args: StellarSdk.xdr.ScVal[],
  fromAddr?: string,
): Promise<StellarSdk.xdr.ScVal | null> {
  if (!contractAddr) return null;
  try {
    const server = new StellarSdk.rpc.Server(RPC_URL);
    const source = fromAddr ?? StellarSdk.Keypair.random().publicKey();
    let account: StellarSdk.Account;
    try {
      account = await server.getAccount(source);
    } catch {
      // Random source unfunded — fabricate a SimpleAccount for simulation.
      account = new StellarSdk.Account(source, "0");
    }
    const contract = new StellarSdk.Contract(contractAddr);
    const tx = new StellarSdk.TransactionBuilder(account, {
      fee: "100",
      networkPassphrase: NETWORK_PASSPHRASE,
    })
      .addOperation(contract.call(fn, ...args))
      .setTimeout(30)
      .build();

    const sim = await server.simulateTransaction(tx);
    if (StellarSdk.rpc.Api.isSimulationError(sim)) return null;
    if (StellarSdk.rpc.Api.isSimulationSuccess(sim) && sim.result) {
      return sim.result.retval;
    }
    return null;
  } catch {
    return null;
  }
}

/** Read VaultConfig from chain. */
export async function queryVaultConfig(): Promise<VaultConfig | null> {
  const v = await simulateRead(VAULT_CONTRACT, "get_config", []);
  if (!v) return null;
  try {
    const obj = StellarSdk.scValToNative(v) as Record<string, unknown>;
    return {
      depositCap: Number(obj.deposit_cap ?? 0),
      withdrawCooldown: Number(obj.withdraw_cooldown ?? 0),
      maxDrawPerKeeper: Number(obj.max_draw_per_keeper ?? 0),
    };
  } catch {
    return null;
  }
}

/** Read VaultState from chain. */
export async function queryVaultState(): Promise<VaultStateOnchain | null> {
  const v = await simulateRead(VAULT_CONTRACT, "get_state", []);
  if (!v) return null;
  try {
    const obj = StellarSdk.scValToNative(v) as Record<string, unknown>;
    return {
      totalUsdc: Number(obj.total_usdc ?? 0),
      totalShares: Number(obj.total_shares ?? 0),
      totalProfit: Number(obj.total_profit ?? 0),
      activeLiq: Number(obj.active_liq ?? 0),
    };
  } catch {
    return null;
  }
}

/** Read a depositor record from chain (returns null if not registered). */
export async function queryDepositor(
  userAddress: string,
): Promise<DepositorOnchain | null> {
  if (!userAddress) return null;
  const v = await simulateRead(VAULT_CONTRACT, "get_depositor", [
    StellarSdk.nativeToScVal(userAddress, { type: "address" }),
  ]);
  if (!v) return null;
  try {
    const obj = StellarSdk.scValToNative(v) as Record<string, unknown>;
    return {
      shares: Number(obj.shares ?? 0),
      depositedAt: Number(obj.deposited_at ?? 0),
      lastDepositTime: Number(obj.last_deposit_time ?? 0),
    };
  } catch {
    return null;
  }
}

/** Read keeper info from registry (returns null if not registered). */
export async function queryKeeper(
  operatorAddress: string,
): Promise<KeeperInfoOnchain | null> {
  if (!operatorAddress || !REGISTRY_CONTRACT) return null;
  const v = await simulateRead(REGISTRY_CONTRACT, "get_keeper", [
    StellarSdk.nativeToScVal(operatorAddress, { type: "address" }),
  ]);
  if (!v) return null;
  try {
    const obj = StellarSdk.scValToNative(v) as Record<string, unknown>;
    // KeeperInfo carries totals (total_response_time_ms, response_count) — the
    // average is a separate contract function, not a struct field. Derive it
    // here so the leaderboard's avg-response column reflects real data.
    const totalResponseMs = Number(obj.total_response_time_ms ?? 0);
    const responseCount = Number(obj.response_count ?? 0);
    return {
      addr: String(obj.addr ?? operatorAddress),
      name: String(obj.name ?? ""),
      stake: Number(obj.stake ?? 0),
      registeredAt: Number(obj.registered_at ?? 0),
      active: Boolean(obj.active ?? false),
      totalExecutions: Number(obj.total_executions ?? 0),
      successfulFills: Number(obj.successful_fills ?? 0),
      totalProfit: Number(obj.total_profit ?? 0),
      lastDrawTime: Number(obj.last_draw_time ?? 0),
      hasActiveDraw: Boolean(obj.has_active_draw ?? false),
      avgResponseTimeMs: responseCount > 0 ? Math.round(totalResponseMs / responseCount) : 0,
    };
  } catch {
    return null;
  }
}

/** Read all registered keeper addresses from the registry (get_keepers). */
export async function queryKeepers(): Promise<string[]> {
  const v = await simulateRead(REGISTRY_CONTRACT, "get_keepers", []);
  if (!v) return [];
  try {
    const native = StellarSdk.scValToNative(v);
    if (!Array.isArray(native)) return [];
    return native.map((a) => String(a)).filter(Boolean);
  } catch {
    return [];
  }
}

/** Read RegistryConfig (min_stake, slash_timeout, slash_rate_bps, usdc_token). */
export async function queryRegistryConfig(): Promise<{
  minStake: number;
  slashTimeout: number;
  slashRateBps: number;
} | null> {
  const v = await simulateRead(REGISTRY_CONTRACT, "get_config", []);
  if (!v) return null;
  try {
    const obj = StellarSdk.scValToNative(v) as Record<string, unknown>;
    return {
      minStake: Number(obj.min_stake ?? 0),
      slashTimeout: Number(obj.slash_timeout ?? 0),
      slashRateBps: Number(obj.slash_rate_bps ?? 0),
    };
  } catch {
    return null;
  }
}

/* ──────────────────────────────────────────────────────────────────────────
 * Keeper register / deregister (signed via Freighter)
 * ────────────────────────────────────────────────────────────────────────── */

async function buildAndSubmit(
  userAddress: string,
  contractAddr: string,
  fn: string,
  args: StellarSdk.xdr.ScVal[],
): Promise<{ txHash: string; success: boolean }> {
  const server = new StellarSdk.rpc.Server(RPC_URL);
  const account = await server.getAccount(userAddress);
  const contract = new StellarSdk.Contract(contractAddr);

  const tx = new StellarSdk.TransactionBuilder(account, {
    fee: "1000000",
    networkPassphrase: NETWORK_PASSPHRASE,
  })
    .addOperation(contract.call(fn, ...args))
    .setTimeout(60)
    .build();

  const sim = await server.simulateTransaction(tx);
  if (StellarSdk.rpc.Api.isSimulationError(sim)) {
    throw new Error(`Simulation failed: ${sim.error}`);
  }

  const prepared = StellarSdk.rpc.assembleTransaction(tx, sim).build();
  const xdr = prepared.toXDR();

  const { signedTxXdr } = await signWithKit(xdr, userAddress);
  const signed = StellarSdk.TransactionBuilder.fromXDR(
    signedTxXdr,
    NETWORK_PASSPHRASE,
  );
  const sendResult = await server.sendTransaction(signed);
  if (sendResult.status === "ERROR") {
    throw new Error("Transaction submission failed");
  }

  let result = await server.getTransaction(sendResult.hash);
  while (result.status === "NOT_FOUND") {
    await new Promise((r) => setTimeout(r, 2000));
    result = await server.getTransaction(sendResult.hash);
  }

  return {
    txHash: sendResult.hash,
    success: result.status === "SUCCESS",
  };
}

/**
 * Register the connected wallet as a keeper. The contract pulls
 * `min_stake` USDC from the operator on success (handled by sim/auth).
 */
export async function registerKeeper(
  userAddress: string,
  name: string,
): Promise<{ txHash: string; success: boolean }> {
  if (!REGISTRY_CONTRACT) {
    throw new Error("Registry contract not configured.");
  }
  return buildAndSubmit(userAddress, REGISTRY_CONTRACT, "register", [
    StellarSdk.nativeToScVal(userAddress, { type: "address" }),
    StellarSdk.nativeToScVal(name, { type: "string" }),
  ]);
}

/** Deregister the connected wallet (refunds stake). */
export async function deregisterKeeper(
  userAddress: string,
): Promise<{ txHash: string; success: boolean }> {
  if (!REGISTRY_CONTRACT) {
    throw new Error("Registry contract not configured.");
  }
  return buildAndSubmit(userAddress, REGISTRY_CONTRACT, "deregister", [
    StellarSdk.nativeToScVal(userAddress, { type: "address" }),
  ]);
}
