package vault

import (
	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/soroban"
)

// Client binds the package-level draw/return functions to a fixed keeper and
// vault, satisfying the adapters.VaultClient interface so protocol adapters can
// move capital without handling RPC/keypair plumbing.
type Client struct {
	rpc        *soroban.Client
	kp         *keypair.Full
	horizonURL string
	passphrase string
	vaultAddr  string
}

// NewClient builds a vault Client bound to one keeper + vault contract.
func NewClient(rpc *soroban.Client, kp *keypair.Full, horizonURL, passphrase, vaultAddr string) *Client {
	return &Client{rpc: rpc, kp: kp, horizonURL: horizonURL, passphrase: passphrase, vaultAddr: vaultAddr}
}

// Draw requests capital from the vault for the bound keeper.
func (c *Client) Draw(amount int64) error {
	return Draw(c.rpc, c.horizonURL, c.kp, c.passphrase, c.vaultAddr, amount)
}

// ReturnProceeds returns capital + profit to the vault, forwarding the observed
// response time for the registry's avg-response-time metric.
func (c *Client) ReturnProceeds(amount, responseTimeMs int64) error {
	return ReturnProceeds(c.rpc, c.horizonURL, c.kp, c.passphrase, c.vaultAddr, amount, responseTimeMs)
}
