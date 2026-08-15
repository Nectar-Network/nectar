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

// Draw requests capital from the vault for the bound keeper, declaring the
// collateral asset the draw targets (contract DECISION F-2a).
func (c *Client) Draw(amount int64, asset string) error {
	return Draw(c.rpc, c.horizonURL, c.kp, c.passphrase, c.vaultAddr, amount, asset)
}

// ReturnProceeds returns capital + profit to the vault, forwarding the observed
// response time for the registry's avg-response-time metric.
func (c *Client) ReturnProceeds(amount, responseTimeMs int64) error {
	return ReturnProceeds(c.rpc, c.horizonURL, c.kp, c.passphrase, c.vaultAddr, amount, responseTimeMs)
}

// LiqPaused reports whether the vault blocks liquidation draws globally or for
// any of the given assets. Read errors surface to the caller, which must fail
// CLOSED (skip the operation) — a pause it cannot read may be a pause.
func (c *Client) LiqPaused(assets []string) (bool, error) {
	paused, err := IsGlobalLiqPaused(c.rpc, c.passphrase, c.vaultAddr)
	if err != nil || paused {
		return paused, err
	}
	for _, asset := range assets {
		paused, err = IsAssetPaused(c.rpc, c.passphrase, c.vaultAddr, asset)
		if err != nil || paused {
			return paused, err
		}
	}
	return false, nil
}
