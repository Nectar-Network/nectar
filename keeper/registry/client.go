package registry

import (
	"fmt"

	"github.com/stellar/go/keypair"

	"github.com/nectar-network/keeper/soroban"
)

// Register registers the keeper with the on-chain KeeperRegistry.
// Returns nil if already registered.
func Register(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, registryAddr, name string) error {
	operatorVal, err := soroban.ScvAddress(kp.Address())
	if err != nil {
		return err
	}
	nameVal := soroban.ScvString(name)

	_, err = rpc.InvokeWithRetry(horizonURL, kp, passphrase, registryAddr, "register",
		soroban.DefaultRetry(), operatorVal, nameVal)
	if err != nil {
		if isAlreadyRegistered(err.Error()) {
			return nil
		}
		return fmt.Errorf("registry register: %w", err)
	}
	return nil
}

// IsRegistered checks whether the keeper address is currently registered.
func IsRegistered(rpc *soroban.Client, passphrase, registryAddr, addr string) (bool, error) {
	addrVal, err := soroban.ScvAddress(addr)
	if err != nil {
		return false, err
	}
	sim, err := rpc.SimulateRead(passphrase, registryAddr, "get_keeper", addrVal)
	if err != nil {
		return false, fmt.Errorf("get_keeper: %w", err)
	}
	if sim.Error != "" {
		if isNotRegistered(sim.Error) {
			return false, nil
		}
		return false, fmt.Errorf("get_keeper: %s", sim.Error)
	}
	return true, nil
}

func isAlreadyRegistered(s string) bool { return contains(s, "AlreadyRegistered") }
func isNotRegistered(s string) bool     { return contains(s, "NotRegistered") }

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
