package registry

import (
	"fmt"

	"github.com/stellar/go/keypair"
	"github.com/stellar/go/xdr"

	"github.com/nectar-network/keeper/soroban"
)

// KeeperInfo mirrors the on-chain KeeperInfo struct fields the keeper cares
// about for the slasher sweep.
type KeeperInfo struct {
	Addr                string
	Name                string
	Stake               int64
	RegisteredAt        uint64
	Active              bool
	TotalExecutions     uint64
	SuccessfulFills     uint64
	TotalProfit         int64
	LastDrawTime        uint64
	HasActiveDraw       bool
	TotalResponseTimeMs uint64
	ResponseCount       uint64
}

// Config mirrors RegistryConfig.
type Config struct {
	MinStake     int64
	SlashTimeout uint64
	SlashRateBPS uint32
	USDCToken    string
}

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

// ListKeepers returns every registered keeper address.
func ListKeepers(rpc *soroban.Client, passphrase, registryAddr string) ([]string, error) {
	sim, err := rpc.SimulateRead(passphrase, registryAddr, "get_keepers")
	if err != nil {
		return nil, fmt.Errorf("get_keepers: %w", err)
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("get_keepers: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return nil, nil
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil, err
	}
	if val.Type != xdr.ScValTypeScvVec || val.Vec == nil || *val.Vec == nil {
		return nil, nil
	}
	out := make([]string, 0, len(**val.Vec))
	for _, item := range **val.Vec {
		if item.Type == xdr.ScValTypeScvAddress && item.Address != nil {
			a, err := soroban.ParseAddress(*item.Address)
			if err == nil {
				out = append(out, a)
			}
		}
	}
	return out, nil
}

// GetKeeper returns KeeperInfo for the operator, or an error if the address
// isn't registered.
func GetKeeper(rpc *soroban.Client, passphrase, registryAddr, addr string) (*KeeperInfo, error) {
	addrVal, err := soroban.ScvAddress(addr)
	if err != nil {
		return nil, err
	}
	sim, err := rpc.SimulateRead(passphrase, registryAddr, "get_keeper", addrVal)
	if err != nil {
		return nil, fmt.Errorf("get_keeper: %w", err)
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("get_keeper: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return nil, fmt.Errorf("get_keeper: no results")
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil, err
	}
	return parseKeeperInfo(val), nil
}

// GetConfig reads the registry's RegistryConfig.
func GetConfig(rpc *soroban.Client, passphrase, registryAddr string) (*Config, error) {
	sim, err := rpc.SimulateRead(passphrase, registryAddr, "get_config")
	if err != nil {
		return nil, fmt.Errorf("get_config: %w", err)
	}
	if sim.Error != "" {
		return nil, fmt.Errorf("get_config: %s", sim.Error)
	}
	if len(sim.Results) == 0 {
		return nil, fmt.Errorf("get_config: no results")
	}
	var val xdr.ScVal
	if err := xdr.SafeUnmarshalBase64(sim.Results[0].XDR, &val); err != nil {
		return nil, err
	}
	return parseConfig(val), nil
}

// Slash invokes the registry's slash() entry point for the given keeper.
// Anyone can call slash (no admin gate) — the contract enforces the timeout.
// Retries on transient infra failures; surfaces SlashTimeout cleanly.
func Slash(rpc *soroban.Client, horizonURL string, kp *keypair.Full, passphrase, registryAddr, keeper string) error {
	addrVal, err := soroban.ScvAddress(keeper)
	if err != nil {
		return err
	}
	_, err = rpc.InvokeWithRetry(horizonURL, kp, passphrase, registryAddr, "slash",
		soroban.DefaultRetry(), addrVal)
	if err != nil {
		return fmt.Errorf("slash: %w", err)
	}
	return nil
}

// ── decoders ────────────────────────────────────────────────────────────────

func parseKeeperInfo(val xdr.ScVal) *KeeperInfo {
	info := &KeeperInfo{}
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return info
	}
	for _, e := range **val.Map {
		if e.Key.Type != xdr.ScValTypeScvSymbol || e.Key.Sym == nil {
			continue
		}
		key := string(*e.Key.Sym)
		switch key {
		case "addr":
			if e.Val.Type == xdr.ScValTypeScvAddress && e.Val.Address != nil {
				if a, err := soroban.ParseAddress(*e.Val.Address); err == nil {
					info.Addr = a
				}
			}
		case "name":
			if e.Val.Type == xdr.ScValTypeScvString && e.Val.Str != nil {
				info.Name = string(*e.Val.Str)
			}
		case "stake":
			info.Stake = scI128Int64(e.Val)
		case "registered_at":
			info.RegisteredAt = scU64(e.Val)
		case "active":
			if e.Val.Type == xdr.ScValTypeScvBool && e.Val.B != nil {
				info.Active = *e.Val.B
			}
		case "total_executions":
			info.TotalExecutions = scU64(e.Val)
		case "successful_fills":
			info.SuccessfulFills = scU64(e.Val)
		case "total_profit":
			info.TotalProfit = scI128Int64(e.Val)
		case "last_draw_time":
			info.LastDrawTime = scU64(e.Val)
		case "has_active_draw":
			if e.Val.Type == xdr.ScValTypeScvBool && e.Val.B != nil {
				info.HasActiveDraw = *e.Val.B
			}
		case "total_response_time_ms":
			info.TotalResponseTimeMs = scU64(e.Val)
		case "response_count":
			info.ResponseCount = scU64(e.Val)
		}
	}
	return info
}

func parseConfig(val xdr.ScVal) *Config {
	cfg := &Config{}
	if val.Type != xdr.ScValTypeScvMap || val.Map == nil || *val.Map == nil {
		return cfg
	}
	for _, e := range **val.Map {
		if e.Key.Type != xdr.ScValTypeScvSymbol || e.Key.Sym == nil {
			continue
		}
		switch string(*e.Key.Sym) {
		case "min_stake":
			cfg.MinStake = scI128Int64(e.Val)
		case "slash_timeout":
			cfg.SlashTimeout = scU64(e.Val)
		case "slash_rate_bps":
			if e.Val.Type == xdr.ScValTypeScvU32 && e.Val.U32 != nil {
				cfg.SlashRateBPS = uint32(*e.Val.U32)
			}
		case "usdc_token":
			if e.Val.Type == xdr.ScValTypeScvAddress && e.Val.Address != nil {
				if a, err := soroban.ParseAddress(*e.Val.Address); err == nil {
					cfg.USDCToken = a
				}
			}
		}
	}
	return cfg
}

func scU64(val xdr.ScVal) uint64 {
	if val.Type == xdr.ScValTypeScvU64 && val.U64 != nil {
		return uint64(*val.U64)
	}
	return 0
}

func scI128Int64(val xdr.ScVal) int64 {
	if val.Type != xdr.ScValTypeScvI128 || val.I128 == nil {
		return 0
	}
	// Treat as signed i64-equivalent — vault/registry never exceed i64 in
	// realistic deployments (USDC TVL is bounded).
	return int64(val.I128.Lo)
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
