package share

import (
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-node/share/p2p/shrexeds"
	"github.com/celestiaorg/celestia-node/share/p2p/shrexnd"
)

var (
	ErrNegativeInterval = errors.New("interval must be positive")
)

type Config struct {
	// PeersLimit defines how many peers will be added during discovery.
	PeersLimit uint
	// DiscoveryInterval is an interval between discovery sessions.
	DiscoveryInterval time.Duration
	// AdvertiseInterval is a interval between advertising sessions.
	// NOTE: only full and bridge can advertise themselves.
	AdvertiseInterval time.Duration
	// UseShareExchange is a flag toggling the usage of shrex protocols for blocksync.
	// NOTE: This config variable only has an effect on full and bridge nodes.
	UseShareExchange bool
	// ShrExEDSParams sets shrexeds client and server configuration parameters
	ShrExEDSParams *shrexeds.Parameters
	// ShrExNDParams sets shrexnd client and server configuration parameters
	ShrExNDParams *shrexnd.Parameters
}

func DefaultConfig() Config {
	return Config{
		PeersLimit:        3,
		DiscoveryInterval: time.Second * 30,
		AdvertiseInterval: time.Second * 30,
		UseShareExchange:  true,
		ShrExEDSParams:    shrexeds.DefaultParameters(),
		ShrExNDParams:     shrexnd.DefaultParameters(),
	}
}

// Validate performs basic validation of the config.
func (cfg *Config) Validate() error {
	if cfg.DiscoveryInterval <= 0 || cfg.AdvertiseInterval <= 0 {
		return fmt.Errorf("nodebuilder/share: %s", ErrNegativeInterval)
	}
	if err := cfg.ShrExNDParams.Validate(); err != nil {
		return err
	}
	return cfg.ShrExEDSParams.Validate()
}
