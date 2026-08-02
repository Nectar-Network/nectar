// 03-set-price.mjs <xlm_price_usd> — admin-set the Nectar Sandbox mock
// oracle's XLM price (USDC stays $1.00). 0.15 puts the 02-borrower position
// underwater (HF ≈ 0.53); 0.42 restores it (HF ≈ 1.50). Run via
// ./run.sh 03 <price>.
import { addressBook } from '../lib/utils/address-book.js';
import { config } from '../lib/utils/env_config.js';
import { invokeSorobanOperation, signWithKeypair } from '../lib/utils/tx.js';
import { OracleContract } from '../lib/external/oracle.js';

const price = parseFloat(process.argv[3]);
if (!(price > 0) || price > 1000) {
  console.error('usage: 03-set-price.mjs <network> <xlm_price_usd in (0,1000]>');
  process.exit(1);
}

const txBuilderOptions = {
  fee: '10000',
  timebounds: { minTime: 0, maxTime: 0 },
  networkPassphrase: config.passphrase,
};

async function main() {
  const oracle = new OracleContract(addressBook.getContractId('nectarSandboxOracle'));
  const adminTxParams = {
    account: await config.rpc.getAccount(config.admin.publicKey()),
    txBuilderOptions,
    signerFunction: (xdr) => signWithKeypair(xdr, config.passphrase, config.admin),
  };
  // Price vector order matches setData assets: [Circle-USDC, XLM].
  await invokeSorobanOperation(
    oracle.setPriceStable([BigInt(1e7), BigInt(Math.round(price * 1e7))]),
    () => undefined,
    adminTxParams
  );
  console.log(`oracle ${oracle.contractId()}: XLM price set to $${price.toFixed(7)}`);
}

await main();
