// 05-fresh-borrower.mjs — create a borrow position on the Nectar Sandbox from a
// keypair GENERATED AT RUNTIME. This is the Session-D discovery proof: the
// address exists in no .env, no keeper config, no wallets.md, and no prior
// evidence file, so a keeper that finds it can only have learned it from
// on-chain events.
//
// The generated SECRET is printed once and never written to disk by this
// script — capture stdout if you want to reuse the account.
//
// Usage: ./run.sh 05 [admin_supply_usdc]   (default admin supply 40; pass 0 to
// skip when the pool already holds enough unlent Circle-USDC for the borrow)
import { Asset, Keypair } from '@stellar/stellar-sdk';
import { PoolContractV2, RequestType } from '@blend-capital/blend-sdk';
import { airdropAccount } from '../lib/utils/contract.js';
import { addressBook } from '../lib/utils/address-book.js';
import { config } from '../lib/utils/env_config.js';
import { invokeClassicOp, invokeSorobanOperation, signWithKeypair } from '../lib/utils/tx.js';
import { TokenContract } from '../lib/external/token.js';

const CIRCLE_USDC = 'CBIELTK6YBZJU5UP2WWQEUCYKLPU6AUNZ2BQ4WWFEIE3USCIHMXQDAMA';
const CIRCLE_ISSUER = 'GBBD47IF6LWK7P7MDEVSCWR7DPUWV3NY3DTQEVFL4NAT4AQH3ZLLFLA5';
const XLM_SAC = 'CDLZFC3SYJYDZT7K67VZ75HPJVIEUVNIXF47ZG2FB2RMQQVU2HHGCYSC';

const SUPPLY_USDC = BigInt(Math.round(parseFloat(process.argv[3] ?? '40') * 1e7));
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

  // The whole point: nobody has ever seen this address before.
  const borrower = Keypair.random();
  console.log('pool:', poolId);
  console.log('=== FRESH BORROWER (generated this run) ===');
  console.log('public :', borrower.publicKey());
  console.log('secret :', borrower.secret());
  console.log('===========================================');

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

  if (SUPPLY_USDC > 0n) {
    console.log('\n--- admin supplies', Number(SUPPLY_USDC) / 1e7, 'Circle-USDC to the pool');
    await invokeSorobanOperation(
      pool.submit({
        from: config.admin.publicKey(),
        spender: config.admin.publicKey(),
        to: config.admin.publicKey(),
        requests: [{ request_type: RequestType.Supply, address: CIRCLE_USDC, amount: SUPPLY_USDC }],
      }),
      PoolContractV2.parsers.submit,
      adminTxParams
    );
  } else {
    console.log('\n--- skipping admin supply (arg 0): pool already has lend liquidity');
  }

  console.log('\n--- fresh borrower supplies', Number(COLLATERAL_XLM) / 1e7, 'XLM collateral and borrows', Number(BORROW_USDC) / 1e7, 'Circle-USDC');
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

  console.log('\n=== FRESH BORROWER POSITION CREATED ===');
  console.log(JSON.stringify({
    pool: poolId,
    borrower: borrower.publicKey(),
    collateral: '100 XLM',
    debt: '20 Circle-USDC',
    hfAtXlm042: '(100*0.42*0.75)/(20/0.95) = 1.4963',
    note: 'address is in NO config file — the keeper must discover it from events',
  }, null, 2));
}

await main();
