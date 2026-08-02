// 01-deploy-sandbox.mjs — deploy the Nectar Sandbox Blend V2 environment.
// Run via ./run.sh 01 (copies this file into reference/blend-utils/nectar/ so
// bare imports resolve against the pinned clone's node_modules).
//
// Deploys, in order: BLND/USDC classic-asset SACs issued by ADMIN, comet
// factory + 80/20 BLND:USDC comet, mock oracle (admin-settable prices),
// emitter + backstop + pool factory (fullDeploy), the "Nectar Sandbox" pool
// with Circle-USDC + native-XLM reserves, then seeds the backstop with
// 50,001 comet LP and sets the pool Active. Mirrors blend-utils
// src/v2/auctionTest/setupEnv.ts with our reserves/oracle (FACTS.md Gate 0.6).
import { randomBytes } from 'crypto';
import { Address, Asset } from '@stellar/stellar-sdk';
import { I128MAX } from '@blend-capital/blend-sdk';
import { deployBlend } from '../lib/v2/deploy/blend.js';
import { deployCometFactory } from '../lib/v1/deploy/comet-factory.js';
import { deployComet } from '../lib/v1/deploy/comet.js';
import { tryDeployStellarAsset } from '../lib/v1/deploy/stellar-asset.js';
import { setupPool } from '../lib/v2/pool/pool-setup.js';
import { setupReserve } from '../lib/v2/pool/reserve-setup.js';
import { setupPoolBackstop } from '../lib/v2/testing-scripts/backstop-pool-setup.js';
import {
  airdropAccount,
  bumpContractCode,
  bumpContractInstance,
  deployContract,
  installContract,
} from '../lib/utils/contract.js';
import { addressBook } from '../lib/utils/address-book.js';
import { config } from '../lib/utils/env_config.js';
import { invokeSorobanOperation, signWithKeypair } from '../lib/utils/tx.js';
import { OracleContract } from '../lib/external/oracle.js';

// The vault's real settlement asset (Circle testnet USDC SAC) and the native
// XLM SAC — both verified in docs/FACTS.md. These are the POOL reserves.
const CIRCLE_USDC = 'CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA';
const XLM_SAC = 'CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC';

const ORACLE_KEY = 'nectarSandboxOracle';
const POOL_NAME = 'Nectar Sandbox';

const txBuilderOptions = {
  fee: '10000',
  timebounds: { minTime: 0, maxTime: 0 },
  networkPassphrase: config.passphrase,
};

async function main() {
  const whale = config.getUser('WHALE');
  console.log('admin:', config.admin.publicKey());
  console.log('whale:', whale.publicKey());
  await airdropAccount(whale);

  const adminTxParams = {
    account: await config.rpc.getAccount(config.admin.publicKey()),
    txBuilderOptions,
    signerFunction: (xdr) => signWithKeypair(xdr, config.passphrase, config.admin),
  };
  const whaleTxParams = {
    account: await config.rpc.getAccount(whale.publicKey()),
    txBuilderOptions,
    signerFunction: (xdr) => signWithKeypair(xdr, config.passphrase, whale),
  };

  // Backstop-side assets we issue (testnet-only; never in the capital path).
  console.log('\n--- deploying admin-issued BLND / USDC SACs');
  const BLND = await tryDeployStellarAsset(new Asset('BLND', config.admin.publicKey()), adminTxParams);
  const NUSDC = await tryDeployStellarAsset(new Asset('USDC', config.admin.publicKey()), adminTxParams);
  console.log('sandbox BLND SAC:', BLND.contractId());
  console.log('sandbox USDC(admin) SAC:', NUSDC.contractId());

  console.log('\n--- deploying comet factory + comet (80/20 BLND:USDC)');
  const cometFactory = await deployCometFactory(adminTxParams);
  const nullAddress = 'GAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAWHF';
  const comet = await deployComet(
    cometFactory,
    adminTxParams,
    [BLND.contractId(), NUSDC.contractId()],
    [BigInt(0.8e7), BigInt(0.2e7)],
    [BigInt(1000e7), BigInt(25e7)],
    BigInt(0.003e7),
    nullAddress
  );
  console.log('comet:', comet.contractId());

  console.log('\n--- deploying mock oracle (assets: Circle-USDC, XLM; decimals 7)');
  await installContract('oraclemock', adminTxParams);
  await bumpContractCode('oraclemock', adminTxParams);
  await deployContract(ORACLE_KEY, 'oraclemock', adminTxParams);
  await bumpContractInstance(ORACLE_KEY, adminTxParams);
  const oracle = new OracleContract(addressBook.getContractId(ORACLE_KEY));
  await invokeSorobanOperation(
    oracle.setData(
      Address.fromString(config.admin.publicKey()),
      { tag: 'Other', values: ['USD'] },
      [
        { tag: 'Stellar', values: [Address.fromString(CIRCLE_USDC)] },
        { tag: 'Stellar', values: [Address.fromString(XLM_SAC)] },
      ],
      7,
      300
    ),
    () => undefined,
    adminTxParams
  );
  await invokeSorobanOperation(
    oracle.setPriceStable([BigInt(1e7), BigInt(0.42e7)]), // USDC=$1.00, XLM=$0.42
    () => undefined,
    adminTxParams
  );
  console.log('oracle:', oracle.contractId());

  console.log('\n--- deploying emitter + backstop + pool factory');
  const [backstop, emitter, poolFactory] = await deployBlend(
    BLND.contractId(),
    comet.contractId(),
    NUSDC.contractId(),
    [],
    true,
    adminTxParams
  );
  console.log('backstop:', backstop.contractId());
  console.log('emitter:', emitter.contractId());
  console.log('poolFactory:', poolFactory.contractId());

  console.log('\n--- deploying pool:', POOL_NAME);
  const pool = await setupPool(
    {
      admin: config.admin.publicKey(),
      name: POOL_NAME,
      salt: randomBytes(32),
      oracle: oracle.contractId(),
      backstop_take_rate: 0.1e7,
      max_positions: 4,
      min_collateral: BigInt(0),
    },
    adminTxParams
  );
  console.log('pool:', pool.contractId());

  // Reserve risk params — recorded in docs/FACTS.md ("Nectar Sandbox").
  console.log('\n--- reserves: Circle-USDC (idx 0), XLM (idx 1)');
  await setupReserve(
    pool.contractId(),
    {
      asset: CIRCLE_USDC,
      metadata: {
        index: 0,
        decimals: 7,
        c_factor: 950_0000,
        l_factor: 950_0000,
        util: 800_0000,
        max_util: 990_0000,
        r_base: 10_0000,
        r_one: 40_0000,
        r_two: 200_0000,
        r_three: 500_0000,
        reactivity: 100,
        supply_cap: I128MAX,
        enabled: true,
      },
    },
    adminTxParams
  );
  await setupReserve(
    pool.contractId(),
    {
      asset: XLM_SAC,
      metadata: {
        index: 1,
        decimals: 7,
        c_factor: 750_0000,
        l_factor: 750_0000,
        util: 600_0000,
        max_util: 900_0000,
        r_base: 5000,
        r_one: 50_0000,
        r_two: 500_0000,
        r_three: 1_500_0000,
        reactivity: 500,
        supply_cap: I128MAX,
        enabled: true,
      },
    },
    adminTxParams
  );

  console.log('\n--- seeding backstop (mints BLND/USDC(admin) to whale, joins 50,001 LP, deposits, setStatus(0))');
  await setupPoolBackstop(
    backstop.contractId(),
    pool.contractId(),
    comet.contractId(),
    BLND.contractId(),
    NUSDC.contractId(),
    adminTxParams,
    whaleTxParams,
    adminTxParams // issuer of BLND is also the admin account
  );

  console.log('\n--- transferring BLND admin to emitter');
  await invokeSorobanOperation(BLND.set_admin(emitter.contractId()), () => undefined, adminTxParams);

  console.log('\n=== NECTAR SANDBOX DEPLOYED ===');
  console.log(JSON.stringify({
    pool: pool.contractId(),
    oracle: oracle.contractId(),
    backstop: backstop.contractId(),
    emitter: emitter.contractId(),
    poolFactory: poolFactory.contractId(),
    comet: comet.contractId(),
    blnd: BLND.contractId(),
    backstopUsdc: NUSDC.contractId(),
    reserves: { circleUsdc: CIRCLE_USDC, xlm: XLM_SAC },
  }, null, 2));
}

await main();
