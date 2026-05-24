import * as StellarSdk from "@stellar/stellar-sdk";

const TESTNET_RPC = "https://soroban-testnet.stellar.org";
const TESTNET_PASSPHRASE = "Test SDF Network ; September 2015";
const HORIZON_TESTNET = "https://horizon-testnet.stellar.org";

// Contract addresses — set via env. USDC_CONTRACT must be the mock-USDC SAC
// the vault was initialized with (registry.get_config().usdc_token), NOT the
// classic-asset Circle USDC trustline a user might happen to hold.
//
// `.trim()` because `echo "$value" | vercel env add` smuggles a trailing
// newline; that bumps the strkey length to 57 and decodeContract rejects it
// with "Invalid contract ID: …". Belt-and-suspenders against env hygiene.
const VAULT_CONTRACT =
  (process.env.NEXT_PUBLIC_VAULT_CONTRACT ?? "").trim();
const REGISTRY_CONTRACT =
  (process.env.NEXT_PUBLIC_REGISTRY_CONTRACT ?? "").trim();
const USDC_CONTRACT =
  (process.env.NEXT_PUBLIC_USDC_CONTRACT ?? "").trim();

const STORAGE_SELECTED_WALLET = "nectar.selectedWalletId";

/** Fetch a user's mock-USDC SAC balance, returned as a 2-decimal-USDC string. */
export async function fetchUsdcBalance(address: string): Promise<string> {
  if (!USDC_CONTRACT || !address) return "0";
  try {
    const server = new StellarSdk.rpc.Server(TESTNET_RPC);
    // Simulator needs a source account; use the user's own — it exists by
    // the time we're displaying their balance.
    let account: StellarSdk.Account;
    try {
      account = await server.getAccount(address);
    } catch {
      account = new StellarSdk.Account(address, "0");
    }
    const contract = new StellarSdk.Contract(USDC_CONTRACT);
    const tx = new StellarSdk.TransactionBuilder(account, {
      fee: "100",
      networkPassphrase: TESTNET_PASSPHRASE,
    })
      .addOperation(
        contract.call(
          "balance",
          StellarSdk.nativeToScVal(address, { type: "address" })
        )
      )
      .setTimeout(30)
      .build();
    const sim = await server.simulateTransaction(tx);
    if (StellarSdk.rpc.Api.isSimulationError(sim)) return "0";
    if (StellarSdk.rpc.Api.isSimulationSuccess(sim) && sim.result) {
      const v = StellarSdk.scValToNative(sim.result.retval) as bigint | number;
      const stroops = typeof v === "bigint" ? v : BigInt(Math.trunc(Number(v)));
      const denom = BigInt(10000000);
      const whole = Number(stroops / denom);
      const frac = Number(stroops % denom) / 10000000;
      return (whole + frac).toFixed(2);
    }
    return "0";
  } catch {
    return "0";
  }
}

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
      network: Networks.TESTNET,
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

    // We initialized the kit with Networks.TESTNET, so we know the network
    // without asking the wallet — Albedo doesn't implement getNetwork() and
    // throws code -3, so we skip the call entirely.
    const walletId =
      window.localStorage.getItem(STORAGE_SELECTED_WALLET) ?? undefined;

    // Fetch XLM via Horizon, USDC via the SAC the vault was initialized with.
    // Reading USDC from the classic-asset list would surface Circle's
    // testnet-USDC trustline balance, which is unrelated to our vault.
    let xlmBalance = "0";
    try {
      const server = new StellarSdk.Horizon.Server(HORIZON_TESTNET);
      const account = await server.loadAccount(address);
      const native = account.balances.find(
        (b: StellarSdk.Horizon.HorizonApi.BalanceLine) => b.asset_type === "native"
      );
      if (native) {
        xlmBalance = parseFloat(native.balance).toFixed(2);
      }
    } catch {
      // Account may not exist yet on testnet (needs Friendbot) — that's fine.
    }
    const usdcBalance = await fetchUsdcBalance(address);

    return {
      connected: true,
      address,
      network: "TESTNET",
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
    networkPassphrase: TESTNET_PASSPHRASE,
    address,
  });
  return { signedTxXdr };
}

/** Build and submit a vault deposit transaction via Soroban */
export async function depositToVault(
  userAddress: string,
  amountStroops: bigint
): Promise<{ txHash: string; success: boolean }> {
  if (!VAULT_CONTRACT) {
    throw new Error("Vault contract address not configured. Set NEXT_PUBLIC_VAULT_CONTRACT.");
  }

  const server = new StellarSdk.rpc.Server(TESTNET_RPC);
  const account = await server.getAccount(userAddress);

  const contract = new StellarSdk.Contract(VAULT_CONTRACT);

  const tx = new StellarSdk.TransactionBuilder(account, {
    fee: "1000000",
    networkPassphrase: TESTNET_PASSPHRASE,
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
    TESTNET_PASSPHRASE
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

  const server = new StellarSdk.rpc.Server(TESTNET_RPC);
  const account = await server.getAccount(userAddress);

  const contract = new StellarSdk.Contract(VAULT_CONTRACT);

  const tx = new StellarSdk.TransactionBuilder(account, {
    fee: "1000000",
    networkPassphrase: TESTNET_PASSPHRASE,
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
    TESTNET_PASSPHRASE
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
    const server = new StellarSdk.rpc.Server(TESTNET_RPC);
    const account = await server.getAccount(userAddress);
    const contract = new StellarSdk.Contract(VAULT_CONTRACT);

    const tx = new StellarSdk.TransactionBuilder(account, {
      fee: "100",
      networkPassphrase: TESTNET_PASSPHRASE,
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
    const server = new StellarSdk.rpc.Server(TESTNET_RPC);
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
      networkPassphrase: TESTNET_PASSPHRASE,
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
    };
  } catch {
    return null;
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
  const server = new StellarSdk.rpc.Server(TESTNET_RPC);
  const account = await server.getAccount(userAddress);
  const contract = new StellarSdk.Contract(contractAddr);

  const tx = new StellarSdk.TransactionBuilder(account, {
    fee: "1000000",
    networkPassphrase: TESTNET_PASSPHRASE,
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
    TESTNET_PASSPHRASE,
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
