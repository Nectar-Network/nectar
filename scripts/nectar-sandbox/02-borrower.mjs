// 02-borrower.mjs — create a genuine borrow position on the Nectar Sandbox
// pool: admin supplies 40 Circle-USDC of lend-side liquidity; BORROWER
// (friendbot-funded) adds a Circle-USDC trustline, supplies 100 XLM as
// collateral and borrows 20 Circle-USDC. At oracle XLM=$0.42 the position's
// HF is (100*0.42*0.75) / (20/0.95) ≈ 1.50 — healthy until 03-set-price.mjs
// lowers XLM. Run via ./run.sh 02.
import { Asset } from '@stellar/stellar-sdk';
import { PoolContractV2, RequestType } from '@blend-capital/blend-sdk';
import { airdropAccount } from '../lib/utils/contract.js';
import { addressBook } from '../lib/utils/address-book.js';
import { config } from '../lib/utils/env_config.js';
import { invokeClassicOp, invokeSorobanOperation, signWithKeypair } from '../lib/utils/tx.js';
import { TokenContract } from '../lib/external/token.js';

const CIRCLE_USDC = 'CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA';
const CIRCLE_ISSUER = 'GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5';
const XLM_SAC = 'CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC';

const SUPPLY_USDC = BigInt(40_0000000); // admin lend-side liquidity
const COLLATERAL_XLM = BigInt(100_0000000);
const BORROW_USDC = BigInt(20_0000000);

const txBuilderOptions = {
  fee: '10000',
  timebounds: { minTime: 0, maxTime: 0 },
  networkPassphrase: config.passphrase,
};

async function main() {
  const poolId = addressBook.getContractId('Nectar Sandbox');
  const pool = new PoolContractV2(poolId);
  const borrower = config.getUser('BORROWER');
  console.log('pool:', poolId);
  console.log('borrower:', borrower.publicKey());
  await airdropAccount(borrower);

  const adminTxParams = {
    account: await config.rpc.getAccount(config.admin.publicKey()),
    txBuilderOptions,
    signerFunction: (xdr) => signWithKeypair(xdr, config.passphrase, config.admin),
  };
  const borrowerTxParams = {
    account: await config.rpc.getAccount(borrower.publicKey()),
    txBuilderOptions,
    signerFunction: (xdr) => signWithKeypair(xdr, config.passphrase, borrower),
  };

  console.log('\n--- borrower trustline to Circle USDC (needed to receive borrowed funds)');
  const circle = new TokenContract(CIRCLE_USDC, new Asset('USDC', CIRCLE_ISSUER));
  await invokeClassicOp(circle.classic_trustline(borrower.publicKey()), borrowerTxParams);

  console.log('\n--- admin supplies', Number(SUPPLY_USDC) / 1e7, 'Circle-USDC to the pool');
  await invokeSorobanOperation(
    pool.submit({
      from: config.admin.publicKey(),
      spender: config.admin.publicKey(),
      to: config.admin.publicKey(),
      requests: [
        { request_type: RequestType.Supply, address: CIRCLE_USDC, amount: SUPPLY_USDC },
      ],
    }),
    PoolContractV2.parsers.submit,
    adminTxParams
  );

  console.log('\n--- borrower supplies', Number(COLLATERAL_XLM) / 1e7, 'XLM collateral and borrows', Number(BORROW_USDC) / 1e7, 'Circle-USDC');
  await invokeSorobanOperation(
    pool.submit({
      from: borrower.publicKey(),
      spender: borrower.publicKey(),
      to: borrower.publicKey(),
      requests: [
        { request_type: RequestType.SupplyCollateral, address: XLM_SAC, amount: COLLATERAL_XLM },
        { request_type: RequestType.Borrow, address: CIRCLE_USDC, amount: BORROW_USDC },
      ],
    }),
    PoolContractV2.parsers.submit,
    borrowerTxParams
  );

  console.log('\n=== BORROWER POSITION CREATED ===');
  console.log(JSON.stringify({
    pool: poolId,
    borrower: borrower.publicKey(),
    collateral: '100 XLM',
    debt: '20 Circle-USDC',
    hfAtXlm042: '(100*0.42*0.75)/(20/0.95) = 1.4963',
  }, null, 2));
}

await main();
