// Command fillonce performs a single Blend/LiquidationLab auction fill using the
// keeper's real FillAuction code path. Used to demonstrate the end-to-end
// draw -> fill -> swap -> return cycle on testnet (Tranche 2 D1 evidence).
package main

import (
	"fmt"
	"os"

	"github.com/nectar-network/keeper/blend"
	"github.com/nectar-network/keeper/soroban"
	"github.com/stellar/go/keypair"
)

func main() {
	secret := os.Getenv("KEEPER_SECRET")
	rpcURL := os.Getenv("SOROBAN_RPC")
	horizon := os.Getenv("HORIZON_URL")
	pass := os.Getenv("NETWORK_PASSPHRASE")
	pool := os.Getenv("BLEND_POOL")
	user := os.Getenv("BORROWER")

	kp, err := keypair.ParseFull(secret)
	if err != nil {
		fmt.Println("parse key:", err)
		os.Exit(1)
	}
	rpc := soroban.NewClient(rpcURL)
	tx, err := blend.FillAuction(rpc, horizon, kp, pass, pool, user)
	if err != nil {
		fmt.Println("FillAuction error:", err)
		os.Exit(1)
	}
	fmt.Println("FILL_TX:", tx)
}
